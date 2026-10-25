package layout

import (
	"browser-lite/css"
	"browser-lite/dom"
)

// containingBlock is the padding box of the nearest positioned ancestor, per
// CSS 2.1 §10.1. The initial containing block degrades to the layout viewport.
type containingBlock struct{ x, y, w, h float64 }

type absoluteChild struct {
	id       dom.NodeID
	x, y     float64 // static-position fallback in document coordinates
	runIndex int
}

// absoluteChildrenForContext collects flex/grid absolute descendants after
// excluding them from item collection. Their fallback is the content origin,
// the documented approximation of the formatting-context static position
// (Flexbox §4.1; Grid §5.1).
func (st *state) absoluteChildrenForContext(doc *dom.Document, styles []css.Style, parent dom.NodeID, x, y float64, runStarts map[dom.NodeID]int) []absoluteChild {
	children := doc.Nodes[parent].Children
	var out []absoluteChild
	for i, id := range children {
		if !styles[id].PositionAbsolute {
			continue
		}
		runIndex := len(st.out.Runs)
		for _, later := range children[i+1:] {
			if start, ok := runStarts[later]; ok {
				runIndex = start
				break
			}
		}
		out = append(out, absoluteChild{id: id, x: x, y: y, runIndex: runIndex})
	}
	return out
}

// recordContainingBlock stores the padding box after the element's border box
// has been sized. CSS 2.1 §10.1 defines the padding edge as the containing
// block for descendants of a relative or absolute box.
func (st *state) recordContainingBlock(id dom.NodeID, style *css.Style) {
	if st.positionedBoxes == nil {
		st.positionedBoxes = make(map[dom.NodeID]containingBlock)
	}
	for i := range st.out.Boxes {
		box := &st.out.Boxes[i]
		if box.Node != id {
			continue
		}
		st.positionedBoxes[id] = containingBlock{
			x: box.X + style.BorderWidth[3],
			y: box.Y + style.BorderWidth[0],
			w: max(box.W-style.BorderWidth[1]-style.BorderWidth[3], 0),
			h: max(box.H-style.BorderWidth[0]-style.BorderWidth[2], 0),
		}
		return
	}
}

func (st *state) nearestContainingBlock(doc *dom.Document, styles []css.Style, id dom.NodeID) containingBlock {
	for parent, ok := doc.Nodes[id].Parent, doc.Nodes[id].HasParent; ok; parent, ok = doc.Nodes[parent].Parent, doc.Nodes[parent].HasParent {
		style := &styles[parent]
		if !style.PositionRelative && !style.PositionAbsolute {
			continue
		}
		if cb, exists := st.positionedBoxes[parent]; exists {
			return cb
		}
	}
	return containingBlock{w: st.out.Width, h: st.out.Height}
}

func insetValue(inset *css.PositionInset, basis float64) (float64, bool) {
	if inset == nil {
		return 0, false
	}
	if inset.Percent {
		return basis * inset.Value / 100, true
	}
	return inset.Value, true
}

// layoutAbsoluteChildren resolves deferred out-of-flow children after their
// static parent's final size is known. Children are spliced at their DOM-order
// slot: z-index and CSS 2.1 §9.9 stacking contexts remain deliberately absent.
func (st *state) layoutAbsoluteChildren(doc *dom.Document, styles []css.Style, parent dom.NodeID, children []absoluteChild) {
	insertedRuns := 0
	for _, child := range children {
		boxAt := st.nextSiblingBoxIndex(doc, parent, child.id)
		runAt := child.runIndex + insertedRuns
		if runAt > len(st.out.Runs) {
			runAt = len(st.out.Runs)
		}
		boxes, runs := st.layoutAbsoluteChild(doc, styles, child)
		st.out.Boxes = spliceBoxes(st.out.Boxes, boxAt, boxes)
		st.out.Runs = spliceRuns(st.out.Runs, runAt, runs)
		insertedRuns += len(runs)
	}
}

func (st *state) nextSiblingBoxIndex(doc *dom.Document, parent, id dom.NodeID) int {
	siblings := doc.Nodes[parent].Children
	for i, sibling := range siblings {
		if sibling != id {
			continue
		}
		for _, later := range siblings[i+1:] {
			for index := range st.out.Boxes {
				if st.out.Boxes[index].Node == later {
					return index
				}
			}
		}
		break
	}
	return len(st.out.Boxes)
}

func (st *state) layoutAbsoluteChild(doc *dom.Document, styles []css.Style, child absoluteChild) ([]LayoutBox, []TextRun) {
	style := &styles[child.id]
	cb := st.nearestContainingBlock(doc, styles, child.id)
	left, hasLeft := insetValue(style.AbsoluteLeft, cb.w)
	right, hasRight := insetValue(style.AbsoluteRight, cb.w)
	top, hasTop := insetValue(style.AbsoluteTop, cb.h)
	bottom, hasBottom := insetValue(style.AbsoluteBottom, cb.h)

	borderW := absoluteBorderWidth(style)
	widthAuto := style.Width == nil
	if widthAuto && hasLeft && hasRight {
		borderW = max(cb.w-left-right-style.Margin[1]-style.Margin[3], 0)
	} else if widthAuto {
		// CSS 2.1 §10.3.5 shrink-to-fit is approximated by max-content,
		// clamped to the remaining containing-block width.
		borderW = min(maxContentWidth(doc, styles, child.id, st)+style.Padding[1]+style.Padding[3], cb.w)
	}
	if style.MaxWidth != nil {
		limit := *style.MaxWidth
		if !style.BorderBox {
			limit += style.Padding[1] + style.Padding[3]
		}
		borderW = min(borderW, limit)
	}

	// CSS 2.1 §§10.3.7 and 10.6.4 solve auto margins in the positioned
	// equations; this first subset treats them as zero (mapped auto margins
	// carry a zero length in Style).
	x := child.x
	switch {
	case hasLeft && hasRight && !widthAuto:
		if style.DirectionRTL {
			x = cb.x + cb.w - right - borderW - style.Margin[1]
		} else {
			// CSS 2.1 §10.3.7: the right inset is ignored for an
			// over-constrained LTR equation.
			x = cb.x + left + style.Margin[3]
		}
	case hasLeft:
		x = cb.x + left + style.Margin[3]
	case hasRight:
		x = cb.x + cb.w - right - borderW - style.Margin[1]
	}

	var forcedHeight *float64
	if style.Height == nil && hasTop && hasBottom {
		height := max(cb.h-top-bottom-style.Margin[0]-style.Margin[2], 0)
		forcedHeight = &height
	}
	y := child.y
	if hasTop {
		y = cb.y + top + style.Margin[0]
	}
	width := borderW
	local := &Layout{Width: st.out.Width}
	tmp := &state{shaper: st.shaper, images: st.images, out: local, positionedBoxes: st.positionedBoxes}
	tmp.layoutNode(doc, styles, child.id,
		x-style.Margin[3], y-style.Margin[0], max(cb.w, 1), forced{
			width: &width, height: forcedHeight, blockify: true,
		})
	var root *LayoutBox
	for i := range local.Boxes {
		if local.Boxes[i].Node == child.id {
			root = &local.Boxes[i]
			break
		}
	}
	if root != nil && !hasTop && hasBottom {
		targetY := cb.y + cb.h - bottom - root.H - style.Margin[2]
		dy := targetY - root.Y
		for i := range local.Boxes {
			local.Boxes[i].Y += dy
		}
		for i := range local.Runs {
			local.Runs[i].Y += dy
		}
		if cbBox, ok := st.positionedBoxes[child.id]; ok {
			cbBox.y += dy
			st.positionedBoxes[child.id] = cbBox
		}
	}
	return local.Boxes, local.Runs
}

func absoluteBorderWidth(style *css.Style) float64 {
	if style.Width == nil {
		return 0
	}
	w := *style.Width
	if !style.BorderBox {
		w += style.Padding[1] + style.Padding[3]
	}
	return w
}

func spliceBoxes(dst []LayoutBox, at int, src []LayoutBox) []LayoutBox {
	if at < 0 {
		at = 0
	}
	if at > len(dst) {
		at = len(dst)
	}
	dst = append(dst, make([]LayoutBox, len(src))...)
	copy(dst[at+len(src):], dst[at:len(dst)-len(src)])
	copy(dst[at:], src)
	return dst
}

func spliceRuns(dst []TextRun, at int, src []TextRun) []TextRun {
	if at < 0 {
		at = 0
	}
	if at > len(dst) {
		at = len(dst)
	}
	dst = append(dst, make([]TextRun, len(src))...)
	copy(dst[at+len(src):], dst[at:len(dst)-len(src)])
	copy(dst[at:], src)
	return dst
}
