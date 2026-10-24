// Package layout is the Go port of the block path of the Rust engine's
// layout (src/layout.rs): the box model, block formatting with anonymous
// text boxes, replaced <img> boxes and Pango text runs.
//
// A box whose computed display is flex or grid lays out as plain block for
// now (T4b/T4c own those formatting contexts); the fallback keeps content
// visible instead of dropping it. The engine has no other formatting
// contexts, so Display values map 1:1.
package layout
