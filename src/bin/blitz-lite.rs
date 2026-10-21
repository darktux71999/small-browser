//! Blitz + QuickJS entry point.
//!
//! Usage: blitz-lite file.html
//!        blitz-lite https://example.com

use std::process::ExitCode;

use browser_lite::{net, url};

fn main() -> ExitCode {
    let Some(target) = std::env::args().nth(1) else {
        eprintln!("usage: blitz-lite <file.html | http(s)://url>");
        return ExitCode::from(2);
    };

    let html = if url::is_http(&target) {
        // `fetch_text` decodes the body with the charset declared in the
        // `Content-Type` header, instead of guessing UTF-8.
        match net::fetch_text(&target) {
            Ok(text) => text,
            Err(err) => {
                eprintln!("blitz-lite: {err}");
                return ExitCode::from(1);
            }
        }
    } else {
        match std::fs::read_to_string(&target) {
            Ok(html) => html,
            Err(err) => {
                eprintln!("blitz-lite: {target}: {err}");
                return ExitCode::from(1);
            }
        }
    };

    // Pass the source URL so relative subresources resolve; without it Blitz
    // falls back to a data: base and panics on the first stylesheet link.
    let base = if url::is_http(&target) {
        Some(target.clone())
    } else {
        url::file_url(&target)
    };
    match browser_lite::blitz_engine::run_html(&html, base) {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("blitz-lite: {err}");
            ExitCode::from(1)
        }
    }
}
