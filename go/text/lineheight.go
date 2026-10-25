package text

import "math"

// LineHeightAbsolute is the one CSS 2.1 line-height projection shared by
// the measuring shaper and the painter (leading-fidelity S1): a unitless
// line-height factor scales the FONT-SIZE, so the line box is
// round(fontSize x lineHeight) device pixels, expressed in Pango units for
// pango_attr_line_height_new_absolute. Pango's factor attribute
// (pango_attr_line_height_new) instead scales the font's natural logical
// height (ascent+descent+line gap), which is the vertical divergence this
// fixes. go/css resolves normal, inherit and percentages to 1.2 before the
// factor reaches here. Both consumers set the attribute from this one
// value, so a run paints exactly where Measure laid it out.
func LineHeightAbsolute(fontSize, lineHeight float64) int {
	if fontSize < 0 {
		fontSize = 0
	}
	if lineHeight < 0 {
		lineHeight = 0
	}
	return int(math.Round(fontSize * lineHeight * PangoScale))
}
