//! T4 — networking v2: charset decoding, HTTP status errors, final URL and
//! the non-blocking fetch variants.
//!
//! The HTTP exchanges are served by a throwaway TCP server so the tests stay
//! hermetic and deterministic.

use std::io::{Read, Write};
use std::net::TcpListener;
use std::thread;

use browser_lite::net::{decode_body, fetch, fetch_async, fetch_result, fetch_text};

/// Serve `responses` in order, one per connection, and return the base URL.
fn spawn_server(responses: Vec<Vec<u8>>) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
    let port = listener.local_addr().expect("server address").port();
    thread::spawn(move || {
        for response in responses {
            let Ok((mut stream, _)) = listener.accept() else {
                break;
            };
            let mut buffer = [0u8; 2048];
            let _ = stream.read(&mut buffer);
            let _ = stream.write_all(&response);
            let _ = stream.flush();
        }
    });
    format!("http://127.0.0.1:{port}")
}

fn response(status: &str, content_type: Option<&str>, body: &[u8]) -> Vec<u8> {
    let mut head = format!("HTTP/1.1 {status}\r\n");
    if let Some(content_type) = content_type {
        head.push_str(&format!("Content-Type: {content_type}\r\n"));
    }
    head.push_str(&format!("Content-Length: {}\r\n", body.len()));
    head.push_str("Connection: close\r\n\r\n");
    let mut raw = head.into_bytes();
    raw.extend_from_slice(body);
    raw
}

#[test]
fn status_400_or_more_is_an_error_carrying_the_status_line() {
    let server = spawn_server(vec![response("404 Not Found", None, b"")]);
    let url = format!("{server}/missing.html");

    let err = fetch_result(&url).expect_err("404 must fail");

    assert!(err.contains("404 Not Found"), "error was: {err}");
    assert!(err.contains(&url), "error must name the url: {err}");
}

#[test]
fn successful_fetch_returns_the_body_bytes() {
    let server = spawn_server(vec![response("200 OK", Some("text/html"), b"<p>hi</p>")]);
    let url = format!("{server}/page.html");

    let bytes = fetch(&url).expect("fetch must succeed");

    assert_eq!(bytes, b"<p>hi</p>");
}

#[test]
fn content_type_charset_is_honoured() {
    // "café" encoded as ISO-8859-1 (0xE9) must decode to U+00E9.
    let server = spawn_server(vec![response(
        "200 OK",
        Some("text/html; charset=iso-8859-1"),
        b"caf\xe9",
    )]);
    let url = format!("{server}/latin1.html");

    let text = fetch_text(&url).expect("fetch must succeed");

    assert_eq!(text, "café");
}

#[test]
fn utf8_is_the_default_charset() {
    let server = spawn_server(vec![response("200 OK", Some("text/html"), "café".as_bytes())]);
    let url = format!("{server}/utf8.html");

    let text = fetch_text(&url).expect("fetch must succeed");

    assert_eq!(text, "café");
}

#[test]
fn final_url_reports_the_url_after_redirects() {
    let server = spawn_server(vec![
        b"HTTP/1.1 302 Found\r\nLocation: /final.html\r\nContent-Length: 0\r\nConnection: close\r\n\r\n".to_vec(),
        response("200 OK", Some("text/html"), b"<p>final</p>"),
    ]);
    let url = format!("{server}/start.html");

    let result = fetch_result(&url).expect("fetch must succeed");

    assert_eq!(result.final_url, format!("{server}/final.html"));
    assert_eq!(result.bytes, b"<p>final</p>");
}

#[test]
fn async_fetch_delivers_the_result_over_the_channel() {
    let server = spawn_server(vec![response("200 OK", Some("text/html"), b"<p>async</p>")]);
    let url = format!("{server}/async.html");

    let receiver = fetch_async(&url);
    let result = receiver.recv().expect("the worker must send a result");

    assert_eq!(result.expect("fetch must succeed"), b"<p>async</p>");
}

#[test]
fn async_fetch_reports_http_errors_over_the_channel() {
    let server = spawn_server(vec![response("500 Internal Server Error", None, b"")]);
    let url = format!("{server}/boom.html");

    let receiver = fetch_async(&url);
    let result = receiver.recv().expect("the worker must send a result");

    assert!(result.expect_err("500 must fail").contains("500"));
}

#[test]
fn decode_body_reads_the_charset_from_the_content_type() {
    // Quoted charset, uppercase, and extra parameters before it.
    let text = decode_body(b"caf\xe9", Some("text/html; foo=bar; charset=\"ISO-8859-1\""))
        .expect("decode must succeed");
    assert_eq!(text, "café");

    // No charset declared: UTF-8 is the default.
    let text = decode_body("café".as_bytes(), Some("text/html")).expect("decode must succeed");
    assert_eq!(text, "café");
}

#[test]
fn decode_body_keeps_the_utf8_bom_out_of_the_text() {
    let mut bytes = vec![0xEF, 0xBB, 0xBF];
    bytes.extend_from_slice(b"<p>bom</p>");

    let text = decode_body(&bytes, Some("text/html; charset=utf-8")).expect("decode must succeed");

    assert_eq!(text, "<p>bom</p>");
}

#[test]
fn decode_body_maps_windows_1252_and_utf16() {
    // 0x80 is the euro sign in windows-1252, but a control character in
    // ISO-8859-1; browsers decode the whole family as windows-1252.
    let text = decode_body(&[0x80], Some("text/html; charset=windows-1252"))
        .expect("decode must succeed");
    assert_eq!(text, "\u{20AC}");

    // UTF-16 with a little-endian byte order mark.
    let mut bytes = vec![0xFF, 0xFE];
    bytes.extend_from_slice(&[0x63, 0x00, 0x61, 0x00, 0x66, 0x00]);
    let text = decode_body(&bytes, Some("text/html; charset=utf-16")).expect("decode must succeed");
    assert_eq!(text, "caf");
}

#[test]
fn decode_body_falls_back_to_utf8_for_an_unknown_charset() {
    let text = decode_body("café".as_bytes(), Some("text/html; charset=x-made-up"))
        .expect("decode must succeed");

    assert_eq!(text, "café");
}
