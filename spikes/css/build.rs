//! Linkado del spike: libcss + libparserutils + libwapcaplet (MIT, construidas por
//! spikes/css/build-libcss.sh en ~/.cache/css-spike) y Pango/cairo del sistema.

use std::process::Command;

fn main() {
    let prefix = std::env::var("CSS_SPIKE_PREFIX").unwrap_or_else(|_| {
        format!(
            "{}/.cache/css-spike/prefix",
            std::env::var("HOME").expect("HOME")
        )
    });
    println!("cargo:rustc-link-search=native={prefix}/lib");
    // Orden: los dependientes primero, como exige el resolvedor de libs estáticas.
    println!("cargo:rustc-link-lib=static=css");
    println!("cargo:rustc-link-lib=static=parserutils");
    println!("cargo:rustc-link-lib=static=wapcaplet");

    // Pango/cairo/glib del sistema, sin crates intermedios (FFI fino, estilo sys.rs).
    let out = Command::new("pkg-config")
        .args(["--libs", "pangocairo", "cairo"])
        .output()
        .expect("pkg-config pangocairo cairo");
    assert!(out.status.success(), "pkg-config fallo: {}", String::from_utf8_lossy(&out.stderr));
    for flag in String::from_utf8_lossy(&out.stdout).split_whitespace() {
        if let Some(lib) = flag.strip_prefix("-l") {
            println!("cargo:rustc-link-lib={lib}");
        } else if let Some(path) = flag.strip_prefix("-L") {
            println!("cargo:rustc-link-search=native={path}");
        }
    }
    println!("cargo:rerun-if-changed=build.rs");
}
