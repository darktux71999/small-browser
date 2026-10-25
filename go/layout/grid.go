// Grid formatting context: the Go port of the oracle's grid path
// (src/layout.rs:898-1144: GridItemBox, layout_grid_container,
// collect_grid_items, place_grid_item, auto_track_width, flex_view_of,
// span_size).
//
// The algorithm order mirrors the oracle step for step: tracks resolve
// (definite px, content-sized auto, fr from the free space left), sparse
// row-major placement honouring grid-column/grid-row spans, auto row
// heights measured at the spanned widths (only row_span == 1 items feed
// their row — the oracle's known limitation, mirrored, not fixed), cell
// origins with stretch-to-cell, then the real layout pass with the item
// sizes forced through (the R3-4 lesson: nested formatting contexts take
// their cell width through forced, never the parent's available width).
// Grid reuses the flex measuring helpers over the same content instead of
// re-implementing them, exactly like the oracle's flex_view_of.
// Approximations: none — float64 arithmetic in the same order.
package layout

import (
	"browser-lite/css"
	"browser-lite/dom"
)

// gridItem is one item of a grid container: a real child or an anonymous
// item built from the container's inline content. It mirrors the oracle's
// GridItemBox (src/layout.rs:901-922): node is NoNode for anonymous items
// (the usize::MAX sentinel) and must never reach the DOM lookups.
type gridItem struct {
	// node is the DOM node, or NoNode for an anonymous item.
	node dom.NodeID
	// run is the inline content of an anonymous item, nil for real items.
	// It is an inlineRun on inline.go's single collector, so atomic boxes
	// survive exactly as in block flow.
	run *inlineRun
	// style is the item's own style (the container's, border-stripped,
	// for anonymous items).
	style css.Style
	// colSpan and rowSpan are how many tracks and rows the item spans.
	colSpan uint32
	rowSpan uint32
	// col and row are the placed column and row of the item's start.
	col int
	row int
	// x, y is the border-box origin of the item.
	x, y float64
	// w, h is the content-box size of the item: its spanned tracks/rows.
	w, h float64
}

// layoutGridContainer lays out a grid container: its children become grid
// items placed into the tracks of grid-template-columns in row-major order
// (sparse packing, one row at a time), with auto row heights from the
// content. Exact port of layout_grid_container (src/layout.rs:933-1144).
func (st *state) layoutGridContainer(doc *dom.Document, styles []css.Style, id dom.NodeID, originX, originY, availableWidth float64, f forced) float64 {
	style := &styles[id]
	tracks := []css.GridTrack{{Kind: css.TrackAuto}}
	if style.Grid != nil && len(style.Grid.Tracks) > 0 {
		tracks = style.Grid.Tracks
	}
	ncols := len(tracks)

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

	items := collectGridItems(doc, styles, id)
	if len(items) == 0 {
		var height float64
		if style.Height != nil {
			height = max(*style.Height, 0)
		}
		st.out.Boxes[boxIndex].H = height
		if style.PositionRelative || style.PositionAbsolute {
			st.recordContainingBlock(id, style)
		}
		st.layoutAbsoluteChildren(doc, styles, id,
			st.absoluteChildrenForContext(doc, styles, id, content.x, content.y, nil))
		return height + style.Margin[0] + style.Margin[2]
	}

	// The gaps travel in the computed Style fields, filled by the CSS
	// layer from gap/row-gap/column-gap; they are reused, never re-parsed
	// (src/layout.rs:990-991).
	colGap := style.ColumnGap
	rowGap := style.RowGap

	// 1. The items are placed row by row: every item takes the first run
	// of free cells at or after the cursor (sparse packing: never
	// backfill) (src/layout.rs:993-1010).
	var occupied [][]bool
	cursorRow, cursorCol := 0, 0
	for i := range items {
		span := int(items[i].colSpan)
		if span < 1 {
			span = 1
		}
		if span > ncols {
			span = ncols
		}
		items[i].colSpan = uint32(span)
		rspan := int(items[i].rowSpan)
		if rspan < 1 {
			rspan = 1
		}
		if rspan > 1000 {
			rspan = 1000
		}
		items[i].rowSpan = uint32(rspan)
		row, col := placeGridItem(&occupied, ncols, cursorRow, cursorCol, span, rspan)
		items[i].row, items[i].col = row, col
		if col+span >= ncols {
			cursorRow, cursorCol = row+1, 0
		} else {
			cursorRow, cursorCol = row, col+span
		}
	}
	nrows := len(occupied)
	if nrows < 1 {
		nrows = 1
	}

	// 2. The track widths: definite tracks keep their size, auto tracks
	// size to the content of the items that occupy only them, minmax
	// tracks clamp that content between their bounds (CSS Grid L3
	// §12.4-§12.5: floor at min, cap at max, auto bounds unbounded), and
	// fr tracks share the free space that is left (src/layout.rs:1013-1039).
	widths := make([]float64, ncols)
	var frTotal float64
	for index, track := range tracks {
		switch track.Kind {
		case css.TrackPx:
			widths[index] = track.Value
		case css.TrackFr:
			frTotal += track.Value
		case css.TrackMinMax:
			content := autoTrackWidth(items, index, doc, styles, st)
			if !track.MinAuto {
				content = max(content, track.Min)
			}
			if !track.MaxAuto {
				content = min(content, track.Max)
			}
			widths[index] = max(content, 0)
		default: // TrackAuto
			widths[index] = autoTrackWidth(items, index, doc, styles, st)
		}
	}
	var used float64
	for _, w := range widths {
		used += w
	}
	used += colGap * float64(ncols-1)
	if used < content.w && frTotal > 0 {
		free := content.w - used
		for index, track := range tracks {
			if track.Kind == css.TrackFr {
				widths[index] += free * track.Value / frTotal
			}
		}
	}
	for i := range widths {
		widths[i] = max(widths[i], 0)
	}

	// 3. The row heights: every row is as tall as the tallest item that
	// starts in it, measured at its spanned width. Items spanning more
	// than one row feed no row: the oracle's known limitation, mirrored
	// here, not fixed (src/layout.rs:1041-1052).
	heights := make([]float64, nrows)
	for i := range items {
		if items[i].rowSpan != 1 {
			continue
		}
		width := spanSize(widths, items[i].col, int(items[i].colSpan), colGap)
		view := flexViewOf(&items[i])
		height := measureItemHeight(doc, styles, &view, true, width, st)
		heights[items[i].row] = max(heights[items[i].row], height)
	}

	// 4. The items are placed at their cell origins and stretched to it
	// (src/layout.rs:1054-1072).
	colX := make([]float64, ncols)
	x := content.x
	for index, width := range widths {
		colX[index] = x
		x += width + colGap
	}
	rowY := make([]float64, nrows)
	y := content.y
	for index, height := range heights {
		rowY[index] = y
		y += height + rowGap
	}
	for i := range items {
		items[i].x = colX[items[i].col]
		items[i].y = rowY[items[i].row]
		items[i].w = spanSize(widths, items[i].col, int(items[i].colSpan), colGap)
		items[i].h = spanSize(heights, items[i].row, int(items[i].rowSpan), rowGap)
		// Self alignment (CSS Box Alignment L3 §§5-6): a non-auto,
		// non-stretch axis shrinks the item to its content and aligns it
		// inside the cell instead of stretching across it. Auto keeps the
		// stretch above: the engine models no *-items properties. The
		// alignment fits the border box (content plus padding) in the
		// cell, so a padded item still ends flush with the cell edge.
		if items[i].style.GridItem.Justify != css.GridSelfAuto &&
			items[i].style.GridItem.Justify != css.GridSelfStretch {
			view := flexViewOf(&items[i])
			wPad := items[i].style.Padding[3] + items[i].style.Padding[1]
			contentW := itemMaxContentWidth(&view, doc, styles, st)
			// The border box must fit the track: the content is whatever
			// is left after the item's own padding.
			borderW := min(contentW+wPad, items[i].w)
			borderW = max(borderW, 0)
			switch items[i].style.GridItem.Justify {
			case css.GridSelfCenter:
				items[i].x += (items[i].w - borderW) / 2
			case css.GridSelfEnd:
				items[i].x += items[i].w - borderW
			}
			items[i].w = max(borderW-wPad, 0)
		}
		if items[i].style.GridItem.Align != css.GridSelfAuto &&
			items[i].style.GridItem.Align != css.GridSelfStretch {
			view := flexViewOf(&items[i])
			hPad := items[i].style.Padding[0] + items[i].style.Padding[2]
			contentH := measureItemHeight(doc, styles, &view, true, max(items[i].w, 0), st)
			borderH := min(contentH+hPad, items[i].h)
			borderH = max(borderH, 0)
			switch items[i].style.GridItem.Align {
			case css.GridSelfCenter:
				items[i].y += (items[i].h - borderH) / 2
			case css.GridSelfEnd:
				items[i].y += items[i].h - borderH
			}
			items[i].h = max(borderH-hPad, 0)
		}
	}

	// 5. The items are laid out for real, stretched to fill their cell
	// (src/layout.rs:1074-1135).
	var contentHeight float64
	runStarts := make(map[dom.NodeID]int, len(items))
	for i := range items {
		item := &items[i]
		fw := max(item.w+item.style.Padding[3]+item.style.Padding[1], 0)
		fh := max(item.h+item.style.Padding[0]+item.style.Padding[2], 0)
		itemForced := forced{width: &fw, height: &fh, blockify: true}
		var bottom float64
		if item.run != nil {
			// An anonymous item: the inline content of the container
			// in a block of its own.
			st.out.Boxes = append(st.out.Boxes, LayoutBox{
				Node:        NoNode,
				Tag:         "#anonymous",
				X:           item.x,
				Y:           item.y,
				W:           item.w,
				H:           item.h,
				Color:       item.style.Color,
				FontSize:    item.style.FontSize,
				Bold:        item.style.Bold,
				BorderColor: [4]css.Color{item.style.Color, item.style.Color, item.style.Color, item.style.Color},
			})
			st.flushInline(doc, styles, &item.style, item.x, item.y, max(item.w, 1), item.run)
			bottom = item.y + item.h
		} else {
			runStarts[item.node] = len(st.out.Runs)
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
	if style.PositionRelative || style.PositionAbsolute {
		st.recordContainingBlock(id, style)
	}
	st.layoutAbsoluteChildren(doc, styles, id,
		st.absoluteChildrenForContext(doc, styles, id, content.x, content.y, runStarts))

	return borderH + style.Margin[0] + style.Margin[2]
}

// collectGridItems gathers the children of a grid container as grid items.
// Every in-flow child is a grid item, and the inline content of the
// container becomes an anonymous item. Exact port of collect_grid_items
// (src/layout.rs:1150-1211).
func collectGridItems(doc *dom.Document, styles []css.Style, id dom.NodeID) []gridItem {
	var items []gridItem
	var run inlineRun
	pushText := func() {
		if anonBlank(&run) {
			run.items = nil
			return
		}
		// An anonymous item has no box of its own in CSS: the
		// container's border and radius must not wrap each text
		// fragment, so they are stripped here (the container box
		// itself still paints them). Padding and margins are stripped
		// too: an anonymous item carries none of its own (CSS Grid
		// §5.1), so keeping the container's would double-count them —
		// once in the item's outer size and once in the container box.
		anon := styles[id]
		anon.BorderWidth = [4]float64{}
		anon.BorderRadius = [4]css.Radius{}
		anon.Padding = [4]float64{}
		anon.Margin = [4]float64{}
		cp := run
		run.items = nil
		items = append(items, gridItem{node: NoNode, run: &cp, style: anon, colSpan: 1, rowSpan: 1})
	}

	for _, child := range doc.Nodes[id].Children {
		childStyle := &styles[child]
		if childStyle.Display == css.DisplayNone || childStyle.PositionAbsolute {
			continue
		}
		if doc.Nodes[child].IsText {
			collectInline(doc, styles, child, &run)
			continue
		}
		if childStyle.Display == css.DisplayInline {
			// The inline content of the container joins the anonymous item.
			collectInline(doc, styles, child, &run)
			continue
		}
		pushText()
		items = append(items, gridItem{
			node:    child,
			style:   *childStyle,
			colSpan: childStyle.GridItem.ColSpan,
			rowSpan: childStyle.GridItem.RowSpan,
		})
	}
	pushText()

	return items
}

// placeGridItem places one item: the first run of span free cells in row
// cursorRow at or after cursorCol, or in a later row from its start. It
// marks the spanned cells occupied and returns the start of the item.
// Exact port of place_grid_item (src/layout.rs:1216-1247).
func placeGridItem(occupied *[][]bool, ncols, cursorRow, cursorCol, span, rspan int) (int, int) {
	row, col := cursorRow, cursorCol
	if col >= ncols {
		row, col = cursorRow+1, 0
	}
	for {
		for len(*occupied) < row+rspan {
			*occupied = append(*occupied, make([]bool, ncols))
		}
		first := 0
		if row == cursorRow {
			first = col
		}
		for start := first; start <= ncols-span; start++ {
			free := true
			for dr := 0; dr < rspan && free; dr++ {
				for dc := 0; dc < span && free; dc++ {
					if (*occupied)[row+dr][start+dc] {
						free = false
					}
				}
			}
			if free {
				for dr := 0; dr < rspan; dr++ {
					for dc := 0; dc < span; dc++ {
						(*occupied)[row+dr][start+dc] = true
					}
				}
				return row, start
			}
		}
		row++
	}
}

// autoTrackWidth is the width of one auto track: the widest content of the
// items that occupy only that track. Tracks with no such item (or only
// spanned items) start at zero and let the fr tracks or the container edge
// decide. Exact port of auto_track_width (src/layout.rs:1252-1274).
func autoTrackWidth(items []gridItem, col int, doc *dom.Document, styles []css.Style, st *state) float64 {
	var width float64
	for i := range items {
		if items[i].col != col || items[i].colSpan != 1 {
			continue
		}
		view := flexViewOf(&items[i])
		width = max(width, itemMaxContentWidth(&view, doc, styles, st))
	}
	return width
}

// flexViewOf is the flex view of a grid item, so grid reuses the flex
// measuring helpers over the same content instead of re-implementing them.
// Exact port of flex_view_of (src/layout.rs:1278-1289).
func flexViewOf(item *gridItem) flexItem {
	return flexItem{node: item.node, run: item.run, style: item.style}
}

// spanSize is the size of span consecutive tracks starting at start,
// including the gaps between them. Exact port of span_size
// (src/layout.rs:1293-1297).
func spanSize(sizes []float64, start, span int, gap float64) float64 {
	end := start + span
	if end > len(sizes) {
		end = len(sizes)
	}
	lo := start
	if lo > len(sizes) {
		lo = len(sizes)
	}
	var size float64
	for _, s := range sizes[lo:end] {
		size += s
	}
	extra := 0
	if end > start+1 {
		extra = end - (start + 1)
	}
	return size + gap*float64(extra)
}
