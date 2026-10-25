package css

import "testing"

// Applying captured var() declarations through the manual cascade (T2 of
// odd/tasks/var-custom-properties.md, S3). The border family — `border`,
// the `border-width`/`border-style`/`border-color` shorthands and the
// per-side longhands — resolves per element and paints through the same
// libcss expansion the non-var path uses, with the manual cascade's
// priority (selector specificity, document order, !important, inline last)
// and the captured @media condition honoured against the run's viewport.
// Assertions live on the computed Style, exactly like the border tests of
// compute_test.go; the tests talk to the public Compute path only.

func TestBorderVarShorthandResolvesAndPaints(t *testing.T) {
	s := styleOf(t,
		"<style>html { --c: red } .hero { border: 1px solid var(--c) }</style>"+
			"<div class=\"hero\">h</div>",
		".hero")
	for i := 0; i < 4; i++ {
		if !near(s.BorderWidth[i], 1.0) {
			t.Errorf("border width[%d]: %v (want 1)", i, s.BorderWidth[i])
		}
		if s.BorderStyle[i] != BorderSolid {
			t.Errorf("border style[%d]: %v (want solid)", i, s.BorderStyle[i])
		}
		if !colorNear(s.BorderColor[i], 1.0, 0.0, 0.0) {
			t.Errorf("border colour[%d]: %+v (want red)", i, s.BorderColor[i])
		}
	}
}

func TestBorderVarColorShorthandAppliesPerSide(t *testing.T) {
	// border-color: <a> <b> fills top/bottom with a and right/left with b
	// — the per-side mapping is libcss' own border expansion, so a resolved
	// multi-value var() declaration must land side by side.
	s := styleOf(t,
		"<style>html { --a: red; --b: blue }"+
			".box { border-color: var(--a) var(--b) }</style>"+
			"<div class=\"box\">x</div>",
		".box")
	want := [4][3]float64{{1, 0, 0}, {0, 0, 1}, {1, 0, 0}, {0, 0, 1}}
	for i, w := range want {
		if !colorNear(s.BorderColor[i], w[0], w[1], w[2]) {
			t.Errorf("border colour[%d]: %+v (want %v)", i, s.BorderColor[i], w)
		}
	}
}

func TestBorderVarSpecificityBeatsDocumentOrder(t *testing.T) {
	// The later, lower-specificity type rule loses to the earlier class
	// rule: the bid carries the selector specificity, not the text order.
	s := styleOf(t,
		"<style>html { --x: red; --y: blue }"+
			".box { border: 2px solid var(--x) }"+
			"div { border: 4px solid var(--y) }</style>"+
			"<div class=\"box\">x</div>",
		".box")
	if !near(s.BorderWidth[0], 2.0) {
		t.Errorf("border width: %v (want 2)", s.BorderWidth[0])
	}
	if !colorNear(s.BorderColor[0], 1.0, 0.0, 0.0) {
		t.Errorf("border colour: %+v (want the class rule's red)", s.BorderColor[0])
	}
}

func TestBorderVarImportantAndInlineBeatSheets(t *testing.T) {
	html := "<style>html { --r: red; --g: green }" +
		".imp { border: 3px solid var(--r) !important }" +
		".norm { border: 5px solid var(--g) }" +
		".sheet { border: 1px solid var(--r) }</style>" +
		"<div class=\"imp norm\" id=\"a\">a</div>" +
		"<div class=\"sheet\" id=\"b\" style=\"--sc: green; border-color: var(--sc)\">b</div>"
	d, styles := doc(t, html, testViewport)

	// The earlier !important declaration beats the later normal one.
	a := styles[mustSelect(t, d, "#a")]
	if !near(a.BorderWidth[0], 3.0) {
		t.Errorf("!important border width: %v (want 3)", a.BorderWidth[0])
	}
	if !colorNear(a.BorderColor[0], 1.0, 0.0, 0.0) {
		t.Errorf("!important border colour: %+v (want red)", a.BorderColor[0])
	}

	// The style="" var() declaration (whose custom property is declared in
	// the same attribute) beats the non-important author sheet, and fills
	// only its colour slots: the sheet's width and style survive.
	b := styles[mustSelect(t, d, "#b")]
	if !near(b.BorderWidth[0], 1.0) {
		t.Errorf("inline border width: %v (want the sheet's 1)", b.BorderWidth[0])
	}
	if b.BorderStyle[0] != BorderSolid {
		t.Errorf("inline border style: %v (want the sheet's solid)", b.BorderStyle[0])
	}
	// The CSS keyword green is #008000, not pure (0,1,0).
	for i := 0; i < 4; i++ {
		if !colorNear(b.BorderColor[i], 0.0, 128.0/255.0, 0.0) {
			t.Errorf("inline border colour[%d]: %+v (want green)", i, b.BorderColor[i])
		}
	}
}

func TestBorderVarMediaConditionIsHonoured(t *testing.T) {
	html := "<style>html { --w: 3px; --c: red; --blue: blue }" +
		"@media (min-width: 500px) { .m { border: var(--w) solid var(--c) } }" +
		"@media (min-width: 5000px) { .m { border: 5px solid var(--blue) } }</style>" +
		"<div class=\"m\">m</div>"
	cases := []struct {
		name     string
		viewport float64
		width    float64
		style    BorderKind
		colour   [3]float64
	}{
		{"matching condition applies at 1000px", 1000.0, 3.0, BorderSolid, [3]float64{1, 0, 0}},
		{"matching condition applies at 600px", 600.0, 3.0, BorderSolid, [3]float64{1, 0, 0}},
		{"no condition matches at 400px", 400.0, 0.0, BorderNone, [3]float64{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, styles := doc(t, html, tc.viewport)
			s := styles[mustSelect(t, d, ".m")]
			if !near(s.BorderWidth[0], tc.width) {
				t.Errorf("border width: %v (want %v)", s.BorderWidth[0], tc.width)
			}
			if s.BorderStyle[0] != tc.style {
				t.Errorf("border style: %v (want %v)", s.BorderStyle[0], tc.style)
			}
			if !colorNear(s.BorderColor[0], tc.colour[0], tc.colour[1], tc.colour[2]) {
				t.Errorf("border colour: %+v (want %v)", s.BorderColor[0], tc.colour)
			}
		})
	}
}

func TestBorderVarUnresolvableIsDropped(t *testing.T) {
	// An unresolvable var() (undefined, no fallback) is invalid at
	// computed-value time: the manual cascade drops the declaration the
	// same way it drops unparsable input, before it ever bids — so a
	// valid sibling declaration still wins the slot.
	html := "<style>html { --ok: red }" +
		".plain { border: 2px solid blue }" +
		".bad { border-color: var(--missing) }" +
		".good { border-color: var(--ok) }</style>" +
		"<div class=\"plain bad\" id=\"a\">a</div>" +
		"<div class=\"bad good\" id=\"b\">b</div>"
	d, styles := doc(t, html, testViewport)

	a := styles[mustSelect(t, d, "#a")]
	if !near(a.BorderWidth[0], 2.0) {
		t.Errorf("plain border width: %v (want 2)", a.BorderWidth[0])
	}
	for i := 0; i < 4; i++ {
		if !colorNear(a.BorderColor[i], 0.0, 0.0, 1.0) {
			t.Errorf("dropped declaration changed colour[%d]: %+v (want the plain blue)", i, a.BorderColor[i])
		}
	}

	b := styles[mustSelect(t, d, "#b")]
	for i := 0; i < 4; i++ {
		if !colorNear(b.BorderColor[i], 1.0, 0.0, 0.0) {
			t.Errorf("valid sibling colour[%d]: %+v (want red)", i, b.BorderColor[i])
		}
	}
}

func TestBorderVarCoexistsWithPlainRadius(t *testing.T) {
	// One block carries a plain border-radius and a var() border: the
	// radius cascade and the var() border pass must both paint.
	s := styleOf(t,
		"<style>html { --c: red }"+
			".both { border-radius: 8px; border: 1px solid var(--c) }</style>"+
			"<div class=\"both\">x</div>",
		".both")
	for i := 0; i < 4; i++ {
		if s.BorderRadius[i].Percent || !near(s.BorderRadius[i].Value, 8.0) {
			t.Errorf("radius[%d]: %+v (want plain 8px)", i, s.BorderRadius[i])
		}
		if !near(s.BorderWidth[i], 1.0) || s.BorderStyle[i] != BorderSolid ||
			!colorNear(s.BorderColor[i], 1.0, 0.0, 0.0) {
			t.Errorf("var border[%d]: %v/%v/%+v (want 1px solid red)",
				i, s.BorderWidth[i], s.BorderStyle[i], s.BorderColor[i])
		}
	}
}
