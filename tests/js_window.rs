//! S4: a minimal `window` surface for inline page scripts.
//!
//! The observable channel is `document.title` (the runtime already exposes it);
//! script errors are observed through `eval`'s `Result`, exactly as the engine
//! reports them today.

use std::process::Command;

use browser_lite::js::JsRuntime;

fn rt() -> JsRuntime {
    JsRuntime::new("start").expect("runtime")
}

fn title_after(mut runtime: JsRuntime, source: &str) -> String {
    runtime.eval(source, "t.js").expect("script should run");
    runtime.title()
}

#[test]
fn window_is_the_global_alias() {
    let title = title_after(
        rt(),
        "document.title = String(window === globalThis) + ':' \
         + String(window.document === document) + ':' \
         + String(window.console === console);",
    );
    assert_eq!(title, "true:true:true");
}

#[test]
fn window_and_bare_globals_share_state() {
    let title = title_after(
        rt(),
        "window.__shared = 41; bare = 7; \
         document.title = String(__shared + 1) + ':' + String(window.bare);",
    );
    assert_eq!(title, "42:7");
}

#[test]
fn unknown_window_members_are_undefined_not_fatal() {
    let title = title_after(
        rt(),
        "document.title = typeof window.__no_such_member + ':' \
         + (window.__no_such_member === undefined);",
    );
    assert_eq!(title, "undefined:true");
}

#[test]
fn location_defaults_to_about_blank() {
    let title = title_after(
        rt(),
        "document.title = location.href + '|' + location.protocol + '|' \
         + location.host + '|' + location.hostname + '|' + location.pathname;",
    );
    assert_eq!(title, "about:blank|about:|||blank");
}

#[test]
fn set_url_fills_location_fields() {
    let mut runtime = rt();
    runtime
        .set_url("https://codeberg.org/explore/repos?q=wiki#top")
        .expect("set_url");
    runtime
        .eval(
            "document.title = location.href + '|' + location.protocol + '|' \
             + location.host + '|' + location.hostname + '|' + location.pathname;",
            "t.js",
        )
        .expect("script");
    assert_eq!(
        runtime.title(),
        "https://codeberg.org/explore/repos?q=wiki#top|https:|codeberg.org|codeberg.org|/explore/repos"
    );
}

#[test]
fn set_url_keeps_the_explicit_port_in_host_only() {
    let mut runtime = rt();
    runtime.set_url("http://127.0.0.1:8080/a").expect("set_url");
    runtime
        .eval(
            "document.title = location.host + '|' + location.hostname + '|' + location.pathname;",
            "t.js",
        )
        .expect("script");
    assert_eq!(runtime.title(), "127.0.0.1:8080|127.0.0.1|/a");
}

#[test]
fn set_url_rejects_invalid_urls() {
    let mut runtime = rt();
    assert!(runtime.set_url("not a url").is_err());
    runtime
        .eval("document.title = location.href;", "t.js")
        .expect("script");
    assert_eq!(runtime.title(), "about:blank");
}

#[test]
fn navigator_exposes_a_user_agent_string() {
    let title = title_after(
        rt(),
        "document.title = typeof navigator.userAgent + ':' + navigator.userAgent;",
    );
    assert_eq!(title, "string:browser-lite/0.1");
}

#[test]
fn location_is_read_only_for_page_scripts() {
    let mut runtime = rt();
    runtime.set_url("https://example.org/x").expect("set_url");

    // Sloppy-mode assignment is ignored, not a crash.
    runtime
        .eval(
            "location.href = 'https://evil.example/'; document.title = location.href;",
            "sloppy.js",
        )
        .expect("sloppy assignment must not throw");
    assert_eq!(runtime.title(), "https://example.org/x");

    // Strict mode turns the same assignment into a reported TypeError.
    let err = runtime
        .eval("'use strict'; location.href = 'https://evil.example/';", "strict.js")
        .expect_err("strict assignment must be rejected");
    assert!(err.contains("TypeError"), "unexpected error: {err}");
    runtime
        .eval("document.title = location.href;", "after.js")
        .expect("script after the error");
    assert_eq!(runtime.title(), "https://example.org/x");
}

#[test]
fn dom_content_loaded_handlers_run_once_each() {
    let mut runtime = rt();
    runtime
        .eval(
            "window.__count = 0; \
             window.addEventListener('DOMContentLoaded', function (event) { \
               window.__count += 1; \
               document.title = 'dcl' + window.__count + ':' + event.type; \
             });",
            "first.js",
        )
        .expect("script");
    assert_eq!(runtime.title(), "dcl1:DOMContentLoaded");

    // A handler registered by a later script still fires, and the earlier one
    // is not re-run.
    runtime
        .eval(
            "window.addEventListener('DOMContentLoaded', function () { \
               window.__count += 10; document.title = 'dcl' + window.__count; \
             });",
            "second.js",
        )
        .expect("script");
    assert_eq!(runtime.title(), "dcl11");
}

#[test]
fn load_handlers_receive_the_event() {
    let title = title_after(
        rt(),
        "window.addEventListener('load', function (event) { \
           document.title = 'load:' + event.type + ':' + String(event.target === window); \
         });",
    );
    assert_eq!(title, "load:load:true");
}

#[test]
fn remove_event_listener_unregisters_the_handler() {
    let mut runtime = rt();
    runtime
        .eval(
            "window.__fired = 0; \
             function handler() { window.__fired += 1; document.title = 'fired' + window.__fired; } \
             window.addEventListener('load', handler); \
             window.removeEventListener('load', handler);",
            "t.js",
        )
        .expect("script");
    assert_eq!(runtime.title(), "start");

    runtime.eval("1 + 1;", "again.js").expect("script");
    assert_eq!(runtime.title(), "start");
}

#[test]
fn throwing_handler_is_reported_but_does_not_stop_other_handlers() {
    let mut runtime = rt();
    runtime
        .eval(
            "window.addEventListener('DOMContentLoaded', function () { throw new Error('boom'); }); \
             window.addEventListener('DOMContentLoaded', function () { document.title = 'after-boom'; });",
            "t.js",
        )
        .expect("a handler error must not fail the page script");
    assert_eq!(runtime.title(), "after-boom");
}

#[test]
fn script_errors_stay_reported_and_non_fatal() {
    let mut runtime = rt();
    let err = runtime
        .eval("window.__missing_function();", "bad.js")
        .expect_err("calling an unknown member must be reported");
    assert!(err.contains("TypeError"), "unexpected error: {err}");

    runtime
        .eval("document.title = window.location.href;", "ok.js")
        .expect("the runtime must still work after a script error");
    assert_eq!(runtime.title(), "about:blank");
}

#[test]
fn inline_window_page_runs_without_reference_errors() {
    let output = Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .arg("--dump")
        .arg("test/data/js-window.html")
        .output()
        .expect("run browser-lite");
    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);

    assert!(output.status.success(), "engine failed: {stderr}");
    assert!(
        !stderr.contains("ReferenceError"),
        "inline script still sees a missing global: {stderr}"
    );
    assert!(
        !stderr.contains("js error"),
        "unexpected script error: {stderr}"
    );
    assert!(
        stdout.contains("title: window:true:true:true:true"),
        "unexpected title in output: {stdout}"
    );
}

#[test]
fn inline_script_errors_are_reported_and_do_not_abort_the_page() {
    let output = Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .arg("--dump")
        .arg("test/data/js-window-error.html")
        .output()
        .expect("run browser-lite");
    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);

    assert!(
        output.status.success(),
        "a page script error must not abort the engine: {stderr}"
    );
    assert!(
        stderr.contains("browser-lite: js error:") && stderr.contains("TypeError"),
        "script error was not reported: {stderr}"
    );
    assert!(
        stdout.contains("title: before-error"),
        "page state before the error was lost: {stdout}"
    );
}
