package main

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"

	"browser-lite/dom"
	"browser-lite/layout"
)

// dumpDOM prints the DOM tree in the oracle's exact format: the DOM header,
// then one line per node (elements as tag#id.class, text as #text "quoted").
func dumpDOM(out io.Writer, doc *dom.Document) {
	fmt.Fprintln(out, "DOM")
	printNode(out, doc, doc.Root, 0)
}

func printNode(out io.Writer, doc *dom.Document, id dom.NodeID, depth int) {
	node := &doc.Nodes[id]
	pad := strings.Repeat("  ", depth)
	if node.IsText {
		text := strings.TrimSpace(node.Text)
		if text == "" {
			return
		}
		fmt.Fprintf(out, "%s#text %s\n", pad, rustDebug(text))
		return
	}
	idAttr := ""
	if v, ok := node.Attr("id"); ok {
		idAttr = "#" + v
	}
	classAttr := ""
	if v, ok := node.Attr("class"); ok {
		classAttr = "." + strings.ReplaceAll(v, " ", ".")
	}
	fmt.Fprintf(out, "%s%s%s%s\n", pad, node.Tag, idAttr, classAttr)
	for _, child := range node.Children {
		printNode(out, doc, child, depth+1)
	}
}

// dumpLayout prints the LAYOUT header and one line per box in the oracle's
// exact format: two spaces, the [x y w h] rect at %6.1f, and the label.
func dumpLayout(out io.Writer, l *layout.Layout) {
	fmt.Fprintf(out, "LAYOUT %sx%s\n", minimalFloat(l.Width), minimalFloat(math.Ceil(l.Height)))
	for i := range l.Boxes {
		b := &l.Boxes[i]
		label := b.Tag
		if b.Text != "" {
			label = "#text " + rustDebug(b.Text)
		}
		fmt.Fprintf(out, "  [%6.1f %6.1f %6.1f %6.1f] %s\n", b.X, b.Y, b.W, b.H, label)
	}
}

// minimalFloat renders a whole-valued float the way Rust's {} does: 1000,
// not 1000.0.
func minimalFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func ceilFloat(v float64) float64 {
	c := float64(int(v))
	if c < v {
		c++
	}
	// Match math.Ceil without importing math into this file's contract.
	if v != v || v > 1e15 || v < -1e15 {
		return v
	}
	return c
}

// rustDebug renders a string the way Rust's {:?} on str does: double-quoted
// with \\, \", \n, \r, \t, \0 short escapes and \u{...} for other controls
// and non-printables, printable Unicode raw.
func rustDebug(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		case 0:
			b.WriteString("\\0")
		default:
			if r < 0x20 || r == 0x7f || !unicode.IsPrint(r) {
				fmt.Fprintf(&b, "\\u{%x}", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
