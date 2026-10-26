package css

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Computed-style tests for the libcss-backed engine path, mirroring
// src/css_libcss.rs tests: tests/css_libcss.rs, tests/css_libcss_verify.rs,
// the computed-style parts of tests/border.rs and the style expectations of
// tests/flex.rs / tests/grid.rs (T3, S3). The tests talk to the public
// engine API only: parse a document, collect its <style> elements and
// compute styles at a given viewport width. The expected values come from
// the CSS spec and from the engine's documented UA sheet, never from an
// implementation detail. The Rust engine is the behavioural oracle: every
// expectation below is one of its passing tests.

const testViewport = 800.0

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "data", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return string(data)
}

func near(a, b float64) bool { return (a-b) < 0.01 && (b-a) < 0.01 }

func colorNear(actual Color, r, g, b float64) bool {
	return near(actual.R, r) && near(actual.G, g) && near(actual.B, b) && near(actual.A, 1.0)
}

func rgbOf(c Color) int {
	return int(c.R*255.0+0.5)<<16 | int(c.G*255.0+0.5)<<8 | int(c.B*255.0+0.5)
}

// doc computes styles for html and returns the fake document.
func doc(t *testing.T, html string, viewport float64) (*fakeDoc, []Style) {
	t.Helper()
	d := parseFakeHTML(html)
	sheets := Stylesheets(d)
	styles, warnings, err := Compute(d, sheets, viewport)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %q", warnings)
	}
	if len(styles) != d.Len() {
		t.Fatalf("styles: got %d, want %d", len(styles), d.Len())
	}
	return d, styles
}

func styleOf(t *testing.T, html string, selector string) Style {
	t.Helper()
	d, styles := doc(t, html, testViewport)
	id := mustSelect(t, d, selector)
	return styles[id]
}

// byClass mirrors the Rust fixture helper.
func byClass(t *testing.T, d *fakeDoc, class string) int {
	t.Helper()
	return int(mustSelect(t, d, "."+class))
}

func TestSpecificityWinnerBeatsClassAndTag(t *testing.T) {
	d, styles := doc(t, fixture(t, "css-specificity.html"), testViewport)

	destino := mustSelect(t, d, "#destino")
	s := styles[destino]
	if !colorNear(s.Color, 0.8, 0.0, 0.0) {
		t.Errorf("#destino colour: %+v", s.Color)
	}
	if bg := s.Background; bg == nil || !colorNear(*bg, 1.0, 0.8, 0.8) {
		t.Errorf("#destino background: %+v", s.Background)
	}
	if !(near(s.Margin[0], 12.0) && near(s.Margin[3], 12.0)) {
		t.Errorf("#destino top/left margin: %+v", s.Margin)
	}
	if !(near(s.Padding[0], 12.0) && near(s.Padding[3], 12.0)) {
		t.Errorf("#destino top/left padding: %+v", s.Padding)
	}
	if !near(s.FontSize, 22.0) {
		t.Errorf("#destino font-size: %v", s.FontSize)
	}

	// The class rule beats the bare tag rule.
	cajas := d.QuerySelectorAll(".caja")
	s = styles[cajas[1]]
	if !colorNear(s.Color, 0.0, 0.0, 1.0) {
		t.Errorf(".caja colour: %+v", s.Color)
	}
	grey := 238.0 / 255.0
	if bg := s.Background; bg == nil || !colorNear(*bg, grey, grey, grey) {
		t.Errorf(".caja background: %+v", s.Background)
	}
	if !near(s.FontSize, 20.0) {
		t.Errorf(".caja font-size: %v", s.FontSize)
	}
}

func TestAuthorRulesBeatTheUAStylesheet(t *testing.T) {
	d, styles := doc(t,
		"<style>p { display: inline; margin: 3px; }</style><p id=x>x</p>", testViewport)
	s := styles[mustSelect(t, d, "#x")]
	if s.Display != DisplayInline {
		t.Errorf("display: %v", s.Display)
	}
	if !near(s.Margin[0], 3.0) {
		t.Errorf("margin: %+v", s.Margin)
	}
}

func TestUAStyleSheetSuppliesDisplayMarginsAndHeadingSizes(t *testing.T) {
	d, styles := doc(t,
		"<head><style></style><script></script></head><body><p>x</p><span>y</span><h1>z</h1></body>",
		testViewport)

	for _, selector := range []string{"head", "style", "script"} {
		id := mustSelect(t, d, selector)
		if styles[id].Display != DisplayNone {
			t.Errorf("%s display: %v", selector, styles[id].Display)
		}
	}

	p := styles[mustSelect(t, d, "p")]
	if p.Display != DisplayBlock {
		t.Errorf("p display: %v", p.Display)
	}
	if !(near(p.Margin[0], 16.0) && near(p.Margin[2], 16.0)) {
		t.Errorf("p margin: %+v", p.Margin)
	}
	if styles[mustSelect(t, d, "span")].Display != DisplayInline {
		t.Errorf("span display")
	}

	body := styles[mustSelect(t, d, "body")]
	if body.Display != DisplayBlock {
		t.Errorf("body display: %v", body.Display)
	}
	for i, m := range body.Margin {
		if !near(m, 8.0) {
			t.Errorf("body margin[%d]: %v", i, m)
		}
	}

	h1 := styles[mustSelect(t, d, "h1")]
	if !near(h1.FontSize, 32.0) {
		t.Errorf("h1 font-size: %v", h1.FontSize)
	}
	if h1.FontWeight != 700 {
		t.Errorf("h1 font-weight: %d", h1.FontWeight)
	}
	if !h1.Bold {
		t.Errorf("h1 bold flag")
	}
}

func TestUASheetDefaultsLoseToAuthorRules(t *testing.T) {
	ua := styleOf(t, "<h1>x</h1>", "h1")
	if ua.Display != DisplayBlock {
		t.Errorf("h1 display: %v", ua.Display)
	}
	if !near(ua.FontSize, 32.0) {
		t.Errorf("h1 size: %v", ua.FontSize)
	}
	if !near(ua.Margin[0], 21.0) {
		t.Errorf("h1 margin: %+v", ua.Margin)
	}
	if ua.FontWeight != 700 {
		t.Errorf("h1 weight: %d", ua.FontWeight)
	}
	if styleOf(t, "<style>p{}</style><p>x</p>", "style").Display != DisplayNone {
		t.Errorf("style display")
	}
	if styleOf(t, "<p>x</p>", "p").Display != DisplayBlock {
		t.Errorf("p display")
	}

	author := styleOf(t,
		"<style>h1 { display: inline; margin: 0; font-size: 10px }</style><h1>x</h1>", "h1")
	if author.Display != DisplayInline {
		t.Errorf("author h1 display: %v", author.Display)
	}
	if !near(author.FontSize, 10.0) {
		t.Errorf("author h1 size: %v", author.FontSize)
	}
	if !(near(author.Margin[0], 0.0) && near(author.Margin[2], 0.0)) {
		t.Errorf("author h1 margins: %+v", author.Margin)
	}
}

func TestComputedStyleInheritsAndResolvesRelativeFontSizes(t *testing.T) {
	d, styles := doc(t, fixture(t, "css-inheritance.html"), testViewport)

	body := styles[mustSelect(t, d, "body")]
	if !near(body.FontSize, 20.0) {
		t.Errorf("body font-size: %v", body.FontSize)
	}

	nivel3 := byClass(t, d, "nivel3")
	s := styles[nivel3]
	if !colorNear(s.Color, 0.0, 68.0/255.0, 170.0/255.0) {
		t.Errorf("inherited colour: %+v", s.Color)
	}
	if !near(s.FontSize, 15.0) {
		t.Errorf("font-size: %v", s.FontSize)
	}
	if s.FontFamily != "Serif" {
		t.Errorf("font-family: %q", s.FontFamily)
	}
	if !near(s.LineHeight, 2.0) {
		t.Errorf("line-height: %v", s.LineHeight)
	}
	if s.FontWeight != 700 {
		t.Errorf("font-weight: %d", s.FontWeight)
	}
	if !s.Bold {
		t.Errorf("bold flag")
	}
}

func TestMediaQueriesResolveAgainstTheViewportWidth(t *testing.T) {
	html := fixture(t, "css-media.html")

	d, wide := doc(t, html, testViewport)
	base := mustSelect(t, d, "#base")
	if !colorNear(wide[base].Color, 0.0, 0.2, 0.8) {
		t.Errorf("screen rule colour: %+v", wide[base].Color)
	}
	if bg := wide[base].Background; bg == nil || !colorNear(*bg, 1.0, 1.0, 0.8) {
		t.Errorf("wide background: %+v", wide[base].Background)
	}
	if !near(wide[base].FontSize, 18.0) {
		t.Errorf("wide font-size: %v", wide[base].FontSize)
	}

	d, narrow := doc(t, html, 400.0)
	base = mustSelect(t, d, "#base")
	if !colorNear(narrow[base].Color, 0.0, 0.2, 0.8) {
		t.Errorf("narrow colour: %+v", narrow[base].Color)
	}
	if bg := narrow[base].Background; bg == nil || !colorNear(*bg, 0.8, 1.0, 0.8) {
		t.Errorf("narrow background: %+v", narrow[base].Background)
	}
	if !near(narrow[base].FontSize, 12.0) {
		t.Errorf("narrow font-size: %v", narrow[base].FontSize)
	}
}

func TestMediaWidthFeaturesTrackTheViewport(t *testing.T) {
	html := "<style>" +
		"@media (min-width: 1000px) { #x { color: #ff0000 } }" +
		"@media (max-width: 999px) { #x { color: #0000ff } }" +
		"</style><p id=x>x</p>"
	styleAt := func(viewport float64) int {
		d := parseFakeHTML(html)
		styles, _, err := Compute(d, Stylesheets(d), viewport)
		if err != nil {
			t.Fatalf("compute: %v", err)
		}
		return rgbOf(styles[mustSelect(t, d, "#x")].Color)
	}
	if got := styleAt(1000.0); got != 0xff0000 {
		t.Errorf("1000px: %06x", got)
	}
	if got := styleAt(999.0); got != 0x0000ff {
		t.Errorf("999px: %06x", got)
	}
	if got := styleAt(2000.0); got != 0xff0000 {
		t.Errorf("2000px: %06x", got)
	}

	printSheet := "<style>@media print { #x { color: #ff0000 } }" +
		"@media screen { #x { color: #00ff00 } }</style><p id=x>x</p>"
	if got := rgbOf(styleOf(t, printSheet, "#x").Color); got != 0x00ff00 {
		t.Errorf("screen beats print: %06x", got)
	}
}

func TestTypographyFieldsComeFromTheCascade(t *testing.T) {
	d, styles := doc(t, fixture(t, "css-typography.html"), testViewport)

	h1 := styles[mustSelect(t, d, "h1")]
	if h1.FontFamily != "T2 Mono" {
		t.Errorf("h1 font-family: %q", h1.FontFamily)
	}
	if !near(h1.FontSize, 28.0) {
		t.Errorf("h1 font-size: %v", h1.FontSize)
	}
	if h1.FontWeight != 800 {
		t.Errorf("h1 font-weight: %d", h1.FontWeight)
	}
	if h1.Italic {
		t.Errorf("h1 italic")
	}
	if !near(h1.LineHeight, 1.5) {
		t.Errorf("h1 line-height: %v", h1.LineHeight)
	}
	if h1.TextAlign != TextAlignCenter {
		t.Errorf("h1 text-align: %v", h1.TextAlign)
	}
	if !near(h1.LetterSpacing, 2.0) {
		t.Errorf("h1 letter-spacing: %v", h1.LetterSpacing)
	}

	p := styles[byClass(t, d, "largo")]
	if p.FontFamily != "Serif" {
		t.Errorf("p font-family: %q", p.FontFamily)
	}
	if !near(p.FontSize, 16.0) {
		t.Errorf("p font-size: %v", p.FontSize)
	}
	if p.FontWeight != 300 {
		t.Errorf("p font-weight: %d", p.FontWeight)
	}
	if !p.Italic {
		t.Errorf("p italic")
	}
	if !near(p.LineHeight, 1.8) {
		t.Errorf("p line-height: %v", p.LineHeight)
	}
	if p.TextAlign != TextAlignJustify {
		t.Errorf("p text-align: %v", p.TextAlign)
	}
	if !near(p.LetterSpacing, 0.5) {
		t.Errorf("p letter-spacing: %v", p.LetterSpacing)
	}
}

func TestInlineStyleAttributeWinsOverAuthorRules(t *testing.T) {
	d, styles := doc(t,
		"<style>p { color: blue; }</style><p id=x style=\"color: #00ff00; font-weight: 700\">x</p>",
		testViewport)
	s := styles[mustSelect(t, d, "#x")]
	if !colorNear(s.Color, 0.0, 1.0, 0.0) {
		t.Errorf("inline colour: %+v", s.Color)
	}
	if s.FontWeight != 700 {
		t.Errorf("inline weight: %d", s.FontWeight)
	}
}

func TestTypographyFieldsCascadeInheritAndMirrorFlags(t *testing.T) {
	html := "<style>div { font-family: \"No Such Font\", serif; font-weight: 300;" +
		"font-style: italic; line-height: 24px; text-align: right; letter-spacing: 1.5px }" +
		"</style><div><span id=x>t</span></div>"
	div := styleOf(t, html, "div")
	if div.FontFamily != "No Such Font" {
		t.Errorf("font-family: %q", div.FontFamily)
	}
	if div.FontWeight != 300 {
		t.Errorf("weight: %d", div.FontWeight)
	}
	if !div.Italic {
		t.Errorf("italic")
	}
	if !near(div.LineHeight, 1.5) {
		t.Errorf("24px/16px: %v", div.LineHeight)
	}
	if div.TextAlign != TextAlignRight {
		t.Errorf("text-align: %v", div.TextAlign)
	}
	if !near(div.LetterSpacing, 1.5) {
		t.Errorf("letter-spacing: %v", div.LetterSpacing)
	}

	span := styleOf(t, html, "#x")
	if span.FontFamily != div.FontFamily {
		t.Errorf("span font-family: %q vs %q", span.FontFamily, div.FontFamily)
	}
	if span.FontWeight != 300 {
		t.Errorf("span weight: %d", span.FontWeight)
	}
	if !span.Italic {
		t.Errorf("span italic")
	}
	if span.TextAlign != TextAlignRight {
		t.Errorf("span text-align: %v", span.TextAlign)
	}
	if !near(span.LetterSpacing, 1.5) {
		t.Errorf("span letter-spacing: %v", span.LetterSpacing)
	}
	if !near(span.LineHeight, 1.5) {
		t.Errorf("span line-height: %v", span.LineHeight)
	}

	plain := styleOf(t, "<p id=x>x</p>", "#x")
	if plain.FontFamily != "Sans" {
		t.Errorf("plain font-family: %q", plain.FontFamily)
	}
	if plain.FontWeight != 400 {
		t.Errorf("plain weight: %d", plain.FontWeight)
	}
	if plain.Italic || plain.Bold || plain.InlineAlign {
		t.Errorf("plain flags")
	}
	if plain.TextAlign != TextAlignLeft {
		t.Errorf("plain text-align: %v", plain.TextAlign)
	}
	if !(near(plain.LineHeight, 1.2) && near(plain.LetterSpacing, 0.0)) {
		t.Errorf("plain line-height/letter-spacing: %v/%v", plain.LineHeight, plain.LetterSpacing)
	}

	bold := styleOf(t, "<style>p { font-weight: 700 }</style><p id=x>x</p>", "#x")
	if bold.FontWeight != 700 || !bold.Bold {
		t.Errorf("bold flag mirrors weight >= 600: %d/%v", bold.FontWeight, bold.Bold)
	}
	centered := styleOf(t, "<style>p { text-align: center }</style><p id=x>x</p>", "#x")
	if centered.TextAlign != TextAlignCenter || !centered.InlineAlign {
		t.Errorf("inline_align mirrors center: %v/%v", centered.TextAlign, centered.InlineAlign)
	}
}

func TestEveryNodeGetsAStyleAndDefaultsAreStable(t *testing.T) {
	d, styles := doc(t, "<p>hola</p>", testViewport)
	if len(styles) != d.Len() {
		t.Fatalf("styles: %d vs nodes: %d", len(styles), d.Len())
	}
	if styles[d.Root()].Display != DisplayBlock {
		t.Errorf("root display: %v", styles[d.Root()].Display)
	}
	var text = -1
	for id := range d.nodes {
		if d.nodes[id].isText {
			text = id
			break
		}
	}
	if text < 0 {
		t.Fatal("no text node")
	}
	if styles[text].Display != DisplayInline {
		t.Errorf("text display: %v", styles[text].Display)
	}
	if !near(styles[text].FontSize, 16.0) {
		t.Errorf("text font-size: %v", styles[text].FontSize)
	}
}

func TestLineHeightNormalRemainsFontDependent(t *testing.T) {
	normal := styleOf(t, `<p id="x">x</p>`, "#x")
	if !normal.LineHeightNormal {
		t.Fatalf("default line-height normal was flattened to factor %v", normal.LineHeight)
	}
	keyword := styleOf(t, `<style>p { line-height: normal }</style><p id="x">x</p>`, "#x")
	if !keyword.LineHeightNormal {
		t.Fatalf("line-height: normal was flattened to factor %v", keyword.LineHeight)
	}
	explicit := styleOf(t, `<style>p { line-height: 1.2 }</style><p id="x">x</p>`, "#x")
	if explicit.LineHeightNormal || !near(explicit.LineHeight, 1.2) {
		t.Fatalf("explicit unitless factor lost: normal=%v factor=%v", explicit.LineHeightNormal, explicit.LineHeight)
	}
	inheritedNormal := styleOf(t, `<style>p { line-height: normal } span { line-height: inherit }</style><p><span id="x">x</span></p>`, "#x")
	if !inheritedNormal.LineHeightNormal {
		t.Fatalf("inherited normal line-height became factor %v", inheritedNormal.LineHeight)
	}
	inheritedFactor := styleOf(t, `<style>p { line-height: 1.4 } span { line-height: inherit }</style><p><span id="x">x</span></p>`, "#x")
	if inheritedFactor.LineHeightNormal || !near(inheritedFactor.LineHeight, 1.4) {
		t.Fatalf("inherited unitless factor lost: normal=%v factor=%v", inheritedFactor.LineHeightNormal, inheritedFactor.LineHeight)
	}
}

func TestMalformedStylesheetsAreSurvivable(t *testing.T) {
	for _, html := range []string{
		"<p id=x>x</p>",
		"<style></style><p id=x>x</p>",
		"<style>p { color: }</style><p id=x>x</p>",
		"<style>@import url(nope.css); p { color: #ff0000 }</style><p id=x>x</p>",
		"<style>p { color: #ff0000 </style><p id=x>x</p>",
		"<style>* { margin: 1px }</style><p id=x style=\"color: blue\">x</p>",
		"<style>div p + span ~ b { color: red }</style><div><p></p><span></span><b id=x></b></div>",
	} {
		_, styles := doc(t, html, testViewport)
		if len(styles) == 0 {
			t.Fatalf("no styles for %q", html)
		}
	}
	if got := rgbOf(styleOf(t,
		"<style>p[data-x=\"1\"] { color: #ff0000 }</style><p id=x data-x=1>x</p>", "#x").Color); got != 0xff0000 {
		t.Errorf("attribute selector: %06x", got)
	}
	if got := rgbOf(styleOf(t,
		"<style>a:link { color: #00ff00 }</style><a id=x href=#>x</a>", "#x").Color); got != 0x00ff00 {
		t.Errorf("a:link: %06x", got)
	}
}

// S4/T1: computed styles for every node; @import survives with an empty
// sheet; child elements inherit through the composed cascade.

func TestFlexGapCarriersReachTheCascade(t *testing.T) {
	html := "<style>" +
		"#gap { display: flex; gap: 20px; width: 600px; }" +
		"#gap div { width: 100px; height: 40px; inline-size: 100px; }" +
		"</style><div id=\"gap\"><div id=\"a\">A</div><div id=\"b\">B</div></div>"
	d, styles := doc(t, html, testViewport)
	gap := styles[mustSelect(t, d, "#gap")]
	if gap.Display != DisplayBlock || gap.Flex == nil {
		t.Fatalf("flex: display %v, flex %+v", gap.Display, gap.Flex)
	}
	if !(near(gap.Flex.RowGap, 20.0) && near(gap.Flex.ColumnGap, 20.0)) {
		t.Errorf("flex gaps: %+v", gap.Flex)
	}
	if !(near(gap.RowGap, 20.0) && near(gap.ColumnGap, 20.0)) {
		t.Errorf("style gaps: %v/%v", gap.RowGap, gap.ColumnGap)
	}

	// Inline declaration lists use the same carriers: gap splits across
	// the two axes.
	inline := "<div id=\"i\" style=\"gap: 10px 20px\">x</div>"
	d2, styles2 := doc(t, inline, testViewport)
	s := styles2[mustSelect(t, d2, "#i")]
	if !(near(s.RowGap, 10.0) && near(s.ColumnGap, 20.0)) {
		t.Errorf("inline gaps: %v/%v", s.RowGap, s.ColumnGap)
	}
}

func TestFlexContainerPropertiesComeFromTheCascade(t *testing.T) {
	html := "<style>.row { display: flex; flex-direction: column; flex-wrap: wrap;" +
		"justify-content: center; align-items: center }" +
		".item { flex-grow: 2; flex-shrink: 3; flex-basis: 120px }" +
		"</style><div class=\"row\"><div class=\"item\" id=\"i\">x</div></div>"
	d, styles := doc(t, html, testViewport)
	row := styles[mustSelect(t, d, ".row")]
	if row.Flex == nil {
		t.Fatal("row is not a flex container")
	}
	flex := *row.Flex
	if flex.Direction != FlexDirectionColumn || flex.Wrap != FlexWrapWrap ||
		flex.Justify != JustifyCenter || flex.Align != AlignItemsCenter {
		t.Errorf("flex container props: %+v", flex)
	}
	item := styles[mustSelect(t, d, "#i")]
	if !(near(item.FlexItem.Grow, 2.0) && near(item.FlexItem.Shrink, 3.0)) {
		t.Errorf("flex item grow/shrink: %+v", item.FlexItem)
	}
	if item.FlexItem.Basis.Kind != BasisPx || !near(item.FlexItem.Basis.Value, 120.0) {
		t.Errorf("flex basis: %+v", item.FlexItem.Basis)
	}
}

func TestGridColumnTracksAndSpans(t *testing.T) {
	html := "<style>" +
		"#g { display: grid; grid-template-columns: 100px 100px; gap: 20px; width: 600px; }" +
		"#g div { height: 40px; }" +
		"#a { grid-column: span 2; }" +
		"</style><div id=\"g\"><div id=\"a\">A</div><div id=\"b\">B</div></div>"
	d, styles := doc(t, html, testViewport)
	g := styles[mustSelect(t, d, "#g")]
	if g.Display != DisplayBlock || g.Grid == nil {
		t.Fatalf("grid: display %v, grid %+v", g.Display, g.Grid)
	}
	if len(g.Grid.Tracks) != 2 || !near(g.Grid.Tracks[0].Value, 100.0) ||
		!near(g.Grid.Tracks[1].Value, 100.0) {
		t.Errorf("tracks: %+v", g.Grid.Tracks)
	}
	if !(near(g.RowGap, 20.0) && near(g.ColumnGap, 20.0)) {
		t.Errorf("grid gaps: %v/%v", g.RowGap, g.ColumnGap)
	}
	if span := styles[mustSelect(t, d, "#a")].GridItem.ColSpan; span != 2 {
		t.Errorf("col span: %d", span)
	}
	if span := styles[mustSelect(t, d, "#b")].GridItem.ColSpan; span != 1 {
		t.Errorf("default col span: %d", span)
	}
}

func TestGridWithoutTracksDefaultsToTheImplicitColumn(t *testing.T) {
	html := "<style>#g2 { display: grid } #g2 div { grid-row: span 4 }</style>" +
		"<div id=\"g2\"><div id=\"a\">A</div></div>"
	d, styles := doc(t, html, testViewport)
	g := styles[mustSelect(t, d, "#g2")]
	if g.Grid == nil || len(g.Grid.Tracks) != 1 || g.Grid.Tracks[0].Kind != TrackAuto {
		t.Errorf("implicit column: %+v", g.Grid)
	}
	if span := styles[mustSelect(t, d, "#a")].GridItem.RowSpan; span != 4 {
		t.Errorf("row span: %d", span)
	}
}

func TestComputedBordersFollowTheCascade(t *testing.T) {
	// The shorthand sets every side.
	d, styles := doc(t,
		"<style>.box { width: 200px; border: 8px solid red; padding: 10px; margin: 10px }</style>"+
			"<div class=\"box\">x</div>", 1000.0)
	s := styles[mustSelect(t, d, ".box")]
	for i := 0; i < 4; i++ {
		if !near(s.BorderWidth[i], 8.0) {
			t.Errorf("border width[%d]: %v", i, s.BorderWidth[i])
		}
		if s.BorderStyle[i] != BorderSolid {
			t.Errorf("border style[%d]: %v", i, s.BorderStyle[i])
		}
		if !colorNear(s.BorderColor[i], 1.0, 0.0, 0.0) {
			t.Errorf("border color[%d]: %+v", i, s.BorderColor[i])
		}
	}

	// Longhands differ per side: the bottom side keeps no style, so its
	// used width falls back to zero.
	d, styles = doc(t,
		"<style>.box { width: 200px; height: 60px; padding: 5px; margin: 10px;"+
			"border-left: 10px solid red; border-top: 4px solid blue; border-right: 6px solid black }</style>"+
			"<div class=\"box\">Hi</div>", 1000.0)
	s = styles[mustSelect(t, d, ".box")]
	wantWidth := []float64{4.0, 6.0, 0.0, 10.0}
	wantStyle := []BorderKind{BorderSolid, BorderSolid, BorderNone, BorderSolid}
	for i := 0; i < 4; i++ {
		if !near(s.BorderWidth[i], wantWidth[i]) {
			t.Errorf("width[%d]: %v (want %v)", i, s.BorderWidth[i], wantWidth[i])
		}
		if s.BorderStyle[i] != wantStyle[i] {
			t.Errorf("style[%d]: %v (want %v)", i, s.BorderStyle[i], wantStyle[i])
		}
	}
	if s.BorderColor[3].R < 0.9 {
		t.Errorf("left border red: %+v", s.BorderColor[3])
	}
	if s.BorderColor[0].B < 0.9 {
		t.Errorf("top border blue: %+v", s.BorderColor[0])
	}

	// currentColor follows the text colour of the same computed style.
	d, styles = doc(t,
		"<style>.box { color: blue; border: 2px solid; }</style><div class=\"box\">x</div>", 1000.0)
	s = styles[mustSelect(t, d, ".box")]
	for i := 0; i < 4; i++ {
		if !near(s.BorderWidth[i], 2.0) || s.BorderColor[i].B < 0.9 || s.BorderColor[i].R > 0.1 {
			t.Errorf("currentcolor border[%d]: %v/%+v", i, s.BorderWidth[i], s.BorderColor[i])
		}
	}

	// The keyword widths follow libcss 0.9.2's computed values (thin 1px,
	// medium 2px, thick 4px): the libcss path passes WIDTH lengths through
	// (oracle src/css_libcss.rs border_width_px, lines 2728-2748), so the
	// legacy cascade's 1/3/5 table (src/css.rs:955-957) does not apply here.
	// No oracle libcss test covers the keywords; none takes no space.
	d, styles = doc(t,
		"<style>.thin { border: thin solid; } .medium { border-width: medium; border-style: solid }"+
			".thick { border-width: thick; border-style: solid } .no { border: 8px none red }</style>"+
			"<div class=\"thin\">a</div><div class=\"medium\">b</div><div class=\"thick\">c</div>"+
			"<div class=\"no\">d</div>", 1000.0)
	if w := styles[mustSelect(t, d, ".thin")].BorderWidth[0]; !near(w, 1.0) {
		t.Errorf("thin: %v", w)
	}
	if w := styles[mustSelect(t, d, ".medium")].BorderWidth[0]; !near(w, 2.0) {
		t.Errorf("medium: %v", w)
	}
	if w := styles[mustSelect(t, d, ".thick")].BorderWidth[0]; !near(w, 4.0) {
		t.Errorf("thick: %v", w)
	}
	none := styles[mustSelect(t, d, ".no")]
	for i := 0; i < 4; i++ {
		if none.BorderStyle[i] != BorderNone || !near(none.BorderWidth[i], 0.0) {
			t.Errorf("no border[%d]: %v/%v", i, none.BorderStyle[i], none.BorderWidth[i])
		}
	}
}

func TestComputedBorderRadiusIsUniformByDefault(t *testing.T) {
	d, styles := doc(t,
		"<style>.plain { border-radius: 20px } .corner { border-radius: 8px;"+
			"border-top-left-radius: 50% } .all { border-radius: 8px 4px }</style>"+
			"<div class=\"plain\">a</div><div class=\"corner\">b</div><div class=\"all\">c</div>", 1000.0)
	plain := styles[mustSelect(t, d, ".plain")]
	for _, r := range plain.BorderRadius {
		if r.Percent || !near(r.Value, 20.0) {
			t.Errorf("plain radius: %+v", plain.BorderRadius)
			break
		}
	}
	corner := styles[mustSelect(t, d, ".corner")]
	if p := corner.BorderRadius[0]; !p.Percent || !near(p.Value, 50.0) {
		t.Errorf("overridden corner: %+v", corner.BorderRadius[0])
	}
	if r := corner.BorderRadius[3]; r.Percent || !near(r.Value, 8.0) {
		t.Errorf("kept corner: %+v", corner.BorderRadius[3])
	}
	all := styles[mustSelect(t, d, ".all")]
	want := []float64{8.0, 4.0, 8.0, 4.0}
	for i, w := range want {
		if all.BorderRadius[i].Percent || !near(all.BorderRadius[i].Value, w) {
			t.Errorf("pairwise radius[%d]: %+v", i, all.BorderRadius[i])
		}
	}
}

func TestBackgroundImageURLResolution(t *testing.T) {
	// Relative urls resolve against the sheet's own URL at parse time (the
	// resolve callback); absolute urls stay verbatim; nothing means no URL.
	d := parseFakeHTML(`<div class="rel">r</div><div class="abs">a</div><div class="none">n</div>`)
	sheets := []Sheet{
		{URL: "http://example.com/dir/page.css",
			CSS: ".rel { background-image: url(tile.png) }" +
				".abs { background-image: url(http://other.site/t.png) }" +
				".none { background-image: none }"},
	}
	styles, warnings, err := ComputeSheets(d, sheets, testViewport)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: %q", warnings)
	}
	rel := styles[mustSelect(t, d, ".rel")]
	if !rel.HasBackgroundImage || rel.BackgroundImage != "http://example.com/dir/tile.png" {
		t.Errorf("relative url: %q", rel.BackgroundImage)
	}
	abs := styles[mustSelect(t, d, ".abs")]
	if !abs.HasBackgroundImage || abs.BackgroundImage != "http://other.site/t.png" {
		t.Errorf("absolute url: %q", abs.BackgroundImage)
	}
	if none := styles[mustSelect(t, d, ".none")]; none.HasBackgroundImage {
		t.Errorf("none keeps no url: %q", none.BackgroundImage)
	}
}

func TestFontFaceWarningsOnTheNoLoaderPath(t *testing.T) {
	css := "@font-face { font-family: \"Woff2 Only\"; src: url(f.woff2) format(\"woff2\"); font-style: normal; }" +
		"@font-face { font-family: \"No Src\"; }" +
		"p { color: red }"
	d := parseFakeHTML("<p id=x>x</p>")
	styles, warnings, err := Compute(d, []string{css}, testViewport)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if got := rgbOf(styles[mustSelect(t, d, "#x")].Color); got != 0xff0000 {
		t.Errorf("@font-face leaked into the cascade: %06x", got)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings: %q", warnings)
	}
	want := `skipping @font-face for family "Woff2 Only", font-style normal: ` +
		`styles were computed without subresource loading`
	if warnings[0] != want {
		t.Errorf("warning:\n got %q\nwant %q", warnings[0], want)
	}
}

// S3: compute is serialised because libwapcaplet intern refcounts are not
// atomic. Eight goroutines with distinct documents must keep their own
// values and never panic.
func TestParallelComputeKeepsValuesAndDoesNotRace(t *testing.T) {
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				n := worker*40 + i
				html := "<style>@media (min-width: 500px) { .c" + itoa(n) +
					" { color: rgb(" + itoa((n*7)%255) + ", 0, 0); font-size: " + itoa(10+n) + "px } }</style>" +
					"<div class=\"c" + itoa(n) + "\" id=\"n" + itoa(n) + "\">x</div>"
				d := parseFakeHTML(html)
				styles, _, err := Compute(d, Stylesheets(d), 800.0)
				if err != nil {
					t.Errorf("compute %d: %v", n, err)
					return
				}
				id := mustSelect(t, d, "#n"+itoa(n))
				s := styles[id]
				if want := float64((n*7)%255) / 255.0; s.Color.R-want > 0.02 || want-s.Color.R > 0.02 {
					t.Errorf("colour %d: %+v", n, s.Color)
					return
				}
				if !near(s.FontSize, float64(10+n)) {
					t.Errorf("font-size %d: %v", n, s.FontSize)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}
