//! T11 — cookie jar: parsing `Set-Cookie`, matching against a URL, and
//! persistence through the SQLite `Store`.
//!
//! The HTTP exchanges are served by a throwaway TCP server, as in the net
//! tests.

use std::io::{Read, Write};
use std::net::TcpListener;
use std::thread;

use browser_lite::cookies::CookieJar;
use browser_lite::net::fetch_with_cookies;
use browser_lite::storage::Store;

/// Serve `responses` in order, one per connection, and return the base URL.
fn spawn_server(responses: Vec<Vec<u8>>) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
    let port = listener.local_addr().expect("server address").port();
    thread::spawn(move || {
        for response in responses {
            let Ok((mut stream, _)) = listener.accept() else {
                break;
            };
            let mut buffer = [0u8; 4096];
            let _ = stream.read(&mut buffer);
            let _ = stream.write_all(&response);
            let _ = stream.flush();
        }
    });
    format!("http://127.0.0.1:{port}")
}

fn response(status: &str, headers: &[(&str, &str)], body: &str) -> Vec<u8> {
    let mut head = format!("HTTP/1.1 {status}\r\n");
    for (name, value) in headers {
        head.push_str(&format!("{name}: {value}\r\n"));
    }
    head.push_str(&format!("Content-Length: {}\r\n", body.len()));
    head.push_str("Connection: close\r\n\r\n");
    let mut raw = head.into_bytes();
    raw.extend_from_slice(body.as_bytes());
    raw
}

#[test]
fn a_set_cookie_header_becomes_a_cookie_for_the_same_url() {
    let mut jar = CookieJar::new();
    jar.set_from_header("http://example.test/page", "session=abc; Path=/")
        .expect("set cookie");

    assert_eq!(
        jar.header_for("http://example.test/page").as_deref(),
        Some("session=abc")
    );
}

#[test]
fn host_only_cookies_do_not_reach_other_hosts() {
    let mut jar = CookieJar::new();
    jar.set_from_header("http://example.test/", "session=abc")
        .expect("set cookie");

    assert!(jar.header_for("http://example.test/other").is_some());
    assert!(jar.header_for("http://sub.example.test/").is_none());
    assert!(jar.header_for("http://other.test/").is_none());
}

#[test]
fn domain_cookies_reach_subdomains() {
    let mut jar = CookieJar::new();
    jar.set_from_header("http://example.test/", "session=abc; Domain=example.test")
        .expect("set cookie");

    assert!(jar.header_for("http://sub.example.test/").is_some());
    assert!(jar.header_for("http://example.test/").is_some());
    assert!(jar.header_for("http://notexample.test/").is_none());
}

#[test]
fn path_scoping_limits_where_a_cookie_is_sent() {
    let mut jar = CookieJar::new();
    jar.set_from_header("http://example.test/", "scoped=1; Path=/account")
        .expect("set cookie");

    assert!(jar.header_for("http://example.test/account").is_some());
    assert!(jar.header_for("http://example.test/account/profile").is_some());
    assert!(jar.header_for("http://example.test/other").is_none());
}

#[test]
fn expired_cookies_are_not_sent() {
    let mut jar = CookieJar::new();
    jar.set_from_header("http://example.test/", "old=1; Expires=Thu, 01 Jan 1970 00:00:00 GMT")
        .expect("set cookie");
    jar.set_from_header("http://example.test/", "gone=1; Max-Age=0")
        .expect("set cookie");

    assert!(jar.header_for("http://example.test/").is_none());
}

#[test]
fn several_cookies_are_sent_together() {
    let mut jar = CookieJar::new();
    jar.set_from_header("http://example.test/", "a=1").expect("set a");
    jar.set_from_header("http://example.test/", "b=2").expect("set b");

    let header = jar.header_for("http://example.test/").expect("cookies");
    assert!(header.contains("a=1"));
    assert!(header.contains("b=2"));
}

#[test]
fn cookies_survive_a_store_round_trip() {
    let dir = std::env::temp_dir().join(format!("bl-cookies-{}", std::process::id()));
    std::fs::create_dir_all(&dir).expect("create temp dir");
    let path = dir.join("cookies.sqlite");
    let _ = std::fs::remove_file(&path);

    {
        let store = Store::open(path.to_str().expect("utf-8 path")).expect("open store");
        let mut jar = CookieJar::new();
        jar.set_from_header("http://example.test/", "session=abc; Domain=example.test; Path=/")
            .expect("set cookie");
        jar.set_from_header("http://example.test/", "plain=1")
            .expect("set cookie");
        jar.store(&store).expect("store the jar");
    }

    let store = Store::open(path.to_str().expect("utf-8 path")).expect("reopen store");
    let jar = CookieJar::load(&store).expect("load the jar");

    let header = jar.header_for("http://example.test/").expect("cookies");
    assert!(header.contains("session=abc"));
    assert!(header.contains("plain=1"));
    assert!(jar.header_for("http://sub.example.test/").is_some());

    let _ = std::fs::remove_file(&path);
    let _ = std::fs::remove_dir(&dir);
}

#[test]
fn fetch_with_cookies_sends_the_jar_and_captures_set_cookie() {
    let server = spawn_server(vec![response(
        "200 OK",
        &[("Set-Cookie", "fromserver=yes; Path=/")],
        "ok",
    )]);
    let url = format!("{server}/page.html");

    let mut jar = CookieJar::new();
    jar.set_from_header(&url, "sent=1").expect("set cookie");

    let fetched = fetch_with_cookies(&url, &jar).expect("fetch must succeed");

    assert_eq!(fetched.result.bytes, b"ok");
    assert_eq!(fetched.set_cookies.len(), 1);
    assert_eq!(fetched.set_cookies[0].1, "fromserver=yes; Path=/");

    // The captured cookie can be stored in the jar for the next request.
    for (set_url, value) in &fetched.set_cookies {
        jar.set_from_header(set_url, value).expect("store captured cookie");
    }
    let header = jar.header_for(&url).expect("cookies");
    assert!(header.contains("sent=1"));
    assert!(header.contains("fromserver=yes"));
}

#[test]
fn fetch_with_cookies_captures_set_cookie_from_a_redirect() {
    let server = spawn_server(vec![
        b"HTTP/1.1 302 Found\r\nLocation: /final.html\r\nSet-Cookie: hop=1; Path=/\r\nContent-Length: 0\r\nConnection: close\r\n\r\n".to_vec(),
        response("200 OK", &[("Set-Cookie", "final=1; Path=/")], "done"),
    ]);
    let url = format!("{server}/start.html");

    let mut jar = CookieJar::new();
    let fetched = fetch_with_cookies(&url, &jar).expect("fetch must succeed");

    assert_eq!(fetched.set_cookies.len(), 2);
    assert!(fetched.set_cookies[0].1.starts_with("hop=1"));
    assert!(fetched.set_cookies[1].1.starts_with("final=1"));
    assert_eq!(fetched.result.final_url, format!("{server}/final.html"));

    for (set_url, value) in &fetched.set_cookies {
        jar.set_from_header(set_url, value).expect("store captured cookie");
    }
    assert!(jar.header_for(&url).expect("cookies").contains("hop=1"));
}

#[test]
fn a_malformed_set_cookie_is_reported() {
    let mut jar = CookieJar::new();
    assert!(jar.set_from_header("http://example.test/", "no-equals-sign").is_err());
}
