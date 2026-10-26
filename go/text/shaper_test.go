// Tests for the Pango text shaper (go/text), mirroring the behavioral
// expectations of the Rust oracle's TextShaper (src/layout.rs) and its
// text_pango.rs suite: wrapping at a given width, line-height scaling,
// letter-spacing widening, alignment offsets and family resolution.
// Table-driven; geometry is asserted exactly (Pango pixel extents are
// integers) except where the oracle itself is float-based.
package text

import (
	"strings"
	"testing"
)

func defaultStyle() Style {
	return Style{
		FontFamilies:  []string{"sans-serif"},
		FontFamily:    "Sans",
		FontSize:      16,
		FontWeight:    400,
		Italic:        false,
		LineHeight:    1.2,
		TextAlign:     AlignLeft,
		LetterSpacing: 0,
	}
}

func TestMeasureSingleLine(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	m := s.Measure("Hello world", defaultStyle(), 1000)
	if len(m.Lines) != 1 {
		t.Fatalf("single short run should be one line, got %d", len(m.Lines))
	}
	line := m.Lines[0]
	if line.Text != "Hello world" {
		t.Fatalf("line text = %q, want %q", line.Text, "Hello world")
	}
	if line.W <= 0 || line.H <= 0 {
		t.Fatalf("line extents must be positive, got w=%v h=%v", line.W, line.H)
	}
	if line.X != 0 {
		t.Fatalf("first left-aligned line starts at the left edge, got x=%v", line.X)
	}
	// CSS half-leading (leading-fidelity S1): the absolute line box can be
	// smaller than the font's natural content height, so Pango centres the
	// glyphs and the first line's y goes negative (this default style is a
	// 19.2px box over the installed sans's 23px natural height: y = -1.9).
	// The old factor attribute always grew the natural height, so y was
	// never negative.
	if line.Y < -line.H || line.Y >= line.H {
		t.Fatalf("first line y=%v should sit within one line box of its height h=%v (CSS half-leading)", line.Y, line.H)
	}
	if m.Height <= 0 {
		t.Fatalf("measured height must be positive, got %v", m.Height)
	}

	// Integral pin (S1): 20px x 1.2 is a 24px line box whatever font
	// resolves; the installed sans's 28px natural content height centres
	// inside it, leaving a -2px first-line half-leading.
	integral := defaultStyle()
	integral.FontSize = 20
	integral.LineHeight = 1.2
	mi := s.Measure("Hello world", integral, 1000)
	if mi.Height != 24 {
		t.Fatalf("20px x 1.2 line box = %v, want 24", mi.Height)
	}
	if len(mi.Lines) != 1 {
		t.Fatalf("20px x 1.2 short run should be one line, got %d", len(mi.Lines))
	}
	if mi.Lines[0].Y != -2 {
		t.Fatalf("20px x 1.2 first line y = %v, want -2 (CSS half-leading)", mi.Lines[0].Y)
	}
}

func TestMeasureWrapsAtWidth(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	text := "the quick brown fox jumps over the lazy dog many times over and over"
	m := s.Measure(text, defaultStyle(), 200)
	if len(m.Lines) < 3 {
		t.Fatalf("narrow width should wrap into several lines, got %d", len(m.Lines))
	}
	for _, line := range m.Lines {
		if line.W > 200+1.0 {
			t.Fatalf("line %q escapes the wrap width: w=%v", line.Text, line.W)
		}
		if strings.TrimSpace(line.Text) == "" {
			t.Fatalf("wrapped lines must carry text")
		}
	}
	// No words lost: the joined lines recover the input words.
	words := strings.Fields(text)
	var got []string
	for _, line := range m.Lines {
		got = append(got, strings.Fields(line.Text)...)
	}
	if strings.Join(got, " ") != strings.Join(words, " ") {
		t.Fatalf("wrapped lines lost words: %q", got)
	}
}

func TestMeasureLineHeightScales(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	tight := defaultStyle()
	tight.LineHeight = 1.0
	loose := defaultStyle()
	loose.LineHeight = 2.0
	text := "line one line two line three line four line five line six"
	hm := s.Measure(text, tight, 200).Height
	hl := s.Measure(text, loose, 200).Height
	if hl <= hm*1.5 {
		t.Fatalf("line-height 2 should clearly exceed line-height 1 (tight=%v loose=%v)", hm, hl)
	}
}

func TestMeasureLineHeightAbsolute(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	// CSS 2.1 (leading-fidelity S1): a unitless line-height factor scales
	// the font-size, so the line box is round(size x factor) regardless of
	// the resolved font's natural height. Pango's absolute attribute takes
	// Pango units and snaps the layout to whole device pixels.
	cases := []struct {
		name       string
		fontSize   float64
		lineHeight float64
		wantHeight float64
	}{
		{"20px at 1.2 is a 24px line box", 20, 1.2, 24},
		{"16px at 1.5 is a 24px line box", 16, 1.5, 24},
		{"16px at 1 is a 16px box below the natural height", 16, 1.0, 16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			style := defaultStyle()
			style.FontSize = tc.fontSize
			style.LineHeight = tc.lineHeight
			m := s.Measure("Hello world", style, 1000)
			if len(m.Lines) != 1 {
				t.Fatalf("single short run should be one line, got %d", len(m.Lines))
			}
			if m.Height != tc.wantHeight {
				t.Fatalf("line box height = %v, want %v (font-size %v x line-height %v)",
					m.Height, tc.wantHeight, tc.fontSize, tc.lineHeight)
			}
		})
	}

	// Half-leading pin: 16px x 1.2 is a 19.2px box over the installed
	// sans's 23px natural height, so the first line starts at y = -1.9
	// (the P9a diagnosis value; whole Pango units make it exact).
	m := s.Measure("Hello world", defaultStyle(), 1000)
	if y := m.Lines[0].Y; y != -1.8994140625 {
		t.Fatalf("16px x 1.2 first line y = %v, want -1.8994140625 (CSS half-leading)", y)
	}
}

func TestMeasureNormalLineHeightUsesFontMetrics(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	normal := defaultStyle()
	normal.LineHeightNormal = true
	natural := s.Measure("MMMM", normal, 1000)
	if natural.Metrics.Ascent <= 0 || natural.Metrics.Descent <= 0 || natural.Metrics.Height <= 0 {
		t.Fatalf("Pango returned invalid natural metrics: %+v", natural.Metrics)
	}
	if len(natural.Lines) != 1 {
		t.Fatalf("natural line should not wrap, got %d lines", len(natural.Lines))
	}
	line := natural.Lines[0]
	if line.H < natural.Height || line.H-natural.Height > 1 || line.Baseline <= 0 || line.Baseline >= line.H {
		t.Fatalf("natural line box/baseline inconsistent: total=%.4f line=%+v metrics=(ascent=%.4f descent=%.4f height=%.4f)", natural.Height, line, natural.Metrics.Ascent, natural.Metrics.Descent, natural.Metrics.Height)
	}
	t.Logf("Pango natural metrics: ascent=%.4f descent=%.4f height=%.4f, line=%+v total=%.4f", natural.Metrics.Ascent, natural.Metrics.Descent, natural.Metrics.Height, line, natural.Height)
	factor := defaultStyle()
	explicit := s.Measure("MMMM", factor, 1000)
	if natural.Height <= explicit.Height {
		t.Fatalf("normal line-height %v should use natural metrics, above explicit 1.2 factor %v (font metrics %+v)", natural.Height, explicit.Height, natural.Metrics)
	}
}

func TestMeasureLetterSpacingWidens(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	plain := s.Measure("Hello world hello world", defaultStyle(), 1000)
	spacedStyle := defaultStyle()
	spacedStyle.LetterSpacing = 5
	spaced := s.Measure("Hello world hello world", spacedStyle, 1000)
	if len(plain.Lines) != 1 || len(spaced.Lines) != 1 {
		t.Fatalf("both runs should fit on one line (plain=%d spaced=%d)", len(plain.Lines), len(spaced.Lines))
	}
	if spaced.Lines[0].W <= plain.Lines[0].W+50 {
		t.Fatalf("letter-spacing 5px should widen the line by ~5px per char (plain=%v spaced=%v)",
			plain.Lines[0].W, spaced.Lines[0].W)
	}
}

func TestMeasureAlignmentOffsets(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	left := s.Measure("Hi", defaultStyle(), 500).Lines[0]
	centered := defaultStyle()
	centered.TextAlign = AlignCenter
	center := s.Measure("Hi", centered, 500).Lines[0]
	right := defaultStyle()
	right.TextAlign = AlignRight
	rightLine := s.Measure("Hi", right, 500).Lines[0]

	if center.X <= left.X+10 {
		t.Fatalf("centered line should start right of the left one (left=%v center=%v)", left.X, center.X)
	}
	if rightLine.X+rightLine.W < 500-1.0 {
		t.Fatalf("right-aligned line should end at the content edge (end=%v edge=500)",
			rightLine.X+rightLine.W)
	}
}

func TestResolveFamily(t *testing.T) {
	cases := []struct {
		name  string
		stack []string
		want  string
	}{
		{"sans keyword", []string{"sans-serif"}, "Sans"},
		{"serif keyword", []string{"serif"}, "Serif"},
		{"mono keyword", []string{"monospace"}, "Monospace"},
		{"system-ui alias", []string{"system-ui"}, "Sans"},
		{"ui-sans-serif alias", []string{"ui-sans-serif"}, "Sans"},
		{"ui-monospace alias", []string{"ui-monospace"}, "Monospace"},
		{"cursive keyword", []string{"cursive"}, "Cursive"},
		{"fantasy keyword", []string{"fantasy"}, "Fantasy"},
		{"case insensitive", []string{"SERIF"}, "Serif"},
		{"quoted and padded", []string{`  "serif"  `}, "Serif"},
		{"empty stack falls back", nil, "Sans"},
		{"blank entries skipped", []string{"", "  "}, "Sans"},
		{"unknown family falls back", []string{"No Such Family XYZ"}, "Sans"},
		{"unknown skips to generic", []string{"No Such Family XYZ", "serif"}, "Serif"},
		{"installed family wins", []string{"JetBrains Mono", "serif"}, "JetBrains Mono"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveFamily(tc.stack); got != tc.want {
				t.Fatalf("ResolveFamily(%q) = %q, want %q", tc.stack, got, tc.want)
			}
		})
	}
}

func TestMeasureTrimsLineText(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	m := s.Measure("  hello  ", defaultStyle(), 1000)
	if len(m.Lines) != 1 {
		t.Fatalf("expected one line, got %d", len(m.Lines))
	}
	if m.Lines[0].Text != "hello" {
		t.Fatalf("line text = %q, want trimmed %q", m.Lines[0].Text, "hello")
	}
}

func TestMeasureWeightAndItalic(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	normal := s.Measure("Hello world", defaultStyle(), 1000)
	bold := defaultStyle()
	bold.FontWeight = 700
	bolded := s.Measure("Hello world", bold, 1000)
	if bolded.Height <= 0 || len(bolded.Lines) != 1 {
		t.Fatalf("bold measure failed: %+v", bolded)
	}
	italic := defaultStyle()
	italic.Italic = true
	slanted := s.Measure("Hello world", italic, 1000)
	if slanted.Height <= 0 || len(slanted.Lines) != 1 {
		t.Fatalf("italic measure failed: %+v", slanted)
	}
	_ = normal
}
