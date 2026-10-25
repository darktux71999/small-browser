package css

/*
#cgo pkg-config: libcss libwapcaplet
#include "glue.h"
*/
import "C"

import (
	"strings"
	"sync"
	"unsafe"
)

type NodeIndex interface {
	// Len is the number of nodes; every id is in [0, Len).
	Len() int
	// Root is the synthetic document node's id.
	Root() int
	// IsText reports a text node (children-free leaf).
	IsText(id int) bool
	// Tag is the lowercased element tag; empty for text nodes.
	Tag(id int) string
	// Attr reads one attribute, false when absent.
	Attr(id int, name string) (string, bool)
	// Parent returns the parent node's id; false for the document root.
	Parent(id int) (int, bool)
	// Children lists the child ids in document order (text included).
	Children(id int) []int
	// TextContent concatenates the descendants' text.
	TextContent(id int) string
	// QuerySelectorAll is the selector engine the manual cascades use.
	QuerySelectorAll(selector string) []int
}

type nodeDataEntry struct {
	id   int
	data unsafe.Pointer
}

type classArray struct {
	raw unsafe.Pointer // [*]C.lwc_string, allocated by us
	len int
}

type selectCtx struct {
	doc NodeIndex
	// Class arrays are allocated by us: libcss 0.9.2 frees the strings but
	// not the array, so it is recovered during teardown.
	classArrays []classArray
	// libcss node data per node, released through the handler at the end.
	nodeData []nodeDataEntry
}

// The selection cookie handed to C is a registry handle encoded with
// nodePtr, never a real Go pointer: cgo forbids passing Go memory (a
// selectCtx references the document interface) into C, and pinning does not
// cover the transitive closure.
var ctxRegistry = struct {
	sync.RWMutex
	m    map[uintptr]*selectCtx
	next uintptr
}{m: map[uintptr]*selectCtx{}, next: 1}

func registerCtx(ctx *selectCtx) unsafe.Pointer {
	ctxRegistry.Lock()
	defer ctxRegistry.Unlock()
	h := ctxRegistry.next
	ctxRegistry.next++
	ctxRegistry.m[h] = ctx
	return nodePtr(int(h))
}

func unregisterCtx(cookie unsafe.Pointer) {
	ctxRegistry.Lock()
	defer ctxRegistry.Unlock()
	delete(ctxRegistry.m, uintptr(nodeID(cookie)))
}

func lookupCtx(cookie unsafe.Pointer) *selectCtx {
	ctxRegistry.RLock()
	defer ctxRegistry.RUnlock()
	return ctxRegistry.m[uintptr(nodeID(cookie))]
}

// nodePtr is the id encoded in a pointer (id plus CSSGO_PTR_BASE, never
// NULL and never below the Go runtime's junk-pointer threshold): the
// synthesis happens C-side (see cssgo_node_ptr in glue.h), keeping the
// integer-to-pointer conversion out of Go's pointer checker.
func nodePtr(id int) unsafe.Pointer {
	return C.cssgo_node_ptr(C.intptr_t(id))
}

func nodeID(node unsafe.Pointer) int {
	return int(C.cssgo_ptr_id(node))
}

// tagMatches: does the node's tag match the selector's qualified name?
func tagMatches(ctx *selectCtx, id int, qname *C.lwc_string) bool {
	if qname == nil {
		return false
	}
	name := lwcBytes(qname)
	if name == "*" {
		return true
	}
	return strings.EqualFold(ctx.doc.Tag(id), name)
}

// prevElement: the node's previous sibling element (none when the node is
// the first child or an orphan).
func prevElement(ctx *selectCtx, id int) int {
	parent, ok := ctx.doc.Parent(id)
	if !ok {
		return -1
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
		return -1
	}
	for i := pos - 1; i >= 0; i-- {
		if !ctx.doc.IsText(children[i]) {
			return children[i]
		}
	}
	return -1
}

// prevMatching: the previous sibling element whose tag matches qname.
func prevMatching(ctx *selectCtx, id int, qname *C.lwc_string) int {
	parent, ok := ctx.doc.Parent(id)
	if !ok {
		return -1
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
		return -1
	}
	for i := pos - 1; i >= 0; i-- {
		child := children[i]
		if !ctx.doc.IsText(child) && tagMatches(ctx, child, qname) {
			return child
		}
	}
	return -1
}

// ancestorMatching: the nearest matching ancestor, skipping the synthetic
// document root (Rust's `p != doc.root`).
func ancestorMatching(ctx *selectCtx, id int, qname *C.lwc_string) int {
	cur, ok := ctx.doc.Parent(id)
	for ok {
		if cur != ctx.doc.Root() && tagMatches(ctx, cur, qname) {
			return cur
		}
		cur, ok = ctx.doc.Parent(cur)
	}
	return -1
}

// classList splits the class attribute on whitespace (Unicode-aware, like
// Rust's split_whitespace).
func classList(ctx *selectCtx, id int) []string {
	value, ok := ctx.doc.Attr(id, "class")
	if !ok {
		return nil
	}
	return strings.Fields(value)
}

// attributeFor reads one attribute by the selector's name. Both sides
// lowercase: libcss lowercases selector names while parsing and the DOM
// layer lowercases attribute names, so the direct lookup mirrors the
// case-insensitive comparison of the Rust attribute() helper.
func attributeFor(ctx *selectCtx, id int, qname *C.lwc_string) (string, bool) {
	if qname == nil {
		return "", false
	}
	return ctx.doc.Attr(id, strings.ToLower(lwcBytes(qname)))
}

// attrMatches runs one of the six attribute operators (attr_* helpers of
// css_libcss.rs).
func attrMatches(op attrOperator, actual, wanted string) bool {
	switch op {
	case attrEqual:
		return actual == wanted
	case attrDashmatch:
		return actual == wanted ||
			(len(actual) > len(wanted) && strings.HasPrefix(actual, wanted) &&
				actual[len(wanted)] == '-')
	case attrIncludes:
		for _, part := range strings.Fields(actual) {
			if part == wanted {
				return true
			}
		}
		return false
	case attrPrefix:
		return strings.HasPrefix(actual, wanted)
	case attrSuffix:
		return strings.HasSuffix(actual, wanted)
	case attrSubstring:
		return strings.Contains(actual, wanted)
	}
	return false
}

type attrOperator uint8

const (
	attrEqual attrOperator = iota
	attrDashmatch
	attrIncludes
	attrPrefix
	attrSuffix
	attrSubstring
)

// hasAttributeValue answers the node_has_attribute_* callbacks.
func hasAttributeValue(ctx *selectCtx, id int, qname, value *C.lwc_string, matched unsafe.Pointer, op attrOperator) {
	want := lwcBytes(value)
	result := false
	if actual, ok := attributeFor(ctx, id, qname); ok {
		result = attrMatches(op, actual, want)
	}
	*(*C.bool)(matched) = C.bool(result)
}

// isRootElement and NodeIndex live in compute.go (the doc-level helpers).
