// Package svg parses and rasterizes the SVG subset that browser-lite needs
// to paint <img src="x.svg"> and CSS background images: the files a
// codeberg.org page actually references (flat-colour logos, octicon-style
// icons, the stroked Forgejo wordmark).
//
// The pipeline is one pass, no shared state: Parse builds an element tree
// and resolves the stylesheet; Render walks it, flattens every path to
// polylines in user space, strokes them into positive-winding outlines,
// transforms them to device pixels and accumulates anti-aliased coverage
// per element in paint order onto a premultiplied float canvas.
//
// Supported: the document root and nested <svg>/<g> containers with
// viewBox and width/height; path (all commands, arcs and curves flattened),
// rect, circle, ellipse, line, polyline and polygon; fill and stroke as
// solid colours (hex 3/4/6/8, rgb()/rgba(), common named colours,
// transparent, currentColor); fill-opacity, stroke-opacity, opacity
// (multiplied down the tree); fill-rule nonzero/evenodd; stroke-width,
// stroke-linecap round/butt; inherited presentation properties; a <style>
// block with type, .class and type.class selectors; the style attribute;
// transform with translate, scale, rotate, skewX, skewY and matrix; and
// display:none.
//
// Deliberately dropped (each degrades to "not painted", never an error):
//   - gradients: url(#id) paints the average colour of the gradient's stops,
//     ignoring offsets, geometry and spread; the codeberg gradients are
//     either near-transparent (cb-logo) or unused (forgejo wordmark).
//   - clipPath, mask, pattern, marker, filter, symbol and <use>: the
//     codeberg icon set uses none of them; they are not rendered.
//   - text: glyph shaping belongs to the text stack, not the image path.
//   - <image>, foreignObject and scripts: no nested resources are loaded.
//   - stroke-dasharray and stroke-dashoffset: the icons use "none".
//   - miter, bevel and square joins/caps: joins are approximated by a round
//     disc at every corner above a threshold; square caps paint as butt.
//     Codeberg's wordmark has miter joins at its corners.
//   - rounded rect corners (rx/ry): the icons use plain rectangles.
//   - preserveAspectRatio other than xMidYMid meet: the raster keeps the
//     viewBox aspect and the image layer centres it inside the box.
//   - percentage lengths: a percentage is treated as absent.
package svg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// DefaultWidth and DefaultHeight are the intrinsic size of an SVG that
// states neither a size nor a viewBox, the CSS replaced-element default.
const (
	DefaultWidth  = 300
	DefaultHeight = 150
)

// MaxRasterSide bounds Render's longest raster side so a hostile maxSide
// cannot allocate without limit.
const MaxRasterSide = 2048

// Document is a parsed SVG document.
type Document struct {
	root *element
	// ids maps every element id to its element, for url(#id) and href.
	ids map[string]*element
	// rules is the stylesheet, sorted by specificity (stable).
	rules []rule

	viewBox    [4]float64
	hasViewBox bool
	width      float64
	height     float64
	hasWidth   bool
	hasHeight  bool
}

type element struct {
	tag      string
	attrs    map[string]string
	children []*element
	// text is the character data of a <style> element.
	text string
}

// Parse parses an SVG document. Only the root element and well-formed XML
// are required; unknown elements and attributes are kept and ignored.
func Parse(body []byte) (*Document, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var root *element
	var stack []*element
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid SVG markup: %v", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			el := &element{tag: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				el.attrs[a.Name.Local] = a.Value
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, errors.New("invalid SVG markup: several root elements")
				}
				root = el
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, el)
			}
			stack = append(stack, el)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 && stack[len(stack)-1].tag == "style" {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, errors.New("not an SVG document: no root element")
	}
	if root.tag != "svg" {
		return nil, fmt.Errorf("not an SVG document: root element is <%s>", root.tag)
	}
	doc := &Document{root: root, ids: map[string]*element{}}
	doc.index(root)
	sort.SliceStable(doc.rules, func(i, j int) bool {
		return doc.rules[i].specificity < doc.rules[j].specificity
	})
	if raw, ok := root.attrs["viewBox"]; ok {
		if vb, ok := parseViewBox(raw); ok {
			doc.viewBox, doc.hasViewBox = vb, true
		}
	}
	if raw, ok := root.attrs["width"]; ok {
		doc.width, doc.hasWidth = parseLength(raw)
	}
	if raw, ok := root.attrs["height"]; ok {
		doc.height, doc.hasHeight = parseLength(raw)
	}
	return doc, nil
}

// index records ids and collects every <style> block in document order.
func (d *Document) index(el *element) {
	if id, ok := el.attrs["id"]; ok {
		if _, seen := d.ids[id]; !seen {
			d.ids[id] = el
		}
	}
	if el.tag == "style" {
		d.rules = append(d.rules, parseStylesheet(el.text)...)
	}
	for _, c := range el.children {
		d.index(c)
	}
}

// IntrinsicSize is the CSS pixel size of the document: width and height
// when given, the viewBox size scaled by a single given dimension, or the
// viewBox size, or DefaultWidth x DefaultHeight without either.
func (d *Document) IntrinsicSize() (width, height float64) {
	switch {
	case d.hasWidth && d.hasHeight:
		return d.width, d.height
	case d.hasViewBox && d.hasWidth:
		vw, vh := d.viewBox[2], d.viewBox[3]
		return d.width, d.width * vh / vw
	case d.hasViewBox && d.hasHeight:
		vw, vh := d.viewBox[2], d.viewBox[3]
		return d.height * vw / vh, d.height
	case d.hasViewBox:
		return d.viewBox[2], d.viewBox[3]
	}
	w, h := float64(DefaultWidth), float64(DefaultHeight)
	if d.hasWidth {
		w = d.width
	}
	if d.hasHeight {
		h = d.height
	}
	return w, h
}

// ---------------------------------------------------------------------------
// Stylesheet
// ---------------------------------------------------------------------------

type decl struct {
	name, value string
}

// rule is one selector of a stylesheet rule. Only type, .class and
// type.class selectors (and *) are supported; anything else is dropped.
type rule struct {
	tag         string
	class       string
	specificity int
	decls       []decl
}

func (r rule) matches(el *element) bool {
	if r.tag != "" && r.tag != el.tag {
		return false
	}
	if r.class == "" {
		return true
	}
	for _, c := range strings.Fields(el.attrs["class"]) {
		if c == r.class {
			return true
		}
	}
	return false
}

// parseStylesheet reads simple rules. At-rules (@media, @font-face, ...)
// are skipped with their nested blocks.
func parseStylesheet(src string) []rule {
	src = stripComments(src)
	var rules []rule
	for {
		open := strings.IndexByte(src, '{')
		if open < 0 {
			break
		}
		selector := strings.TrimSpace(src[:open])
		end := blockEnd(src, open)
		if end < 0 {
			break
		}
		body := src[open+1 : end]
		src = src[end+1:]
		if strings.HasPrefix(selector, "@") {
			continue
		}
		decls := parseDecls(body)
		for _, s := range strings.Split(selector, ",") {
			if r, ok := parseSelector(strings.TrimSpace(s)); ok {
				r.decls = decls
				rules = append(rules, r)
			}
		}
	}
	return rules
}

// blockEnd returns the index of the brace closing the one at open, or -1.
func blockEnd(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func stripComments(src string) string {
	for {
		start := strings.Index(src, "/*")
		if start < 0 {
			return src
		}
		end := strings.Index(src[start+2:], "*/")
		if end < 0 {
			return src[:start]
		}
		src = src[:start] + src[start+2+end+2:]
	}
}

// parseSelector accepts "*", "tag", ".class" and "tag.class".
func parseSelector(s string) (rule, bool) {
	if s == "*" {
		return rule{specificity: 0}, true
	}
	tag, class := s, ""
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		tag, class = s[:dot], s[dot+1:]
		if class == "" || !isIdent(class) {
			return rule{}, false
		}
	}
	if tag != "" && !isIdent(tag) {
		return rule{}, false
	}
	r := rule{tag: tag, class: class}
	switch {
	case class != "" && tag != "":
		r.specificity = 11
	case class != "":
		r.specificity = 10
	case tag != "":
		r.specificity = 1
	}
	return r, true
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c == '-' || c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// parseDecls reads "name: value; ..." declarations, dropping !important.
func parseDecls(src string) []decl {
	var out []decl
	for _, part := range strings.Split(src, ";") {
		colon := strings.IndexByte(part, ':')
		if colon < 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(part[:colon]))
		value := strings.TrimSpace(part[colon+1:])
		value = strings.TrimSpace(strings.TrimSuffix(value, "!important"))
		if name == "" || value == "" {
			continue
		}
		out = append(out, decl{name: name, value: value})
	}
	return out
}

// ---------------------------------------------------------------------------
// Cascade
// ---------------------------------------------------------------------------

// presentationProps are the properties the renderer reads. Only these are
// taken from attributes, rules and the style attribute.
var presentationProps = []string{
	"fill", "fill-opacity", "fill-rule",
	"stroke", "stroke-width", "stroke-opacity", "stroke-linecap", "stroke-linejoin", "stroke-miterlimit",
	"color", "opacity", "display", "stop-color", "stop-opacity",
}

var knownProp = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range presentationProps {
		m[p] = true
	}
	return m
}()

// inheritedProps are the presentation properties that pass from a parent
// to its children. opacity, display and the stop properties do not.
var inheritedProps = map[string]bool{
	"fill": true, "fill-opacity": true, "fill-rule": true,
	"stroke": true, "stroke-width": true, "stroke-opacity": true,
	"stroke-linecap": true, "stroke-linejoin": true, "stroke-miterlimit": true,
	"color": true,
}

// props computes the cascaded property values of el: inherited values from
// parent, then presentation attributes, then matching rules in specificity
// order, then the style attribute (the highest author origin here).
func (d *Document) props(el *element, parent map[string]string) map[string]string {
	props := make(map[string]string, len(presentationProps))
	for name, value := range parent {
		if inheritedProps[name] {
			props[name] = value
		}
	}
	for _, name := range presentationProps {
		if v, ok := el.attrs[name]; ok {
			props[name] = strings.TrimSpace(v)
		}
	}
	for _, r := range d.rules {
		if r.matches(el) {
			applyDecls(props, r.decls)
		}
	}
	if s, ok := el.attrs["style"]; ok {
		applyDecls(props, parseDecls(s))
	}
	return props
}

func applyDecls(props map[string]string, decls []decl) {
	for _, d := range decls {
		if knownProp[d.name] {
			props[d.name] = d.value
		}
	}
}
