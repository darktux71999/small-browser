// Inline formatting context tests for atomic inline boxes: replaced
// elements (<img>, inline <svg>) and inline-block boxes that sit inside a
// text run (CSS 2.1 §9.2.2 inline-level boxes, §10.8 inline replaced
// elements: the bottom margin edge sits on the baseline of the line).
package layout

import (
	"path/filepath"
	"strings"
	"testing"

	"browser-lite/css"
	"browser-lite/dom"
	"browser-lite/img"
	"browser-lite/text"
)

// pageWithImages cascades html and lays it out with the fixture directory
// as the document base, so <img src> and inline <svg> rasterize.
func pageWithImages(t *testing.T, html string) (*dom.Document, *Layout) {
	t.Helper()
	dir, err := filepath.Abs("../../test/data")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	doc := dom.Parse(html)
	idx := domIndex{doc}
	styles, _, err := css.Compute(idx, css.Stylesheets(idx), 1000)
	if err != nil {
		t.Fatalf("css.Compute: %v", err)
	}
	return doc, RenderWithImages(doc, styles, 1000, img.Load(doc, dir))
}

// atomBox returns the box that paints the node matching selector.
func atomBox(t *testing.T, l *Layout, doc *dom.Document, selector string) *LayoutBox {
	t.Helper()
	return boxFor(t, l, doc, selector)
}

// textBoxAt returns the first text line whose text contains needle.
func textBoxAt(t *testing.T, l *Layout, needle string) *LayoutBox {
	t.Helper()
	for i := range l.Boxes {
		if l.Boxes[i].Tag == "#text" && strings.Contains(l.Boxes[i].Text, needle) {
			return &l.Boxes[i]
		}
	}
	t.Fatalf("no text line contains %q", needle)
	return nil
}

// RED for gap 1: an inline <svg> inside a text run is a 16x16 replaced box
// on the same line, its bottom edge on the text baseline, placed after the
// text that precedes it and before the text that follows it.
func TestInlineSVGIsReplacedBoxOnTextBaseline(t *testing.T) {
	doc, l := pageWithImages(t,
		`<p id="p">Hi <svg id="ic" width="16" height="16"><rect width="16" height="16" fill="red"/></svg> there</p>`)
	ic := atomBox(t, l, doc, "#ic")
	if ic.Tag != "svg" {
		t.Fatalf("inline svg box tag = %q, want svg", ic.Tag)
	}
	if ic.W != 16 || ic.H != 16 {
		t.Fatalf("inline svg box = %vx%v, want 16x16", ic.W, ic.H)
	}
	if ic.Image == nil {
		t.Fatalf("inline svg box must carry its rasterized pixels")
	}
	before := textBoxAt(t, l, "Hi")
	if ic.X < before.X+before.W-1 {
		t.Fatalf("svg at x=%v overlaps the text before it (ends %v)", ic.X, before.X+before.W)
	}
	if ic.Y+ic.H != textBaseline(t, before) {
		t.Fatalf("svg bottom %v, want the text baseline %v", ic.Y+ic.H, textBaseline(t, before))
	}
	after := textBoxAt(t, l, "there")
	if after.X < ic.X+ic.W-1 {
		t.Fatalf("text after the svg starts at %v, before the svg ends at %v", after.X, ic.X+ic.W)
	}
	if after.Y != before.Y {
		t.Fatalf("svg and surrounding text must share one line (text y %v vs %v)", after.Y, before.Y)
	}
}

// RED for gap 2: an <img> inside an inline-block (here a div styled
// display:inline-block) gets its own measured box at the declared size.
func TestImageInsideInlineBlockGetsBox(t *testing.T) {
	doc, l := pageWithImages(t,
		`<style>.inline-block { display: inline-block; }</style>`+
			`<body><div class="inline-block"><img id="pic" src="probe-svg-image.svg" width="240" height="120"></div></body>`)
	pic := atomBox(t, l, doc, "#pic")
	if pic.W != 240 || pic.H != 120 {
		t.Fatalf("image inside inline-block box = %vx%v, want 240x120", pic.W, pic.H)
	}
	if pic.Image == nil {
		t.Fatalf("image inside inline-block must carry decoded pixels")
	}
}

func TestInlineFontWeightChangesWrapping(t *testing.T) {
	_, l := pageWithImages(t,
		`<style>p { width: 580px; font-size: 18px; font-weight: 500; }</style>`+
			`<p>Codeberg is a non-profit, community-led effort that provides services to `+
			`<b>free and open-source projects</b>, such as Git hosting (using `+
			`<a>Forgejo</a>), <a>Pages</a>, CI/CD, and a Weblate instance.</p>`+
			`<p><b>Bold only</b></p>`)
	var lines []string
	for _, box := range l.Boxes {
		if box.Tag == "#text" {
			lines = append(lines, box.Text)
		}
	}
	want := []string{
		"Codeberg is a non-profit, community-led effort that provides",
		"services to free and open-source projects, such as Git hosting",
		"(using Forgejo), Pages, CI/CD, and a Weblate instance.",
		"Bold only",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paragraph lines = %#v, want %#v", lines, want)
	}

	var boldLine *LayoutBox
	for i := range l.Boxes {
		if l.Boxes[i].Tag == "#text" && strings.Contains(l.Boxes[i].Text, "Bold only") {
			boldLine = &l.Boxes[i]
			break
		}
	}
	if boldLine == nil || !boldLine.Bold {
		t.Fatalf("bold phrase box = %+v, want a bold text line", boldLine)
	}
	var foundBoldSpan bool
	for _, run := range l.Runs {
		for _, span := range run.Spans {
			if run.Text[span.Start:span.End] == "free and open-source projects" && span.Style.FontWeight == 700 {
				foundBoldSpan = true
			}
		}
	}
	if !foundBoldSpan {
		t.Fatal("paint run is missing the bold span for free and open-source projects")
	}
}

// textBaseline independently measures the public Pango baseline used to align
// the atomic boxes with their surrounding text.
func textBaseline(t *testing.T, b *LayoutBox) float64 {
	t.Helper()
	s, err := text.New()
	if err != nil {
		t.Fatalf("text.New: %v", err)
	}
	defer s.Close()
	m := s.Measure(b.Text, text.Style{
		FontFamilies:     []string{"sans-serif"},
		FontSize:         b.FontSize,
		FontWeight:       400,
		LineHeightNormal: true,
	}, 1000)
	return b.Y + m.Lines[0].Baseline
}
