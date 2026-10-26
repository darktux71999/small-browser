package css

/*
#cgo pkg-config: libcss libwapcaplet
#include "glue.h"
*/
import "C"

import (
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

// applyComputedVarStyles applies captured var() declarations for computed
// Style fields not already owned by the border, background, or radius passes.
// Resolved declarations are sorted by the shared cascade bid, replayed as one
// synthetic rule list per affected element, then read through mapStyle. This
// keeps libcss responsible for CSS value parsing and shorthand expansion.
// CSS Cascading and Inheritance Level 5 §6 and CSS Custom Properties §3
// motivate the cascade ordering and invalid-at-computed-value-time drop.
func applyComputedVarStyles(doc NodeIndex, sheets []string, out []Style,
	computed []unsafe.Pointer, unitCtx *C.css_unit_ctx, viewportWidth float64,
	handler *C.css_select_handler, pw unsafe.Pointer) error {
	cp := NewCustomPropertiesAtViewport(doc, sheets, viewportWidth)
	bids := collectComputedVarBids(doc, cp)
	if len(bids) == 0 {
		return nil
	}

	var media C.css_media
	C.cssgo_media_init(&media, C.double(viewportWidth))

	ids := make([]int, 0, len(bids))
	for id := range bids {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		list := bids[id]
		sort.SliceStable(list, func(i, j int) bool {
			return borderVarWeaker(list[i].bid, list[j].bid)
		})
		text := computedVarReplayText(list)
		sheet, err := parseSheet(text, "browser-lite://varcomputed/"+strconv.Itoa(id), false)
		if err != nil {
			return err
		}
		if err := replayComputedVarSheet(doc, id, sheet, list, out, computed, unitCtx, &media, handler, pw); err != nil {
			C.css_stylesheet_destroy(sheet)
			return err
		}
		C.css_stylesheet_destroy(sheet)
	}
	return nil
}

type computedVarBid struct {
	bid      cascadeBid
	property string
	value    string
	media    string
}

// collectComputedVarBids matches and resolves the captured declarations.
// Unresolvable values never reach the generated rules: CSS Custom Properties
// §3 says they become invalid at computed-value time, leaving another valid
// declaration to win.
func collectComputedVarBids(doc NodeIndex, cp *CustomProperties) map[int][]computedVarBid {
	groups := map[int][]computedVarBid{}
	consider := func(id int, decl CustomDecl, bid cascadeBid) {
		if strings.HasPrefix(decl.Property, "--") || !containsVarFunction(decl.Value) ||
			!computedVarPropertySupported(decl.Property) {
			return
		}
		value, ok := cp.Resolve(id, decl.Value)
		if !ok {
			return
		}
		groups[id] = append(groups[id], computedVarBid{
			bid: bid, property: decl.Property, value: value, media: decl.Media,
		})
	}

	for _, decl := range cp.Declarations() {
		for _, selector := range strings.Split(decl.Selector, ",") {
			selector = strings.TrimSpace(selector)
			if selector == "" {
				continue
			}
			specificity := selectorSpecificity(selector)
			for _, id := range doc.QuerySelectorAll(selector) {
				consider(id, decl, cascadeBid{
					important: decl.Important, specificity: specificity,
					order: decl.Order, layerRank: cp.layerRank(decl.Layer),
				})
			}
		}
	}

	for id := 0; id < doc.Len(); id++ {
		raw, ok := doc.Attr(id, "style")
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		_, inline := CaptureInline(raw)
		for _, decl := range inline {
			consider(id, decl, cascadeBid{
				important: decl.Important, inline: true,
				order: ^uint32(0), layerRank: cp.layerRank(decl.Layer),
			})
		}
	}
	return groups
}

// computedVarPropertySupported is intentionally the Style mapping boundary:
// properties without a faithful field in mapStyle stay unapplied rather than
// acquiring a Go-side approximation.
func computedVarPropertySupported(property string) bool {
	switch property {
	case "color", "font", "font-family", "font-size", "font-weight", "font-style", "line-height",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"width", "height", "max-width", "gap", "row-gap", "column-gap", "text-align":
		return true
	default:
		return false
	}
}

// computedVarReplayText keeps each candidate in its own rule so captured
// media conditions remain attached. Universal selectors are safe because the
// sheet is selected only for the already-matched element; sorting by the
// common bid comparator encodes specificity, source order, importance, inline
// status, and layer rank in the synthetic sheet's rule order.
func computedVarReplayText(bids []computedVarBid) string {
	var text strings.Builder
	for _, bid := range bids {
		var rule strings.Builder
		rule.WriteString("*{border-top-color:#dead00!important;")
		rule.WriteString(bid.property)
		rule.WriteByte(':')
		rule.WriteString(bid.value)
		if bid.bid.important {
			rule.WriteString("!important")
		}
		rule.WriteByte('}')
		if bid.media != "" {
			text.WriteString("@media ")
			text.WriteString(bid.media)
			text.WriteString("{")
			text.WriteString(rule.String())
			text.WriteByte('}')
		} else {
			text.WriteString(rule.String())
		}
	}
	return text.String()
}

func replayComputedVarSheet(doc NodeIndex, id int, sheet *C.css_stylesheet, bids []computedVarBid, out []Style,
	computed []unsafe.Pointer, unitCtx *C.css_unit_ctx, media *C.css_media,
	handler *C.css_select_handler, pw unsafe.Pointer) error {
	var ctx *C.css_select_ctx
	if err := check(C.css_select_ctx_create(&ctx), "css_select_ctx_create (var computed)"); err != nil {
		return err
	}
	defer C.css_select_ctx_destroy(ctx)
	if err := check(C.css_select_ctx_append_sheet(ctx, sheet, C.css_origin(cssOriginAuthor), nil),
		"css_select_ctx_append_sheet (var computed)"); err != nil {
		return err
	}

	var results *C.css_select_results
	if err := check(C.css_select_style(ctx, nodePtr(id), unitCtx, media, nil,
		handler, pw, &results), "css_select_style (var computed)"); err != nil {
		return err
	}
	if results == nil {
		return nil
	}
	defer C.css_select_results_destroy(results)
	partial := results.styles[0]
	if partial == nil {
		return nil
	}
	var probe C.css_color
	if C.css_computed_border_top_color(partial, &probe) != C.uint8_t(cssBorderColorColor) ||
		uint32(probe) != borderVarProbeColor {
		return nil // all captured media conditions were false at this viewport
	}

	style := partial
	// Compose the captured declarations over this element's existing computed
	// style, not only its DOM parent. That preserves unrelated plain declarations
	// (for example font-size when a captured line-height uses px) while letting
	// the resolved declaration override its own computed slot.
	base := computed[id]
	if base == nil {
		base = ancestorStyle(computed, doc, id)
	}
	if base != nil {
		var composed *C.css_computed_style
		if err := check(C.css_computed_style_compose((*C.css_computed_style)(base), partial,
			unitCtx, &composed), "css_computed_style_compose (var computed)"); err != nil {
			return err
		}
		C.css_computed_style_destroy(partial)
		style = composed
	}
	results.styles[0] = nil
	defer C.css_computed_style_destroy(style)
	resolved := mapStyle(style, unitCtx, isRootElement(doc, id))
	for _, bid := range bids {
		copyComputedVarProperty(&out[id], resolved, bid.property)
	}
	return nil
}

func copyComputedVarProperty(dst *Style, src Style, property string) {
	switch property {
	case "color":
		dst.Color = src.Color
	case "font":
		dst.FontFamily, dst.FontFamilies = src.FontFamily, src.FontFamilies
		dst.FontSize, dst.FontWeight = src.FontSize, src.FontWeight
		dst.Bold, dst.Italic = src.Bold, src.Italic
		dst.LineHeight = src.LineHeight
		dst.LineHeightNormal = src.LineHeightNormal
	case "font-family":
		dst.FontFamily, dst.FontFamilies = src.FontFamily, src.FontFamilies
	case "font-size":
		dst.FontSize = src.FontSize
	case "font-weight":
		dst.FontWeight, dst.Bold = src.FontWeight, src.Bold
	case "font-style":
		dst.Italic = src.Italic
	case "line-height":
		dst.LineHeight = src.LineHeight
		dst.LineHeightNormal = src.LineHeightNormal
	case "margin":
		dst.Margin, dst.MarginAuto = src.Margin, src.MarginAuto
	case "margin-top":
		dst.Margin[0], dst.MarginAuto[0] = src.Margin[0], src.MarginAuto[0]
	case "margin-right":
		dst.Margin[1], dst.MarginAuto[1] = src.Margin[1], src.MarginAuto[1]
	case "margin-bottom":
		dst.Margin[2], dst.MarginAuto[2] = src.Margin[2], src.MarginAuto[2]
	case "margin-left":
		dst.Margin[3], dst.MarginAuto[3] = src.Margin[3], src.MarginAuto[3]
	case "padding":
		dst.Padding = src.Padding
	case "padding-top":
		dst.Padding[0] = src.Padding[0]
	case "padding-right":
		dst.Padding[1] = src.Padding[1]
	case "padding-bottom":
		dst.Padding[2] = src.Padding[2]
	case "padding-left":
		dst.Padding[3] = src.Padding[3]
	case "width":
		dst.Width = src.Width
	case "height":
		dst.Height = src.Height
	case "max-width":
		dst.MaxWidth = src.MaxWidth
	case "gap", "row-gap", "column-gap":
		if property != "column-gap" {
			dst.RowGap = src.RowGap
			if dst.Flex != nil && src.Flex != nil {
				dst.Flex.RowGap = src.Flex.RowGap
			}
		}
		if property != "row-gap" {
			dst.ColumnGap = src.ColumnGap
			if dst.Flex != nil && src.Flex != nil {
				dst.Flex.ColumnGap = src.Flex.ColumnGap
			}
		}
	case "text-align":
		dst.TextAlign, dst.InlineAlign = src.TextAlign, src.InlineAlign
	}
}
