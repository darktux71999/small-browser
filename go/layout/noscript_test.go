// Tests for the noscript layout policy (leading-fidelity S2,
// odd/tasks/leading-fidelity.md): no boxes are produced for <noscript>
// subtrees and no noscript text is collected, in any formatting context.
// With scripting enabled Firefox never lays out or paints a <noscript>'s
// contents, whatever the computed style says; the DOM keeps the parsed
// markup (go/dom pins) and paint has nothing left to draw.
package layout

import (
	"strings"
	"testing"

	"browser-lite/dom"
)

// noscriptSubtree lists the ids of the noscript element and every
// descendant node, in document order.
func noscriptSubtree(t *testing.T, doc *dom.Document) []dom.NodeID {
	t.Helper()
	ns, ok := doc.QuerySelector("noscript")
	if !ok {
		t.Fatal("fixture has no noscript element")
	}
	var out []dom.NodeID
	var walk func(dom.NodeID)
	walk = func(id dom.NodeID) {
		out = append(out, id)
		for _, c := range doc.Nodes[id].Children {
			walk(c)
		}
	}
	walk(ns)
	return out
}

func TestNoscriptSubtreeProducesNoBoxes(t *testing.T) {
	cases := []struct {
		name string
		html string
		// wantGap is the expected #b.Y - #a.Y (100px each, the skipped
		// noscript between them taking zero space), 0 to skip.
		wantGap float64
	}{
		{
			// The default inline noscript: its text must not join the
			// parent block's inline run.
			name: "block flow inline noscript",
			html: `<div id="a" style="height: 100px">a</div>` +
				`<noscript><div style="height: 100px">No JS banner</div></noscript>` +
				`<div id="b" style="height: 100px">b</div>`,
			wantGap: 100,
		},
		{
			// An author-blockified noscript: layoutNode must skip the
			// subtree itself, not just its text.
			name: "block flow block noscript",
			html: `<style>noscript { display: block }</style>` +
				`<div id="a" style="height: 100px">a</div>` +
				`<noscript><div style="height: 100px">No JS banner</div></noscript>` +
				`<div id="b" style="height: 100px">b</div>`,
			wantGap: 100,
		},
		{
			name: "flex container item",
			html: `<style>#f { display: flex; width: 600px } noscript { display: block }</style>` +
				`<div id="f"><div id="a" style="width: 100px; height: 40px">A</div>` +
				`<noscript><div style="width: 100px; height: 100px">No JS banner</div></noscript></div>`,
		},
		{
			name: "flex container inline noscript",
			html: `<style>#f { display: flex; width: 600px }</style>` +
				`<div id="f"><div id="a" style="width: 100px; height: 40px">A</div>` +
				`<noscript><div style="width: 100px; height: 100px">No JS banner</div></noscript></div>`,
		},
		{
			name: "grid container item",
			html: `<style>#g { display: grid; width: 600px } noscript { display: block }</style>` +
				`<div id="g"><div id="a" style="height: 40px">A</div>` +
				`<noscript><div style="height: 100px">No JS banner</div></noscript></div>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, l := cascade(t, tc.html)

			// Nothing from the noscript subtree paints: no box may carry
			// one of its node ids, no text box its contents.
			for _, id := range noscriptSubtree(t, doc) {
				for i := range l.Boxes {
					if l.Boxes[i].Node == id {
						t.Fatalf("noscript-subtree node %d (%s) produced box %d",
							id, doc.Nodes[id].Tag, i)
					}
				}
			}
			for i := range l.Boxes {
				if strings.Contains(l.Boxes[i].Text, "No JS") {
					t.Fatalf("noscript text painted in box %d: %q", i, l.Boxes[i].Text)
				}
			}

			// The skip takes zero space: #b stacks directly under #a.
			if tc.wantGap > 0 {
				a := boxFor(t, l, doc, "#a")
				b := boxFor(t, l, doc, "#b")
				if b.Y-a.Y != tc.wantGap {
					t.Fatalf("#b.Y - #a.Y = %v, want %v (the skipped noscript takes no space)",
						b.Y-a.Y, tc.wantGap)
				}
			}
		})
	}
}

func TestLayoutKeepsRenderingNonNoscriptContent(t *testing.T) {
	// Preserve pin: the same page without the noscript element lays out
	// identically to the one with it — the skip rule must not disturb the
	// siblings before or after.
	withDoc, _, withLayout := cascade(t,
		`<div id="a" style="height: 100px">a</div>`+
			`<noscript><div style="height: 100px">No JS banner</div></noscript>`+
			`<div id="b" style="height: 100px">b</div>`)
	bareDoc, _, bareLayout := cascade(t,
		`<div id="a" style="height: 100px">a</div>`+
			`<div id="b" style="height: 100px">b</div>`)
	if len(withLayout.Boxes) != len(bareLayout.Boxes) {
		t.Fatalf("boxes with noscript %d vs without %d", len(withLayout.Boxes), len(bareLayout.Boxes))
	}
	// Node ids are not compared: the noscript subtree shifts the numbering
	// of every following node, so only the visible shape must match.
	for i := range withLayout.Boxes {
		a, b := withLayout.Boxes[i], bareLayout.Boxes[i]
		if a.Tag != b.Tag || a.Text != b.Text || a.X != b.X || a.Y != b.Y || a.W != b.W || a.H != b.H {
			t.Fatalf("box %d differs:\n with noscript: %+v\n without:        %+v", i, a, b)
		}
	}
	if withLayout.Height != bareLayout.Height {
		t.Fatalf("page height with noscript %v vs without %v", withLayout.Height, bareLayout.Height)
	}
	// Sanity: both documents really did lay out a and b.
	boxFor(t, withLayout, withDoc, "#a")
	boxFor(t, withLayout, withDoc, "#b")
	boxFor(t, bareLayout, bareDoc, "#a")
	boxFor(t, bareLayout, bareDoc, "#b")
}
