//! Link the MIT NetSurf CSS stack into the engine.
//!
//! The three static archives are packaged by the KISS recipes (T1) and installed
//! system-wide, so the default case is to let the linker find them in /usr/lib.
//! During development, before `kiss i`, point `CSS_LIB_PREFIX` at a private
//! prefix such as `~/.cache/css-spike/prefix`.
//!
//! Link order matters for static archives: dependants come first.

use std::env;

fn main() {
    // System-first: only an EXPLICIT CSS_LIB_PREFIX adds a search path, so a
    // plain `cargo build` links /usr/lib and never silently picks up dev
    // archives from a private prefix (verifier advisory A1).
    if let Ok(prefix) = env::var("CSS_LIB_PREFIX") {
        println!("cargo:rustc-link-search=native={prefix}/lib");
    }

    println!("cargo:rustc-link-lib=static=css");
    println!("cargo:rustc-link-lib=static=parserutils");
    println!("cargo:rustc-link-lib=static=wapcaplet");

    println!("cargo:rerun-if-env-changed=CSS_LIB_PREFIX");
    println!("cargo:rerun-if-changed=build.rs");
}
