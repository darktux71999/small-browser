// Grid tests for go/layout, mirroring the Rust oracle suite tests/grid.rs.
// Every expectation below comes from the oracle file: the cascade carries
// the grid tracks and spans, and the layout shares rows, splits fr tracks,
// sizes auto tracks to content, gaps tracks and rows, spans tracks, leaves
// non-grid pages alone and lays nested contexts out at their cell width
// (src/layout.rs:933-1144). Fixtures are small locals with the same shapes
// as test/data/grid-*.html; test/data stays read-only.
package layout

import (
	"testing"

	"browser-lite/css"
)

func TestGridContainerCarriesItsTracksAndSpans(t *testing.T) {
	// Oracle: the grid cascade (grid-template-columns / grid-column span).
	doc, styles, _ := cascade(t, `<style>
  #g { display: grid; grid-template-columns: 100px 1fr auto; width: 600px; }
  #a { grid-column: span 2; grid-row: span 2; }
</style><section id="g"><div id="a">A</div><div id="b">B</div></section>`)
	g := &styles[mustID(t, doc, "#g")]
	if g.Display != css.DisplayBlock {
		t.Fatalf("grid container display = %v, want Block", g.Display)
	}
	if g.Grid == nil {
		t.Fatal("the grid container carries its tracks")
	}
	tracks := g.Grid.Tracks
	if len(tracks) != 3 {
		t.Fatalf("tracks = %+v, want 3", tracks)
	}
	if tracks[0].Kind != css.TrackPx || !near(tracks[0].Value, 100.0) {
		t.Fatalf("first track = %+v, want Px(100)", tracks[0])
	}
	if tracks[1].Kind != css.TrackFr || !near(tracks[1].Value, 1.0) {
		t.Fatalf("second track = %+v, want Fr(1)", tracks[1])
	}
	if tracks[2].Kind != css.TrackAuto {
		t.Fatalf("third track = %+v, want Auto", tracks[2])
	}
	a := &styles[mustID(t, doc, "#a")]
	if a.GridItem.ColSpan != 2 || a.GridItem.RowSpan != 2 {
		t.Fatalf("spans = %+v, want col 2 row 2", a.GridItem)
	}
	b := &styles[mustID(t, doc, "#b")]
	if b.GridItem.ColSpan != 1 || b.GridItem.RowSpan != 1 {
		t.Fatalf("default spans = %+v, want col 1 row 1", b.GridItem)
	}
}

func TestGridBasicTwoFixedTracksShareARow(t *testing.T) {
	// Oracle: tests/grid.rs grid_basic_two_fixed_tracks_share_a_row.
	doc, _, l := cascade(t, `<style>
  #g { display: grid; grid-template-columns: 100px 100px; width: 600px; }
  #g div { height: 40px; }
</style><section id="g"><div id="a">A</div><div id="b">B</div><div id="c">C</div></section>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	c := boxFor(t, l, doc, "#c")
	if !near(a.Y, b.Y) {
		t.Fatalf("the first two items share a row: a.y=%v b.y=%v", a.Y, b.Y)
	}
	if !(b.X > a.X) {
		t.Fatalf("the items run left to right: a.x=%v b.x=%v", a.X, b.X)
	}
	if !near(b.X-(a.X+a.W), 0.0) {
		t.Fatalf("adjacent fixed tracks touch: a ends at %v and b starts at %v", a.X+a.W, b.X)
	}
	if !near(a.W, 100.0) || !near(b.W, 100.0) {
		t.Fatalf("the px tracks are 100px wide: a.w=%v b.w=%v", a.W, b.W)
	}
	if !(c.Y > b.Y) {
		t.Fatalf("the third item wraps to a second row: b.y=%v c.y=%v", b.Y, c.Y)
	}
	if !near(c.Y-a.Y, 40.0) {
		t.Fatalf("the rows are 40px tall: a.y=%v c.y=%v", a.Y, c.Y)
	}
	if !near(c.X, a.X) {
		t.Fatalf("the wrapped item is back in column one: c.x=%v a.x=%v", c.X, a.X)
	}
	g := boxFor(t, l, doc, "#g")
	if !near(g.H, 80.0) {
		t.Fatalf("two 40px rows make an 80px container, got %v", g.H)
	}
}

func TestGridFrTracksShareTheFreeSpace(t *testing.T) {
	// Oracle: tests/grid.rs grid_fr_tracks_share_the_free_space.
	doc, _, l := cascade(t, `<style>
  #g { display: grid; grid-template-columns: 1fr 2fr; width: 600px; }
  #g div { height: 40px; }
</style><section id="g"><div id="a">A</div><div id="b">B</div></section>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	if !near(a.Y, b.Y) {
		t.Fatalf("the items share a row: a.y=%v b.y=%v", a.Y, b.Y)
	}
	if !near(a.W, 200.0) {
		t.Fatalf("1fr of 600px is 200px wide, got %v", a.W)
	}
	if !near(b.W, 400.0) {
		t.Fatalf("2fr of 600px is 400px wide, got %v", b.W)
	}
	if !near(b.X-(a.X+a.W), 0.0) {
		t.Fatalf("the fr tracks touch: a ends at %v and b starts at %v", a.X+a.W, b.X)
	}
}

func TestGridAutoTrackSizesToItsContent(t *testing.T) {
	// Oracle: tests/grid.rs grid_auto_track_sizes_to_its_content.
	doc, _, l := cascade(t, `<style>
  #g { display: grid; grid-template-columns: 100px auto; width: 600px; }
  #g div { height: 40px; }
</style><section id="g"><div id="a">A</div><div id="b">B</div><div id="c">C</div></section>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	c := boxFor(t, l, doc, "#c")
	if !near(a.W, 100.0) {
		t.Fatalf("the px track stays 100px wide, got %v", a.W)
	}
	if !near(a.Y, b.Y) {
		t.Fatalf("the first two items share a row: a.y=%v b.y=%v", a.Y, b.Y)
	}
	if !near(b.X-(a.X+a.W), 0.0) {
		t.Fatalf("the auto track starts where the px track ends: %v vs %v", b.X, a.X+a.W)
	}
	if !(b.W > 0.0 && b.W < 150.0) {
		t.Fatalf("the auto track fits its short content instead of stretching, got %v", b.W)
	}
	if !(c.Y > b.Y && near(c.X, a.X)) {
		t.Fatalf("the third item wraps to the first column: c=(%v,%v) a.x=%v", c.X, c.Y, a.X)
	}
}

func TestGridGapSeparatesTracksAndRows(t *testing.T) {
	// Oracle: tests/grid.rs grid_gap_separates_tracks_and_rows.
	doc, _, l := cascade(t, `<style>
  #g { display: grid; grid-template-columns: 100px 100px; gap: 20px; width: 600px; }
  #g div { height: 40px; }
</style><section id="g"><div id="a">A</div><div id="b">B</div><div id="c">C</div></section>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	c := boxFor(t, l, doc, "#c")
	if !near(b.X-(a.X+a.W), 20.0) {
		t.Fatalf("the column gap is 20px, got %v", b.X-(a.X+a.W))
	}
	if !(c.Y > b.Y) {
		t.Fatalf("the third item wraps: b.y=%v c.y=%v", b.Y, c.Y)
	}
	if !near(c.Y-(a.Y+a.H), 20.0) {
		t.Fatalf("the row gap is 20px, got %v", c.Y-(a.Y+a.H))
	}
}

func TestGridColumnSpanCoversTwoTracks(t *testing.T) {
	// Oracle: tests/grid.rs grid_column_span_covers_two_tracks.
	doc, _, l := cascade(t, `<style>
  #g { display: grid; grid-template-columns: 100px 100px 100px; width: 600px; }
  #g div { height: 40px; }
  #a { grid-column: span 2; }
</style><section id="g"><div id="a">A</div><div id="b">B</div><div id="c">C</div></section>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	c := boxFor(t, l, doc, "#c")
	if !near(a.W, 200.0) {
		t.Fatalf("span 2 over 100px tracks is 200px wide, got %v", a.W)
	}
	if !near(a.Y, b.Y) {
		t.Fatalf("the spanned item shares its row with B: a.y=%v b.y=%v", a.Y, b.Y)
	}
	if !near(b.X-(a.X+a.W), 0.0) {
		t.Fatalf("B sits in the third track: a ends at %v and b starts at %v", a.X+a.W, b.X)
	}
	if !near(b.W, 100.0) {
		t.Fatalf("the remaining track stays 100px wide, got %v", b.W)
	}
	if !(c.Y > b.Y && near(c.X, a.X)) {
		t.Fatalf("the third item wraps to the first column: c=(%v,%v) a.x=%v", c.X, c.Y, a.X)
	}
}

func TestGridBlockPagesWithoutGridStillStack(t *testing.T) {
	// Oracle: tests/grid.rs block_pages_without_grid_still_stack.
	doc, _, l := cascade(t, `<style>
  #a { height: 30px; }
  #b { height: 40px; }
</style><section id="plain"><div id="a">A</div><div id="b">B</div></section>`)
	a := boxFor(t, l, doc, "#a")
	b := boxFor(t, l, doc, "#b")
	if !(b.Y > a.Y) {
		t.Fatalf("blocks stack vertically: a.y=%v b.y=%v", a.Y, b.Y)
	}
	if !near(b.Y-a.Y, 30.0) {
		t.Fatalf("the first 30px block sets the gap: a.y=%v b.y=%v", a.Y, b.Y)
	}
	if !near(a.X, b.X) {
		t.Fatalf("blocks share the left edge: a.x=%v b.x=%v", a.X, b.X)
	}
}

func TestGridNestedFlexItemUsesItsCellWidth(t *testing.T) {
	// Oracle: tests/grid.rs nested_flex_item_uses_its_cell_width (R3-4 pin).
	doc, _, l := cascade(t, `<style>
  body { margin: 0; }
  #grid { display: grid; grid-template-columns: 200px 1fr; }
  #cellflex { display: flex; }
</style><div id="grid"><div id="cellflex"><b>inner</b></div><p>right</p></div>`)
	inner := boxFor(t, l, doc, "#cellflex")
	if diff := inner.W - 200.0; diff < -2.0 || diff > 2.0 {
		t.Fatalf("the nested flex box must take its 200px cell, got width %v", inner.W)
	}
}
