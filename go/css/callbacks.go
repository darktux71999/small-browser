package css

/*
#cgo pkg-config: libcss libwapcaplet
#include <libcss/libcss.h>
extern void cssgo_hint_set_color(css_hint *hint, css_color color);
extern void cssgo_hint_set_strings(css_hint *hint);
*/
import "C"

import "unsafe"

func recoverCss(f func() C.css_error) (result C.css_error) {
	defer func() {
		if recover() != nil {
			result = cssInvalid
		}
	}()
	return f()
}

func ctxOf(pw unsafe.Pointer) *selectCtx {
	return lookupCtx(pw)
}

// internFresh interns a string and hands the fresh reference to libcss
// (which releases it when it finishes with the node).
func internFresh(text string) *C.lwc_string {
	return internC(text)
}

func qnameName(qname *C.css_qname) *C.lwc_string {
	if qname == nil {
		return nil
	}
	return qname.name
}

//export cssgo_node_name
func cssgo_node_name(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		id := nodeID(node)
		qname.ns = nil
		qname.name = internFresh(ctx.doc.Tag(id))
		return cssOK
	})
}

//export cssgo_node_classes
func cssgo_node_classes(pw unsafe.Pointer, node unsafe.Pointer, classes ***C.lwc_string, nClasses *C.uint32_t) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		list := classList(ctx, nodeID(node))
		if len(list) == 0 {
			*classes = nil
			*nClasses = 0
			return cssOK
		}
		raw := C.malloc(C.size_t(len(list)) * C.size_t(unsafe.Sizeof(uintptr(0))))
		items := (*[1 << 20]*C.lwc_string)(raw)
		for i, class := range list {
			items[i] = internFresh(class)
		}
		// libcss frees the strings but not the array: the array is released
		// during teardown (Rust keeps (raw, len) in ctx.class_arrays).
		ctx.classArrays = append(ctx.classArrays, classArray{raw: raw, len: len(list)})
		*classes = (**C.lwc_string)(unsafe.Pointer(&items[0]))
		*nClasses = C.uint32_t(len(list))
		return cssOK
	})
}

//export cssgo_node_id
func cssgo_node_id(pw unsafe.Pointer, node unsafe.Pointer, idOut **C.lwc_string) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		*idOut = nil
		if id, ok := ctx.doc.Attr(nodeID(node), "id"); ok && id != "" {
			*idOut = internFresh(id)
		}
		return cssOK
	})
}

//export cssgo_named_ancestor_node
func cssgo_named_ancestor_node(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, ancestor **C.void) C.css_error { //nolint:golint,unused
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		if found := ancestorMatching(ctx, nodeID(node), qnameName(qname)); found >= 0 {
			*ancestor = (*C.void)(nodePtr(found))
		} else {
			*ancestor = nil
		}
		return cssOK
	})
}

//export cssgo_named_parent_node
func cssgo_named_parent_node(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, parent **C.void) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		id := nodeID(node)
		*parent = nil
		p, ok := ctx.doc.Parent(id)
		if ok && p != ctx.doc.Root() && tagMatches(ctx, p, qnameName(qname)) {
			*parent = (*C.void)(nodePtr(p))
		}
		return cssOK
	})
}

// siblingAnswer fills the two named sibling callbacks and the generic one
// shares it verbatim (the Rust code does the same).
func siblingAnswer(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, sibling **C.void) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		if found := prevMatching(ctx, nodeID(node), qnameName(qname)); found >= 0 {
			*sibling = (*C.void)(nodePtr(found))
		} else {
			*sibling = nil
		}
		return cssOK
	})
}

//export cssgo_named_sibling_node
func cssgo_named_sibling_node(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, sibling **C.void) C.css_error {
	return siblingAnswer(pw, node, qname, sibling)
}

//export cssgo_named_generic_sibling_node
func cssgo_named_generic_sibling_node(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, sibling **C.void) C.css_error {
	return siblingAnswer(pw, node, qname, sibling)
}

//export cssgo_parent_node
func cssgo_parent_node(pw unsafe.Pointer, node unsafe.Pointer, parent **C.void) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		id := nodeID(node)
		*parent = nil
		// The synthetic #document node counts as "no parent": this makes
		// :root match <html>, as in NetSurf's DOM.
		p, ok := ctx.doc.Parent(id)
		if ok && p != ctx.doc.Root() {
			*parent = (*C.void)(nodePtr(p))
		}
		return cssOK
	})
}

//export cssgo_sibling_node
func cssgo_sibling_node(pw unsafe.Pointer, node unsafe.Pointer, sibling **C.void) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		if found := prevElement(ctx, nodeID(node)); found >= 0 {
			*sibling = (*C.void)(nodePtr(found))
		} else {
			*sibling = nil
		}
		return cssOK
	})
}

//export cssgo_node_has_name
func cssgo_node_has_name(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		*match = C.bool(tagMatches(ctx, nodeID(node), qnameName(qname)))
		return cssOK
	})
}

//export cssgo_node_has_class
func cssgo_node_has_class(pw unsafe.Pointer, node unsafe.Pointer, name *C.lwc_string, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		want := lwcBytes(name)
		found := false
		for _, class := range classList(ctx, nodeID(node)) {
			if class == want {
				found = true
				break
			}
		}
		*match = C.bool(found)
		return cssOK
	})
}

//export cssgo_node_has_id
func cssgo_node_has_id(pw unsafe.Pointer, node unsafe.Pointer, name *C.lwc_string, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		want := lwcBytes(name)
		id, ok := ctx.doc.Attr(nodeID(node), "id")
		*match = C.bool(ok && id == want)
		return cssOK
	})
}

//export cssgo_node_has_attribute
func cssgo_node_has_attribute(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		_, ok := attributeFor(ctx, nodeID(node), qnameName(qname))
		*match = C.bool(ok)
		return cssOK
	})
}

//export cssgo_node_has_attribute_equal
func cssgo_node_has_attribute_equal(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, value *C.lwc_string, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		hasAttributeValue(ctxOf(pw), nodeID(node), qnameName(qname), value, unsafe.Pointer(match), attrEqual)
		return cssOK
	})
}

//export cssgo_node_has_attribute_dashmatch
func cssgo_node_has_attribute_dashmatch(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, value *C.lwc_string, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		hasAttributeValue(ctxOf(pw), nodeID(node), qnameName(qname), value, unsafe.Pointer(match), attrDashmatch)
		return cssOK
	})
}

//export cssgo_node_has_attribute_includes
func cssgo_node_has_attribute_includes(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, value *C.lwc_string, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		hasAttributeValue(ctxOf(pw), nodeID(node), qnameName(qname), value, unsafe.Pointer(match), attrIncludes)
		return cssOK
	})
}

//export cssgo_node_has_attribute_prefix
func cssgo_node_has_attribute_prefix(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, value *C.lwc_string, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		hasAttributeValue(ctxOf(pw), nodeID(node), qnameName(qname), value, unsafe.Pointer(match), attrPrefix)
		return cssOK
	})
}

//export cssgo_node_has_attribute_suffix
func cssgo_node_has_attribute_suffix(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, value *C.lwc_string, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		hasAttributeValue(ctxOf(pw), nodeID(node), qnameName(qname), value, unsafe.Pointer(match), attrSuffix)
		return cssOK
	})
}

//export cssgo_node_has_attribute_substring
func cssgo_node_has_attribute_substring(pw unsafe.Pointer, node unsafe.Pointer, qname *C.css_qname, value *C.lwc_string, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		hasAttributeValue(ctxOf(pw), nodeID(node), qnameName(qname), value, unsafe.Pointer(match), attrSubstring)
		return cssOK
	})
}

//export cssgo_node_is_root
func cssgo_node_is_root(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		id := nodeID(node)
		parent, ok := ctx.doc.Parent(id)
		*match = C.bool(!ok || parent == ctx.doc.Root())
		return cssOK
	})
}

//export cssgo_node_count_siblings
func cssgo_node_count_siblings(pw unsafe.Pointer, node unsafe.Pointer, sameName C.bool, after C.bool, count *C.int32_t) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		id := nodeID(node)
		*count = 0
		parent, ok := ctx.doc.Parent(id)
		if !ok {
			return cssOK
		}
		children := ctx.doc.Children(parent)
		pos := -1
		for i, child := range children {
			if child == id {
				pos = i
				break
			}
		}
		if pos < 0 {
			return cssOK
		}
		name := ctx.doc.Tag(id)
		var n int32
		var rangeIDs []int
		if after {
			rangeIDs = children[pos+1:]
		} else {
			rangeIDs = children[:pos]
		}
		for _, child := range rangeIDs {
			if ctx.doc.IsText(child) {
				continue // :nth-child counts elements
			}
			if bool(sameName) && !asciiEqualFold(ctx.doc.Tag(child), name) {
				continue
			}
			n++
		}
		*count = C.int32_t(n)
		return cssOK
	})
}

//export cssgo_node_is_empty
func cssgo_node_is_empty(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		*match = C.bool(len(ctx.doc.Children(nodeID(node))) == 0)
		return cssOK
	})
}

//export cssgo_node_is_link
func cssgo_node_is_link(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		id := nodeID(node)
		_, hasHref := ctx.doc.Attr(id, "href")
		*match = C.bool(asciiEqualFold(ctx.doc.Tag(id), "a") && hasHref)
		return cssOK
	})
}

// falseCallback answers every boolean state callback that stays false in
// the minimal engine (visited/hover/active/focus/enabled/disabled/checked/
// target), mirroring cb_false.
func falseCallback(node unsafe.Pointer, match *C.bool) C.css_error {
	_ = node
	*match = C.bool(false)
	return cssOK
}

//export cssgo_node_is_visited
func cssgo_node_is_visited(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return falseCallback(node, match)
}

//export cssgo_node_is_hover
func cssgo_node_is_hover(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return falseCallback(node, match)
}

//export cssgo_node_is_active
func cssgo_node_is_active(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return falseCallback(node, match)
}

//export cssgo_node_is_focus
func cssgo_node_is_focus(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return falseCallback(node, match)
}

//export cssgo_node_is_enabled
func cssgo_node_is_enabled(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return falseCallback(node, match)
}

//export cssgo_node_is_disabled
func cssgo_node_is_disabled(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return falseCallback(node, match)
}

//export cssgo_node_is_checked
func cssgo_node_is_checked(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return falseCallback(node, match)
}

//export cssgo_node_is_target
func cssgo_node_is_target(pw unsafe.Pointer, node unsafe.Pointer, match *C.bool) C.css_error {
	return falseCallback(node, match)
}

//export cssgo_node_is_lang
func cssgo_node_is_lang(pw unsafe.Pointer, node unsafe.Pointer, lang *C.lwc_string, match *C.bool) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		want := lwcBytes(lang)
		*match = C.bool(false)
		cur := nodeID(node)
		for cur >= 0 {
			actual, ok := ctx.doc.Attr(cur, "lang")
			if !ok {
				actual, ok = ctx.doc.Attr(cur, "xml:lang")
			}
			if ok {
				if asciiEqualFold(actual, want) ||
					(len(actual) < len(want) && asciiEqualFold(want[:len(actual)], actual) && want[len(actual)] == '-') {
					*match = C.bool(true)
				}
				break
			}
			parent, hasParent := ctx.doc.Parent(cur)
			if !hasParent {
				break
			}
			cur = parent
		}
		return cssOK
	})
}

//export cssgo_node_presentational_hint
func cssgo_node_presentational_hint(pw unsafe.Pointer, node unsafe.Pointer, nhints *C.uint32_t, hints ***C.css_hint) C.css_error {
	_ = pw
	_ = node
	*nhints = 0
	*hints = nil
	return cssOK
}

//export cssgo_ua_default_for_property
func cssgo_ua_default_for_property(pw unsafe.Pointer, property C.uint32_t, hint *C.css_hint) C.css_error {
	_ = pw
	return recoverCss(func() C.css_error {
		switch uint32(property) {
		case cssPropColor:
			// AARRGGBB: opaque black (the value NetSurf uses).
			cssgoHintSetColor(hint, 0xff000000)
			hint.status = C.CSS_COLOR_COLOR
		case cssPropFontFamily:
			cssgoHintSetStrings(hint)
			hint.status = C.CSS_FONT_FAMILY_SANS_SERIF
		case cssPropQuotes:
			cssgoHintSetStrings(hint)
			hint.status = C.CSS_QUOTES_NONE
		case cssPropVoiceFamily:
			cssgoHintSetStrings(hint)
			hint.status = 0
		default:
			return cssInvalid
		}
		return cssOK
	})
}

//export cssgo_set_libcss_node_data
func cssgo_set_libcss_node_data(pw unsafe.Pointer, node unsafe.Pointer, data unsafe.Pointer) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		id := nodeID(node)
		if data == nil {
			kept := ctx.nodeData[:0]
			for _, entry := range ctx.nodeData {
				if entry.id != id {
					kept = append(kept, entry)
				}
			}
			ctx.nodeData = kept
		} else {
			ctx.nodeData = append(ctx.nodeData, nodeDataEntry{id: id, data: data})
		}
		return cssOK
	})
}

//export cssgo_get_libcss_node_data
func cssgo_get_libcss_node_data(pw unsafe.Pointer, node unsafe.Pointer, data **C.void) C.css_error {
	return recoverCss(func() C.css_error {
		ctx := ctxOf(pw)
		id := nodeID(node)
		*data = nil
		for i := len(ctx.nodeData) - 1; i >= 0; i-- {
			if ctx.nodeData[i].id == id {
				*data = (*C.void)(ctx.nodeData[i].data)
				break
			}
		}
		return cssOK
	})
}

//export cssgo_resolve
func cssgo_resolve(pw unsafe.Pointer, base *C.char, rel *C.lwc_string, abs **C.lwc_string) C.css_error {
	_ = pw
	return recoverCss(func() C.css_error {
		// libcss passes the sheet's own URL as `base`, which is why every
		// sheet is created with the URL its relative @imports resolve
		// against.
		if rel == nil {
			*abs = nil
			return cssOK
		}
		relText := lwcBytes(rel)
		baseText := ""
		if base != nil {
			baseText = C.GoString(base)
		}
		if absolute, ok := joinURL(baseText, relText); ok {
			*abs = internFresh(absolute)
		} else {
			// Unresolvable: hand the reference back unchanged. The import
			// then has no fetchable URL and is registered as an empty sheet.
			*abs = lwcRef(rel)
		}
		return cssOK
	})
}
// (asciiEqualFold lives in rewriter.go.)

// cssgoHintSetColor fills the css_hint union's color member from Go (the
// union itself is not addressable from Go).
func cssgoHintSetColor(hint *C.css_hint, color uint32) {
	C.cssgo_hint_set_color(hint, C.uint32_t(uint32(color)))
}

// cssgoHintSetStrings clears the css_hint union (the "strings" members).
func cssgoHintSetStrings(hint *C.css_hint) {
	C.cssgo_hint_set_strings(hint)
}
