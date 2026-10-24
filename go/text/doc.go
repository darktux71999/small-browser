// Package text measures text through Pango via cgo, mirroring the Rust
// oracle's TextShaper (src/layout.rs:1655-1729). It uses the same C
// libraries the oracle links (S2): pangocairo for the measuring context at
// 72 dpi (so 1pt == 1px and CSS px sizes map 1:1), pango for layout, and
// fontconfig for family availability.
//
// Pango rounds every glyph advance to whole pixels at 72 dpi; measured
// widths are integer multiples of per-glyph advances. Matching the oracle's
// rounding exactly means using the same calls, not reimplementing them:
// line extents come from pango_layout_line_get_pixel_extents and the
// absolute line position from pango_layout_index_to_pos, exactly as the
// oracle reads them (src/layout.rs:1702-1719).
package text
