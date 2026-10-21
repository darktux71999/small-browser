//! Blitz + QuickJS entry point.
//!
//! Usage: blitz-lite file.html

use std::process::ExitCode;

fn main() -> ExitCode {
    let Some(path) = std::env::args().nth(1) else {
        eprintln!("usage: blitz-lite file.html");
        return ExitCode::from(2);
    };

    let html = match std::fs::read_to_string(&path) {
        Ok(html) => html,
        Err(err) => {
            eprintln!("blitz-lite: {path}: {err}");
            return ExitCode::from(1);
        }
    };

    match browser_lite::blitz_engine::run(&html) {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("blitz-lite: {err}");
            ExitCode::from(1)
        }
    }
}
