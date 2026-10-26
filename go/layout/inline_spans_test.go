// Regression pins for the inline-typography span work: layout measurement
// (including the flex/grid anonymous-item probes) must agree with paint on
// each span's x/y/width. Paint rebuilds its Pango layout from the run's
// text, width and spans, so every probe that sizes or places a styled run
// has to shape the same styled text — never the flattened one.
package layout

import (
	"strings"
	"testing"

	"browser-lite/css"
	"browser-lite/dom"
	"browser-lite/text"
)

// styledRun parses html, collects the inline run of the <p> it contains and
// returns the document, styles, run and the paragraph's style for probing.
func styledRun(t *testing.T, html string) (*dom.Document, []css.Style, *inlineRun, *css.Style) {
	t.Helper()
	doc := dom.Parse(html)
	idx := domIndex{doc}
	styles, _, err := css.Compute(idx, css.Stylesheets(idx), 1000)
	if err != nil {
		t.Fatalf("css.Compute: %v", err)
	}
	var pid dom.NodeID = -1
	for id, node := range doc.Nodes {
		if !node.IsText && strings.EqualFold(node.Tag, "p") {
			pid = dom.NodeID(id)
			break
		}
	}
	if pid == -1 {
		t.Fatal("fixture has no <p>")
	}
	var run inlineRun
	for _, child := range doc.Nodes[pid].Children {
		collectInline(doc, styles, child, &run)
	}
	return doc, styles, &run, &styles[pid]
}

// TestAnonMaxContentWidthCountsStyledSpans pins the width probe: an
// anonymous flex/grid item whose run carries a bold span must size from the
// styled advance. The flattened advance is narrower, so sizing from it
// squeezes the item and the painted (styled) line overflows into the next
// column — the footer-columns overlap class of defect.
func TestAnonMaxContentWidthCountsStyledSpans(t *testing.T) {
	s, err := text.New()
	if err != nil {
		t.Fatalf("text.New: %v", err)
	}
	defer s.Close()
	_, styles, run, base := styledRun(t,
		`<style>p { font-size: 20px; }</style>`+
			`<p>alpha beta <b>gamma delta epsilon zeta</b> eta theta</p>`)
	st := &state{shaper: s}
	got := st.anonMaxContentWidth(nil, styles, base, run)

	content, spans := styledInlineText(run.items, base)
	if len(spans) == 0 {
		t.Fatal("fixture produced no spans; the probe has nothing styled to count")
	}
	styled := s.MeasureStyled(content, adaptStyle(base), 1000000, spans)
	want := 0.0
	for _, line := range styled.Lines {
		want = max(want, line.W)
	}
	flat := s.Measure(content, adaptStyle(base), 1000000)
	flatW := 0.0
	for _, line := range flat.Lines {
		flatW = max(flatW, line.W)
	}
	if !(want > flatW) {
		t.Fatalf("fixture is not discriminating: styled %v <= flattened %v", want, flatW)
	}
	if got != want {
		t.Fatalf("anonMaxContentWidth = %v, want styled %v (flattened %v)", got, want, flatW)
	}
}

// TestAnonHeightMatchesStyledFlush pins the height probe across widths: for
// a run with spans, the anonymous-item height must equal the height the
// flush path (and hence paint) actually lays out. A shortfall leaves the
// item box too small and the wrapped line paints over the section below —
// the support-section vertical overlap class of defect.
func TestAnonHeightMatchesStyledFlush(t *testing.T) {
	s, err := text.New()
	if err != nil {
		t.Fatalf("text.New: %v", err)
	}
	defer s.Close()
	_, styles, run, base := styledRun(t,
		`<style>p { font-size: 20px; }</style>`+
			`<p>alpha beta gamma <b>delta epsilon zeta eta theta iota kappa</b> lambda mu</p>`)
	content, spans := styledInlineText(run.items, base)
	if len(spans) == 0 {
		t.Fatal("fixture produced no spans; the probe has nothing styled to count")
	}
	discriminating := false
	for _, width := range []float64{80, 120, 160, 200, 240, 300, 400} {
		scratch := &state{shaper: s, out: &Layout{}}
		cp := &inlineRun{items: append([]inlineItem(nil), run.items...)}
		flushed := scratch.flushInline(nil, styles, base, 0, 0, width, cp)
		probed := (&state{shaper: s}).anonHeight(nil, styles, base,
			&inlineRun{items: append([]inlineItem(nil), run.items...)}, width)
		if flushed != probed {
			t.Errorf("width %v: probe height %v, want flushed height %v", width, probed, flushed)
		}
		flat := s.Measure(content, adaptStyle(base), width).Height
		if flat != flushed {
			discriminating = true
		}
	}
	if !discriminating {
		t.Fatal("fixture never separates styled flush from the flattened measure; it cannot pin the overlap")
	}
}

// TestStyledParagraphEmitsOneRun guards the double-print class of defect at
// the layout level: a styled paragraph must emit exactly one TextRun (never
// the old flattened run plus a new styled one), carrying the spans paint
// needs. Paint draws each run once, so one run means one ink layer.
// This passes on the current tree (no duplication exists); it pins the
// invariant so the reported doubling cannot regress in.
func TestStyledParagraphEmitsOneRun(t *testing.T) {
	_, l := pageWithImages(t,
		`<style>p { width: 580px; font-size: 18px; font-weight: 500; }</style>`+
			`<p>Codeberg is a non-profit, community-led effort that provides services to `+
			`<b>free and open-source projects</b>, such as Git hosting.</p>`)
	if len(l.Runs) != 1 {
		t.Fatalf("styled paragraph emitted %d runs, want exactly 1", len(l.Runs))
	}
	run := &l.Runs[0]
	if len(run.Spans) == 0 {
		t.Fatal("styled paragraph run carries no spans; bold would paint flattened")
	}
	for _, span := range run.Spans {
		if span.Start < 0 || span.End > len(run.Text) || span.End <= span.Start {
			t.Fatalf("span [%d %d) out of range for text of length %d",
				span.Start, span.End, len(run.Text))
		}
	}
}
