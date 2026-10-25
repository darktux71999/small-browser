package css

import (
	"strings"
	"testing"
)

// The test DOM: a tiny HTML parser plus a NodeIndex implementation, so the
// css package itself stays DOM-free. It mirrors the subset of dom::Document
// the Rust oracle uses in its css_libcss tests: elements with (lowercased)
// attributes, text nodes, children in document order, the synthetic
// #document root and a query-selector for the manual cascades
// (apply_grid_styles / apply_border_radius_styles).

type fakeNode struct {
	tag      string
	attrs    map[string]string
	isText   bool
	text     string
	parent   int // -1: no parent (only the synthetic root)
	children []int
}

type fakeDoc struct {
	nodes []fakeNode
}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true,
	"embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true,
}

var rawTextElements = map[string]bool{"style": true, "script": true, "title": true}

// parseFakeHTML builds the node tree: every included tag opens an element,
// text outside raw-text elements becomes a text node, and the synthetic
// #document node sits at index 0.
//
// Like lexbor (the oracle's parser, src/dom.rs Document::parse), bare
// fragments are wrapped so html/head/body always exist: without them every
// top-level element would be a "root element" and libcss would blockify its
// display, which the oracle never does.
func parseFakeHTML(html string) *fakeDoc {
	lower := strings.ToLower(html)
	if !strings.Contains(lower, "<html") {
		if !strings.Contains(lower, "<head") && !strings.Contains(lower, "<body") {
			html = "<html><head></head><body>" + html + "</body></html>"
		} else {
			html = "<html>" + html + "</html>"
		}
	}
	doc := &fakeDoc{nodes: []fakeNode{{tag: "#document", parent: -1}}}
	stack := []int{0}
	current := func() int { return stack[len(stack)-1] }
	appendNode := func(node fakeNode) int {
		id := len(doc.nodes)
		node.parent = current()
		doc.nodes = append(doc.nodes, node)
		doc.nodes[current()].children = append(doc.nodes[current()].children, id)
		return id
	}
	i := 0
	for i < len(html) {
		if html[i] != '<' {
			// Text up to the next '<' (or the end).
			end := strings.IndexByte(html[i:], '<')
			if end < 0 {
				end = len(html) - i
			}
			appendNode(fakeNode{isText: true, text: html[i : i+end]})
			i += end
			continue
		}
		switch {
		case strings.HasPrefix(html[i:], "<!--"):
			if end := strings.Index(html[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
			} else {
				return doc
			}
			continue
		case strings.HasPrefix(html[i:], "<!"):
			// A doctype or a malformed declaration: skip to '>'.
			end := strings.IndexByte(html[i:], '>')
			if end < 0 {
				return doc
			}
			i += end + 1
			continue
		}
		if strings.HasPrefix(html[i:], "</") {
			// A closing tag: pop until the matching element (or ignore).
			end := strings.IndexByte(html[i:], '>')
			if end < 0 {
				return doc
			}
			name := strings.ToLower(strings.TrimSpace(html[i+2 : i+end]))
			for k := len(stack) - 1; k > 0; k-- {
				if doc.nodes[stack[k]].tag == name {
					stack = stack[:k]
					break
				}
			}
			i += end + 1
			continue
		}
		// An opening tag.
		end := strings.IndexByte(html[i:], '>')
		if end < 0 {
			// Unterminated tag: treat the rest as text.
			appendNode(fakeNode{isText: true, text: html[i:]})
			return doc
		}
		inside := html[i+1 : i+end]
		selfClosed := strings.HasSuffix(inside, "/")
		inside = strings.TrimSuffix(inside, "/")
		name, attrs := parseTag(inside)
		id := appendNode(fakeNode{tag: name, attrs: attrs})
		if rawTextElements[name] && !selfClosed {
			// The content up to the closing tag is one text node OWNED by
			// the element (like lexbor): the style/script/title element
			// owns its text, so TextContent(element) sees it. The old code
			// left the element childless and the text as its sibling,
			// which made Stylesheets() find no sheets at all.
			stack = append(stack, id)
			closeTag := "</" + name + ">"
			content := html[i+end+1:]
			stop := strings.Index(strings.ToLower(content), closeTag)
			if stop < 0 {
				if content != "" {
					appendNode(fakeNode{isText: true, text: content})
				}
				return doc
			}
			if content[:stop] != "" {
				appendNode(fakeNode{isText: true, text: content[:stop]})
			}
			i += end + 1 + stop + len(closeTag)
			stack = stack[:len(stack)-1]
			continue
		}
		if voidElements[name] || selfClosed {
			// Void elements have no children and never touch the stack
			// (HTML5): popping here would abandon the current parent and
			// reattach its following siblings to the grandparent.
			i += end + 1
			continue
		}
		stack = append(stack, id)
		i += end + 1
	}
	return doc
}

// parseTag splits `name k=v k2="v 2"`: attribute names lowercased, quoted
// values kept whole (a style="" value carries spaces and semicolons, like
// lexbor). Unquoted values end at whitespace; a bare name maps to "".
func parseTag(inside string) (string, map[string]string) {
	attrs := map[string]string{}
	i := 0
	for i < len(inside) && !isASCIIWhitespace(inside[i]) {
		i++
	}
	name := strings.ToLower(inside[:i])
	for i < len(inside) {
		for i < len(inside) && isASCIIWhitespace(inside[i]) {
			i++
		}
		if i >= len(inside) {
			break
		}
		j := i
		for j < len(inside) && inside[j] != '=' && !isASCIIWhitespace(inside[j]) {
			j++
		}
		key := strings.ToLower(inside[i:j])
		i = j
		if i >= len(inside) || inside[i] != '=' {
			attrs[key] = ""
			continue
		}
		i++ // '='
		for i < len(inside) && isASCIIWhitespace(inside[i]) {
			i++
		}
		var value string
		if i < len(inside) && (inside[i] == '"' || inside[i] == '\'') {
			quote := inside[i]
			i++
			j := i
			for j < len(inside) && inside[j] != quote {
				j++
			}
			value = inside[i:j]
			i = j
			if i < len(inside) {
				i++
			}
		} else {
			j := i
			for j < len(inside) && !isASCIIWhitespace(inside[j]) {
				j++
			}
			value = inside[i:j]
			i = j
		}
		attrs[key] = value
	}
	return name, attrs
}

// NodeIndex implementation.

func (d *fakeDoc) Len() int           { return len(d.nodes) }
func (d *fakeDoc) Root() int          { return 0 }
func (d *fakeDoc) IsText(id int) bool { return d.nodes[id].isText }
func (d *fakeDoc) Tag(id int) string  { return d.nodes[id].tag }
func (d *fakeDoc) Attr(id int, name string) (string, bool) {
	v, ok := d.nodes[id].attrs[name]
	return v, ok
}
func (d *fakeDoc) Parent(id int) (int, bool) {
	p := d.nodes[id].parent
	return p, p >= 0
}
func (d *fakeDoc) Children(id int) []int { return d.nodes[id].children }
func (d *fakeDoc) TextContent(id int) string {
	var out strings.Builder
	var walk func(int)
	walk = func(id int) {
		if d.nodes[id].isText {
			out.WriteString(d.nodes[id].text)
			return
		}
		for _, child := range d.nodes[id].children {
			walk(child)
		}
	}
	walk(id)
	return out.String()
}

// selectorPart is one compound selector: its tag ("" or "*"), the ids,
// classes and the attribute test it carries.
type selectorPart struct {
	tag       string
	wantsAll  bool
	ids       []string
	classes   []string
	attrName  string
	attrValue string
	pseudo    string
}

func parseSelector(selector string) (parts []selectorPart, ops []byte) {
	var raw []string
	ops = []byte{0}
	var current strings.Builder
	flush := func(op byte) {
		if current.Len() > 0 {
			raw = append(raw, current.String())
			ops = append(ops, op)
			current.Reset()
		}
	}
	for i := 0; i < len(selector); {
		switch c := selector[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			flush(0)
			i++
		case c == '>' || c == '+' || c == '~':
			flush(c)
			i++
		default:
			current.WriteByte(c)
			i++
		}
	}
	flush(0)
	for _, compound := range raw {
		parts = append(parts, parseCompound(compound))
	}
	return parts, ops
}

func parseCompound(compound string) selectorPart {
	var part selectorPart
	for i := 0; i < len(compound); {
		switch c := compound[i]; {
		case c == '#':
			j := i + 1
			for j < len(compound) && !strings.ContainsRune(".#[ >+~", rune(compound[j])) {
				j++
			}
			part.ids = append(part.ids, compound[i+1:j])
			i = j
		case c == '.':
			j := i + 1
			for j < len(compound) && !strings.ContainsRune(".#[ >+~", rune(compound[j])) {
				j++
			}
			part.classes = append(part.classes, compound[i+1:j])
			i = j
		case c == '[':
			j := i + 1
			for j < len(compound) && compound[j] != ']' {
				j++
			}
			body := compound[i+1 : j]
			if key, value, found := strings.Cut(body, "="); found {
				value = strings.Trim(value, "\"'")
				if strings.HasSuffix(key, "~") || strings.HasSuffix(key, "^") ||
					strings.HasSuffix(key, "$") || strings.HasSuffix(key, "*") || strings.HasSuffix(key, "|") {
					key = strings.TrimRight(key, "~^$*|")
				}
				part.attrName, part.attrValue = strings.ToLower(key), value
			} else {
				part.attrName = strings.ToLower(body)
			}
			i = j + 1
		case c == ':':
			j := i + 1
			if j < len(compound) && compound[j] == ':' {
				j++
			}
			for j < len(compound) && !strings.ContainsRune(".#[ >+~", rune(compound[j])) {
				j++
			}
			part.pseudo = strings.ToLower(compound[i+1 : j])
			i = j
		default:
			j := i
			for j < len(compound) && !strings.ContainsRune(".#[ >+~", rune(compound[j])) {
				j++
			}
			part.tag = strings.ToLower(compound[i:j])
			i = j
		}
	}
	return part
}

func (d *fakeDoc) classesOf(id int) []string {
	v, ok := d.nodes[id].attrs["class"]
	if !ok {
		return nil
	}
	return strings.Fields(v)
}

func (p *selectorPart) matches(d *fakeDoc, id int) bool {
	if d.nodes[id].isText {
		return false
	}
	if p.tag != "" && p.tag != "*" && d.nodes[id].tag != p.tag {
		return false
	}
	for _, idwant := range p.ids {
		if got, _ := d.Attr(id, "id"); got != idwant {
			return false
		}
	}
	classes := d.classesOf(id)
	for _, want := range p.classes {
		found := false
		for _, got := range classes {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if p.attrName != "" {
		got, ok := d.Attr(id, p.attrName)
		if !ok {
			return false
		}
		if p.attrValue != "" && got != p.attrValue {
			return false
		}
	}
	return true
}

// prevElement walks the parent's children backwards from id, returning the
// nearest element.
func (d *fakeDoc) prevElement(id int) int {
	parent := d.nodes[id].parent
	if parent < 0 {
		return -1
	}
	for i, child := range d.nodes[parent].children {
		if child != id {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if !d.nodes[d.nodes[parent].children[j]].isText {
				return d.nodes[parent].children[j]
			}
		}
		return -1
	}
	return -1
}

// QuerySelectorAll implements NodeIndex: complex selectors with descendant,
// child, adjacent-sibling and general-sibling combinators, comma-separated
// selector groups, in document order.
func (d *fakeDoc) QuerySelectorAll(selector string) []int {
	var out []int
	for _, group := range strings.Split(selector, ",") {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		parts, ops := parseSelector(group)
		if len(parts) == 0 {
			continue
		}
		for id := range d.nodes {
			if d.matchComplex(id, parts, ops) {
				out = append(out, id)
			}
		}
	}
	return out
}

func (d *fakeDoc) matchComplex(id int, parts []selectorPart, ops []byte) bool {
	last := len(parts) - 1
	if !parts[last].matches(d, id) {
		return false
	}
	if last == 0 {
		return true
	}
	op := ops[last]
	candidate := id
	switch op {
	case '>':
		parent, ok := d.Parent(id)
		if !ok {
			return false
		}
		candidate = parent
		return parts[last-1].matches(d, candidate) && d.matchComplex(candidate, parts[:last], ops)
	case '+':
		candidate = d.prevElement(id)
		if candidate < 0 {
			return false
		}
		return parts[last-1].matches(d, candidate) && d.matchComplex(candidate, parts[:last], ops)
	case '~':
		anc := candidate
		for {
			anc = d.prevElement(anc)
			if anc < 0 {
				return false
			}
			if parts[last-1].matches(d, anc) && d.matchComplex(anc, parts[:last], ops) {
				return true
			}
		}
	default:
		cur, ok := d.Parent(id)
		for ok {
			if parts[last-1].matches(d, cur) && d.matchComplex(cur, parts[:last], ops) {
				return true
			}
			cur, ok = d.Parent(cur)
		}
		return false
	}
}

// queryOne is the query_selector twin used by tests.
func queryOne(d NodeIndex, selector string) (int, bool) {
	for _, id := range d.QuerySelectorAll(selector) {
		return id, true
	}
	return 0, false
}

// mustSelect finds exactly one node or fails the test.
func mustSelect(t *testing.T, d *fakeDoc, selector string) FakeNodeID {
	t.Helper()
	id, ok := queryOne(d, selector)
	if !ok {
		t.Fatalf("fixture has no %s", selector)
	}
	return FakeNodeID(id)
}

// FakeNodeID keeps test helpers below self-documenting.
type FakeNodeID int
