package text

import "testing"

// LineHeightAbsolute is a pure helper (no Pango context): it clamps negative
// inputs to zero and projects font-size x line-height into Pango units.
// These pins cover the clamping edge cases the helper already handles
// correctly (leading-fidelity S1): no contract is violated, the behavior is
// just not yet pinned, so they are coverage-GREEN on arrival.
func TestLineHeightAbsoluteClampsNegativesToZero(t *testing.T) {
	cases := []struct {
		name       string
		fontSize   float64
		lineHeight float64
		want       int
	}{
		{"negative font size", -5, 1.2, 0},
		{"negative line height", 16, -1, 0},
		{"zero font size", 0, 1.2, 0},
		{"zero line height", 16, 0, 0},
		{"both negative", -3, -2, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LineHeightAbsolute(tc.fontSize, tc.lineHeight); got != tc.want {
				t.Fatalf("LineHeightAbsolute(%v, %v) = %d, want %d", tc.fontSize, tc.lineHeight, got, tc.want)
			}
		})
	}
}

func TestLineHeightAbsoluteProjectsFontSizeTimesFactor(t *testing.T) {
	// CSS 2.1 (leading-fidelity S1): line box = round(fontSize x lineHeight)
	// Pango units, never the font's natural height. 16px x 1.2 -> 19.2px ->
	// round(19.2 * 1024) = 19661 Pango units.
	if got := LineHeightAbsolute(16, 1.2); got != 19661 {
		t.Fatalf("LineHeightAbsolute(16, 1.2) = %d, want 19661 (16px x 1.2)", got)
	}
}
