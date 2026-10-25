package css

/*
#cgo pkg-config: libcss libwapcaplet
#include "glue.h"
*/
import "C"

import (
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

type Sheet struct {
	// URL is the sheet's own URL (a base for the resolve callback).
	URL string
	// CSS is the raw stylesheet text.
	CSS string
}

// computeLock mirrors the Rust COMPUTE_LOCK: libwapcaplet interns strings
// in a process-wide dictionary with non-atomic refcounts, so concurrent
// selection must never overlap. The engine renders one document at a time;
// the lock also keeps tests that compute styles in parallel from
// corrupting that dictionary.
var computeLock sync.Mutex

// Compute computes styles for the document's <style> sheets; each sheet
// gets the synthetic URL browser-lite://sheet/{i} (like the Rust legacy
// compute path).
func Compute(doc NodeIndex, sheets []string, viewportWidth float64) ([]Style, []string, error) {
	inputs := make([]Sheet, len(sheets))
	for i, cssText := range sheets {
		inputs[i] = Sheet{URL: "browser-lite://sheet/" + strconv.Itoa(i), CSS: cssText}
	}
	return computeInner(doc, inputs, viewportWidth)
}

// ComputeSheets computes styles for explicitly URLed sheets (the loader
// entry point of compute_with_sheets, minus the fetching this unit does
// not do).
func ComputeSheets(doc NodeIndex, sheets []Sheet, viewportWidth float64) ([]Style, []string, error) {
	return computeInner(doc, sheets, viewportWidth)
}

// Stylesheets collects the document's <style> sections, in document order,
// mirroring css_libcss::stylesheets. A <style> whose ancestors include a
// <noscript> is not collected: with scripting enabled Firefox treats the
// noscript contents as raw text and never applies their CSS, so the
// phantom banner sheets must stay out of the author cascade
// (leading-fidelity S2, odd/tasks/leading-fidelity.md). The DOM itself
// keeps the parsed markup (the P1 parity with the oracle).
func Stylesheets(doc NodeIndex) []string {
	var out []string
	for _, id := range doc.QuerySelectorAll("style") {
		if HasNoscriptAncestor(doc, id) {
			continue
		}
		text := doc.TextContent(id)
		if strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	return out
}

// HasNoscriptAncestor reports whether the node has a <noscript> ancestor
// element. It is the one noscript rule the stylesheet-collection paths
// share: go/css Stylesheets and the CLI loader's inline-style collection
// both drop what it flags, matching Firefox with scripting enabled.
func HasNoscriptAncestor(doc NodeIndex, id int) bool {
	for cur, ok := doc.Parent(id); ok; cur, ok = doc.Parent(cur) {
		if !doc.IsText(cur) && strings.EqualFold(doc.Tag(cur), "noscript") {
			return true
		}
	}
	return false
}

// UAStyleSheet is the minimal user-agent stylesheet, copied verbatim from
// css_libcss.rs. libcss ships none, so display defaults, document margins
// and heading sizes are the client's responsibility. Rules are applied at
// the UA origin, which loses to every author rule in the cascade; this
// sheet therefore only fills gaps. It deliberately stays small and
// CSS 2.1-only.
const UAStyleSheet = `
html, body, div, p, section, article, header, footer, main, nav, form,
fieldset, figure, blockquote, pre, ul, ol, li, table,
h1, h2, h3, h4, h5, h6 {
    display: block;
    margin: 0;
}
head, script, style, title, meta, link {
    display: none;
}
body {
    margin: 8px;
}
p {
    margin: 16px 0;
}
blockquote {
    margin: 16px 40px;
}
ul, ol {
    margin: 16px 0;
    padding-left: 40px;
}
li {
    margin: 4px 0 4px 24px;
}
h1 { font-size: 32px; font-weight: bold; margin: 21px 0; }
h2 { font-size: 24px; font-weight: bold; margin: 19px 0; }
h3 { font-size: 19px; font-weight: bold; margin: 19px 0; }
h4 { font-size: 16px; font-weight: bold; margin: 21px 0; }
h5 { font-size: 13px; font-weight: bold; margin: 22px 0; }
h6 { font-size: 11px; font-weight: bold; margin: 25px 0; }
a:link {
    color: #0000ff;
}
strong, b {
    font-weight: bold;
}
em, i {
    font-style: italic;
}
pre, code, kbd, samp {
    font-family: monospace;
}
`

// ancestorStyle walks up the parent chain and returns the first computed
// style (the compose parent).
func ancestorStyle(styles []unsafe.Pointer, doc NodeIndex, id int) unsafe.Pointer {
	cur, ok := doc.Parent(id)
	for ok {
		if styles[cur] != nil {
			return styles[cur]
		}
		cur, ok = doc.Parent(cur)
	}
	return nil
}

// computeInner is the compute loop, mirroring compute_inner of
// css_libcss.rs (no-loader path only).
func computeInner(doc NodeIndex, sheets []Sheet, viewportWidth float64) ([]Style, []string, error) {
	if viewportWidth < 1.0 {
		viewportWidth = 1.0
	}
	computeLock.Lock()
	defer computeLock.Unlock()

	// Every node starts at the engine defaults (text nodes and the
	// synthetic #document node keep them: the loop below skips both),
	// mirroring `vec![Style::default(); doc.nodes.len()]`.
	out := make([]Style, doc.Len())
	for i := range out {
		out[i] = New()
	}
	if doc.Root() < len(out) {
		out[doc.Root()].Display = DisplayBlock
	}

	selectCtxPtr, err := newSelectHandle()
	if err != nil {
		return nil, nil, err
	}
	defer selectCtxPtr.destroy()

	// Sheets parsed for this run, freed in reverse order of creation.
	parsed := []*C.css_stylesheet{}
	defer func() {
		for _, sheet := range parsed {
			C.css_stylesheet_destroy(sheet)
		}
	}()

	appendChild := func(sheet *C.css_stylesheet, origin C.uint32_t, what string) error {
		if err := check(C.css_select_ctx_append_sheet(selectCtxPtr.ctx, sheet, C.css_origin(origin), nil), what); err != nil {
			return err
		}
		parsed = append(parsed, sheet)
		return nil
	}

	ua, err := parseSheet(UAStyleSheet, "browser-lite://ua", false)
	if err != nil {
		return nil, nil, err
	}
	if err := appendChild(ua, cssOriginUA, "css_select_ctx_append_sheet (ua)"); err != nil {
		return nil, nil, err
	}

	for i, input := range sheets {
		sheet, err := parseSheet(input.CSS, input.URL, false)
		if err != nil {
			return nil, nil, err
		}
		if err := appendChild(sheet, cssOriginAuthor, "css_select_ctx_append_sheet (author)"); err != nil {
			return nil, nil, err
		}
		_ = i
	}

	unitCtx, media := C.css_unit_ctx{}, C.css_media{}
	C.cssgo_unit_ctx_init(&unitCtx, C.double(viewportWidth), defaultViewportHeight, defaultFontSize, 96.0)
	C.cssgo_media_init(&media, C.double(viewportWidth))
	handler := C.cssgo_get_handler()
	// The select context mirrors Rust's SelectCtx::new: only the DOM view,
	// the class arrays and node data fill in during selection.
	ctx := &selectCtx{doc: doc}
	pw := registerCtx(ctx)
	defer unregisterCtx(pw)

	var warnings []string

	var styles []unsafe.Pointer
	if doc.Len() > 0 {
		styles = make([]unsafe.Pointer, doc.Len())
	}

	for id := 0; id < doc.Len(); id++ {
		if doc.IsText(id) || id == doc.Root() {
			continue
		}
		parentStyle := ancestorStyle(styles, doc, id)

		// style="" is a declaration list: its own sheet, not the rule
		// sheets.
		var inline *C.css_stylesheet
		if decls, ok := doc.Attr(id, "style"); ok && strings.TrimSpace(decls) != "" {
			var err error
			inline, err = parseSheet(decls, "browser-lite://inline/"+strconv.Itoa(id), true)
			if err != nil {
				return nil, nil, err
			}
			parsed = append(parsed, inline)
		}

		var results *C.css_select_results
		if err := check(C.css_select_style(selectCtxPtr.ctx, nodePtr(id),
			&unitCtx, &media, inline, handler, unsafe.Pointer(pw), &results),
			"css_select_style"); err != nil {
			return nil, nil, err
		}

		partial := results.styles[0]
		if partial == nil {
			// libcss did not hand a style back (malformed input): keep the
			// engine defaults for this node, as the teardown-safe fallback.
			C.css_select_results_destroy(results)
			continue
		}

		// The root element (no parent according to parent_node) leaves
		// css_select_style with absolute values already resolved: composing
		// it against NULL hits CSS_BADPARM when inherited properties are
		// copied. Every other element composes.
		style := partial
		if parentStyle != nil {
			var composed *C.css_computed_style
			if err := check(C.css_computed_style_compose(
				(*C.css_computed_style)(parentStyle), partial, &unitCtx, &composed),
				"css_computed_style_compose"); err != nil {
				return nil, nil, err
			}
			C.css_computed_style_destroy(partial)
			style = composed
		}
		// The pointer moves to `styles`; it is freed during teardown.
		results.styles[0] = nil
		C.css_select_results_destroy(results)

		styles[id] = unsafe.Pointer(style)
		out[id] = mapStyle(style, &unitCtx, isRootElement(doc, id))
	}

	// The author sheet texts, as collected: the manual passes below read
	// the properties libcss does not compute from the same text.
	cssTexts := make([]string, len(sheets))
	for i, input := range sheets {
		cssTexts[i] = input.CSS
	}

	// var() borders (T2/S3): the rewriter stored the declarations libcss
	// 0.9.2 discards in a side table. The border family resolves per
	// element and replays through libcss' own select — the same border
	// expansion and the same css_media @media match the non-var path uses
	// (see bordervar.go). This runs while the node data, the handler and
	// the unit context are still alive.
	if err := applyBorderVarStyles(doc, cssTexts, out, &unitCtx, viewportWidth, handler, pw); err != nil {
		return nil, nil, err
	}
	// Remaining mapped computed-style families (color, typography, box
	// geometry and gaps) use the same captured resolver and replay the
	// resolved declarations through libcss while the parent computed styles
	// are still alive (CSS Cascading 5 §6; Custom Properties §3).
	if err := applyComputedVarStyles(doc, cssTexts, out, styles, &unitCtx, viewportWidth, handler, pw); err != nil {
		return nil, nil, err
	}

	// Teardown: libcss node data, styles, class arrays, sheets and context.
	for _, entry := range ctx.nodeData {
		C.css_libcss_node_data_handler(handler, C.CSS_NODE_DELETED, unsafe.Pointer(pw),
			nodePtr(entry.id), nil, entry.data)
	}
	for _, style := range styles {
		if style != nil {
			C.css_computed_style_destroy((*C.css_computed_style)(style))
		}
	}
	for _, array := range ctx.classArrays {
		C.free(array.raw)
	}

	// Grid placement properties are invisible to libcss: they are read from
	// the same author sheets here, after the cascade above fixed display.
	// The viewport travels along so @media-guarded track lists apply only
	// when their query matches (height is the same 600px libcss selects
	// against).
	applyGridStyles(doc, cssTexts, viewportWidth, defaultViewportHeight, out)
	// border-radius is invisible to libcss too (no computed getter): the
	// same manual cascade reads it from the same author sheets.
	applyBorderRadiusStyles(doc, cssTexts, viewportWidth, out)

	// The background family's manual cascade (S10): the gradient, var()
	// and cascade-layer declarations libcss 0.9.2 discards, read from the
	// same author sheets. The viewport gates the captured @media
	// conditions. Documents without captured background declarations are
	// untouched (see applyBackgroundStyles).
	applyBackgroundStyles(doc, cssTexts, viewportWidth, out)

	// @font-face never reaches libcss (the rewriter strips it): the font
	// files are fetched and registered with fontconfig later (T10's loader
	// wave). The no-loader path fetches nothing -- its contract says so --
	// and reports the dropped faces instead of hiding them.
	for _, input := range sheets {
		for _, face := range FontFaceDeclarations(input.CSS) {
			warnings = append(warnings, "skipping @font-face for family "+
				strconv.Quote(face.Family)+FaceDetail(face)+
				": styles were computed without subresource loading")
		}
	}

	return out, warnings, nil
}

// selectHandle owns one css_select_ctx for a compute run.
type selectHandle struct {
	ctx *C.css_select_ctx
}

func newSelectHandle() (*selectHandle, error) {
	var ctx *C.css_select_ctx
	if err := check(C.css_select_ctx_create(&ctx), "css_select_ctx_create"); err != nil {
		return nil, err
	}
	return &selectHandle{ctx: ctx}, nil
}

func (h *selectHandle) destroy() {
	C.css_select_ctx_destroy(h.ctx)
}

// isRootElement of the NodeIndex view: no parent at all, or the direct
// child of the synthetic document root (mirroring is_root_element).
func isRootElement(doc NodeIndex, id int) bool {
	parent, ok := doc.Parent(id)
	if !ok {
		return true
	}
	return parent == doc.Root()
}
