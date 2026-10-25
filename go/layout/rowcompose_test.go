// Row-composition tests: the M3 defect family on live codeberg.org
// (odd/tasks/meta1-codeberg.md L22) — flex/grid rows that compose stacked
// instead of sideways. Each test is a minimal local fixture in the shape of
// the live page (codeberg-style nav row, support repeat(2,1fr) grid with
// place-self items inside a flex item, footer link columns) and pins the
// sideways geometry the engine must produce.
package layout

import (
	"testing"
)

// A nav-style flex row whose first item is a link list: the list must size
// to its content width and keep its links on one horizontal row (live: the
// engine sized navbar-left 56px so Explore/About/FAQ/Help/Donate wrapped
// into a vertical stack).
func TestFlexRowLinkListKeepsLinksHorizontal(t *testing.T) {
	doc, _, l := cascade(t, `<style>
  #nav { display: flex; }
  #left { display: flex; }
  #left a { display: flex; padding: 3px; }
  #right { display: flex; }
  #right a { display: flex; }
</style><nav id="nav"><div id="left"><a id="l1">Explore</a><a id="l2">About</a><a id="l3">FAQ</a><a id="l4">Help</a><a id="l5">Donate</a></div><div id="right"><a id="r1">Register</a><a id="r2">Sign in</a></div></nav>`)
	left := boxFor(t, l, doc, "#left")
	if left.W < 200 {
		t.Fatalf("the link list sizes to its content, got width %v", left.W)
	}
	first := boxFor(t, l, doc, "#l1")
	for _, sel := range []string{"#l2", "#l3", "#l4", "#l5"} {
		other := boxFor(t, l, doc, sel)
		if !near(other.Y, first.Y) {
			t.Fatalf("links share one row: %s y=%v, first y=%v", sel, other.Y, first.Y)
		}
		if !(other.X > first.X) {
			t.Fatalf("links run left to right: %s x=%v, first x=%v", sel, other.X, first.X)
		}
	}
}

// A grid with a false @media branch must keep the base repeat(2,1fr): the
// manual grid cascade used to apply every @media block unconditionally, so
// a later max-width:739px single-column rule won on a 1000px viewport and
// the support rows composed stacked.
func TestGridFalseMediaBranchKeepsTwoColumns(t *testing.T) {
	doc, _, l := cascade(t, `<style>
  #s { display: flex; flex-direction: column; width: 872px; }
  #g { display: grid; grid-template-columns: repeat(2,1fr); }
  @media (max-width: 739px) { #g { grid-template-columns: 1fr; } }
  #g a { display: inline-flex; }
  #g p { margin: 0; }
</style><section id="s"><div id="g"><a id="btn">Become a member</a><p id="txt">Some text here</p></div></section>`)
	btn := boxFor(t, l, doc, "#btn")
	txt := boxFor(t, l, doc, "#txt")
	if !near(btn.Y, txt.Y) {
		t.Fatalf("the items share one row: btn y=%v txt y=%v", btn.Y, txt.Y)
	}
	if !(txt.X > btn.X) {
		t.Fatalf("the items run left to right: btn x=%v txt x=%v", btn.X, txt.X)
	}
}

// A grid with a true @media branch must follow it: same fixture shape with
// a matching min-width query lays out stacked.
func TestGridTrueMediaBranchAppliesSingleColumn(t *testing.T) {
	doc, _, l := cascade(t, `<style>
  #s { display: flex; flex-direction: column; width: 872px; }
  #g { display: grid; grid-template-columns: repeat(2,1fr); }
  @media (min-width: 700px) { #g { grid-template-columns: 1fr; } }
  #g a { display: inline-flex; }
  #g p { margin: 0; }
</style><section id="s"><div id="g"><a id="btn">Become a member</a><p id="txt">Some text here</p></div></section>`)
	btn := boxFor(t, l, doc, "#btn")
	txt := boxFor(t, l, doc, "#txt")
	if !(txt.Y > btn.Y) {
		t.Fatalf("the true media branch stacks the rows: btn y=%v txt y=%v", btn.Y, txt.Y)
	}
}

// A support-style grid: repeat(2,1fr) with place-self:center right items
// composes 2-wide; the button keeps its content width, right-aligned in its
// cell; the grid lays out inside its flex item at the item's width (live:
// the button composed 938px wide across the full row).
func TestGridPlaceSelfShrinksAndRightAlignsButton(t *testing.T) {
	doc, _, l := cascade(t, `<style>
  #s { display: flex; flex-direction: column; width: 872px; }
  #g { display: grid; grid-template-columns: repeat(2,1fr); gap: 2em; }
  #g a { display: inline-flex; place-self: center right; }
  #g p { margin: 0; }
</style><section id="s"><div id="g"><a id="btn">Become a member</a><p id="txt">Some text here in the right cell</p></div></section>`)
	grid := boxFor(t, l, doc, "#g")
	btn := boxFor(t, l, doc, "#btn")
	txt := boxFor(t, l, doc, "#txt")
	if !near(grid.W, 872.0) {
		t.Fatalf("the grid lays out at its flex item width, got %v", grid.W)
	}
	if !near(btn.Y, txt.Y) {
		t.Fatalf("the items share one row: btn y=%v txt y=%v", btn.Y, txt.Y)
	}
	track := (872.0 - 32.0) / 2
	if !(btn.W < track) {
		t.Fatalf("the button keeps its content width inside the %vpx track, got %v", track, btn.W)
	}
	cellRight := grid.X + track
	if !near(btn.X+btn.W, cellRight) {
		t.Fatalf("the button right-aligns in its cell: right edge %v, cell edge %v", btn.X+btn.W, cellRight)
	}
}

// A nowrap run never wraps, even when its container is narrower than the
// run (live: the support buttons carry white-space:nowrap on the button
// itself from the true 740px media branch; without it the button text
// wrapped and the rows grew two lines tall). nowrap is honored at
// formatting-context roots — the block container here, the flex/grid
// anonymous items on the live page; mixed inline runs inside a block keep
// the container's behavior (the engine flattens inline runs, unwrapped
// text excepted).
func TestNowrapRunStaysOnOneLine(t *testing.T) {
	doc, _, l := cascade(t, `<style>
  #box { width: 100px; white-space: nowrap; }
</style><div id="box">Become a member today</div>`)
	box := boxFor(t, l, doc, "#box")
	count := 0
	for i := range l.Boxes {
		b := &l.Boxes[i]
		if b.Tag != "#text" {
			continue
		}
		center := b.Y + b.H/2
		if center >= box.Y && center < box.Y+box.H {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the nowrap run lays out on one line, got %d text lines", count)
	}
}

// A footer-style grid: repeat(5,minmax(177px,240px)) sizes every track
// between its bounds so the link columns share one row and the section
// height is exactly the tallest column's content (live: the columns sized
// to max-content, wrapped, and stretched the footer to 365px).
func TestFooterMinmaxColumnsShareOneRow(t *testing.T) {
	doc, _, l := cascade(t, `<style>
  #ft { width: 1000px; }
  #fg { display: grid; grid-template-columns: repeat(5,minmax(177px,240px)); column-gap: 20px; }
  #fg ul { margin: 0; padding: 0; }
</style><footer id="ft"><div id="fg"><div id="c1">Codeberg<ul><li>Blog</li><li>Documentation</li></ul></div><div id="c2">Association<ul><li>Who are we?</li><li>Donate</li></ul></div><div id="c3">Services</div><div id="c4">Legal</div><div id="c5">Extra</div></div></footer>`)
	first := boxFor(t, l, doc, "#c1")
	for _, sel := range []string{"#c2", "#c3", "#c4", "#c5"} {
		other := boxFor(t, l, doc, sel)
		if !near(other.Y, first.Y) {
			t.Fatalf("columns share one row: %s y=%v, first y=%v", sel, other.Y, first.Y)
		}
		if !(other.W >= 177.0-0.5 && other.W <= 240.0+0.5) {
			t.Fatalf("column %s clamps to its minmax bounds, got %v", sel, other.W)
		}
	}
	if !(first.W >= 177.0-0.5 && first.W <= 240.0+0.5) {
		t.Fatalf("column #c1 clamps to its minmax bounds, got %v", first.W)
	}
	grid := boxFor(t, l, doc, "#fg")
	tallest := first.H
	for _, sel := range []string{"#c2", "#c3", "#c4", "#c5"} {
		tallest = max(tallest, boxFor(t, l, doc, sel).H)
	}
	if !near(grid.H, tallest) {
		t.Fatalf("the grid hugs its tallest column: grid h=%v tallest=%v", grid.H, tallest)
	}
}
