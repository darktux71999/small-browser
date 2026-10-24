// Package dom builds the engine's mutable DOM tree from an HTML5 parse.
//
// It mirrors the Rust oracle (src/dom.rs): a spec-compliant parser owns tree
// construction (lexbor there, golang.org/x/net/html here) while this package
// owns the arena of stable node ids that layout and JS hold on to. Node ids
// are document order; the root is always the "#document" node.
//
// The supported selector subset matches the oracle exactly: `tag`, `#id`,
// `.class`, `tag.class`, and descendant chains separated by spaces. A ">"
// token is accepted and treated as a descendant separator, like the oracle.
package dom

import (
	"strings"

	"golang.org/x/net/html"
)

// NodeID identifies a node in a Document. Ids are indexes into
// Document.Nodes in document order.
type NodeID int

// Node is a single element or text node. Element nodes carry the lowercase
// tag name and attributes; text nodes have IsText set, Tag "#text", and the
// raw text. Comments and doctypes are dropped by the parser, like the oracle.
type Node struct {
	Tag       string
	Attrs     map[string]string
	Text      string
	Parent    NodeID
	HasParent bool
	Children  []NodeID
	IsText    bool
}

// Attr returns the attribute value for name, or false when absent.
func (n *Node) Attr(name string) (string, bool) {
	v, ok := n.Attrs[name]
	return v, ok
}

// Document is the parsed page: an arena of nodes plus the root id.
type Document struct {
	Nodes []Node
	Root  NodeID
}

// Parse builds the DOM for html, implying html/head/body around fragments the
// way the HTML5 tree-construction rules require.
func Parse(htmlText string) *Document {
	src, err := html.Parse(strings.NewReader(htmlText))
	doc := &Document{}
	doc.Root = doc.push("#document", false, false, NodeID(0), nil, "")
	if err != nil || src == nil {
		return doc
	}
	for c := src.FirstChild; c != nil; c = c.NextSibling {
		doc.walk(c, doc.Root)
	}
	return doc
}

func (d *Document) push(tag string, isText bool, hasParent bool, parent NodeID, attrs map[string]string, text string) NodeID {
	id := NodeID(len(d.Nodes))
	if attrs == nil {
		attrs = map[string]string{}
	}
	d.Nodes = append(d.Nodes, Node{
		Tag:       tag,
		Attrs:     attrs,
		Text:      text,
		Parent:    parent,
		HasParent: hasParent,
		IsText:    isText,
	})
	if hasParent {
		p := &d.Nodes[parent]
		p.Children = append(p.Children, id)
	}
	return id
}

// walk copies one parsed node and its subtree into the arena. Element and
// text nodes are kept; comments, doctypes, and anything else is skipped,
// like the oracle's walk_children.
func (d *Document) walk(n *html.Node, parent NodeID) {
	switch n.Type {
	case html.ElementNode:
		attrs := make(map[string]string, len(n.Attr))
		for _, a := range n.Attr {
			attrs[a.Key] = a.Val
		}
		id := d.push(n.Data, false, true, parent, attrs, "")
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			d.walk(c, id)
		}
	case html.TextNode:
		d.push("#text", true, true, parent, nil, n.Data)
	}
}

// TextContent concatenates the raw text of id and all its descendants.
func (d *Document) TextContent(id NodeID) string {
	var out strings.Builder
	d.collectText(id, &out)
	return out.String()
}

func (d *Document) collectText(id NodeID, out *strings.Builder) {
	node := &d.Nodes[id]
	if node.IsText {
		out.WriteString(node.Text)
	}
	for _, c := range node.Children {
		d.collectText(c, out)
	}
}

// QuerySelectorAll returns the ids of all element nodes matching selector, in
// document order.
func (d *Document) QuerySelectorAll(selector string) []NodeID {
	chain := parseSelector(selector)
	if len(chain) == 0 {
		return nil
	}
	var out []NodeID
	for id := range d.Nodes {
		nid := NodeID(id)
		if d.Nodes[nid].IsText {
			continue
		}
		if d.matchesChain(nid, chain) {
			out = append(out, nid)
		}
	}
	return out
}

// QuerySelector returns the first element node matching selector, in document
// order, or false when nothing matches.
func (d *Document) QuerySelector(selector string) (NodeID, bool) {
	ids := d.QuerySelectorAll(selector)
	if len(ids) == 0 {
		return 0, false
	}
	return ids[0], true
}

func (d *Document) matchesChain(id NodeID, chain []compound) bool {
	if !d.matchesCompound(id, chain[len(chain)-1]) {
		return false
	}
	// Walk the leading compounds from right to left over strict ancestors.
	var anc NodeID
	hasAnc := false
	if p, ok := d.parentOf(id); ok {
		anc, hasAnc = p, true
	}
	for i := len(chain) - 2; i >= 0; i-- {
		found := false
		for hasAnc {
			if d.matchesCompound(anc, chain[i]) {
				if p, ok := d.parentOf(anc); ok {
					anc, hasAnc = p, true
				} else {
					hasAnc = false
				}
				found = true
				break
			}
			p, ok := d.parentOf(anc)
			anc, hasAnc = p, ok
		}
		if !found {
			return false
		}
	}
	return true
}

func (d *Document) parentOf(id NodeID) (NodeID, bool) {
	n := &d.Nodes[id]
	if !n.HasParent {
		return 0, false
	}
	return n.Parent, true
}

func (d *Document) matchesCompound(id NodeID, c compound) bool {
	node := &d.Nodes[id]
	if node.IsText {
		return false
	}
	if c.tag != "" && !equalFoldASCII(node.Tag, c.tag) {
		return false
	}
	if c.id != "" {
		v, ok := node.Attr("id")
		if !ok || v != c.id {
			return false
		}
	}
	for _, class := range c.classes {
		v, ok := node.Attr("class")
		if !ok {
			return false
		}
		has := false
		for _, field := range strings.Fields(v) {
			if field == class {
				has = true
				break
			}
		}
		if !has {
			return false
		}
	}
	return true
}

// compound is one `tag#id.class` step of a selector chain.
type compound struct {
	tag     string
	id      string
	classes []string
}

// parseSelector splits selector on whitespace, dropping ">" tokens (the
// oracle treats the child combinator as a descendant separator).
func parseSelector(selector string) []compound {
	var chain []compound
	for _, part := range strings.Fields(selector) {
		if part == ">" {
			continue
		}
		var c compound
		end := strings.IndexAny(part, "#.")
		var head, rest string
		if end < 0 {
			head, rest = part, ""
		} else {
			head, rest = part[:end], part[end:]
		}
		if head != "" {
			c.tag = head
		}
		for len(rest) > 0 {
			kind := rest[0]
			tail := rest[1:]
			next := strings.IndexAny(tail, "#.")
			var name string
			if next < 0 {
				name, rest = tail, ""
			} else {
				name, rest = tail[:next], tail[next:]
			}
			switch kind {
			case '#':
				c.id = name
			case '.':
				c.classes = append(c.classes, name)
			}
		}
		chain = append(chain, c)
	}
	return chain
}

// equalFoldASCII reports ASCII case-insensitive equality, like the oracle's
// eq_ignore_ascii_case (Unicode folds do not apply).
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
