// Flex/grid anonymous items carrying atomic inline boxes: the codeberg
// support-button shape (an inline <svg> octicon plus text inside a flex or
// grid container) must produce the svg box and the text run. Flex and grid
// anonymous items collect the container's inline content; the text-only
// collector drops atomic boxes, so the icon paints nothing.
package layout

import (
	"testing"
)

// flexButtonFixture is the codeberg support-button shape: a flex container
// whose inline content (link with a 16x16 icon plus text) forms a single
// anonymous item.
const flexButtonFixture = `<style>#row { display: flex; width: 600px; }</style>` +
	`<div id="row"><a id="btn" href="#"><svg id="ic" width="16" height="16"><rect width="16" height="16" fill="red"/></svg>Sign in</a></div>`

// gridButtonFixture is the same shape in a grid container.
const gridButtonFixture = `<style>#grid { display: grid; grid-template-columns: auto; width: 600px; }</style>` +
	`<div id="grid"><a id="btn" href="#"><svg id="ic" width="16" height="16"><rect width="16" height="16" fill="red"/></svg>Sign in</a></div>`

// RED: a flex anonymous item must keep the inline svg atom: the icon box
// paints at 16x16 with ink, followed by the text run on its line.
func TestFlexAnonymousItemKeepsInlineSVGAtom(t *testing.T) {
	doc, l := pageWithImages(t, flexButtonFixture)
	ic := atomBox(t, l, doc, "#ic")
	if ic.Tag != "svg" {
		t.Fatalf("inline svg box tag = %q, want svg", ic.Tag)
	}
	if ic.W != 16 || ic.H != 16 {
		t.Fatalf("inline svg box = %vx%v, want 16x16", ic.W, ic.H)
	}
	assertIconInk(t, ic)
	after := textBoxAt(t, l, "Sign in")
	if after.X < ic.X+ic.W-1 {
		t.Fatalf("text after the svg starts at %v, before the svg ends at %v", after.X, ic.X+ic.W)
	}
	if ic.Y+ic.H != textBaseline(t, after) {
		t.Fatalf("svg bottom %v, want the text baseline %v", ic.Y+ic.H, textBaseline(t, after))
	}
	if !(ic.Y < after.Y+after.H && after.Y < ic.Y+ic.H) {
		t.Fatalf("svg and text must share one line (svg y %v h %v vs text y %v h %v)", ic.Y, ic.H, after.Y, after.H)
	}
}

// RED: a grid anonymous item must keep the inline svg atom, same shape.
func TestGridAnonymousItemKeepsInlineSVGAtom(t *testing.T) {
	doc, l := pageWithImages(t, gridButtonFixture)
	ic := atomBox(t, l, doc, "#ic")
	if ic.Tag != "svg" {
		t.Fatalf("inline svg box tag = %q, want svg", ic.Tag)
	}
	if ic.W != 16 || ic.H != 16 {
		t.Fatalf("inline svg box = %vx%v, want 16x16", ic.W, ic.H)
	}
	assertIconInk(t, ic)
	after := textBoxAt(t, l, "Sign in")
	if after.X < ic.X+ic.W-1 {
		t.Fatalf("text after the svg starts at %v, before the svg ends at %v", after.X, ic.X+ic.W)
	}
	if ic.Y+ic.H != textBaseline(t, after) {
		t.Fatalf("svg bottom %v, want the text baseline %v", ic.Y+ic.H, textBaseline(t, after))
	}
	if !(ic.Y < after.Y+after.H && after.Y < ic.Y+ic.H) {
		t.Fatalf("svg and text must share one line (svg y %v h %v vs text y %v h %v)", ic.Y, ic.H, after.Y, after.H)
	}
}

// assertIconInk checks the box carries rasterized pixels with at least one
// inked (non-transparent) pixel: the paint step has something to draw.
func assertIconInk(t *testing.T, b *LayoutBox) {
	t.Helper()
	if b.Image == nil {
		t.Fatalf("inline svg box must carry its rasterized pixels")
	}
	for i := 3; i < len(b.Image.RGBA); i += 4 {
		if b.Image.RGBA[i] > 0 {
			return
		}
	}
	t.Fatalf("inline svg raster has no inked pixel")
}
