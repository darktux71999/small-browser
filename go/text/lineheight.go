package text

import "math"

// LineHeightAbsolute projects a numeric CSS line-height factor: it scales the
// font-size (CSS 2.1 / leading-fidelity S1), not Pango's natural font height.
// Normal line-height is selected separately and resolved from Pango metrics by
// both consumers. This shared projection keeps numeric runs measured and
// painted with the same Pango units.
func LineHeightAbsolute(fontSize, lineHeight float64) int {
	if fontSize < 0 {
		fontSize = 0
	}
	if lineHeight < 0 {
		lineHeight = 0
	}
	return int(math.Round(fontSize * lineHeight * PangoScale))
}
