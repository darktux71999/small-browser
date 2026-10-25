package layout

import (
	"testing"
)

// S3 RED tests (leading-fidelity T3): CSS 2.1 §8.3.1 vertical margin
// collapsing in the block formatting context. Flex/grid items never
// collapse (their tests pin the summing behavior).

func TestSiblingMarginsCollapse(t *testing.T) {
	doc, _, l := cascade(t, `<div id="a" style="margin-bottom: 20px">x</div><div id="b" style="margin-top: 40px">y</div>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	if gap := b.Y - (a.Y + a.H); !near(gap, 40.0) {
		t.Fatalf("adjoining sibling margins collapse to max: gap = %v, want 40 (not 20+40=60)", gap)
	}
}

func TestThreeSiblingsCollapsePairwise(t *testing.T) {
	doc, _, l := cascade(t, `<div id="a" style="margin-bottom: 10px">x</div><div id="b" style="margin-top: 30px; margin-bottom: 5px">y</div><div id="c" style="margin-top: 20px">z</div>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	c := boxFor(t, l, doc, "#c")
	if gap := b.Y - (a.Y + a.H); !near(gap, 30.0) {
		t.Fatalf("first pair collapses to max(10,30): gap = %v, want 30", gap)
	}
	if gap := c.Y - (b.Y + b.H); !near(gap, 20.0) {
		t.Fatalf("second pair collapses to max(5,20): gap = %v, want 20", gap)
	}
}

func TestParentFirstChildMarginsCollapse(t *testing.T) {
	doc, _, l := cascade(t, `<div id="p" style="margin-top: 25px"><div id="c" style="margin-top: 15px">x</div></div>`)
	p := boxFor(t, l, doc, "#p")
	c := boxFor(t, l, doc, "#c")
	if !near(c.Y-p.Y, 0.0) {
		t.Fatalf("first-child top margin collapses through the parent: c.y-p.y = %v, want 0 (not 15)", c.Y-p.Y)
	}
	body := boxFor(t, l, doc, "body")
	if !near(p.Y-body.Y, 0.0) {
		t.Fatalf("collapsed margin sits above the parent border: p.y-body.y = %v, want 0", p.Y-body.Y)
	}
}

func TestParentLastChildMarginsCollapse(t *testing.T) {
	doc, _, l := cascade(t, `<div id="p" style="margin-bottom: 5px"><div id="c" style="margin-bottom: 30px">x</div></div>`)
	p := boxFor(t, l, doc, "#p")
	c := boxFor(t, l, doc, "#c")
	if gap := (p.Y + p.H) - (c.Y + c.H); !near(gap, 0.0) {
		t.Fatalf("last-child bottom margin collapses through the auto-height parent: gap = %v, want 0 (not 30)", gap)
	}
}

func TestParentLastChildNoCollapseWithBottomBorder(t *testing.T) {
	doc, _, l := cascade(t, `<div id="p" style="margin-bottom: 5px; border-bottom: 2px solid black"><div id="c" style="margin-bottom: 30px">x</div></div>`)
	p := boxFor(t, l, doc, "#p")
	c := boxFor(t, l, doc, "#c")
	// The 2px bottom border separates the margins: the child's 30px stays
	// inside the parent, above the border.
	if gap := (p.Y + p.H - 2) - (c.Y + c.H); !near(gap, 30.0) {
		t.Fatalf("bottom border blocks the collapse: inner gap = %v, want 30", gap)
	}
}

func TestEmptyBlockMarginsCollapse(t *testing.T) {
	doc, _, l := cascade(t, `<div id="a" style="margin-bottom: 30px">x</div><div id="e" style="margin-top: 10px; margin-bottom: 20px"></div><div id="b" style="margin-top: 5px">y</div>`)
	a := boxFor(t, l, doc, "#a")
	e := boxFor(t, l, doc, "#e")
	b := boxFor(t, l, doc, "#b")
	if e.H != 0 {
		t.Fatalf("empty block has no height: h = %v, want 0", e.H)
	}
	if gap := e.Y - (a.Y + a.H); !near(gap, 30.0) {
		t.Fatalf("empty block merges its own margins, then collapses with the previous sibling: gap = %v, want max(30,10,20)=30 (not 40)", gap)
	}
	if gap := b.Y - (e.Y + e.H); !near(gap, 20.0) {
		t.Fatalf("empty block collapses with the next sibling: gap = %v, want max(20,5)=20 (not 25)", gap)
	}
}

func TestNegativeMarginsCollapse(t *testing.T) {
	doc, _, l := cascade(t, `<div id="a" style="margin-bottom: -10px">x</div><div id="b" style="margin-top: 20px">y</div><div id="c" style="margin-bottom: -5px">z</div><div id="d" style="margin-top: -8px">w</div>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	c := boxFor(t, l, doc, "#c")
	d := boxFor(t, l, doc, "#d")
	if gap := b.Y - (a.Y + a.H); !near(gap, 10.0) {
		t.Fatalf("positive+negative collapses to maxPos+minNeg: gap = %v, want 20+(-10)=10", gap)
	}
	if gap := d.Y - (c.Y + c.H); !near(gap, -8.0) {
		t.Fatalf("negative+negative collapses to the most negative: gap = %v, want min(-5,-8)=-8 (not -5+-8=-13)", gap)
	}
}

func TestWhitespaceAroundBlocksStillCollapses(t *testing.T) {
	// Pretty-printed HTML leaves whitespace text runs between blocks;
	// blank runs flush nothing, so they must not disturb the collapse
	// (regression: the walkers once mistook text runs for participants).
	doc, _, l := cascade(t, "<body>\n  <div id=\"a\" style=\"margin: 10px; height: 80px\"></div>\n  <div id=\"b\" style=\"margin: 10px; height: 80px\"></div>\n</body>")
	body := boxFor(t, l, doc, "body")
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	if !near(body.Y, 10.0) {
		t.Fatalf("body top margin collapses with the first block: body.y = %v, want 10", body.Y)
	}
	if !near(a.Y-body.Y, 0.0) {
		t.Fatalf("first block sits at the body content top: a.y-body.y = %v, want 0", a.Y-body.Y)
	}
	if gap := b.Y - (a.Y + a.H); !near(gap, 10.0) {
		t.Fatalf("sibling margins collapse across the blank runs: gap = %v, want 10 (not 20)", gap)
	}
}

func TestFlexColumnSiblingsDoNotCollapse(t *testing.T) { // PRESERVE pin: flex items never collapse their margins (CSS 2.1
	// §8.3.1: only block formatting contexts collapse). The legacy total
	// is 100: the 20px bottom margin rides the main-axis advance, and the
	// 40px top margin applies once at item placement and once inside
	// layoutNode — the exact quirk collapse must not touch.
	doc, _, l := cascade(t, `<div id="f" style="display: flex; flex-direction: column"><div id="i1" style="margin-bottom: 20px">x</div><div id="i2" style="margin-top: 40px">y</div></div>`)
	i1 := boxFor(t, l, doc, "#i1")
	i2 := boxFor(t, l, doc, "#i2")
	if gap := i2.Y - (i1.Y + i1.H); !near(gap, 100.0) {
		t.Fatalf("flex item margins must stay exactly as today: gap = %v, want 100", gap)
	}
}

func TestGridSiblingsDoNotCollapse(t *testing.T) {
	// PRESERVE pin: grid items never collapse their margins. Legacy grid
	// sizes rows from content only, so the previous item's bottom margin
	// drops out and only the next item's top margin (10px) separates them;
	// collapsing would give max(20,10)=20 instead.
	doc, _, l := cascade(t, `<div id="g" style="display: grid"><div id="a" style="margin-bottom: 20px">x</div><div id="b" style="margin-top: 10px">y</div></div>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	if gap := b.Y - (a.Y + a.H); !near(gap, 10.0) {
		t.Fatalf("grid item margins must stay exactly as today: gap = %v, want 10", gap)
	}
}
