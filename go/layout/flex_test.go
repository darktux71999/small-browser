// Flex tests for go/layout, mirroring the Rust oracle suite tests/flex.rs.
// Every expectation below comes from the oracle file: the cascade carries
// the flex properties, and the layout shares rows, stacks columns, grows,
// gaps, justifies, aligns and wraps like the oracle's layout_flex_container
// (src/layout.rs:380-663). Fixtures are small locals with the same shapes
// as test/data/flex-*.html; test/data stays read-only.
package layout

import (
	"math"
	"testing"

	"browser-lite/css"
	"browser-lite/dom"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.5 }

func flexCascade(t *testing.T, html string) (*dom.Document, []css.Style, *Layout) {
	t.Helper()
	doc := dom.Parse(html)
	idx := domIndex{doc}
	sheets := css.Stylesheets(idx)
	styles, _, err := css.Compute(idx, sheets, 1000)
	if err != nil {
		t.Fatalf("css.Compute: %v", err)
	}
	return doc, styles, Render(doc, styles, 1000)
}

const flexRowHTML = `<style>
  #row { display: flex; width: 600px; }
  #row div { width: 100px; height: 40px; }
</style><div id="row"><div id="a">A</div><div id="b">B</div><div id="c">C</div></div>`

const flexGrowHTML = `<style>
  #grow { display: flex; width: 600px; }
  #grow #a { flex: 1; }
  #grow #b { flex: 2; }
  #grow div { height: 40px; }
</style><div id="grow"><div id="a">A</div><div id="b">B</div></div>`

func TestFlexContainerCarriesItsFlexProperties(t *testing.T) {
	// Oracle: tests/flex.rs a_flex_container_keeps_display_block_and_carries_its_flex_properties.
	doc, styles, _ := flexCascade(t, flexRowHTML)
	row := &styles[mustID(t, doc, "#row")]
	if row.Display != css.DisplayBlock {
		t.Fatalf("flex container display = %v, want Block", row.Display)
	}
	if row.Flex == nil {
		t.Fatal("the flex container carries its flex properties")
	}
	flex := *row.Flex
	if flex.Direction != css.FlexDirectionRow || flex.Wrap != css.FlexWrapNoWrap ||
		flex.Justify != css.JustifyFlexStart || flex.Align != css.AlignItemsStretch {
		t.Fatalf("flex container props = %+v", flex)
	}
	a := &styles[mustID(t, doc, "#a")]
	if a.FlexItem.Grow != 0.0 || a.FlexItem.Shrink != 1.0 {
		t.Fatalf("default flex item grow/shrink = %v/%v, want 0/1", a.FlexItem.Grow, a.FlexItem.Shrink)
	}
	if a.FlexItem.Basis.Kind != css.BasisAuto {
		t.Fatalf("default flex item basis = %+v, want Auto", a.FlexItem.Basis)
	}
}

func mustID(t *testing.T, doc *dom.Document, sel string) dom.NodeID {
	t.Helper()
	id, ok := doc.QuerySelector(sel)
	if !ok {
		t.Fatalf("fixture has no %s", sel)
	}
	return id
}

func TestFlexShorthandFillsGrowShrinkAndBasis(t *testing.T) {
	// Oracle: tests/flex.rs the_flex_shorthand_fills_grow_shrink_and_basis.
	doc, styles, _ := flexCascade(t, flexGrowHTML)
	a := &styles[mustID(t, doc, "#a")]
	b := &styles[mustID(t, doc, "#b")]
	if a.FlexItem.Grow != 1.0 || b.FlexItem.Grow != 2.0 {
		t.Fatalf("grow = %v/%v, want 1/2", a.FlexItem.Grow, b.FlexItem.Grow)
	}
	if a.FlexItem.Shrink != 1.0 {
		t.Fatalf("shrink = %v, want 1", a.FlexItem.Shrink)
	}
	if a.FlexItem.Basis.Kind != css.BasisPx || !near(a.FlexItem.Basis.Value, 0.0) {
		t.Fatalf("basis = %+v, want Px(0)", a.FlexItem.Basis)
	}
}

func TestFlexRowItemsShareARow(t *testing.T) {
	// Oracle: tests/flex.rs flex_row_items_share_a_row.
	doc, _, l := flexCascade(t, flexRowHTML)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	c := boxFor(t, l, doc, "#c")
	if !(near(a.Y, b.Y) && near(b.Y, c.Y)) {
		t.Fatalf("items share a row: a.y=%v b.y=%v c.y=%v", a.Y, b.Y, c.Y)
	}
	if !(a.X < b.X && b.X < c.X) {
		t.Fatalf("items left to right: a.x=%v b.x=%v c.x=%v", a.X, b.X, c.X)
	}
	if !near(b.X-(a.X+a.W), 0.0) {
		t.Fatalf("items adjacent: a ends at %v, b starts at %v", a.X+a.W, b.X)
	}
	row := boxFor(t, l, doc, "#row")
	if !near(row.H, 40.0) {
		t.Fatalf("row is 40px tall, got %v", row.H)
	}
}

func TestFlexColumnItemsShareAColumn(t *testing.T) {
	// Oracle: tests/flex.rs flex_column_items_share_a_column.
	doc, _, l := flexCascade(t, `<style>
  #col { display: flex; flex-direction: column; width: 600px; }
  #col div { width: 100px; height: 40px; }
</style><div id="col"><div id="a">A</div><div id="b">B</div><div id="c">C</div></div>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	c := boxFor(t, l, doc, "#c")
	if !(near(a.X, b.X) && near(b.X, c.X)) {
		t.Fatalf("items share a column: a.x=%v b.x=%v c.x=%v", a.X, b.X, c.X)
	}
	if !(a.Y < b.Y && b.Y < c.Y) {
		t.Fatalf("items stacked: a.y=%v b.y=%v c.y=%v", a.Y, b.Y, c.Y)
	}
	col := boxFor(t, l, doc, "#col")
	if !near(col.H, 120.0) {
		t.Fatalf("column is 120px tall, got %v", col.H)
	}
}

func TestFlexGrowSharesTheFreeSpace(t *testing.T) {
	// Oracle: tests/flex.rs flex_grow_shares_the_free_space.
	doc, _, l := flexCascade(t, flexGrowHTML)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	if !near(a.W, 200.0) {
		t.Fatalf("flex:1 takes a third of 600px, got %v", a.W)
	}
	if !near(b.W, 400.0) {
		t.Fatalf("flex:2 takes two thirds of 600px, got %v", b.W)
	}
}

func TestFlexGapSeparatesTheItems(t *testing.T) {
	// Oracle: tests/flex.rs gap_separates_the_items.
	doc, styles, l := flexCascade(t, `<style>
  #gap { display: flex; gap: 20px; width: 600px; }
  #gap div { width: 100px; height: 40px; }
</style><div id="gap"><div id="a">A</div><div id="b">B</div><div id="c">C</div></div>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	c := boxFor(t, l, doc, "#c")
	if !near(b.X-(a.X+a.W), 20.0) {
		t.Fatalf("first gap is 20px, got %v", b.X-(a.X+a.W))
	}
	if !near(c.X-(b.X+b.W), 20.0) {
		t.Fatalf("second gap is 20px, got %v", c.X-(b.X+b.W))
	}
	flex := styles[mustID(t, doc, "#gap")].Flex
	if flex == nil {
		t.Fatal("gap container carries flex properties")
	}
	if !near(flex.ColumnGap, 20.0) || !near(flex.RowGap, 20.0) {
		t.Fatalf("gaps reach the cascade: %+v", flex)
	}
}

func TestFlexJustifyContentDistributesTheItems(t *testing.T) {
	// Oracle: tests/flex.rs justify_content_distributes_the_items.
	doc, _, l := flexCascade(t, `<style>
  .row { display: flex; width: 600px; }
  .row div { width: 100px; height: 40px; }
  #center { justify-content: center; }
  #end { justify-content: flex-end; }
  #between { justify-content: space-between; }
</style><div id="center" class="row"><div id="ca">A</div><div id="cb">B</div><div id="cc">C</div></div>
<div id="end" class="row"><div id="ea">A</div><div id="eb">B</div><div id="ec">C</div></div>
<div id="between" class="row"><div id="ba">A</div><div id="bb">B</div><div id="bc">C</div></div>`)
	center := boxFor(t, l, doc, "#center")
	ca := boxFor(t, l, doc, "#ca")
	if !near(ca.X-center.X, 150.0) {
		t.Fatalf("centred items start 150px into the row, got %v", ca.X-center.X)
	}
	end := boxFor(t, l, doc, "#end")
	ea := boxFor(t, l, doc, "#ea")
	if !near(ea.X-end.X, 300.0) {
		t.Fatalf("end items start 300px into the row, got %v", ea.X-end.X)
	}
	between := boxFor(t, l, doc, "#between")
	ba := boxFor(t, l, doc, "#ba")
	bb := boxFor(t, l, doc, "#bb")
	bc := boxFor(t, l, doc, "#bc")
	if !near(ba.X-between.X, 0.0) {
		t.Fatalf("first item hugs the start, got %v", ba.X-between.X)
	}
	if !near(bb.X-(ba.X+ba.W), 150.0) {
		t.Fatalf("space-between leaves 150px, got %v", bb.X-(ba.X+ba.W))
	}
	if !near(bc.X-(bb.X+bb.W), 150.0) {
		t.Fatalf("space-between leaves 150px, got %v", bc.X-(bb.X+bb.W))
	}
}

func TestFlexAlignItemsPlacesTheItemsAcross(t *testing.T) {
	// Oracle: tests/flex.rs align_items_places_the_items_across_the_cross_axis.
	doc, _, l := flexCascade(t, `<style>
  .row { display: flex; width: 600px; height: 100px; }
  .row div { width: 100px; }
  #stretch { align-items: stretch; }
  #center { align-items: center; }
  #start { align-items: flex-start; }
</style><div id="stretch" class="row"><div id="sa">A</div></div>
<div id="center" class="row"><div id="ca">A</div></div>
<div id="start" class="row"><div id="ta">A</div></div>`)
	stretch := boxFor(t, l, doc, "#stretch")
	sa := boxFor(t, l, doc, "#sa")
	if !near(sa.H, 100.0) {
		t.Fatalf("stretched item fills the 100px row, got %v", sa.H)
	}
	if !near(sa.Y, stretch.Y) {
		t.Fatalf("stretched item starts at the row top: %v vs %v", sa.Y, stretch.Y)
	}
	center := boxFor(t, l, doc, "#center")
	ca := boxFor(t, l, doc, "#ca")
	if ca.H >= 100.0 {
		t.Fatalf("centred item keeps its content height, got %v", ca.H)
	}
	above, below := ca.Y-center.Y, center.Y+center.H-(ca.Y+ca.H)
	if !near(above, below) {
		t.Fatalf("centred item sits in the middle: %v above, %v below", above, below)
	}
	start := boxFor(t, l, doc, "#start")
	ta := boxFor(t, l, doc, "#ta")
	if !near(ta.Y, start.Y) {
		t.Fatalf("flex-start item hugs the row top: %v vs %v", ta.Y, start.Y)
	}
}

func TestFlexWrapBreaksTheItemsIntoLines(t *testing.T) {
	// Oracle: tests/flex.rs flex_wrap_breaks_the_items_into_lines.
	doc, _, l := flexCascade(t, `<style>
  #wrap { display: flex; flex-wrap: wrap; width: 250px; }
  #wrap div { width: 100px; height: 40px; }
</style><div id="wrap"><div id="a">A</div><div id="b">B</div><div id="c">C</div></div>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	c := boxFor(t, l, doc, "#c")
	if !near(a.Y, b.Y) {
		t.Fatalf("first two items share a line: a.y=%v b.y=%v", a.Y, b.Y)
	}
	if !(c.Y > b.Y) {
		t.Fatalf("third item wraps: b.y=%v c.y=%v", b.Y, c.Y)
	}
	wrap := boxFor(t, l, doc, "#wrap")
	if !near(wrap.H, 80.0) {
		t.Fatalf("two 40px lines make 80px, got %v", wrap.H)
	}
}

func TestNestedFlexItemIsContentSized(t *testing.T) {
	// Oracle: tests/flex.rs nested_flex_item_is_content_sized (R3-4 pin).
	doc, _, l := flexCascade(t, `<style>
  body { margin: 0; }
  nav { display: flex; padding: 20px; }
  .auth { margin-left: auto; display: flex; }
</style><nav><a href="#">Explore</a><a href="#">About</a>
<div id="auth" class="auth"><a href="#">Register</a><a href="#">Sign in</a></div></nav>`)
	auth := boxFor(t, l, doc, "#auth")
	if auth.W >= 300.0 {
		t.Fatalf("nested flex item is content-sized, got width %v", auth.W)
	}
	if auth.X+auth.W > 1000.5 {
		t.Fatalf("auth group stays inside the canvas: %v+%v", auth.X, auth.W)
	}
}
