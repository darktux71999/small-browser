// Package js embeds QuickJS via cgo, mirroring the Rust oracle's runtime
// (src/js.rs, system libqjs): scripts evaluate in a persistent context,
// thrown exceptions surface as oracle-format errors, and objects or
// functions that outlive one Eval are addressed through a handle registry
// (T2 hangs document/window/timers off these handles). The C side of the
// bridge lives in glue.h/glue.c; no Go pointer ever enters C.
package js
