// Package layout is the Go port of the block and flex paths of the Rust
// engine's layout (src/layout.rs): the box model, block formatting with
// anonymous text boxes, replaced <img> boxes, Pango text runs, and the flex
// formatting context (row/column, wrap, gap, grow/shrink, justify, align).
//
// A box whose computed display is grid lays out as plain block for now
// (T4c owns that formatting context); the fallback keeps content visible
// instead of dropping it. The engine has no other formatting contexts, so
// Display values map 1:1.
package layout
