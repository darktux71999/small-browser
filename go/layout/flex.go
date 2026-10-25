// Flex formatting context: the Go port of the oracle's flex path
// (src/layout.rs:349-895: FlexItemBox, layout_flex_container,
// collect_flex_items, flex_base_size, flex_lines, line_main_size,
// item_outer_main, item/main/cross padding and margin helpers,
// cross_size_is_auto, justify_offset, column_item_width,
// item_max_content_width, measure_item_height, measure_text_height,
// natural_text_width, definite_inner_height).
//
// The algorithm order mirrors the oracle step for step: base sizes from
// flex-basis (or the content measure), items collected into lines, free
// space shared per line with grow/shrink, cross sizes measured at the
// allotted width, justify/align placement, then the real layout pass with
// the item widths forced through (the R3-4 lesson: nested contexts take
// their allotted width through forced, never the parent's available
// width). Approximations: none — float64 arithmetic in the same order.
package layout

import (
	"math"
	"strings"

	"browser-lite/css"
	"browser-lite/dom"
)

// flexItem is one item of a flex container: a real child or an anonymous
// item built from the container's inline content. It mirrors the oracle's
// FlexItemBox (src/layout.rs:353-370): node is NoNode for anonymous items
// (the usize::MAX sentinel) and must never reach the DOM lookups.
type flexItem struct {
	// node is the DOM node, or NoNode for an anonymous item.
	node dom.NodeID
	// text is the inline content of an anonymous item, nil for real items.
	text *string
	// style is the item's own style (the container's, border-stripped,
	// for anonymous items).
	style css.Style
	// base is the flex base size: the content-box size along the main axis.
	base float64
	// main is the final content-box size along the main axis.
	main float64
	// cross is the content-box size along the cross axis.
	cross float64
	// x, y is the border-box origin of the item.
	x, y float64
}

// layoutFlexContainer lays out a flex container: its children become flex
// items along the container's main axis instead of stacking in the block
// flow. Exact port of layout_flex_container (src/layout.rs:380-663).
func (st *state) layoutFlexContainer(doc *dom.Document, styles []css.Style, id dom.NodeID, originX, originY, availableWidth float64, f forced) float64 {
	style := &styles[id]
	flex := css.FlexStyle{}
	if style.Flex != nil {
		flex = *style.Flex
	}
	row := flex.Direction == css.FlexDirectionRow

	content := computeContentBox(style, originX, originY, availableWidth, f)
	borderY := originY + style.Margin[0]

	boxIndex := len(st.out.Boxes)
	st.out.Boxes = append(st.out.Boxes, LayoutBox{
		Node:            id,
		Tag:             doc.Nodes[id].Tag,
		X:               content.borderX,
		Y:               borderY,
		W:               content.borderW,
		FontSize:        style.FontSize,
		Bold:            style.Bold,
		Centered:        style.InlineAlign,
		Color:           style.Color,
		Background:      style.Background,
		BackgroundImage: backgroundFor(style, st.images),
		// The placement fields travel with the image (S10), like the
		// block box in layout.go.
		BackgroundRepeat:    style.BackgroundRepeat,
		BackgroundPositionX: style.BackgroundPositionX,
		BackgroundPositionY: style.BackgroundPositionY,
		BackgroundGradient:  style.BackgroundGradient,
		BorderWidth:         style.BorderWidth,
		BorderStyle:         style.BorderStyle,
		BorderColor:         style.BorderColor,
		BorderRadius:        style.BorderRadius,
	})

	// The inner main size: the width of a row container, the height of a
	// column one (definite only when the author set it).
	// (src/layout.rs:425-431).
	var mainAvail float64
	if row {
		mainAvail = content.w
	} else if h, ok := definiteInnerHeight(style); ok {
		mainAvail = h
	} else {
		mainAvail = math.Inf(1)
	}
	// The inner cross size: the height of a row container, the width of a
	// column one (src/layout.rs:432-438).
	var crossAvail float64
	if row {
		if h, ok := definiteInnerHeight(style); ok {
			crossAvail = h
		} else {
			crossAvail = math.Inf(1)
		}
	} else {
		crossAvail = content.w
	}

	items := collectFlexItems(doc, styles, id)
	if len(items) == 0 {
		var height float64
		if style.Height != nil {
			height = max(*style.Height, 0)
		}
		st.out.Boxes[boxIndex].H = height
		return height + style.Margin[0] + style.Margin[2]
	}

	// The gap between the items of a line and the gap between the lines
	// (src/layout.rs:447-449). The computed Style fields carry them; they
	// are reused, never re-parsed.
	var mainGap, crossGap float64
	if row {
		mainGap, crossGap = flex.ColumnGap, flex.RowGap
	} else {
		mainGap, crossGap = flex.RowGap, flex.ColumnGap
	}

	// 1. The cross size of the items of a column container: their width,
	// which the base size (their height) depends on
	// (src/layout.rs:451-457).
	if !row {
		for i := range items {
			items[i].cross = columnItemWidth(doc, styles, &items[i], crossAvail, st)
		}
	}

	// 2. The flex base size of every item (src/layout.rs:459-465).
	for i := range items {
		items[i].base = flexBaseSize(doc, styles, &items[i], row, mainAvail, st)
		items[i].main = items[i].base
	}

	// 3. The items are collected into lines (src/layout.rs:467-469).
	lines := flexLines(items, row, mainAvail, mainGap)

	// 4. The flexible lengths: the free space of every line is shared with
	// the grow factors, or taken back with the shrink factors
	// (src/layout.rs:471-506).
	for _, line := range lines {
		used := lineMainSize(items[line[0]:line[0]+line[1]], row, mainGap)
		var free float64
		if !math.IsInf(mainAvail, 1) {
			free = mainAvail - used
		}
		if free > 0 {
			var grow float64
			for _, item := range items[line[0] : line[0]+line[1]] {
				grow += item.style.FlexItem.Grow
			}
			if grow > 0 {
				for i := line[0]; i < line[0]+line[1]; i++ {
					items[i].main += free * items[i].style.FlexItem.Grow / grow
				}
			}
		} else if free < 0 {
			// The shrink factors are weighted with the base size.
			var shrink float64
			for _, item := range items[line[0] : line[0]+line[1]] {
				shrink += item.style.FlexItem.Shrink * item.base
			}
			if shrink > 0 {
				for i := line[0]; i < line[0]+line[1]; i++ {
					weight := items[i].style.FlexItem.Shrink * items[i].base
					items[i].main += free * weight / shrink
				}
			}
		}
		for i := line[0]; i < line[0]+line[1]; i++ {
			items[i].main = max(items[i].main, 0)
		}
	}

	// 5. The cross size of the items of a row container: their height, at
	// the width the flex algorithm gave them (src/layout.rs:508-514).
	if row {
		for i := range items {
			items[i].cross = measureItemHeight(doc, styles, &items[i], row, items[i].main, st)
		}
	}

	// 6. The cross size of every line (src/layout.rs:516-530).
	lineCross := make([]float64, 0, len(lines))
	for _, line := range lines {
		var cross float64
		for _, item := range items[line[0] : line[0]+line[1]] {
			pStart, pEnd := crossPadding(&item.style, row)
			mStart, mEnd := crossMargins(&item.style, row)
			cross = max(cross, item.cross+pStart+pEnd+mStart+mEnd)
		}
		// A container with a definite cross size gives that size to its lines.
		if !math.IsInf(crossAvail, 1) {
			cross = crossAvail
		}
		lineCross = append(lineCross, cross)
	}

	// 7. The items are placed along the main axis and across the cross
	// axis (src/layout.rs:532-578).
	var crossPos float64
	if row {
		crossPos = content.y
	} else {
		crossPos = content.x
	}
	for index, line := range lines {
		lineSize := lineCross[index]
		used := lineMainSize(items[line[0]:line[0]+line[1]], row, mainGap)
		var free float64
		if !math.IsInf(mainAvail, 1) {
			free = mainAvail - used
		}
		offset, between := justifyOffset(flex.Justify, free, line[1])
		var mainPos float64
		if row {
			mainPos = content.x + offset
		} else {
			mainPos = content.y + offset
		}
		for i := line[0]; i < line[0]+line[1]; i++ {
			mStart, _ := mainMargins(&items[i].style, row)
			if row {
				items[i].x = mainPos + mStart
			} else {
				items[i].y = mainPos + mStart
			}
			// Every junction carries the container gap plus the extra
			// space the justify mode distributes (only the last item
			// skips both).
			mainPos += itemOuterMain(&items[i], row) + mainGap + between
		}

		for i := line[0]; i < line[0]+line[1]; i++ {
			mStart, mEnd := crossMargins(&items[i].style, row)
			pStart, pEnd := crossPadding(&items[i].style, row)
			if flex.Align == css.AlignItemsStretch && crossSizeIsAuto(&items[i], row) {
				// A stretched item fills the line.
				items[i].cross = max(lineSize-mStart-mEnd-pStart-pEnd, 0)
			}
			outer := items[i].cross + pStart + pEnd + mStart + mEnd
			var aligned float64
			switch flex.Align {
			case css.AlignItemsFlexStart:
				aligned = mStart
			case css.AlignItemsCenter:
				aligned = mStart + (lineSize-outer)/2
			case css.AlignItemsFlexEnd:
				aligned = lineSize - outer + mStart
			default: // AlignItemsStretch
				aligned = mStart
			}
			if row {
				items[i].y = crossPos + aligned
			} else {
				items[i].x = crossPos + aligned
			}
		}

		crossPos += lineSize + crossGap
	}

	// 8. The items are laid out for real (src/layout.rs:580-654).
	var contentHeight float64
	if row {
		for _, c := range lineCross {
			contentHeight += c
		}
		contentHeight += crossGap * float64(len(lines)-1)
	}
	for i := range items {
		item := &items[i]
		mainPad, crossPad := itemPadding(&item.style, row)
		var fw, fh float64
		if row {
			fw, fh = item.main+mainPad, item.cross+crossPad
		} else {
			fw, fh = item.cross+crossPad, item.main+mainPad
		}
		itemForced := forced{width: &fw, height: &fh, blockify: true}
		var bottom float64
		if item.text != nil {
			// An anonymous item: the inline content of the container
			// in a block of its own.
			st.out.Boxes = append(st.out.Boxes, LayoutBox{
				Node:        NoNode,
				Tag:         "#anonymous",
				X:           item.x,
				Y:           item.y,
				W:           item.main,
				H:           item.cross,
				Color:       item.style.Color,
				FontSize:    item.style.FontSize,
				Bold:        item.style.Bold,
				BorderColor: [4]css.Color{item.style.Color, item.style.Color, item.style.Color, item.style.Color},
			})
			var run strings.Builder
			run.WriteString(*item.text)
			st.flushText(&item.style, item.x, item.y, max(item.main, 1), &run)
			bottom = item.y + item.cross
		} else {
			// The R3-4 lesson lives here: the item is laid out at its
			// allotted width through forced (honoured by content_box),
			// never at the parent's available width.
			total := st.layoutNode(doc, styles, item.node, item.x, item.y, content.w, itemForced)
			borderH := total - item.style.Margin[0] - item.style.Margin[2]
			bottom = item.y + borderH
		}
		contentHeight = max(contentHeight, bottom-content.y)
	}

	borderH := contentHeight + style.Padding[0] + style.Padding[2] + style.BorderWidth[0] + style.BorderWidth[2]
	if style.Height != nil {
		borderH = max(borderH, *style.Height)
	}
	st.out.Boxes[boxIndex].H = max(borderH, 0)

	return borderH + style.Margin[0] + style.Margin[2]
}

// collectFlexItems gathers the children of a flex container as flex items.
// Every in-flow child is a flex item, and the inline content of the
// container becomes an anonymous item. Exact port of collect_flex_items
// (src/layout.rs:665-726).
func collectFlexItems(doc *dom.Document, styles []css.Style, id dom.NodeID) []flexItem {
	var items []flexItem
	var text strings.Builder
	pushText := func() {
		if strings.TrimSpace(text.String()) == "" {
			text.Reset()
			return
		}
		// An anonymous item has no box of its own in CSS: the
		// container's border and radius must not wrap each text
		// fragment, so they are stripped here (the container box
		// itself still paints them). Padding and margins are stripped
		// too: an anonymous item carries none of its own (CSS Flexbox
		// §4.1), so keeping the container's would double-count them —
		// once in the item's outer size (forcing a bogus shrink and
		// wrap) and once in the container's own box.
		anon := styles[id]
		anon.BorderWidth = [4]float64{}
		anon.BorderRadius = [4]css.Radius{}
		anon.Padding = [4]float64{}
		anon.Margin = [4]float64{}
		s := text.String()
		items = append(items, flexItem{node: NoNode, text: &s, style: anon})
		text.Reset()
	}

	for _, child := range doc.Nodes[id].Children {
		childStyle := &styles[child]
		if childStyle.Display == css.DisplayNone {
			continue
		}
		if doc.Nodes[child].IsText {
			text.WriteString(doc.Nodes[child].Text)
			continue
		}
		if childStyle.Display == css.DisplayInline {
			// The inline content of the container joins the anonymous item.
			collectInlineText(doc, child, &text)
			continue
		}
		pushText()
		items = append(items, flexItem{node: child, style: *childStyle})
	}
	pushText()

	return items
}

// flexBaseSize is the flex base size of an item: its flex-basis when it is
// definite, and the size of its content otherwise. Exact port of
// flex_base_size (src/layout.rs:728-762).
func flexBaseSize(doc *dom.Document, styles []css.Style, item *flexItem, row bool, mainAvail float64, st *state) float64 {
	switch item.style.FlexItem.Basis.Kind {
	case css.BasisPx:
		return item.style.FlexItem.Basis.Value
	case css.BasisPercent:
		if !math.IsInf(mainAvail, 1) {
			return mainAvail * item.style.FlexItem.Basis.Value / 100
		}
		return 0
	default: // BasisAuto | BasisContent: the size of the content of the item.
		// A definite width/height acts as the basis.
		if row {
			if item.style.Width != nil {
				return *item.style.Width
			}
			return itemMaxContentWidth(item, doc, styles, st)
		}
		if item.style.Height != nil {
			return *item.style.Height
		}
		return measureItemHeight(doc, styles, item, row, item.cross, st)
	}
}

// flexLines groups the items of a flex container into lines: a line breaks
// when the next item does not fit in the main size of the container.
// Exact port of flex_lines (src/layout.rs:764-789). Each entry is
// (start, count).
func flexLines(items []flexItem, row bool, mainAvail, mainGap float64) [][2]int {
	var lines [][2]int
	if len(items) == 0 {
		return lines
	}
	start := 0
	used := 0.0
	for index := range items {
		outer := itemOuterMain(&items[index], row)
		var needed float64
		if index == start {
			needed = outer
		} else {
			needed = used + mainGap + outer
		}
		if index > start && needed > mainAvail {
			lines = append(lines, [2]int{start, index - start})
			start = index
			used = outer
		} else {
			used = needed
		}
	}
	return append(lines, [2]int{start, len(items) - start})
}

// lineMainSize is the main size of a line: the outer sizes of its items
// plus the gaps (src/layout.rs:791-798).
func lineMainSize(items []flexItem, row bool, mainGap float64) float64 {
	used := mainGap * float64(len(items)-1)
	if len(items) == 0 {
		used = 0
	}
	for i := range items {
		used += itemOuterMain(&items[i], row)
	}
	return used
}

// itemOuterMain is the outer size of an item along the main axis: its
// content plus its padding and its margins (src/layout.rs:800-806).
func itemOuterMain(item *flexItem, row bool) float64 {
	pad, _ := itemPadding(&item.style, row)
	mStart, mEnd := mainMargins(&item.style, row)
	return item.main + pad + mStart + mEnd
}

// itemPadding is the padding of an item along the main and the cross axis
// (src/layout.rs:808-815).
func itemPadding(style *css.Style, row bool) (float64, float64) {
	if row {
		return style.Padding[3] + style.Padding[1], style.Padding[0] + style.Padding[2]
	}
	return style.Padding[0] + style.Padding[2], style.Padding[3] + style.Padding[1]
}

// mainMargins are the margins of an item along the main axis (start, end)
// (src/layout.rs:817-824).
func mainMargins(style *css.Style, row bool) (float64, float64) {
	if row {
		return style.Margin[3], style.Margin[1]
	}
	return style.Margin[0], style.Margin[2]
}

// crossMargins are the margins of an item along the cross axis (start, end)
// (src/layout.rs:826-833).
func crossMargins(style *css.Style, row bool) (float64, float64) {
	if row {
		return style.Margin[0], style.Margin[2]
	}
	return style.Margin[3], style.Margin[1]
}

// crossPadding is the padding of an item along the cross axis (start, end)
// (src/layout.rs:844-851).
func crossPadding(style *css.Style, row bool) (float64, float64) {
	if row {
		return style.Padding[0], style.Padding[2]
	}
	return style.Padding[3], style.Padding[1]
}

// crossSizeIsAuto reports whether the cross size of the item is automatic,
// so that align-items: stretch applies to it
// (src/layout.rs:853-861).
func crossSizeIsAuto(item *flexItem, row bool) bool {
	if row {
		return item.style.Height == nil
	}
	return item.style.Width == nil
}

// justifyOffset is the offset of the items of a line and the extra space
// between them (src/layout.rs:863-895). It mirrors the oracle's exact gap
// semantics: space-between spreads the free space over the junctions only,
// space-around gives every item half a gap at the edges.
func justifyOffset(justify css.JustifyContent, free float64, count int) (float64, float64) {
	switch justify {
	case css.JustifyCenter:
		return free / 2, 0
	case css.JustifyFlexEnd:
		return free, 0
	case css.JustifySpaceBetween:
		free = max(free, 0)
		if count > 1 {
			return 0, free / float64(count-1)
		}
		return 0, 0
	case css.JustifySpaceAround:
		free = max(free, 0)
		var each float64
		if count > 0 {
			each = free / float64(count)
		}
		return each / 2, each
	case css.JustifySpaceEvenly:
		free = max(free, 0)
		var each float64
		if count > 0 {
			each = free / float64(count+1)
		}
		return each, each
	default: // JustifyFlexStart
		return 0, 0
	}
}

// columnItemWidth is the width of an item of a column container: the width
// of the line when it stretches, and the width of its content otherwise.
// Exact port (src/layout.rs:1299-1320).
func columnItemWidth(doc *dom.Document, styles []css.Style, item *flexItem, crossAvail float64, st *state) float64 {
	mStart, mEnd := crossMargins(&item.style, false)
	pStart, pEnd := crossPadding(&item.style, false)
	if crossSizeIsAuto(item, false) {
		content := crossAvail - mStart - mEnd - pStart - pEnd
		if !math.IsInf(content, 1) {
			return max(content, 0)
		}
	}
	if item.style.Width != nil {
		return *item.style.Width
	}
	return math.Min(itemMaxContentWidth(item, doc, styles, st), crossAvail)
}

// itemMaxContentWidth is the max-content width of a flex item: the DOM
// subtree of a real item, or the unwrapped text of an anonymous one (whose
// node is the NoNode sentinel and must never reach the DOM lookups).
// Exact port (src/layout.rs:1322-1340).
func itemMaxContentWidth(item *flexItem, doc *dom.Document, styles []css.Style, st *state) float64 {
	if item.node == NoNode {
		text := ""
		if item.text != nil {
			text = *item.text
		}
		// A huge width keeps every word on one line: the layout width
		// is an i32 of Pango units, so stay far below its range.
		measured := st.shaper.Measure(text, adaptStyle(&item.style), 1000000)
		widest := 0.0
		for _, line := range measured.Lines {
			widest = max(widest, line.W)
		}
		return widest
	}
	return maxContentWidth(doc, styles, item.node, st)
}

// measureItemHeight is the height of the content of an item, measured in a
// scratch buffer so the real layout can place the item afterwards. Exact
// port of measure_item_height (src/layout.rs:1342-1380).
func measureItemHeight(doc *dom.Document, styles []css.Style, item *flexItem, row bool, contentWidth float64, st *state) float64 {
	if item.text != nil {
		return measureTextHeight(st, *item.text, &item.style, contentWidth)
	}
	pStart, pEnd := crossPadding(&item.style, row)
	fw := max(contentWidth+pStart+pEnd, 0)
	scratch := &state{shaper: st.shaper, images: st.images, out: &Layout{}}
	total := scratch.layoutNode(doc, styles, item.node, 0, 0, contentWidth, forced{width: &fw, blockify: true})
	// The returned height is the border height plus the vertical margins.
	borderH := total - item.style.Margin[0] - item.style.Margin[2]
	return max(borderH-item.style.Padding[0]-item.style.Padding[2], 0)
}

// measureTextHeight is the height of a text run laid out at width
// (src/layout.rs:1382-1389). A nowrap run measures unwrapped, like the
// flush path.
func measureTextHeight(st *state, text string, style *css.Style, width float64) float64 {
	normalized := strings.Join(strings.Fields(text), " ")
	if normalized == "" {
		return 0
	}
	if style.WhiteSpaceNowrap {
		width = nowrapWidth
	}
	return st.shaper.Measure(normalized, adaptStyle(style), max(width, 1)).Height
}

// maxContentWidth is the width the content of an item would take without
// wrapping. Exact port (src/layout.rs:1391-1422). A row flex container sums
// its items along the main axis (CSS Flexbox L1 §9.2: the max-content size
// of a single-line row container is the sum of its flex items); every
// other box keeps the block-style max below. The caller adds the node's
// own padding and margins (flexBaseSize feeds itemOuterMain), so the sum
// runs over content widths only.
func maxContentWidth(doc *dom.Document, styles []css.Style, id dom.NodeID, st *state) float64 {
	style := &styles[id]
	if isImage(doc, id) {
		if image := st.images.Image(id); image != nil {
			return float64(image.Width)
		}
		return 0
	}
	if style.Flex != nil && style.Flex.Direction == css.FlexDirectionRow {
		return maxContentWidthRowFlex(style, doc, styles, id, st)
	}
	var width float64
	var text strings.Builder
	for _, child := range doc.Nodes[id].Children {
		childStyle := &styles[child]
		if childStyle.Display == css.DisplayNone {
			continue
		}
		if isImage(doc, child) {
			if image := st.images.Image(child); image != nil {
				width = max(width, float64(image.Width))
			}
		} else if childStyle.Display == css.DisplayBlock {
			width = max(width, maxContentWidth(doc, styles, child, st))
		} else {
			collectInlineText(doc, child, &text)
		}
	}
	if strings.TrimSpace(text.String()) != "" {
		width = max(width, naturalTextWidth(st, text.String(), style))
	}
	return width
}

// maxContentWidthRowFlex is the max-content width of a row flex
// container: the main-axis sum of its items' content widths plus the gaps
// between them. Block-level children recurse (nested row containers sum
// again, column containers and blocks take their max); inline runs form
// the single anonymous item the collector would build, contributing their
// unwrapped width; images contribute their intrinsic width. Blank runs
// contribute nothing, mirroring the collector dropping blank text.
func maxContentWidthRowFlex(style *css.Style, doc *dom.Document, styles []css.Style, id dom.NodeID, st *state) float64 {
	var total float64
	var text strings.Builder
	items := 0
	flush := func() {
		if strings.TrimSpace(text.String()) == "" {
			text.Reset()
			return
		}
		total += naturalTextWidth(st, text.String(), style)
		items++
		text.Reset()
	}
	for _, child := range doc.Nodes[id].Children {
		childStyle := &styles[child]
		if childStyle.Display == css.DisplayNone {
			continue
		}
		if isImage(doc, child) {
			flush()
			if image := st.images.Image(child); image != nil {
				total += float64(image.Width)
			}
			items++
			continue
		}
		if childStyle.Display == css.DisplayBlock {
			flush()
			pad := childStyle.Padding[3] + childStyle.Padding[1]
			mStart, mEnd := mainMargins(childStyle, true)
			total += maxContentWidth(doc, styles, child, st) + pad + mStart + mEnd
			items++
			continue
		}
		collectInlineText(doc, child, &text)
	}
	flush()
	gaps := items - 1
	if gaps < 0 {
		gaps = 0
	}
	total += style.Flex.ColumnGap * float64(gaps)
	return total
}

// naturalTextWidth is the width of a text run measured without wrapping
// (src/layout.rs:1424-1432).
func naturalTextWidth(st *state, text string, style *css.Style) float64 {
	normalized := strings.Join(strings.Fields(text), " ")
	if normalized == "" {
		return 0
	}
	measured := st.shaper.Measure(normalized, adaptStyle(style), 100000)
	var width float64
	for _, line := range measured.Lines {
		width += line.W
	}
	return width
}

// definiteInnerHeight is the inner height of a box with a definite height
// (src/layout.rs:1434-1439).
func definiteInnerHeight(style *css.Style) (float64, bool) {
	if style.Height == nil {
		return 0, false
	}
	return max(*style.Height-style.Padding[0]-style.Padding[2], 0), true
}
