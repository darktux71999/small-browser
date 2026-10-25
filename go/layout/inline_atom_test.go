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
	if ic.Y+ic.H != textBaseline(before) {
		t.Fatalf("svg bottom %v, want the text baseline %v", ic.Y+ic.H, textBaseline(before))
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

// textBaseline is the baseline the atomic boxes sit on: the text line's
// top plus its ascent. It is the single definition the layout uses.
func textBaseline(b *LayoutBox) float64 {
	return b.Y + lineAscent(b.H, b.FontSize)
}
