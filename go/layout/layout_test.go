// Tests for the block layout (go/layout), mirroring the block path of the
// Rust oracle (src/layout.rs: layout_node, content_box, flush_text,
// layout_image) and the block-stacking/geometry assertions of the Rust
// suites. Table-driven; geometry is asserted exactly except where the
// oracle is float-based.
package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"browser-lite/css"
	"browser-lite/dom"
	"browser-lite/img"
)

// domIndex adapts the wave-1 DOM to the css NodeIndex interface so layout
// tests drive the real cascade.
type domIndex struct{ doc *dom.Document }

func (d domIndex) Len() int           { return len(d.doc.Nodes) }
func (d domIndex) Root() int          { return int(d.doc.Root) }
func (d domIndex) IsText(id int) bool { return d.doc.Nodes[id].IsText }
func (d domIndex) Tag(id int) string {
	n := &d.doc.Nodes[id]
	if n.IsText {
		return ""
	}
	return strings.ToLower(n.Tag)
}
func (d domIndex) Attr(id int, name string) (string, bool) {
	return d.doc.Nodes[id].Attr(name)
}
func (d domIndex) Parent(id int) (int, bool) {
	n := &d.doc.Nodes[id]
	if !n.HasParent {
		return 0, false
	}
	return int(n.Parent), true
}
func (d domIndex) Children(id int) []int {
	out := make([]int, 0, len(d.doc.Nodes[id].Children))
	for _, c := range d.doc.Nodes[id].Children {
		out = append(out, int(c))
	}
	return out
}
func (d domIndex) TextContent(id int) string { return d.doc.TextContent(dom.NodeID(id)) }
func (d domIndex) QuerySelectorAll(selector string) []int {
	out := []int{}
	for _, id := range d.doc.QuerySelectorAll(selector) {
		out = append(out, int(id))
	}
	return out
}

// cascade parses html, runs the real libcss cascade at width 1000 and lays
// out, mirroring the Rust suites' load() helper.
func cascade(t *testing.T, html string) (*dom.Document, []css.Style, *Layout) {
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

// boxFor finds the layout box of the node matching selector.
func boxFor(t *testing.T, l *Layout, doc *dom.Document, selector string) *LayoutBox {
	t.Helper()
	id, ok := doc.QuerySelector(selector)
	if !ok {
		t.Fatalf("fixture has no %s", selector)
	}
	for i := range l.Boxes {
		if l.Boxes[i].Node == id {
			return &l.Boxes[i]
		}
	}
	t.Fatalf("no box for %s", selector)
	return nil
}

// textIn returns the #text boxes whose vertical centre falls inside block,
// in document order (mirrors the oracle test helper).
func textIn(l *Layout, block *LayoutBox) []*LayoutBox {
	var out []*LayoutBox
	for i := range l.Boxes {
		b := &l.Boxes[i]
		if b.Tag != "#text" {
			continue
		}
		center := b.Y + b.H/2
		if center >= block.Y && center < block.Y+block.H {
			out = append(out, b)
		}
	}
	return out
}

func TestContentBoxMath(t *testing.T) {
	// Exact port of content_box (src/layout.rs:320): margins offset the
	// border box, padding and borders live INSIDE w, h grows with top and
	// bottom border like padding (T11).
	doc := dom.Parse(`<div id="a" style="margin: 10px 20px 30px 40px; padding: 1px 2px 3px 4px; width: 500px; border: 5px solid black">x</div>`)
	idx := domIndex{doc}
	sheets := css.Stylesheets(idx)
	styles, _, err := css.Compute(idx, sheets, 1000)
	if err != nil {
		t.Fatalf("css.Compute: %v", err)
	}
	l := Render(doc, styles, 1000)
	body := boxFor(t, l, doc, "body")
	b := boxFor(t, l, doc, "#a")
	// border_x = body content + margin-left(40); the body carries the UA
	// 8px margin, so its content starts at body.X.
	if b.X != body.X+40 {
		t.Fatalf("border x = %v, want body %v + margin-left 40", b.X, body.X)
	}
	if b.W != 506 {
		t.Fatalf("border w = %v, want 506 (width + padding)", b.W)
	}
	// Content offset: border edge + border-width(5) + padding(4).
	cx := b.X + 5 + 4
	if len(textIn(l, b)) == 0 {
		t.Fatalf("expected a #text line inside #a")
	}
	line := textIn(l, b)[0]
	if line.X != cx {
		t.Fatalf("content x = %v, want %v", line.X, cx)
	}
	if line.X+line.W > b.X+b.W+0.5 {
		t.Fatalf("line escapes the border box: %v+%v > %v", line.X, line.W, b.X+b.W)
	}
}

func TestBlockStacking(t *testing.T) {
	doc, _, l := cascade(t, `<div id="one">first</div><div id="two">second</div>`)
	body := boxFor(t, l, doc, "body")
	one := boxFor(t, l, doc, "#one")
	two := boxFor(t, l, doc, "#two")
	if one.Y != body.Y {
		t.Fatalf("first block y = %v, want body content top %v", one.Y, body.Y)
	}
	if two.Y != one.Y+one.H {
		t.Fatalf("second block y = %v, want %v (stacked)", two.Y, one.Y+one.H)
	}
	if l.Width != 1000 {
		t.Fatalf("layout width = %v, want viewport 1000", l.Width)
	}
	if l.Height < two.Y+two.H {
		t.Fatalf("layout height %v does not cover content %v", l.Height, two.Y+two.H)
	}
}

func TestDisplayNoneEmitsNoBox(t *testing.T) {
	doc, _, l := cascade(t, `<div id="hide" style="display: none">gone</div><div id="show">here</div>`)
	if _, ok := doc.QuerySelector("#hide"); !ok {
		t.Fatalf("fixture has no #hide")
	}
	hideID, _ := doc.QuerySelector("#hide")
	for _, b := range l.Boxes {
		if b.Node == hideID {
			t.Fatalf("display:none node must emit no box")
		}
	}
	show := boxFor(t, l, doc, "#show")
	body := boxFor(t, l, doc, "body")
	if show.Y != body.Y {
		t.Fatalf("visible block after display:none starts at %v, want body top %v", show.Y, body.Y)
	}
}

func TestAnonymousTextBoxes(t *testing.T) {
	doc, _, l := cascade(t, `<div id="para">hello <b>bold</b> world</div><div id="blank">   </div>`)
	para := boxFor(t, l, doc, "#para")
	lines := textIn(l, para)
	if len(lines) != 1 {
		t.Fatalf("one short run should flush one #text box, got %d", len(lines))
	}
	if lines[0].Text != "hello bold world" {
		t.Fatalf("run text = %q, want joined inline text", lines[0].Text)
	}
	if lines[0].Node != NoNode {
		t.Fatalf("anonymous box node = %v, want the anonymous sentinel", lines[0].Node)
	}
	blank := boxFor(t, l, doc, "#blank")
	if got := textIn(l, blank); len(got) != 0 {
		t.Fatalf("whitespace-only run must emit no #text boxes, got %d", len(got))
	}
	if blank.H != 0 {
		t.Fatalf("empty block h = %v, want 0", blank.H)
	}
	if len(l.Runs) != 1 {
		t.Fatalf("expected one TextRun for the painter, got %d", len(l.Runs))
	}
	if l.Runs[0].Text != "hello bold world" {
		t.Fatalf("run text = %q", l.Runs[0].Text)
	}
}

func TestImageBoxes(t *testing.T) {
	dir, err := filepath.Abs("../../test/data")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	doc := dom.Parse(`<img id="sized" src="img-red.png" width="30"><img id="broken" src="no-such-file.png">`)
	styles := make([]css.Style, len(doc.Nodes))
	for i := range styles {
		styles[i] = css.New()
	}
	// The cascade blockifies the root elements (libcss UA sheet); the
	// hand-built styles must do the same or nothing lays out.
	for _, sel := range []string{"html", "body"} {
		if id, ok := doc.QuerySelector(sel); ok {
			styles[id].Display = css.DisplayBlock
		}
	}
	images := img.Load(doc, dir)
	l := RenderWithImages(doc, styles, 1000, images)
	sized := boxFor(t, l, doc, "#sized")
	// img-red.png is 4x3: width 30 keeps the aspect ratio.
	if sized.W != 30 || sized.H != 22.5 {
		t.Fatalf("sized image box = %vx%v, want 30x22.5", sized.W, sized.H)
	}
	if sized.Image == nil {
		t.Fatalf("sized image box must carry decoded pixels")
	}
	broken := boxFor(t, l, doc, "#broken")
	if broken.W != 0 || broken.H != 0 {
		t.Fatalf("broken image box = %vx%v, want 0x0", broken.W, broken.H)
	}
}

func TestTextAlignCenterIntegration(t *testing.T) {
	raw, err := os.ReadFile("../../test/data/text-align.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doc, _, l := cascade(t, string(raw))
	left := textIn(l, boxFor(t, l, doc, "#l"))
	center := textIn(l, boxFor(t, l, doc, "#c"))
	right := textIn(l, boxFor(t, l, doc, "#r"))
	wide := boxFor(t, l, doc, "#wide")
	if len(left) == 0 || len(center) == 0 || len(right) == 0 {
		t.Fatalf("alignment blocks must wrap text (l=%d c=%d r=%d)", len(left), len(center), len(right))
	}
	if center[0].X <= left[0].X+10 {
		t.Fatalf("centred line should start right of the left one (left=%v center=%v)", left[0].X, center[0].X)
	}
	edge := wide.X + wide.W
	if right[0].X+right[0].W < edge-1.0 {
		t.Fatalf("right line should end at the content edge (end=%v edge=%v)", right[0].X+right[0].W, edge)
	}
}

func TestWrappedTextStaysInside(t *testing.T) {
	raw, err := os.ReadFile("../../test/data/text-wrap.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doc, _, l := cascade(t, string(raw))
	narrow := boxFor(t, l, doc, "#narrow")
	lines := textIn(l, narrow)
	if len(lines) < 3 {
		t.Fatalf("narrow box should wrap into several lines, got %d", len(lines))
	}
	for _, line := range lines {
		if line.X < narrow.X-0.5 || line.X+line.W > narrow.X+narrow.W+0.5 {
			t.Fatalf("line %q escapes the content box", line.Text)
		}
	}
}

func TestLineHeightIntegration(t *testing.T) {
	raw, err := os.ReadFile("../../test/data/text-line-height.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doc, _, l := cascade(t, string(raw))
	tight := boxFor(t, l, doc, ".tight")
	loose := boxFor(t, l, doc, ".loose")
	if loose.H <= tight.H*1.5 {
		t.Fatalf("line-height 2 should clearly exceed 1 (tight=%v loose=%v)", tight.H, loose.H)
	}
}

func TestNaturalLineHeightRippleIncludesOnlyContentHeight(t *testing.T) {
	doc, _, l := cascade(t, `<style>
		html, body { margin: 0; padding: 0 }
		body { font-family: "Noto Sans", sans-serif; font-size: 16px; line-height: normal }
		.card { width: 220px; padding: 7px 5px; border: 2px solid }
		#fixed { line-height: 1.2 }
	</style><div id="normal" class="card">Measure this card</div><div id="fixed" class="card">Measure this card</div>`)
	normal := boxFor(t, l, doc, "#normal")
	fixed := boxFor(t, l, doc, "#fixed")
	if normal.W != fixed.W {
		t.Fatalf("same padded card width changed: normal=%v fixed=%v", normal.W, fixed.W)
	}
	if delta := normal.H - fixed.H; delta < 2 || delta > 4 {
		t.Fatalf("natural line-height should add only its content-height delta (2-4px), got normal=%v fixed=%v delta=%v", normal.H, fixed.H, delta)
	}
}

func TestSpecifiedHeightCapsNaturalTextGrowthInBothBoxSizingModes(t *testing.T) {
	doc, _, l := cascade(t, `<style>
		html, body { margin: 0; padding: 0 }
		body { font-family: "Noto Sans", sans-serif; font-size: 16px; line-height: normal }
		#content-box { box-sizing: content-box; height: 20px; padding: 4px 0; border: 1px solid }
		#border-box { box-sizing: border-box; height: 30px; padding: 4px 0; border: 1px solid }
	</style><div id="content-box">Text may overflow</div><div id="border-box">Text may overflow</div>`)
	contentBox := boxFor(t, l, doc, "#content-box")
	borderBox := boxFor(t, l, doc, "#border-box")
	if contentBox.H != 30 {
		t.Errorf("content-box height = %v, want declared 20px content + 8px padding + 2px border = 30px", contentBox.H)
	}
	if borderBox.H != 30 {
		t.Errorf("border-box height = %v, want declared 30px border box", borderBox.H)
	}
	if borderBox.Y != contentBox.Y+contentBox.H {
		t.Errorf("following box y = %v, want fixed content-box bottom %v", borderBox.Y, contentBox.Y+contentBox.H)
	}
}

func TestFlexRowDoesNotStackItemsAsBlock(t *testing.T) {
	// T4b owns flex: a row container shares one row instead of stacking
	// its items like the old plain-block fallback did.
	doc, _, l := cascade(t, `<div id="f" style="display: flex"><div id="i1">one</div><div id="i2">two</div></div>`)
	i1 := boxFor(t, l, doc, "#i1")
	i2 := boxFor(t, l, doc, "#i2")
	if i1.Y != i2.Y {
		t.Fatalf("flex row must share one row, not stack (i1.y=%v i2.y=%v)", i1.Y, i2.Y)
	}
	if !(i1.X < i2.X) {
		t.Fatalf("flex row lays out left to right (i1.x=%v i2.x=%v)", i1.X, i2.X)
	}
}
