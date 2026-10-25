package css

import (
	"strings"
	"unicode"
)

// Custom properties (--name) and var() support, phase 1 of
// odd/tasks/var-custom-properties.md (S1 capture, S2 resolution). The Go
// engine only: libcss 0.9.2 discards every declaration whose value
// contains var() and knows no --* properties at all, so the rewriter
// extracts both into a side table before libcss sees the text, and this
// file answers the effective --* map of an element plus var()
// substitution for the manual cascade (T2 applies it; phase 1 never
// applies anything to a Style).

// CustomDecl is one declaration the rewriter extracted into the
// side table: a custom property (--name) or a declaration whose value
// contains a var(...) call.
type CustomDecl struct {
	// Property is the custom property exactly as authored ("--name",
	// case preserved: custom properties are case-sensitive), or the
	// lowercased property name for an ordinary declaration.
	Property string
	// Value is the raw value as authored, whitespace-trimmed, with
	// !important split off into Important.
	Value string
	// Selector is the rule's selector text (the trimmed prelude); ""
	// for a declaration captured from an inline style="" list, which the
	// caller matches to its own element.
	Selector string
	// Media is the condition text of the enclosing rule-holding
	// at-rules (" "-joined, "" at the top level). Phase 1 records it
	// but does NOT evaluate it: a declaration behind a media condition
	// that would not match the viewport is still captured, and phase 2
	// decides whether to honour the condition before applying it.
	Media string
	// Important reports the declaration's !important flag.
	Important bool
	// Order numbers the captured declaration within its capture run:
	// 0-based, in document order. NewCustomProperties re-stamps the
	// sheets it captures into one document-wide sequence.
	Order uint32
}

// capture is the side-table collector threaded through the rewriter: the
// declaration walkers append here when it is non-nil, and the rewrite
// itself is byte-identical to the gap-only walk either way.
type capture struct {
	decls []CustomDecl
	// media is the stack of enclosing rule-holding at-rule conditions.
	media []string
}

// CaptureSheet rewrites a rule list for libcss and returns the side
// table of the declarations libcss would discard: custom properties and
// declarations whose value contains a complete var(...) call (only
// outside strings and comments; a malformed declaration stays in the
// text for libcss to discard, with the same net effect).
//
// The rewritten text is the input minus exactly those declarations —
// gap carriers, font-family normalisation, trivia and @font-face
// extraction keep their byte-for-byte behaviour.
func CaptureSheet(css string) (string, []CustomDecl) {
	var out strings.Builder
	out.Grow(len(css))
	cap := &capture{}
	rewriteRuleList(css, &out, cap)
	return out.String(), cap.decls
}

// CaptureInline is CaptureSheet for an inline declaration list (the
// style="" attribute): the same extraction, no selector (Selector is
// ""; the caller knows the element).
func CaptureInline(decls string) (string, []CustomDecl) {
	var out strings.Builder
	out.Grow(len(decls))
	cap := &capture{}
	rewriteDeclarations(decls, &out, cap, "")
	return out.String(), cap.decls
}

// containsVarFunction reports whether value holds a complete var(...)
// function call outside strings and comments. Function names are
// case-insensitive and must directly precede the '(' (the CSS tokenizer
// spells a function that way); an identifier character before "var"
// makes it part of a larger name (myvar(, --var().
func containsVarFunction(value string) bool {
	_, _, _, _, _, ok := findVarCall(value, 0)
	return ok
}

// findVarCall locates the first complete var( ... ) call in value at or
// after from, outside strings and comments. It returns the span of the
// whole call [start, end), the custom property name, the raw fallback
// ("" without a separator comma) and whether a fallback comma exists.
// An unterminated call reports ok=false.
func findVarCall(value string, from int) (start, end int, name, fallback string, hasFallback, ok bool) {
	b := []byte(value)
	i := from
	for i < len(b) {
		switch {
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			i = skipComment(b, i)
			continue
		case b[i] == '"' || b[i] == '\'':
			i = skipString(b, i)
			continue
		case !isVarCallStart(b, i):
			i++
			continue
		}
		open := i + 3 // the '(' of var(
		depth := 1
		comma := -1 // index of the top-level fallback separator
		j := open + 1
		for j < len(b) {
			switch c := b[j]; {
			case c == '/' && j+1 < len(b) && b[j+1] == '*':
				j = skipComment(b, j)
				continue
			case c == '"' || c == '\'':
				j = skipString(b, j)
				continue
			case c == '(':
				depth++
			case c == ')':
				depth--
				if depth == 0 {
					if comma >= 0 {
						return i, j + 1,
							strings.TrimSpace(value[open+1 : comma]),
							strings.TrimSpace(value[comma+1 : j]),
							true, true
					}
					return i, j + 1, strings.TrimSpace(value[open+1 : j]), "", false, true
				}
			case c == ',' && depth == 1 && comma < 0:
				comma = j
			}
			j++
		}
		// An unterminated call: not a var() the engine can parse.
		return 0, 0, "", "", false, false
	}
	return 0, 0, "", "", false, false
}

// isVarCallStart reports whether value[i:] opens a var( call: the
// function name is immediately followed by '(' and preceded by no
// identifier byte.
func isVarCallStart(b []byte, i int) bool {
	if i+4 > len(b) || b[i+3] != '(' {
		return false
	}
	if !asciiEqualFold(string(b[i:i+3]), "var") {
		return false
	}
	return i == 0 || !isIdentByte(b[i-1])
}

func isIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '-' || c == '_' || c >= 0x80
}

// substituteVar replaces every var(--name[, fallback]) in value with
// lookup's answer. A reference lookup cannot satisfy falls back to the
// fallback (recursively substituted: fallbacks may nest var()); a value
// without a fallback makes the whole substitution unresolvable
// ("", false). lookup answers (string, false) for undefined properties
// and for properties invalid at computed-value time (cycles).
func substituteVar(value string, lookup func(name string) (string, bool)) (string, bool) {
	var out strings.Builder
	i := 0
	for {
		start, end, name, fallback, hasFallback, ok := findVarCall(value, i)
		if !ok {
			out.WriteString(value[i:])
			return out.String(), true
		}
		out.WriteString(value[i:start])
		if resolved, found := lookup(name); found {
			out.WriteString(resolved)
		} else if hasFallback {
			substituted, ok := substituteVar(fallback, lookup)
			if !ok {
				return "", false
			}
			out.WriteString(substituted)
		} else {
			return "", false
		}
		i = end
	}
}

// ---------------------------------------------------------------------------
// S2: per-document resolution
// ---------------------------------------------------------------------------

// CustomProperties resolves the custom-property map of one document:
// which --* declarations each element owns (matched with the document's
// selector machinery and ordered by the shared cascade rule), how they
// inherit down the tree, and how var() substitutes against them.
//
// The resolver never touches a Style: applying resolved declarations is
// phase 2 (T2), which reads Declarations() and calls Resolve per
// element.
type CustomProperties struct {
	doc NodeIndex
	// own is the winning raw --* declaration per element (after the
	// cascade of the rules that match it); custom properties inherit.
	own map[int]map[string]string
	// maps memoises the effective, fully resolved map per element.
	maps map[int]map[string]string
	// decls is the captured side table of the sheets this resolver was
	// built from, sheets in document order, Order re-stamped into one
	// document-wide sequence. Inline style="" declarations are NOT part
	// of it (the caller captures them with CaptureInline per element);
	// they do take part in the element's own map.
	decls []CustomDecl
}

// NewCustomProperties captures the author sheets (in document order),
// matches every captured custom-property selector against the document
// with NodeIndex.QuerySelectorAll — the selector machinery the manual
// cascades already use — and folds each element's winning declarations
// together with its style="" attribute (inline declarations win, like
// every other manual cascade here).
//
// The capture is a pure function of the sheet text: parseSheet strips
// the same declarations from the libcss input with CaptureSheet /
// CaptureInline, so both sides see identical side tables.
func NewCustomProperties(doc NodeIndex, sheets []string) *CustomProperties {
	cp := &CustomProperties{
		doc:  doc,
		own:  map[int]map[string]string{},
		maps: map[int]map[string]string{},
	}
	var all []CustomDecl
	for _, sheet := range sheets {
		_, decls := CaptureSheet(sheet)
		all = append(all, decls...)
	}
	for i := range all {
		all[i].Order = uint32(i)
	}
	cp.decls = all

	type customBid struct {
		value string
		bid   cascadeBid
	}
	best := map[int]map[string]customBid{}
	consider := func(id int, property, value string, bid cascadeBid) {
		m, ok := best[id]
		if !ok {
			m = map[string]customBid{}
			best[id] = m
		}
		if prev, ok := m[property]; ok && !bid.winsOver(prev.bid) {
			return
		}
		m[property] = customBid{value: value, bid: bid}
	}

	for _, decl := range all {
		if !strings.HasPrefix(decl.Property, "--") {
			continue
		}
		for _, selector := range strings.Split(decl.Selector, ",") {
			selector = strings.TrimSpace(selector)
			if selector == "" {
				continue
			}
			specificity := selectorSpecificity(selector)
			for _, id := range doc.QuerySelectorAll(selector) {
				consider(id, decl.Property, decl.Value, cascadeBid{
					important:   decl.Important,
					specificity: specificity,
					order:       decl.Order,
				})
			}
		}
	}

	// The style="" attribute: an inline declaration list, matched to
	// its own element and winning over every rule (like grid/radius).
	for id := 0; id < doc.Len(); id++ {
		decls, ok := doc.Attr(id, "style")
		if !ok || strings.TrimSpace(decls) == "" {
			continue
		}
		_, captured := CaptureInline(decls)
		for _, decl := range captured {
			if !strings.HasPrefix(decl.Property, "--") {
				continue
			}
			consider(id, decl.Property, decl.Value, cascadeBid{
				important: decl.Important,
				inline:    true,
				order:     ^uint32(0), // u32::MAX
			})
		}
	}

	for id, winners := range best {
		own := make(map[string]string, len(winners))
		for property, winner := range winners {
			own[property] = winner.value
		}
		cp.own[id] = own
	}
	return cp
}

// Declarations returns the captured side table of the sheets this
// resolver was built from: custom properties and var() declarations
// alike, sheets in document order, Order one document-wide sequence.
// Read-only; T2 feeds the non-custom entries to its manual cascade.
func (cp *CustomProperties) Declarations() []CustomDecl {
	return cp.decls
}

// EffectiveMap returns the effective --* map of an element: the
// parent's resolved map overridden by the element's own declarations
// (custom properties inherit; an own declaration that is invalid at
// computed-value time — an unresolved var() or a cycle — drops out,
// the guaranteed-invalid value not inheriting). Keys keep the "--"
// prefix. The map is memoised and read-only for the caller.
func (cp *CustomProperties) EffectiveMap(element int) map[string]string {
	if element < 0 || element >= cp.doc.Len() {
		return map[string]string{}
	}
	if memo, ok := cp.maps[element]; ok {
		return memo
	}
	inherited := map[string]string{}
	if parent, ok := cp.doc.Parent(element); ok && parent != element {
		inherited = cp.EffectiveMap(parent)
	}
	own := cp.own[element]
	result := make(map[string]string, len(inherited)+len(own))
	for name, value := range inherited {
		result[name] = value
	}
	for name, raw := range own {
		visiting := map[string]bool{name: true}
		if resolved, ok := substituteCustom(raw, own, inherited, visiting); ok {
			result[name] = resolved
		} else {
			delete(result, name)
		}
	}
	cp.maps[element] = result
	return result
}

// Resolve substitutes every var(--name[, fallback]) in value against
// the element's effective map (recursively: fallbacks may nest var()).
// It answers ("", false) when a reference has neither a value nor a
// fallback — undefined, or invalid at computed-value time (a cycle);
// phase 2 decides what that means for the property being resolved. A
// value without var() comes back unchanged.
func (cp *CustomProperties) Resolve(element int, value string) (string, bool) {
	m := cp.EffectiveMap(element)
	return substituteVar(value, func(name string) (string, bool) {
		resolved, ok := m[name]
		return resolved, ok
	})
}

// substituteCustom resolves one raw custom-property value against the
// declaring element's context: the element's own declarations (raw,
// resolved on demand through visiting, so cycles are detected) win
// over the already-resolved inherited map. A reference that is itself
// invalid behaves as undefined: the fallback applies, or the whole
// value becomes unresolvable.
func substituteCustom(raw string, own, inherited map[string]string, visiting map[string]bool) (string, bool) {
	lookup := func(name string) (string, bool) {
		if visiting[name] {
			return "", false // cycle: invalid at computed-value time
		}
		if value, ok := own[name]; ok {
			visiting[name] = true
			resolved, ok := substituteCustom(value, own, inherited, visiting)
			delete(visiting, name)
			if !ok {
				return "", false
			}
			return resolved, true
		}
		if value, ok := inherited[name]; ok {
			return value, true
		}
		return "", false
	}
	return substituteVar(raw, lookup)
}

// atRuleCondition returns the condition text of a rule-holding at-rule
// prelude: "@media (min-width: 500px)" -> "(min-width: 500px)".
func atRuleCondition(prelude string) string {
	trimmed := strings.TrimLeftFunc(prelude, unicode.IsSpace)
	rest, found := strings.CutPrefix(trimmed, "@")
	if !found {
		return ""
	}
	name := takeAtRuleName(rest)
	return strings.TrimSpace(rest[len(name):])
}
