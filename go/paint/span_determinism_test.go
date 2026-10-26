package paint_test

import (
	"bytes"
	"testing"

	"browser-lite/css"
	"browser-lite/layout"
	"browser-lite/paint"
	"browser-lite/text"
)

// TestSpanPaintIgnoresRecycledHeapMemory pins the double-print/overlap
// class of defects at the painter: a span the page marks upright must paint
// upright no matter what a previous allocation left in the recycled span
// buffer. The span buffer once left its Pango style slot uninitialized, so
// paint randomly picked up italic (or worse) from heap garbage: the painted
// advances then diverged from the measured ones, doubling glyphs and
// shifting lines over their neighbours.
//
// The test saturates the allocator with italic-span renders (which write
// style=1 into the span buffers before freeing them) and requires later
// upright-span renders to stay pixel-identical to the first one.
func TestSpanPaintIgnoresRecycledHeapMemory(t *testing.T) {
	base := css.Style{
		FontFamilies: []string{"sans-serif"},
		FontFamily:   "Sans",
		FontSize:     20,
		LineHeight:   1.2,
		Color:        css.Color{R: 0, G: 0, B: 0, A: 1},
	}
	spanText := func(italic bool) text.Style {
		return text.Style{
			FontFamilies: []string{"sans-serif"},
			FontFamily:   "Sans",
			FontSize:     20,
			FontWeight:   700,
			Italic:       italic,
			LineHeight:   1.2,
			Color:        text.Color{R: 0, G: 0, B: 0, A: 1},
		}
	}
	runFor := func(italic bool, spans int) layout.TextRun {
		content := "alpha beta gamma delta"
		var ss []text.Span
		for i := 0; i < spans && i*6+3 <= len(content); i++ {
			ss = append(ss, text.Span{Start: i * 6, End: i*6 + 3, Style: spanText(italic)})
		}
		return layout.TextRun{
			X: 8, Y: 8, Width: 384, Text: content,
			Style: base,
			Spans: ss,
		}
	}
	render := func(run layout.TextRun) paint.Frame {
		l := &layout.Layout{Width: 400, Height: 60, Runs: []layout.TextRun{run}}
		f, err := paint.Render(l, nil)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		return f
	}
	upright := runFor(false, 1)
	first := render(upright)
	for round := 0; round < 2; round++ {
		for i := 0; i < 30; i++ {
			render(runFor(true, 1+i%6))
		}
		got := render(upright)
		if first.Width != got.Width || first.Height != got.Height ||
			!bytes.Equal(first.Pixels, got.Pixels) {
			t.Fatalf("upright span paint changed after recycling italic span buffers (round %d)", round)
		}
	}
}
