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

// The var() application pass (T2 of odd/tasks/var-custom-properties.md,
// S3). libcss 0.9.2 discards every declaration whose value contains
// var(...); the rewriter captured those into the side table (custom.go)
// before libcss saw the text. This pass takes the border family — the
// `border` shorthand, its width/style/colour longhand shorthands, the four
// per-side shorthands and the twelve per-side longhands — resolves each
// declaration per element against the document's custom properties, and
// replays the resolved text through libcss' own select. Expansion,
// currentColor, keyword widths and @media matching all stay libcss' code:
// the pass reads the result back through readComputedBorders, the same
// reader the non-var path uses, so no border math exists twice.
//
// Every other captured property (margin: var(--m), color: var(--c), ...)
// stays captured-but-unapplied: a later phase widens the scope.
//
// The cascade order is the manual passes' (grid/radius): the cascadeBid
// tuple (important, inline, specificity, order) decides priority. For one
// element the resolved bids are fed weakest first, each as its own rule
// (selector matched via the document's selector machinery, wrapped in the
// captured @media condition) or, for the style="" captures, as its own
// inline sheet; the last bid covering a slot keeps it. Shorthand and
// longhands therefore interleave per side exactly like a declaration list,
// without any Go-side expansion.
//
// Media is honoured, not reimplemented: the fed rule keeps the captured
// @media wrapper, so the same css_media struct built by cssgo_media_init —
// the one the author sheets' own rules match against — filters it inside
// css_select_style. To learn that verdict the pass reads a probe
// declaration (`color:#dead00`) fed alongside the border declaration: the
// colour only appears in the computed style when libcss matched the rule,
// so a non-matching (or unparsable, exactly like today's non-var sheets)
// condition leaves the element untouched. An unresolvable var() value is
// dropped before the bid, the way the grid and radius cascades drop
// unparsable declarations: a valid sibling declaration still wins the
// slot.

// borderVarProbeColor is the AARRGGBB reading of the probe colour
// `color:#dead00` fed into every replayed rule.
const borderVarProbeColor = uint32(0xffdead00)

// borderVarBid is one captured var() border declaration matched to one
// element, resolved for that element and ready to replay through libcss.
type borderVarBid struct {
	element  int
	bid      cascadeBid
	property string
	value    string
	selector string // matched rule selector; "" marks a style="" capture
	media    string // enclosing @media condition; "" at the top level
}

// borderVarScope maps one captured property name to the per-side slots its
// declaration fills once libcss expands it (side order: top, right,
// bottom, left — the Style array order). Only the border family is in this
// phase's scope; every other name reports ok=false and stays unapplied.
func borderVarScope(property string) (widths, styles, colors [4]bool, ok bool) {
	mark := func(side int, width, style, color bool) {
		widths[side] = widths[side] || width
		styles[side] = styles[side] || style
		colors[side] = colors[side] || color
	}
	switch property {
	case "border":
		for side := 0; side < 4; side++ {
			mark(side, true, true, true)
		}
		return widths, styles, colors, true
	case "border-width":
		for side := 0; side < 4; side++ {
			mark(side, true, false, false)
		}
		return widths, styles, colors, true
	case "border-style":
		for side := 0; side < 4; side++ {
			mark(side, false, true, false)
		}
		return widths, styles, colors, true
	case "border-color":
		for side := 0; side < 4; side++ {
			mark(side, false, false, true)
		}
		return widths, styles, colors, true
	}
	side, rest := -1, ""
	for candidate, name := range [4]string{"top", "right", "bottom", "left"} {
		if tail, found := strings.CutPrefix(property, "border-"+name); found {
			side, rest = candidate, tail
			break
		}
	}
	if side < 0 {
		return widths, styles, colors, false
	}
	switch rest {
	case "":
		mark(side, true, true, true)
	case "-width":
		mark(side, true, false, false)
	case "-style":
		mark(side, false, true, false)
	case "-color":
		mark(side, false, false, true)
	default:
		return widths, styles, colors, false
	}
	return widths, styles, colors, true
}

// borderVarWeaker orders two bids by ascending cascade priority: exactly
// the tuple cascadeBid.winsOver compares, reversed — important, then the
// layer sort key (Cascade 5 §6.4.2, reversed with it), then inline,
// specificity and order. Equal tuples are weaker in neither direction, so
// a stable sort keeps their capture order (the last declaration of a
// style="" list wins, like the sheet cascade).
func borderVarWeaker(a, b cascadeBid) bool {
	if a.important != b.important {
		return !a.important
	}
	if ak, bk := a.layerKey(), b.layerKey(); ak != bk {
		return ak < bk
	}
	if a.inline != b.inline {
		return !a.inline
	}
	if a.specificity != b.specificity {
		for i := 0; i < 3; i++ {
			if a.specificity[i] != b.specificity[i] {
				return a.specificity[i] < b.specificity[i]
			}
		}
	}
	return a.order < b.order
}

// applyBorderVarStyles resolves the captured border var() declarations
// per element and replays them through libcss, overwriting the covered
// border slots of `out`. It must run inside the compute window (the node
// data, the handler and the unit context are still alive); the grid and
// radius passes below it need none of that. sheets is the raw author text
// — NewCustomProperties re-captures it as the pure function of the text
// it is, so the side table matches the one parseSheet produced for libcss.
func applyBorderVarStyles(doc NodeIndex, sheets []string, out []Style,
	unitCtx *C.css_unit_ctx, viewportWidth float64,
	handler *C.css_select_handler, pw unsafe.Pointer) error {
	cp := NewCustomPropertiesAtViewport(doc, sheets, viewportWidth)
	groups := collectBorderVarBids(doc, cp)
	if len(groups) == 0 {
		return nil
	}

	ids := make([]int, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	// The same media struct the author sheets' @media rules match against.
	var media C.css_media
	C.cssgo_media_init(&media, C.double(viewportWidth))

	bidCount := 0
	for _, id := range ids {
		list := groups[id]
		sort.SliceStable(list, func(i, j int) bool {
			return borderVarWeaker(list[i].bid, list[j].bid)
		})
		for i := range list {
			bidCount++
			if err := replayBorderVarBid(&list[i], bidCount, out, unitCtx, &media, handler, pw); err != nil {
				return err
			}
		}
		// mapStyle's invariant, re-applied after the overwrites: a side
		// whose style is none takes no space even when the cascade (or
		// just the new style slot) leaves a width standing.
		for side := 0; side < 4; side++ {
			if out[id].BorderStyle[side] == BorderNone {
				out[id].BorderWidth[side] = 0.0
			}
		}
	}
	return nil
}

// collectBorderVarBids matches every captured border var() declaration to
// the elements its selector selects (the manual cascades' own selector
// machinery), resolves its value for that element, and drops the
// unresolvable ones before they ever bid. Inline style="" declarations
// capture and bid like the grid/radius inline pass: inline:true, order
// u32::MAX.
func collectBorderVarBids(doc NodeIndex, cp *CustomProperties) map[int][]borderVarBid {
	groups := map[int][]borderVarBid{}
	consider := func(id int, decl CustomDecl, selector string, bid cascadeBid) {
		if strings.HasPrefix(decl.Property, "--") {
			return // custom properties only feed the resolver map
		}
		if _, _, _, scoped := borderVarScope(decl.Property); !scoped {
			return // captured, not in this phase's scope
		}
		value, ok := cp.Resolve(id, decl.Value)
		if !ok {
			// Unresolvable: invalid at computed-value time. The manual
			// cascades drop unparsable declarations before bidding (a
			// gridDeclarations span that fails to parse never reaches a
			// bid) — the same drop, mirrored.
			return
		}
		groups[id] = append(groups[id], borderVarBid{
			element:  id,
			bid:      bid,
			property: decl.Property,
			value:    value,
			selector: selector,
			media:    decl.Media,
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
				consider(id, decl, selector, cascadeBid{
					important:   decl.Important,
					specificity: specificity,
					order:       decl.Order,
					layerRank:   cp.layerRank(decl.Layer),
				})
			}
		}
	}

	for id := 0; id < doc.Len(); id++ {
		raw, ok := doc.Attr(id, "style")
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		_, captured := CaptureInline(raw)
		for _, decl := range captured {
			consider(id, decl, "", cascadeBid{
				important: decl.Important,
				inline:    true,
				order:     ^uint32(0), // u32::MAX
				layerRank: cp.layerRank(decl.Layer),
			})
		}
	}
	return groups
}

// replayBorderVarBid feeds one resolved declaration back through libcss
// and copies the covered slots out of the result. The declaration travels
// with a probe colour in a one-rule sheet: a rule whose selector (or
// captured @media condition) libcss does not accept never reaches the
// computed style, and the probe tells the pass so. A declaration whose
// resolved text libcss rejects parses away on its own — the probe stays
// and the covered slots read back their initial values, which is what the
// radius cascade does for a winning declaration it cannot parse.
func replayBorderVarBid(bid *borderVarBid, urlCount int, out []Style,
	unitCtx *C.css_unit_ctx, media *C.css_media,
	handler *C.css_select_handler, pw unsafe.Pointer) error {
	body := "color:#dead00;" + bid.property + ":" + bid.value
	inline := bid.selector == ""
	text := body
	if !inline {
		if bid.media != "" {
			text = "@media " + bid.media + " {" + bid.selector + "{" + body + "}}"
		} else {
			text = bid.selector + "{" + body + "}"
		}
	}
	sheet, err := parseSheet(text, "browser-lite://varborder/"+strconv.Itoa(urlCount), inline)
	if err != nil {
		return err
	}
	defer C.css_stylesheet_destroy(sheet)

	var selCtx *C.css_select_ctx
	if err := check(C.css_select_ctx_create(&selCtx), "css_select_ctx_create (var borders)"); err != nil {
		return err
	}
	defer C.css_select_ctx_destroy(selCtx)

	var inlineSheet *C.css_stylesheet
	if inline {
		inlineSheet = sheet
	} else if err := check(C.css_select_ctx_append_sheet(selCtx, sheet, C.css_origin(cssOriginAuthor), nil),
		"css_select_ctx_append_sheet (var borders)"); err != nil {
		return err
	}

	var results *C.css_select_results
	if err := check(C.css_select_style(selCtx, nodePtr(bid.element), unitCtx, media, inlineSheet,
		handler, pw, &results), "css_select_style (var borders)"); err != nil {
		return err
	}
	defer C.css_select_results_destroy(results)

	style := results.styles[0]
	if style == nil {
		return nil
	}
	var probe C.css_color
	if C.css_computed_color(style, &probe) != C.uint8_t(cssColorColor) || uint32(probe) != borderVarProbeColor {
		return nil // the rule did not match this element at this viewport
	}
	widths, styles, colors, _ := borderVarScope(bid.property)
	readComputedBorders(style, unitCtx, out[bid.element].Color, widths, styles, colors, &out[bid.element])
	return nil
}
