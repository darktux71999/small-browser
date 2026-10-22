//! S2 — external CSS subresources: `<link rel="stylesheet">`, remote
//! `@import` chains, per-run caching, URL resolution and graceful degradation.
//!
//! The tests drive the public fetch-aware API only: they read fixtures from
//! `test/data/`, load them with an explicit base (the fixture directory for
//! local pages, an HTTP base for remote pages) and assert on the computed
//! style of real DOM nodes. Nothing reaches into the libcss FFI.

use std::io::{Read, Write};
use std::net::TcpListener;
use std::thread;
use std::time::{Duration, Instant};

use browser_lite::css_libcss::{self, SheetLimits, SheetSet, StylesheetRef};
use browser_lite::css::{Color, Display, Style};
use browser_lite::dom::Document;

fn fixture(name: &str) -> String {
    std::fs::read_to_string(format!(
        "{}/test/data/{name}",
        env!("CARGO_MANIFEST_DIR")
    ))
    .unwrap_or_else(|e| panic!("fixture {name}: {e}"))
}

/// The base for the local fixtures: their containing directory.
fn fixture_base() -> String {
    format!("{}/test/data/", env!("CARGO_MANIFEST_DIR"))
}

fn fixture_doc(name: &str) -> Document {
    Document::parse(&fixture(name))
}

fn compute(doc: &Document, base: &str, viewport_width: f64) -> Vec<Style> {
    css_libcss::compute_with_subresources(doc, base, viewport_width)
}

fn rgb(c: Color) -> (i32, i32, i32) {
    (
        (c.r * 255.0).round() as i32,
        (c.g * 255.0).round() as i32,
        (c.b * 255.0).round() as i32,
    )
}

fn style_of<'a>(doc: &Document, styles: &'a [Style], selector: &str) -> &'a Style {
    let id = doc
        .query_selector(selector)
        .unwrap_or_else(|| panic!("no {selector} in fixture"));
    &styles[id]
}

/// A tiny HTTP server: each request path is answered from `routes`, anything
/// else gets a 404. Connections are served one at a time, in arrival order.
fn spawn_server(routes: Vec<(&'static str, String)>) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
    let port = listener.local_addr().expect("server address").port();
    thread::spawn(move || {
        for stream in listener.incoming() {
            let Ok(mut stream) = stream else { break };
            let mut buffer = [0u8; 2048];
            let n = stream.read(&mut buffer).unwrap_or(0);
            let request = String::from_utf8_lossy(&buffer[..n]).into_owned();
            let path = request
                .lines()
                .next()
                .and_then(|line| line.split_whitespace().nth(1))
                .unwrap_or("/");
            let (status, body) = match routes.iter().find(|(p, _)| *p == path) {
                Some((_, body)) => ("200 OK", body.as_str()),
                None => ("404 Not Found", ""),
            };
            let response = format!(
                "HTTP/1.1 {status}\r\nContent-Type: text/css\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            );
            let _ = stream.write_all(response.as_bytes());
            let _ = stream.flush();
        }
    });
    format!("http://127.0.0.1:{port}")
}

/// A server that accepts the connection and never answers.
fn spawn_hanging_server() -> String {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind hanging server");
    let port = listener.local_addr().expect("server address").port();
    thread::spawn(move || {
        let Ok((stream, _)) = listener.accept() else {
            return;
        };
        thread::sleep(Duration::from_secs(5));
        drop(stream);
    });
    format!("http://127.0.0.1:{port}")
}

/// S2: external sheets and inline blocks apply in document order.
#[test]
fn external_sheets_and_inline_blocks_apply_in_document_order() {
    let doc = fixture_doc("sub-order.html");
    let styles = compute(&doc, &fixture_base(), 800.0);

    // `.order`: last link wins over the earlier one.
    let order = style_of(&doc, &styles, "#order");
    assert_eq!(rgb(order.color), (0, 0, 255), "last stylesheet wins");
    // The earlier external sheet did apply (its font-size survives).
    assert_eq!(order.font_size, 31.0, "first stylesheet applied");

    // `.between`: the inline block sits after the first link, so it wins there.
    assert_eq!(rgb(style_of(&doc, &styles, "#between").color), (0, 255, 0));
}

/// S2: a `media="print"` link contributes nothing to a screen render.
#[test]
fn print_only_links_are_not_applied() {
    let doc = fixture_doc("sub-order.html");
    let styles = compute(&doc, &fixture_base(), 800.0);
    assert_eq!(rgb(style_of(&doc, &styles, "#print-only").color), (0, 0, 0));
}

/// S2: `@import` chains are fetched recursively, relative URLs resolve against
/// the importing sheet, and imports cascade before the importing sheet's rules.
#[test]
fn external_imports_load_recursively_and_cascade_before_the_importing_sheet() {
    let doc = fixture_doc("sub-import.html");
    let styles = compute(&doc, &fixture_base(), 800.0);

    // sub-import.css is applied, and its own rule beats the imported one.
    assert_eq!(rgb(style_of(&doc, &styles, "#tie").color), (0, 255, 0));
    // A rule that only exists in the imported child is applied.
    assert_eq!(rgb(style_of(&doc, &styles, "#chain").color), (0, 0, 255));
    // Two hops down the chain.
    assert_eq!(style_of(&doc, &styles, "#grand").font_size, 27.0);
}

/// S2: `@import` inside an inline `<style>` block resolves against the
/// document base, exactly like a `<link>` href.
#[test]
fn inline_style_imports_resolve_against_the_document_base() {
    let html = "<style>@import url(\"sub-import-grandchild.css\");</style>\
                <p class=grand id=g>g</p>";
    let doc = Document::parse(html);
    let styles = compute(&doc, &fixture_base(), 800.0);
    assert_eq!(style_of(&doc, &styles, "#g").font_size, 27.0);
}

/// S2: the same resolved URL is fetched once per run.
#[test]
fn each_resolved_url_is_fetched_once() {
    let doc = fixture_doc("sub-dedupe.html");
    let mut set = SheetSet::load(&doc, &fixture_base());
    assert_eq!(set.fetched(), 1, "one URL, two links");
    let styles = css_libcss::compute_with_sheets(&doc, &mut set, 800.0);
    assert_eq!(style_of(&doc, &styles, "#order").font_size, 31.0);
    assert!(set.warnings().is_empty(), "warnings: {:?}", set.warnings());
}

/// S2: an unreachable sheet degrades; the rest of the page still styles.
#[test]
fn unreachable_sheet_degrades_with_a_warning() {
    let doc = fixture_doc("sub-broken.html");
    let mut set = SheetSet::load(&doc, &fixture_base());
    let styles = css_libcss::compute_with_sheets(&doc, &mut set, 800.0);

    assert_eq!(style_of(&doc, &styles, "#order").font_size, 31.0);
    assert!(
        set.warnings().iter().any(|w| w.contains("sub-missing.css")),
        "warnings: {:?}",
        set.warnings()
    );
}

/// S2: a sheet over the size cap is skipped as a whole, never truncated.
#[test]
fn oversized_sheet_is_skipped_with_a_warning() {
    let doc = fixture_doc("sub-order.html");
    let limits = SheetLimits {
        max_bytes: 16,
        ..SheetLimits::default()
    };
    let mut set = SheetSet::load_with_limits(&doc, &fixture_base(), limits);
    let styles = css_libcss::compute_with_sheets(&doc, &mut set, 800.0);

    // Nothing from the oversized sheets applied: the default font size stands,
    // and the inline block (not a fetched resource) still wins its element.
    assert_eq!(style_of(&doc, &styles, "#order").font_size, 16.0);
    assert_eq!(rgb(style_of(&doc, &styles, "#order").color), (0, 0, 0));
    assert_eq!(rgb(style_of(&doc, &styles, "#between").color), (0, 255, 0));
    assert!(
        set.warnings().iter().any(|w| w.contains("sub-order-a.css")),
        "warnings: {:?}",
        set.warnings()
    );
}

/// S2: over HTTP the body is rejected as a whole when it passes the cap, so a
/// truncated sheet never reaches the cascade.
#[test]
fn oversized_http_sheet_is_rejected_without_truncation() {
    let big = format!(".big {{ color: #ff0000 }} /* {} */", "x".repeat(4096));
    let server = spawn_server(vec![("/big.css", big)]);
    let html = format!(
        "<link rel=stylesheet href=\"{server}/big.css\"><p class=big id=big>b</p>"
    );
    let doc = Document::parse(&html);
    let limits = SheetLimits {
        max_bytes: 256,
        ..SheetLimits::default()
    };

    let mut set = SheetSet::load_with_limits(&doc, "about:blank", limits);
    let styles = css_libcss::compute_with_sheets(&doc, &mut set, 800.0);

    assert_eq!(rgb(style_of(&doc, &styles, "#big").color), (0, 0, 0));
    assert!(
        set.warnings()
            .iter()
            .any(|w| w.contains("big.css") && w.contains("larger than")),
        "warnings: {:?}",
        set.warnings()
    );
}

/// S2: a slow sheet hits the timeout and the remaining sheets still apply.
#[test]
fn slow_sheet_times_out_without_blocking_the_rest() {
    let slow = spawn_hanging_server();
    let fast = spawn_server(vec![("/ok.css", ".ok { color: #112233 }".to_string())]);
    let html = format!(
        "<link rel=stylesheet href=\"{slow}/slow.css\">\
         <link rel=stylesheet href=\"{fast}/ok.css\">\
         <p class=ok id=ok>ok</p>"
    );
    let doc = Document::parse(&html);
    let limits = SheetLimits {
        timeout: Duration::from_millis(300),
        ..SheetLimits::default()
    };

    let started = Instant::now();
    let mut set = SheetSet::load_with_limits(&doc, "about:blank", limits);
    let styles = css_libcss::compute_with_sheets(&doc, &mut set, 800.0);
    let elapsed = started.elapsed();

    assert_eq!(rgb(style_of(&doc, &styles, "#ok").color), (17, 34, 51));
    assert!(
        set.warnings().iter().any(|w| w.contains("slow.css")),
        "warnings: {:?}",
        set.warnings()
    );
    assert!(
        elapsed < Duration::from_secs(2),
        "timeout did not bound the fetch: {elapsed:?}"
    );
}

/// S2: absolute `file://` links are read from disk.
#[test]
fn file_url_links_are_read_from_disk() {
    let sheet = format!(
        "file://{}/test/data/sub-order-a.css",
        env!("CARGO_MANIFEST_DIR")
    );
    let html = format!(
        "<link rel=stylesheet href=\"{sheet}\"><p class=order id=order>o</p>"
    );
    let doc = Document::parse(&html);
    let styles = compute(&doc, "/nonexistent/", 800.0);
    assert_eq!(style_of(&doc, &styles, "#order").font_size, 31.0);
}

/// S2: on http(s) pages relative links and imports resolve against the
/// document's final URL (the base passed by the caller).
#[test]
fn http_bases_resolve_relative_links_and_imports() {
    let server = spawn_server(vec![
        (
            "/page.css",
            "@import url(\"nested/child.css\"); .net { font-size: 23px }".to_string(),
        ),
        ("/nested/child.css", ".net { color: #112233 }".to_string()),
    ]);
    let html = "<link rel=stylesheet href=\"page.css\"><p class=net id=net>n</p>";
    let doc = Document::parse(html);

    let mut set = SheetSet::load(&doc, &format!("{server}/"));
    let styles = css_libcss::compute_with_sheets(&doc, &mut set, 800.0);

    let net = style_of(&doc, &styles, "#net");
    assert_eq!(rgb(net.color), (17, 34, 51));
    assert_eq!(net.font_size, 23.0);
    assert_eq!(set.fetched(), 2, "page.css plus its import");
    assert!(set.warnings().is_empty(), "warnings: {:?}", set.warnings());
}

/// S2: the collected sources keep document order across `<link>` and `<style>`,
/// and report the resolved-agnostic href/href media of each link.
#[test]
fn stylesheet_refs_follow_document_order() {
    let doc = fixture_doc("sub-order.html");
    let refs = css_libcss::subresource_sheets(&doc);

    assert_eq!(refs.len(), 4, "refs: {refs:?}");
    assert_eq!(
        refs[0],
        StylesheetRef::External {
            href: "sub-order-a.css".to_string(),
            media: None,
        }
    );
    assert!(matches!(&refs[1], StylesheetRef::Inline(css) if css.contains(".between")));
    assert_eq!(
        refs[2],
        StylesheetRef::External {
            href: "sub-order-b.css".to_string(),
            media: None,
        }
    );
    assert_eq!(
        refs[3],
        StylesheetRef::External {
            href: "sub-order-print.css".to_string(),
            media: Some("print".to_string()),
        }
    );
}

/// PRESERVE: the pinned legacy API still sees inline `<style>` only, and still
/// ignores external sheets entirely.
#[test]
fn legacy_stylesheets_and_compute_stay_inline_only() {
    let doc = fixture_doc("sub-order.html");
    let sheets = css_libcss::stylesheets(&doc);
    assert_eq!(sheets.len(), 1, "sheets: {sheets:?}");
    assert!(sheets[0].contains(".between"));

    let styles = css_libcss::compute(&doc, &sheets, 800.0);
    let order = style_of(&doc, &styles, "#order");
    assert_eq!(rgb(order.color), (0, 0, 0), "no external CSS in the legacy path");
    assert_eq!(order.font_size, 16.0);
    assert_eq!(rgb(style_of(&doc, &styles, "#between").color), (0, 255, 0));
    // `Display` stays part of the mapped style surface.
    assert_eq!(style_of(&doc, &styles, "#order").display, Display::Block);
}
