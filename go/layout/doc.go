// Package layout is the Go port of the block, flex and grid paths of the
// Rust engine's layout (src/layout.rs): the box model, block formatting with
// anonymous text boxes, replaced <img> boxes, Pango text runs, the flex
// formatting context (row/column, wrap, gap, grow/shrink, justify, align)
// and the grid formatting context (px/fr/auto tracks, sparse placement,
// spans, gaps, stretch-to-cell).
//
// The engine has no other formatting contexts, so Display values map 1:1
// (flex and grid containers keep DisplayBlock and are marked by their
// Flex/Grid properties).
package layout
