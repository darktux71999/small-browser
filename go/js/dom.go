package js

// DOM core (dom-apis-track S1): document/element members over the live
// go/dom arena, installed by BindDOM on top of the oracle's 7-member
// surface (bindings.go stays byte-exact; this only ADDS members — the
// deliberate divergence recorded in KNOWN_DIFFS.md).
//
// Bridge shape: one Go dispatch func (`__bl_dom`, hidden from page scripts
// after the bootstrap captures it) plus a JS bootstrap that builds the
// WHATWG-flavored surface (document object, element wrappers, classList)
// out of driver ops. Node handles cross the boundary as plain JS numbers
// holding go/dom NodeIDs — the same registry idea as the QuickJS handle
// registry (js.go), but ids are arena indexes, never Go pointers
// (no Go pointer enters cgo, as everywhere in this package).
//
// Semantics follow WHATWG DOM where implemented; every approximation is
// cited at the op. Reference is WHATWG DOM + Firefox, NOT the frozen
// oracle (which has no DOM APIs at all).

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"browser-lite/dom"
)

// domVoid is the WHATWG HTML void-element set (fragment serialization
// emits no end tag for these; DOM Standard §3.5 fragment serializing).
var domVoid = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"source": true, "track": true, "wbr": true,
}

// BindDOM installs the DOM surface over doc. Call once per runtime, right
// after Bind and before any page script (the pipeline order in
// go/cmd/browser-lite/main.go). The previous plain `document` object's
// title seeds the new document's title; every other oracle member is
// untouched.
func (r *Runtime) BindDOM(doc *dom.Document) error {
	r.mu.Lock()
	r.domDoc = doc
	r.mu.Unlock()
	if err := r.SetGlobalFunc("__bl_dom", r.domDispatch); err != nil {
		return err
	}
	_, err := r.Eval(domBootstrap, "browser-lite:dom")
	return err
}

// DomDirty reports whether a page script mutated the DOM tree or an
// attribute since BindDOM (class changes included). The CLI recomputes
// styles only then: scripts run after style resolution in oracle order
// (src/main.rs:82-109), which is safe for the oracle because its scripts
// cannot touch the DOM — ours now can, so the re-render is conditional on
// this flag instead of moving the pipeline.
func (r *Runtime) DomDirty() bool {
	return r.domDirty.Load()
}

// domDispatch is the `__bl_dom` driver: op names a primitive, the rest are
// operands (node ids as numbers, everything else as strings). Errors carry
// a "Name: detail" payload that the bootstrap converts to a real Error or
// DOMException throw; the driver itself never fails the Eval.
func (r *Runtime) domDispatch(args []Value) (Value, error) {
	fail := func(name, format string, a ...any) (Value, error) {
		return Undefined(), fmt.Errorf("%s: %s", name, fmt.Sprintf(format, a...))
	}
	if len(args) == 0 {
		return fail("TypeError", "not enough arguments")
	}
	opV := args[0]
	if opV.Kind != KindString {
		return fail("TypeError", "operator must be a string")
	}
	r.mu.Lock()
	d := r.domDoc
	r.mu.Unlock()
	if d == nil {
		return fail("TypeError", "DOM not bound")
	}
	op, rest := opV.S, args[1:]
	need := func(n int) error {
		if len(rest) < n {
			return fmt.Errorf("TypeError: %s needs %d arguments", op, n)
		}
		return nil
	}
	num := func(v Value) (dom.NodeID, error) {
		if v.Kind != KindNumber {
			return 0, fmt.Errorf("TypeError: node id must be a number")
		}
		id := dom.NodeID(int(v.N))
		if id < 0 || int(id) >= len(d.Nodes) {
			return 0, fmt.Errorf("TypeError: invalid node id")
		}
		return id, nil
	}
	str := func(v Value) (string, error) {
		if v.Kind != KindString {
			return "", fmt.Errorf("TypeError: argument must be a string")
		}
		return v.S, nil
	}
	switch op {
	case "byId": // first attached element with the id, else -1
		if err := need(1); err != nil {
			return Undefined(), err
		}
		s, err := str(rest[0])
		if err != nil {
			return Undefined(), err
		}
		for i := range d.Nodes {
			id := dom.NodeID(i)
			n := &d.Nodes[id]
			if n.IsText || !domAttached(d, id) {
				continue
			}
			if v, ok := n.Attr("id"); ok && v == s {
				return Number(float64(id)), nil
			}
		}
		return Number(-1), nil
	case "qs": // first attached match, else -1 (reuses the selector engine)
		if err := need(1); err != nil {
			return Undefined(), err
		}
		s, err := str(rest[0])
		if err != nil {
			return Undefined(), err
		}
		if domSelectorEmpty(s) {
			return fail("SyntaxError", "%q is not a valid selector", s)
		}
		for _, id := range d.QuerySelectorAll(s) {
			if domAttached(d, id) {
				return Number(float64(id)), nil
			}
		}
		return Number(-1), nil
	case "qsa": // every attached match as "id,id,..." ("" when none)
		if err := need(1); err != nil {
			return Undefined(), err
		}
		s, err := str(rest[0])
		if err != nil {
			return Undefined(), err
		}
		if domSelectorEmpty(s) {
			return fail("SyntaxError", "%q is not a valid selector", s)
		}
		var sb strings.Builder
		for _, id := range d.QuerySelectorAll(s) {
			if !domAttached(d, id) {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(strconv.Itoa(int(id)))
		}
		return String(sb.String()), nil
	case "tag":
		if err := need(1); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		return String(d.Nodes[id].Tag), nil
	case "parent":
		if err := need(1); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		if n := &d.Nodes[id]; n.HasParent {
			return Number(float64(n.Parent)), nil
		}
		return Number(-1), nil
	case "getAttr":
		if err := need(2); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		name, err := str(rest[1])
		if err != nil {
			return Undefined(), err
		}
		if v, ok := d.Nodes[id].Attr(name); ok {
			return String(v), nil
		}
		return Null(), nil
	case "setAttr":
		if err := need(3); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		name, err := str(rest[1])
		if err != nil {
			return Undefined(), err
		}
		val, err := str(rest[2])
		if err != nil {
			return Undefined(), err
		}
		// WHATWG DOM §4.9 setAttribute validates the qualified name and
		// lowercases it for HTML elements; the lowercasing is approximated
		// away (stored as-given, like the parser keeps attribute case) so
		// foreign names such as viewBox round-trip.
		if !domValidName(name) {
			return fail("InvalidCharacterError", "invalid attribute name %q", name)
		}
		n := &d.Nodes[id]
		if n.IsText {
			return fail("HierarchyRequestError", "cannot set attributes on a text node")
		}
		if n.Attrs == nil {
			n.Attrs = map[string]string{}
		}
		n.Attrs[name] = val
		r.domDirty.Store(true)
		return Undefined(), nil
	case "hasAttr":
		if err := need(2); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		name, err := str(rest[1])
		if err != nil {
			return Undefined(), err
		}
		_, ok := d.Nodes[id].Attr(name)
		return Bool(ok), nil
	case "removeAttr":
		if err := need(2); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		name, err := str(rest[1])
		if err != nil {
			return Undefined(), err
		}
		delete(d.Nodes[id].Attrs, name)
		r.domDirty.Store(true)
		return Undefined(), nil
	case "create": // detached element (WHATWG §4.9 createElement, ASCII-lowercased)
		if err := need(1); err != nil {
			return Undefined(), err
		}
		tag, err := str(rest[0])
		if err != nil {
			return Undefined(), err
		}
		if !domValidName(tag) {
			return fail("InvalidCharacterError", "invalid element name %q", tag)
		}
		id := dom.NodeID(len(d.Nodes))
		d.Nodes = append(d.Nodes, dom.Node{Tag: domASCIILower(tag), Attrs: map[string]string{}})
		return Number(float64(id)), nil
	case "append": // WHATWG §4.5 pre-insert: adopt-or-move, cycles rejected
		if err := need(2); err != nil {
			return Undefined(), err
		}
		parent, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		kid, err := num(rest[1])
		if err != nil {
			return Undefined(), err
		}
		if d.Nodes[parent].IsText {
			return fail("HierarchyRequestError", "cannot append to a text node")
		}
		if kid == d.Root {
			return fail("HierarchyRequestError", "cannot adopt the document")
		}
		if kid == parent || domIsAncestor(d, kid, parent) {
			return fail("HierarchyRequestError", "cannot append a node to itself or its descendant")
		}
		domDetach(d, kid)
		k := &d.Nodes[kid]
		k.Parent, k.HasParent = parent, true
		p := &d.Nodes[parent]
		p.Children = append(p.Children, kid)
		r.domDirty.Store(true)
		return Undefined(), nil
	case "remove": // WHATWG §4.5 remove: stranger children are NotFoundError
		if err := need(2); err != nil {
			return Undefined(), err
		}
		parent, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		kid, err := num(rest[1])
		if err != nil {
			return Undefined(), err
		}
		p := &d.Nodes[parent]
		at := -1
		for i, c := range p.Children {
			if c == kid {
				at = i
				break
			}
		}
		if at < 0 {
			return fail("NotFoundError", "node is not a child")
		}
		p.Children = append(p.Children[:at], p.Children[at+1:]...)
		k := &d.Nodes[kid]
		k.HasParent = false
		r.domDirty.Store(true)
		return Number(float64(kid)), nil
	case "text": // reuses go/dom's TextContent (never re-implemented)
		if err := need(1); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		return String(d.TextContent(id)), nil
	case "setText": // WHATWG §4.5 string-replace-all: one text child (none when empty)
		if err := need(2); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		s, err := str(rest[1])
		if err != nil {
			return Undefined(), err
		}
		n := &d.Nodes[id]
		if n.IsText {
			return fail("HierarchyRequestError", "cannot set children of a text node")
		}
		n.Children = nil
		if s != "" {
			kid := dom.NodeID(len(d.Nodes))
			d.Nodes = append(d.Nodes, dom.Node{Tag: "#text", IsText: true, Text: s,
				Attrs: map[string]string{}, Parent: id, HasParent: true})
			d.Nodes[id].Children = append(d.Nodes[id].Children, kid)
		}
		r.domDirty.Store(true)
		return Undefined(), nil
	case "html": // WHATWG DOM §3.5 fragment serialization, approximated
		if err := need(1); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		var sb strings.Builder
		domSerializeChildren(d, id, &sb)
		return String(sb.String()), nil
	case "setHTML": // WHATWG §4.5 fragment parsing, approximated via x/net/html
		if err := need(2); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		src, err := str(rest[1])
		if err != nil {
			return Undefined(), err
		}
		if d.Nodes[id].IsText {
			return fail("HierarchyRequestError", "cannot set children of a text node")
		}
		frag, ferr := domParseFragment(d.Nodes[id].Tag, src)
		if ferr != nil {
			return fail("SyntaxError", "cannot parse HTML fragment: %s", ferr)
		}
		d.Nodes[id].Children = nil
		domAdopt(d, id, frag)
		r.domDirty.Store(true)
		return Undefined(), nil
	case "matches": // membership in the shared engine (detached nodes match too, per spec)
		if err := need(2); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		s, err := str(rest[1])
		if err != nil {
			return Undefined(), err
		}
		if domSelectorEmpty(s) {
			return fail("SyntaxError", "%q is not a valid selector", s)
		}
		for _, m := range d.QuerySelectorAll(s) {
			if m == id {
				return Bool(true), nil
			}
		}
		return Bool(false), nil
	case "closest": // self-first walk over strict ancestors, like matchesChain
		if err := need(2); err != nil {
			return Undefined(), err
		}
		id, err := num(rest[0])
		if err != nil {
			return Undefined(), err
		}
		s, err := str(rest[1])
		if err != nil {
			return Undefined(), err
		}
		if domSelectorEmpty(s) {
			return fail("SyntaxError", "%q is not a valid selector", s)
		}
		set := map[dom.NodeID]bool{}
		for _, m := range d.QuerySelectorAll(s) {
			set[m] = true
		}
		for cur, ok := id, true; ok; {
			if set[cur] {
				return Number(float64(cur)), nil
			}
			var p dom.NodeID
			p, ok = domParentOf(d, cur)
			cur = p
		}
		return Number(-1), nil
	case "body":
		for i := range d.Nodes {
			id := dom.NodeID(i)
			n := &d.Nodes[id]
			if !n.IsText && n.Tag == "body" && domAttached(d, id) {
				return Number(float64(id)), nil
			}
		}
		return Number(-1), nil
	case "rootEl":
		for i := range d.Nodes {
			id := dom.NodeID(i)
			n := &d.Nodes[id]
			if !n.IsText && n.Tag == "html" && domAttached(d, id) {
				return Number(float64(id)), nil
			}
		}
		return Number(-1), nil
	}
	return fail("TypeError", "unknown DOM operator %q", op)
}

// domAttached reports whether id reaches the document root through parent
// links (created-but-detached nodes and removed subtrees do not).
func domAttached(d *dom.Document, id dom.NodeID) bool {
	for cur := id; ; {
		if cur == d.Root {
			return true
		}
		n := &d.Nodes[cur]
		if !n.HasParent {
			return false
		}
		cur = n.Parent
	}
}

// domParentOf mirrors go/dom's private parentOf over the exported arena.
func domParentOf(d *dom.Document, id dom.NodeID) (dom.NodeID, bool) {
	n := &d.Nodes[id]
	if !n.HasParent {
		return 0, false
	}
	return n.Parent, true
}

// domIsAncestor reports whether anc sits on id's ancestor chain.
func domIsAncestor(d *dom.Document, anc, id dom.NodeID) bool {
	for cur, ok := domParentOf(d, id); ok; cur, ok = domParentOf(d, cur) {
		if cur == anc {
			return true
		}
	}
	return false
}

// domDetach removes kid from its current parent, if any (the adopt step of
// pre-insert: appending an attached node moves it).
func domDetach(d *dom.Document, kid dom.NodeID) {
	n := &d.Nodes[kid]
	if !n.HasParent {
		return
	}
	p := &d.Nodes[n.Parent]
	for i, c := range p.Children {
		if c == kid {
			p.Children = append(p.Children[:i], p.Children[i+1:]...)
			break
		}
	}
	n.HasParent = false
}

// domSelectorEmpty detects the selectors our shared engine parses to an
// empty chain (go/dom parseSelector: whitespace split, ">" dropped), which
// WHATWG treats as a SyntaxError instead of an empty match.
func domSelectorEmpty(sel string) bool {
	fields := strings.Fields(sel)
	if len(fields) == 0 {
		return true
	}
	for _, f := range fields {
		if f != ">" {
			return false
		}
	}
	return true
}

// domValidName is the lite qualified-name check for createElement and
// attribute names (WHATWG XML Name production approximated: non-empty,
// no whitespace and none of <>&"'=/).
func domValidName(s string) bool {
	if s == "" {
		return false
	}
	return !strings.ContainsAny(s, " \t\n\f\r<>&\"'=/")
}

// domASCIILower mirrors go/dom's asciiLower (the oracle stores lexbor local
// names, already lowercase): createElement ASCII-lowercases the tag for
// HTML elements (WHATWG DOM §4.9).
func domASCIILower(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			if b == nil {
				b = []byte(s)
			}
			b[i] = c + ('a' - 'A')
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}

// domParseFragment parses an innerHTML string with the WHATWG fragment case
// approximated through x/net/html's ParseFragment (the same entry point
// go/dom's parseNoscriptFragment uses for its re-parse), with the target
// element as context.
func domParseFragment(contextTag, src string) ([]*html.Node, error) {
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	if a := atom.Lookup([]byte(contextTag)); a != 0 {
		ctx = &html.Node{Type: html.ElementNode, DataAtom: a, Data: contextTag}
	}
	return html.ParseFragment(strings.NewReader(src), ctx)
}

// domAdopt copies fragment nodes into the arena as children of parent. The
// shape mirrors go/dom's walk (element attribute copy with lowercased tags,
// text kept, comments/doctypes dropped like the oracle's walk_children),
// except the noscript raw-text re-parse, which is a documented T1 gap:
// a <noscript> inside innerHTML keeps x/net/html's scripting-enabled
// raw-text child instead of parsed markup.
func domAdopt(d *dom.Document, parent dom.NodeID, nodes []*html.Node) {
	for _, n := range nodes {
		switch n.Type {
		case html.ElementNode:
			attrs := make(map[string]string, len(n.Attr))
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			id := dom.NodeID(len(d.Nodes))
			d.Nodes = append(d.Nodes, dom.Node{Tag: domASCIILower(n.Data),
				Attrs: attrs, Parent: parent, HasParent: true})
			d.Nodes[parent].Children = append(d.Nodes[parent].Children, id)
			var kids []*html.Node
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				kids = append(kids, c)
			}
			domAdopt(d, id, kids)
		case html.TextNode:
			id := dom.NodeID(len(d.Nodes))
			d.Nodes = append(d.Nodes, dom.Node{Tag: "#text", IsText: true, Text: n.Data,
				Attrs: map[string]string{}, Parent: parent, HasParent: true})
			d.Nodes[parent].Children = append(d.Nodes[parent].Children, id)
		}
	}
}

// domSerializeChildren writes the WHATWG fragment serialization of id's
// children (DOM Standard §3.5 approximated): elements recurse with sorted
// attributes for determinism, void elements take no end tag, text escapes
// &<>, and U+00A0 becomes &nbsp;.
func domSerializeChildren(d *dom.Document, id dom.NodeID, sb *strings.Builder) {
	for _, c := range d.Nodes[id].Children {
		domSerializeNode(d, c, sb)
	}
}

func domSerializeNode(d *dom.Document, id dom.NodeID, sb *strings.Builder) {
	n := &d.Nodes[id]
	if n.IsText {
		sb.WriteString(domEscapeText(n.Text))
		return
	}
	sb.WriteByte('<')
	sb.WriteString(n.Tag)
	keys := make([]string, 0, len(n.Attrs))
	for k := range n.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteByte(' ')
		sb.WriteString(k)
		sb.WriteString(`="`)
		sb.WriteString(domEscapeAttr(n.Attrs[k]))
		sb.WriteByte('"')
	}
	sb.WriteByte('>')
	if domVoid[n.Tag] {
		return
	}
	domSerializeChildren(d, id, sb)
	sb.WriteString("</")
	sb.WriteString(n.Tag)
	sb.WriteByte('>')
}

func domEscapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", " ", "&nbsp;")
	return r.Replace(s)
}

func domEscapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", " ", "&nbsp;")
	return r.Replace(s)
}

// domBootstrap builds the page-visible surface from the `__bl_dom` driver
// and then hides the driver: document (title accessor plus the S1 members),
// element wrappers (stable per node id, so === holds), and classList.
// document.cookie and per-node listeners stay absent here — T2 owns them.
const domBootstrap = `(function () {
  "use strict";
  const g = globalThis;
  const driver = g.__bl_dom;

  class DOMException extends Error {
    constructor(message, name) {
      super(message);
      this.name = name;
    }
  }
  if (!g.DOMException) {
    Object.defineProperty(g, "DOMException", { value: DOMException,
      writable: true, enumerable: false, configurable: true });
  }

  function fail(text) {
    const m = String(text);
    const i = m.indexOf(": ");
    const name = i < 0 ? "Error" : m.slice(0, i);
    const body = i < 0 ? m : m.slice(i + 2);
    if (name === "TypeError" || name === "RangeError" || name === "SyntaxError") {
      return new g[name](body);
    }
    return new DOMException(body, name);
  }
  function call(op) {
    const args = [op];
    for (let k = 1; k < arguments.length; k++) args.push(arguments[k]);
    try {
      return driver.apply(null, args);
    } catch (e) {
      throw fail(e);
    }
  }

  const cache = new Map();
  function wrap(id) {
    let el = cache.get(id);
    if (el) return el;
    el = makeElement(id);
    cache.set(id, el);
    return el;
  }
  function isEl(v) {
    return typeof v === "object" && v !== null && typeof v.__bl_id === "number";
  }

  function checkToken(token) {
    const t = String(token);
    if (t === "") {
      throw new DOMException("The token must not be empty.", "InvalidCharacterError");
    }
    if (/[\t\n\f\r ]/.test(t)) {
      throw new DOMException("The token must not contain whitespace.", "InvalidCharacterError");
    }
    return t;
  }
  function classTokens(id) {
    const raw = call("getAttr", id, "class");
    if (raw === null) return [];
    return String(raw).split(/[\t\n\f\r ]+/).filter(function (t) { return t !== ""; });
  }
  function writeTokens(id, toks) {
    call("setAttr", id, "class", toks.join(" "));
  }
  function makeClassList(id) {
    return {
      add: function () {
        const toks = classTokens(id);
        let dirty = false;
        for (const a of arguments) {
          const t = checkToken(a);
          if (toks.indexOf(t) === -1) { toks.push(t); dirty = true; }
        }
        if (dirty) writeTokens(id, toks);
      },
      remove: function () {
        const toks = classTokens(id);
        const drop = new Set();
        for (const a of arguments) drop.add(checkToken(a));
        const kept = toks.filter(function (t) { return !drop.has(t); });
        if (kept.length !== toks.length) writeTokens(id, kept);
      },
      toggle: function (token, force) {
        const t = checkToken(token);
        const toks = classTokens(id);
        const has = toks.indexOf(t) !== -1;
        const want = arguments.length > 1 ? !!force : !has;
        if (want && !has) { toks.push(t); writeTokens(id, toks); return true; }
        if (!want && has) {
          writeTokens(id, toks.filter(function (x) { return x !== t; }));
          return false;
        }
        return has;
      },
      contains: function (token) {
        return classTokens(id).indexOf(checkToken(token)) !== -1;
      },
      get value() {
        const v = call("getAttr", id, "class");
        return v === null ? "" : String(v);
      },
    };
  }

  function makeElement(id) {
    const classList = makeClassList(id);
    const el = {
      get tagName() { return String(call("tag", id)).toUpperCase(); },
      get id() {
        const v = call("getAttr", id, "id");
        return v === null ? "" : String(v);
      },
      set id(v) { call("setAttr", id, "id", String(v)); },
      get className() {
        const v = call("getAttr", id, "class");
        return v === null ? "" : String(v);
      },
      set className(v) { call("setAttr", id, "class", String(v)); },
      get textContent() { return String(call("text", id)); },
      set textContent(v) {
        call("setText", id, v === null || v === undefined ? "" : String(v));
      },
      get innerHTML() { return String(call("html", id)); },
      set innerHTML(v) {
        call("setHTML", id, v === null || v === undefined ? "" : String(v));
      },
      get classList() { return classList; },
      getAttribute: function (n) {
        const v = call("getAttr", id, String(n));
        return v === null ? null : String(v);
      },
      setAttribute: function (n, v) { call("setAttr", id, String(n), String(v)); },
      removeAttribute: function (n) { call("removeAttr", id, String(n)); },
      hasAttribute: function (n) { return !!call("hasAttr", id, String(n)); },
      appendChild: function (kid) {
        if (!isEl(kid)) throw new TypeError("appendChild: argument is not a Node");
        call("append", id, kid.__bl_id);
        return kid;
      },
      removeChild: function (kid) {
        if (!isEl(kid)) throw new TypeError("removeChild: argument is not a Node");
        call("remove", id, kid.__bl_id);
        return kid;
      },
      matches: function (sel) { return !!call("matches", id, String(sel)); },
      closest: function (sel) {
        const hit = call("closest", id, String(sel));
        return hit < 0 ? null : wrap(hit);
      },
    };
    Object.defineProperty(el, "__bl_id", { value: id });
    return el;
  }

  function nodeList(ids) {
    const list = [];
    if (ids !== "") {
      for (const part of String(ids).split(",")) list.push(wrap(Number(part)));
    }
    list.item = function (i) {
      i = Number(i);
      return i >= 0 && i < list.length ? list[i] : null;
    };
    return list;
  }

  let titleState = "";
  try {
    if (g.document) titleState = String(g.document.title);
  } catch (_) {}
  // WHATWG title is a DOMString: the setter coerces (the old plain-object
  // member stored any value and coerced only on read; only a direct read
  // of a non-string assignment can tell them apart).
  const document = {
    get title() { return titleState; },
    set title(v) { titleState = String(v); },
    get body() {
      const id = call("body");
      return id < 0 ? null : wrap(id);
    },
    get documentElement() {
      const id = call("rootEl");
      return id < 0 ? null : wrap(id);
    },
    getElementById: function (i) {
      const id = call("byId", String(i));
      return id < 0 ? null : wrap(id);
    },
    querySelector: function (s) {
      const id = call("qs", String(s));
      return id < 0 ? null : wrap(id);
    },
    querySelectorAll: function (s) { return nodeList(call("qsa", String(s))); },
    createElement: function (t) { return wrap(call("create", String(t))); },
  };
  Object.defineProperty(g, "document", { value: document,
    writable: true, enumerable: true, configurable: true });
  delete g.__bl_dom;
})();`
