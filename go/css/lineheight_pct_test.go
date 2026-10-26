package css

import "testing"

// CSS 2.1 §10.8: a percentage line-height resolves against the element's own
// font size — the used value is that percentage as a factor of font-size. It
// used to fall back to 1.2, which cramped every percentage-styled block on
// live pages (the user-visible "text too close together").
func TestLineHeightPercentageResolvesAsFactor(t *testing.T) {
	s := styleOf(t, `<style>p { font-size: 10px; line-height: 200% }</style><p id="x">x</p>`, "#x")
	if s.LineHeightNormal {
		t.Fatal("percentage line-height collapsed to the normal mode")
	}
	if !near(s.LineHeight, 2.0) {
		t.Fatalf("line-height: 200%% resolved to factor %v, want 2.0", s.LineHeight)
	}
}

// The codeberg support block uses a ~175% line height; pin the realistic
// value too so the factor cannot silently drift back to 1.2.
func TestLineHeightPercentageRealisticValue(t *testing.T) {
	s := styleOf(t, `<style>p { font-size: 16px; line-height: 175% }</style><p id="x">x</p>`, "#x")
	if s.LineHeightNormal || !near(s.LineHeight, 1.75) {
		t.Fatalf("line-height: 175%% resolved to normal=%v factor=%v, want 1.75",
			s.LineHeightNormal, s.LineHeight)
	}
}
