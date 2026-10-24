// The css.NodeIndex view over the parsed DOM: the compute entry point only
// needs this interface, so the CLI adapts the document instead of the CSS
// layer reaching into the DOM package.
package main

import (
	"strings"

	"browser-lite/css"
	"browser-lite/dom"
)

type domIndex struct {
	doc *dom.Document
}

func (d domIndex) Len() int { return len(d.doc.Nodes) }

func (d domIndex) Root() int { return int(d.doc.Root) }

func (d domIndex) IsText(id int) bool { return d.doc.Nodes[id].IsText }

func (d domIndex) Tag(id int) string {
	n := &d.doc.Nodes[id]
	if n.IsText {
		return ""
	}
	return strings.ToLower(n.Tag)
}

func (d domIndex) Attr(id int, name string) (string, bool) {
	return d.doc.Nodes[id].Attr(strings.ToLower(name))
}

func (d domIndex) Parent(id int) (int, bool) {
	n := &d.doc.Nodes[id]
	if !n.HasParent {
		return 0, false
	}
	return int(n.Parent), true
}

func (d domIndex) Children(id int) []int {
	src := d.doc.Nodes[id].Children
	out := make([]int, len(src))
	for i, c := range src {
		out[i] = int(c)
	}
	return out
}

func (d domIndex) TextContent(id int) string {
	return d.doc.TextContent(dom.NodeID(id))
}

func (d domIndex) QuerySelectorAll(selector string) []int {
	src := d.doc.QuerySelectorAll(selector)
	out := make([]int, len(src))
	for i, c := range src {
		out[i] = int(c)
	}
	return out
}

var _ css.NodeIndex = domIndex{}
