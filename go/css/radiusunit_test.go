package css

import "testing"

// rem/em border-radius support (T3 of odd/tasks/var-custom-properties.md,
// S4). The resolution contract: rem resolves against the root element's
// computed font-size — 16px unless html declares another size — and em
// against the element's own computed font-size, 16px when unknown; both
// reuse Style.FontSize (libcss' computed value) and never recompute it.
// px and % keep their exact behaviour, and an unresolvable unit keeps the
// degradation the radius reader had before rem/em existed. The tests talk
// to the public Compute path only, like every computed-style test in this
// package; the Rust engine is the behavioural oracle.

// Rule 1: a rem radius resolves against the root font-size (16px unless
// html declares another size) and reaches the computed radius — the
// Forgejo button radius .28571429rem is ≈4.57px at the default root.
func TestRadiusRemResolvesAgainstTheRootFontSize(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  float64
	}{
		{"forgejo button radius", ".28571429rem", .28571429 * 16}, // ≈4.57px
		{"one rem is the root size", "1rem", 16},
		{"two rem scale with the root", "2rem", 32},
		{"fractional rem", ".5rem", 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := styleOf(t,
				"<style>.btn { border-radius: "+tc.value+" }</style>"+
					"<div class=\"btn\">x</div>",
				".btn")
			for i := 0; i < 4; i++ {
				if s.BorderRadius[i].Percent {
					t.Fatalf("radius[%d]: percent (want px)", i)
				}
				if !near(s.BorderRadius[i].Value, tc.want) {
					t.Errorf("radius[%d]: %v (want %v)", i, s.BorderRadius[i].Value, tc.want)
				}
			}
		})
	}

	// The shorthand spreads rem like px: 1-4 lengths, the CSS order.
	s := styleOf(t,
		"<style>.btn { border-radius: 1rem 2px }</style>"+
			"<div class=\"btn\">x</div>",
		".btn")
	want := []float64{16, 2, 16, 2}
	for i, w := range want {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, w) {
			t.Errorf("pairwise rem/px radius[%d]: %+v (want %v)", i, s.BorderRadius[i], w)
		}
	}
}

// Rule 2: an em radius resolves against the element's own computed
// font-size.
func TestRadiusEmResolvesAgainstTheElementFontSize(t *testing.T) {
	cases := []struct {
		name  string
		decl  string // the element's own extra declarations
		value string
		want  float64
	}{
		{"own font-size", "font-size: 20px", "1em", 20},
		{"fractional em", "font-size: 20px", ".5em", 10},
		{"two em", "font-size: 20px", "2em", 40},
		{"inherited font-size", "", "1em", 16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extra := ""
			if tc.decl != "" {
				extra = tc.decl + ";"
			}
			s := styleOf(t,
				"<style>.box {"+extra+"border-radius: "+tc.value+"}</style>"+
					"<div class=\"box\">x</div>",
				".box")
			for i := 0; i < 4; i++ {
				if s.BorderRadius[i].Percent {
					t.Fatalf("radius[%d]: percent (want px)", i)
				}
				if !near(s.BorderRadius[i].Value, tc.want) {
					t.Errorf("radius[%d]: %v (want %v)", i, s.BorderRadius[i].Value, tc.want)
				}
			}
		})
	}

	// em inherits down the tree: the child's own font-size is the parent's.
	s := styleOf(t,
		"<style>.outer { font-size: 20px }"+
			".outer .inner { border-radius: 1em }</style>"+
			"<div class=\"outer\"><div class=\"inner\">x</div></div>",
		".inner")
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, 20.0) {
			t.Errorf("inherited em radius[%d]: %+v (want 20px)", i, s.BorderRadius[i])
		}
	}

	// The shorthand spreads em like px.
	s = styleOf(t,
		"<style>.box { font-size: 20px; border-radius: 1em 2em }</style>"+
			"<div class=\"box\">x</div>",
		".box")
	want := []float64{20, 40, 20, 40}
	for i, w := range want {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, w) {
			t.Errorf("pairwise em radius[%d]: %+v (want %v)", i, s.BorderRadius[i], w)
		}
	}
}

// Rule 3: rem ignores the element's own font-size and always scales with
// the root element's declared size.
func TestRadiusRemHonoursTheRootFontsizeDeclaration(t *testing.T) {
	s := styleOf(t,
		"<style>html { font-size: 20px }"+
			".box { border-radius: 1rem } .px { font-size: 40px; border-radius: 1rem }</style>"+
			"<div class=\"box\">a</div><div class=\"px\">b</div>",
		".box")
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, 20.0) {
			t.Errorf("root-declared rem radius[%d]: %+v (want 20px)", i, s.BorderRadius[i])
		}
	}

	// The element's own 40px font-size does not move a rem radius.
	s = styleOf(t,
		"<style>html { font-size: 20px }"+
			".px { font-size: 40px; border-radius: 1rem }</style>"+
			"<div class=\"px\">b</div>",
		".px")
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, 20.0) {
			t.Errorf("own-font rem radius[%d]: %+v (want 20px)", i, s.BorderRadius[i])
		}
	}

	s = styleOf(t,
		"<style>html { font-size: 20px }"+
			".box { border-radius: .5rem }</style>"+
			"<div class=\"box\">a</div>",
		".box")
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, 10.0) {
			t.Errorf("fractional root rem radius[%d]: %+v (want 10px)", i, s.BorderRadius[i])
		}
	}
}

// Rule 4: a radius arriving through var() resolution resolves identically
// to the plain path — cp.Resolve's answer is parsed by the same reader
// with the same font-size sources.
func TestRadiusVarResolvedRemResolvesIdentically(t *testing.T) {
	// The Forgejo radius through a custom property.
	s := styleOf(t,
		"<style>html { --r: .28571429rem }"+
			".btn { border-radius: var(--r) }</style>"+
			"<div class=\"btn\">x</div>",
		".btn")
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent {
			t.Fatalf("var rem radius[%d]: percent (want px)", i)
		}
		if !near(s.BorderRadius[i].Value, .28571429*16) {
			t.Errorf("var rem radius[%d]: %v (want %v)", i, s.BorderRadius[i].Value, .28571429*16)
		}
	}

	// A fallback may carry the rem value.
	s = styleOf(t,
		"<style>.btn { border-radius: var(--missing, .5rem) }</style>"+
			"<div class=\"btn\">x</div>",
		".btn")
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, 8.0) {
			t.Errorf("fallback rem radius[%d]: %+v (want 8px)", i, s.BorderRadius[i])
		}
	}

	// An inline custom property feeds an inline rem radius.
	d, styles := doc(t,
		"<div style=\"--r: .5rem; border-radius: var(--r)\">x</div>", testViewport)
	s = styles[mustSelect(t, d, "div")]
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, 8.0) {
			t.Errorf("inline var rem radius[%d]: %+v (want 8px)", i, s.BorderRadius[i])
		}
	}

	// A var() resolving to a plain px value behaves like the plain path.
	s = styleOf(t,
		"<style>html { --r: 8px } .btn { border-radius: var(--r) }</style>"+
			"<div class=\"btn\">x</div>",
		".btn")
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, 8.0) {
			t.Errorf("var px radius[%d]: %+v (want 8px)", i, s.BorderRadius[i])
		}
	}

	// An unresolvable var() degrades exactly like the unparseable text it
	// was before S4: the declaration contributes nothing and the default
	// 0px radius stands.
	s = styleOf(t,
		"<style>.btn { border-radius: var(--missing) }</style>"+
			"<div class=\"btn\">x</div>",
		".btn")
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent || s.BorderRadius[i].Value != 0 {
			t.Errorf("unresolvable var radius[%d]: %+v (want 0px)", i, s.BorderRadius[i])
		}
	}
}

// Rule 5: px and % radii keep their exact behaviour through the
// unit-aware reader — including the parse_px quirks (pt, unitless) the
// radius reader always had. This is the PRESERVE pin for the parser the
// rem/em branches join; the pre-existing radius tests of compute_test.go
// run unmodified alongside it.
func TestRadiusPxAndPercentBehaviourUnchanged(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		percent bool
		want    [4]float64
	}{
		{"px uniform", "8px", false, [4]float64{8, 8, 8, 8}},
		{"percent uniform", "50%", true, [4]float64{50, 50, 50, 50}},
		{"px pairwise", "8px 4px", false, [4]float64{8, 4, 8, 4}},
		{"percent pairwise", "50% 25%", true, [4]float64{50, 25, 50, 25}},
		{"pt stays px like parse_px", "10pt", false, [4]float64{10, 10, 10, 10}},
		{"unitless stays px like parse_px", "8", false, [4]float64{8, 8, 8, 8}},
		{"vertical half dropped", "8px / 4px", false, [4]float64{8, 8, 8, 8}},
		{"three lengths", "8px 4px 2px", false, [4]float64{8, 4, 2, 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := styleOf(t,
				"<style>.box { border-radius: "+tc.value+" }</style>"+
					"<div class=\"box\">x</div>",
				".box")
			for i := 0; i < 4; i++ {
				if s.BorderRadius[i].Percent != tc.percent {
					t.Fatalf("radius[%d]: percent=%v (want %v)", i, s.BorderRadius[i].Percent, tc.percent)
				}
				if !near(s.BorderRadius[i].Value, tc.want[i]) {
					t.Errorf("radius[%d]: %v (want %v)", i, s.BorderRadius[i].Value, tc.want[i])
				}
			}
		})
	}
}

// The degradation contract: an unknown unit keeps exactly what the radius
// reader did before rem/em — the failing length parses as invalid, a
// failing shorthand leaves all four corners at the default 0px, a failing
// corner keeps the value the winning shorthand left, and unit matching
// stays case-sensitive like parsePxText's px/pt.
func TestRadiusUnknownUnitsDegradeLikeBefore(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  [4]float64
	}{
		{"unknown unit", "8in", [4]float64{0, 0, 0, 0}},
		{"negative px", "-5px", [4]float64{0, 0, 0, 0}},
		{"negative rem", "-1rem", [4]float64{0, 0, 0, 0}},
		{"uppercase REM is not a unit", "8REM", [4]float64{0, 0, 0, 0}},
		{"bare word", "bold", [4]float64{0, 0, 0, 0}},
		{"one bad length invalidates the shorthand", "8px 4px in", [4]float64{0, 0, 0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := styleOf(t,
				"<style>.box { border-radius: "+tc.value+" }</style>"+
					"<div class=\"box\">x</div>",
				".box")
			for i := 0; i < 4; i++ {
				if s.BorderRadius[i].Percent || s.BorderRadius[i].Value != tc.want[i] {
					t.Errorf("radius[%d]: %+v (want %v px)", i, s.BorderRadius[i], tc.want[i])
				}
			}
		})
	}

	// A bad corner keeps the shorthand's value, exactly like before.
	s := styleOf(t,
		"<style>.box { border-radius: 8px; border-top-left-radius: 8in }</style>"+
			"<div class=\"box\">x</div>",
		".box")
	want := [4]float64{8, 8, 8, 8}
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, want[i]) {
			t.Errorf("bad corner radius[%d]: %+v (want %v px)", i, s.BorderRadius[i], want[i])
		}
	}
}
