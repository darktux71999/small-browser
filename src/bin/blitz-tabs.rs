//! Multi-window tabs MVP.
//!
//! Usage: blitz-tabs <file-or-url> [file-or-url...]
//! Each argument opens its own window; per-tab history lives in `tabs.rs` and
//! gets wired to keyboard/window events once T2 lands.

use std::process::ExitCode;

use browser_lite::{blitz_engine, net, url};

fn load(target: &str) -> Result<String, String> {
    if url::is_http(target) {
        net::fetch(target).map(|bytes| String::from_utf8_lossy(&bytes).into_owned())
    } else {
        std::fs::read_to_string(target).map_err(|e| format!("{target}: {e}"))
    }
}

fn main() -> ExitCode {
    let targets: Vec<String> = std::env::args().skip(1).collect();
    if targets.is_empty() {
        eprintln!("usage: blitz-tabs <file-or-url> [file-or-url...]");
        return ExitCode::from(2);
    }

    let mut docs = Vec::new();
    for target in &targets {
        match load(target) {
            Ok(html) => docs.push(blitz_engine::build_document(&html)),
            Err(err) => {
                eprintln!("blitz-tabs: {err}");
                return ExitCode::from(1);
            }
        }
    }

    match blitz_engine::run_documents(docs) {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("blitz-tabs: {err}");
            ExitCode::from(1)
        }
    }
}
