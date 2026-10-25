package dom

import (
	"strings"
	"testing"
)

// The oracle parses <noscript> contents as markup: lexbor runs with the
// scripting flag disabled, so children become real elements (src/dom.rs
// walk_children). x/net/html assumes scripting enabled and yields the
// contents as one raw-text node. Mirror the oracle by re-parsing.
func TestParseNoscriptContentsAsMarkup(t *testing.T) {
	const page = `<html><head></head><body><noscript><style>#t { display: flex !important; }</style><div class="x">No JS</div></noscript></body></html>`
	doc := Parse(page)

	ns, ok := doc.QuerySelector("noscript")
	if !ok {
		t.Fatal("noscript element not found")
	}
	children := doc.Nodes[ns].Children
	if len(children) != 2 {
		t.Fatalf("noscript children = %d (%v), want 2 elements", len(children), children)
	}

	style := doc.Nodes[children[0]]
	if style.IsText || style.Tag != "style" {
		t.Fatalf("first child = text=%v tag=%q, want element style", style.IsText, style.Tag)
	}
	if got := doc.TextContent(children[0]); got != "#t { display: flex !important; }" {
		t.Fatalf("style text = %q", got)
	}

	div := doc.Nodes[children[1]]
	if div.IsText || div.Tag != "div" {
		t.Fatalf("second child = text=%v tag=%q, want element div", div.IsText, div.Tag)
	}
	if v, _ := div.Attr("class"); v != "x" {
		t.Fatalf("div class = %q, want %q", v, "x")
	}

	for _, id := range children {
		if doc.Nodes[id].IsText && strings.Contains(doc.Nodes[id].Text, "<") {
			t.Fatal("raw markup leaked through as visible text")
		}
	}
}

// Plain-text noscript contents stay a single text node, as the oracle
// produces (no element to parse).
func TestParseNoscriptPlainTextStaysText(t *testing.T) {
	doc := Parse(`<html><head></head><body><noscript>plain text</noscript></body></html>`)
	ns, ok := doc.QuerySelector("noscript")
	if !ok {
		t.Fatal("noscript element not found")
	}
	children := doc.Nodes[ns].Children
	if len(children) != 1 {
		t.Fatalf("noscript children = %d, want 1", len(children))
	}
	n := doc.Nodes[children[0]]
	if !n.IsText || n.Text != "plain text" {
		t.Fatalf("child = %+v, want text %q", n, "plain text")
	}
}
