package dom_test

import (
	"testing"

	"browser-lite/dom"
)

// Mirrors the Rust smoke test dom_builds_html5_tree: the HTML5 parser must
// imply html/head/body around a bare fragment.
func TestParseBuildsHTML5Tree(t *testing.T) {
	doc := dom.Parse("<!doctype html><title>t</title><h1>Hi</h1><p>x</p>")
	for _, sel := range []string{"html", "head", "body", "h1", "title"} {
		if _, ok := doc.QuerySelector(sel); !ok {
			t.Fatalf("selector %q matched nothing", sel)
		}
	}
	h1, _ := doc.QuerySelector("h1")
	if got := doc.TextContent(h1); got != "Hi" {
		t.Fatalf("h1 text = %q, want %q", got, "Hi")
	}
}

// Mirrors the oracle's parse-time rule (src/dom.rs walk_children takes
// lexbor's local name, which arrives already lowercased): foreign-element
// tags with spec camelCase (linearGradient, radialGradient, foreignObject)
// are stored lowercase, so the --dump text matches the oracle byte for
// byte (fixture probe-svg-gradient).
func TestParseLowercasesForeignElementTags(t *testing.T) {
	doc := dom.Parse(`<svg xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="lin"><stop/></linearGradient><radialGradient id="rad"/></defs><foreignObject><div>x</div></foreignObject></svg>`)
	for _, want := range []string{"lineargradient", "radialgradient", "foreignobject", "stop", "defs"} {
		found := false
		for _, n := range doc.Nodes {
			if !n.IsText && n.Tag == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no stored tag %q (foreign-element case not lowercased)", want)
		}
	}
	// The camelCase spellings must not survive anywhere in the arena.
	for _, n := range doc.Nodes {
		if !n.IsText && (n.Tag == "linearGradient" || n.Tag == "radialGradient" || n.Tag == "foreignObject") {
			t.Fatalf("stored tag %q keeps foreign camelCase", n.Tag)
		}
	}
}

func TestTreeParentAndChildren(t *testing.T) {
	doc := dom.Parse("<body><p>one</p><p>two</p></body>")
	if doc.Nodes[doc.Root].Tag != "#document" {
		t.Fatalf("root tag = %q, want %q", doc.Nodes[doc.Root].Tag, "#document")
	}
	if doc.Nodes[doc.Root].HasParent {
		t.Fatalf("root must have no parent")
	}
	body, ok := doc.QuerySelector("body")
	if !ok {
		t.Fatal("no body element")
	}
	htmlEl, ok := doc.QuerySelector("html")
	if !ok {
		t.Fatal("no html element")
	}
	if !doc.Nodes[htmlEl].HasParent || doc.Nodes[htmlEl].Parent != doc.Root {
		t.Fatalf("html parent = %v (present=%v), want root %v",
			doc.Nodes[htmlEl].Parent, doc.Nodes[htmlEl].HasParent, doc.Root)
	}
	if !doc.Nodes[body].HasParent || doc.Nodes[body].Parent != htmlEl {
		t.Fatalf("body parent = %v (present=%v), want html %v",
			doc.Nodes[body].Parent, doc.Nodes[body].HasParent, htmlEl)
	}
	ps := doc.QuerySelectorAll("p")
	if len(ps) != 2 {
		t.Fatalf("found %d <p> elements, want 2", len(ps))
	}
	for _, id := range ps {
		if !doc.Nodes[id].HasParent || doc.Nodes[id].Parent != body {
			t.Fatalf("p parent = %v, want body %v", doc.Nodes[id].Parent, body)
		}
	}
	// Children of body, in document order, are exactly the two paragraphs.
	children := doc.Nodes[body].Children
	if len(children) != 2 || children[0] != ps[0] || children[1] != ps[1] {
		t.Fatalf("body children = %v, want %v", children, ps)
	}
}

func TestTextNodes(t *testing.T) {
	doc := dom.Parse("<p>hello <b>li</b>te</p>")
	p, ok := doc.QuerySelector("p")
	if !ok {
		t.Fatal("no p element")
	}
	if got := doc.TextContent(p); got != "hello lite" {
		t.Fatalf("p text = %q, want %q", got, "hello lite")
	}
	// At least one child of <p> is a text node carrying its text.
	found := false
	for _, c := range doc.Nodes[p].Children {
		n := doc.Nodes[c]
		if n.IsText {
			if n.Tag != "#text" {
				t.Fatalf("text node tag = %q, want %q", n.Tag, "#text")
			}
			if n.Text == "" {
				t.Fatalf("text node has empty text")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no text-node child under <p>")
	}
}

func TestAttributes(t *testing.T) {
	doc := dom.Parse(`<div id="main" class="a b"><p title="hi">x</p></div>`)
	div, ok := doc.QuerySelector("div")
	if !ok {
		t.Fatal("no div element")
	}
	if v, ok := doc.Nodes[div].Attr("id"); !ok || v != "main" {
		t.Fatalf("id attr = %q,%v; want main,true", v, ok)
	}
	if v, ok := doc.Nodes[div].Attr("class"); !ok || v != "a b" {
		t.Fatalf("class attr = %q,%v; want 'a b',true", v, ok)
	}
	if _, ok := doc.Nodes[div].Attr("missing"); ok {
		t.Fatal("missing attr reported present")
	}
}

func TestQuerySelectorSubset(t *testing.T) {
	html := `<div id="outer" class="wrap"><p id="a" class="x y">one</p>` +
		`<section><p id="b" class="y">two</p></section></div><p id="c">three</p>`
	doc := dom.Parse(html)
	cases := []struct {
		selector string
		wantIDs  []string // expected element ids in document order; empty = no match
	}{
		{"#a", []string{"a"}},
		{"#missing", nil},
		{"p", []string{"a", "b", "c"}},
		{".y", []string{"a", "b"}},
		{"p.y", []string{"a", "b"}},
		{"p.x", []string{"a"}},
		{"div p", []string{"a", "b"}},
		{"div .y", []string{"a", "b"}},
		{"section p", []string{"b"}},
		{"div > p", []string{"a", "b"}}, // Rust treats ">" as a descendant separator
		{"DIV", []string{"outer"}},
		{"div.wrap #b", []string{"b"}},
		{".x.y", []string{"a"}},
		{"span", nil},
		{"", nil},
	}
	for _, tc := range cases {
		got := doc.QuerySelectorAll(tc.selector)
		var gotIDs []string
		for _, id := range got {
			v, _ := doc.Nodes[id].Attr("id")
			gotIDs = append(gotIDs, v)
		}
		if len(gotIDs) != len(tc.wantIDs) {
			t.Errorf("selector %q matched ids %v, want %v", tc.selector, gotIDs, tc.wantIDs)
			continue
		}
		for i := range gotIDs {
			if gotIDs[i] != tc.wantIDs[i] {
				t.Errorf("selector %q matched ids %v, want %v", tc.selector, gotIDs, tc.wantIDs)
				break
			}
		}
		// QuerySelector returns the first match (or nothing).
		id, ok := doc.QuerySelector(tc.selector)
		if len(tc.wantIDs) == 0 {
			if ok {
				t.Errorf("selector %q: QuerySelector matched %v, want nothing", tc.selector, id)
			}
		} else if !ok {
			t.Errorf("selector %q: QuerySelector matched nothing, want #%s", tc.selector, tc.wantIDs[0])
		} else if v, _ := doc.Nodes[id].Attr("id"); v != tc.wantIDs[0] {
			t.Errorf("selector %q: QuerySelector matched #%s, want #%s", tc.selector, v, tc.wantIDs[0])
		}
	}
}

func TestQuerySelectorSkipsTextNodes(t *testing.T) {
	doc := dom.Parse("<p>plain</p>")
	if got := doc.QuerySelectorAll("#text"); len(got) != 0 {
		t.Fatalf("#text matched %d nodes, want 0 (text nodes never match)", len(got))
	}
	if _, ok := doc.QuerySelector("plain"); ok {
		t.Fatal("text content matched as a tag selector")
	}
}

func TestClassMatchingIsWhitespaceSeparated(t *testing.T) {
	doc := dom.Parse(`<p class="  a   b ">x</p><p class="ab">y</p>`)
	got := doc.QuerySelectorAll(".a")
	if len(got) != 1 {
		t.Fatalf(".a matched %d nodes, want 1", len(got))
	}
	if got := doc.QuerySelectorAll(".ab"); len(got) != 1 {
		t.Fatalf(".ab matched %d nodes, want 1", len(got))
	}
}
