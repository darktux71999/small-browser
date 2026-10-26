package text

/*
#cgo pkg-config: pangocairo pango cairo gobject-2.0 fontconfig
#include <stdlib.h>
#include <string.h>
#include <cairo.h>
#include <pango/pangocairo.h>
#include <fontconfig/fontconfig.h>

// Scratch measuring context: a Pango context hanging off a throwaway cairo
// image surface, mirroring TextShaper::new (src/layout.rs:1666-1680). The
// real painting context is built by paint from its own cairo target; both
// use 72 dpi so they agree on metrics.
typedef struct {
	cairo_surface_t *surface;
	cairo_t *cr;
	PangoContext *ctx;
} textgo_ctx;

static void textgo_add_span(PangoLayout *layout, int start, int end,
		const char *family, int size_pango, int weight, int italic,
		int spacing_pango, double r, double g, double b, double a) {
	PangoAttrList *attrs = pango_layout_get_attributes(layout);
	attrs = attrs ? pango_attr_list_copy(attrs) : pango_attr_list_new();
	if (!attrs) {
		return;
	}
	PangoFontDescription *desc = pango_font_description_new();
	pango_font_description_set_family(desc, family);
	pango_font_description_set_size(desc, size_pango);
	pango_font_description_set_weight(desc, (PangoWeight)weight);
	pango_font_description_set_style(desc, italic ? PANGO_STYLE_ITALIC : PANGO_STYLE_NORMAL);
	PangoAttribute *font = pango_attr_font_desc_new(desc);
	pango_font_description_free(desc);
	font->start_index = (guint)start;
	font->end_index = (guint)end;
	pango_attr_list_insert(attrs, font);
	PangoAttribute *spacing = pango_attr_letter_spacing_new(spacing_pango);
	spacing->start_index = (guint)start;
	spacing->end_index = (guint)end;
	pango_attr_list_insert(attrs, spacing);
	PangoAttribute *foreground = pango_attr_foreground_new(
		(guint16)(r * 65535.0), (guint16)(g * 65535.0), (guint16)(b * 65535.0));
	foreground->start_index = (guint)start;
	foreground->end_index = (guint)end;
	pango_attr_list_insert(attrs, foreground);
	PangoAttribute *alpha = pango_attr_foreground_alpha_new((guint16)(a * 65535.0));
	alpha->start_index = (guint)start;
	alpha->end_index = (guint)end;
	pango_attr_list_insert(attrs, alpha);
	pango_layout_set_attributes(layout, attrs);
	pango_attr_list_unref(attrs);
}

static textgo_ctx *textgo_new(void) {
	textgo_ctx *c = (textgo_ctx *)calloc(1, sizeof *c);
	if (!c) {
		return NULL;
	}
	c->surface = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, 8, 8);
	if (cairo_surface_status(c->surface) != CAIRO_STATUS_SUCCESS) {
		free(c);
		return NULL;
	}
	c->cr = cairo_create(c->surface);
	if (cairo_status(c->cr) != CAIRO_STATUS_SUCCESS) {
		cairo_surface_destroy(c->surface);
		free(c);
		return NULL;
	}
	c->ctx = pango_cairo_create_context(c->cr);
	if (!c->ctx) {
		cairo_destroy(c->cr);
		cairo_surface_destroy(c->surface);
		free(c);
		return NULL;
	}
	// 72 dpi: 1pt == 1px, so CSS px sizes map 1:1 to Pango font sizes.
	pango_cairo_context_set_resolution(c->ctx, 72.0);
	return c;
}

static void textgo_free(textgo_ctx *c) {
	if (!c) {
		return;
	}
	g_object_unref(c->ctx);
	cairo_destroy(c->cr);
	cairo_surface_destroy(c->surface);
	free(c);
}

// One Pango layout built with the font, width, alignment, line height and
// tracking the oracle sets in pango::create_layout (src/layout.rs:1828-1902).
// NUL would truncate the C string, so the caller replaces it with a space
// first; the explicit length is then redundant and -1 is equivalent.
// line_height_absolute is the CSS used value for numeric line-height. Normal
// uses the Pango font metrics' recommended natural logical height instead.
static void textgo_font_metrics(PangoContext *ctx, const char *family,
		int size_pango, int weight, int italic,
		int *ascent, int *descent, int *height);
static int textgo_natural_line_height(PangoFontMetrics *metrics) {
	int height = pango_font_metrics_get_height(metrics);
	int extents = pango_font_metrics_get_ascent(metrics) + pango_font_metrics_get_descent(metrics);
	// Some hinted fonts report a recommended height one Pango pixel below
	// their separately rounded ascent+descent; natural leading cannot be negative.
	return height > extents ? height : extents;
}
static PangoLayout *textgo_layout_new(PangoContext *ctx, const char *text, const char *family,
		int size_pango, int weight, int italic, int width_pango, int align, int justify,
		int line_height_absolute, int line_height_normal, int has_spacing, int spacing_pango) {
	PangoLayout *layout = pango_layout_new(ctx);
	if (!layout) {
		return NULL;
	}
	PangoFontDescription *desc = pango_font_description_from_string("Sans");
	pango_font_description_set_family(desc, family);
	pango_font_description_set_size(desc, size_pango);
	pango_font_description_set_weight(desc, (PangoWeight)weight);
	pango_font_description_set_style(desc, italic ? PANGO_STYLE_ITALIC : PANGO_STYLE_NORMAL);
	pango_layout_set_font_description(layout, desc);
	if (line_height_normal) {
		PangoFontMetrics *metrics = pango_context_get_metrics(ctx, desc, pango_language_get_default());
		if (metrics) {
			line_height_absolute = textgo_natural_line_height(metrics);
			pango_font_metrics_unref(metrics);
		}
	}
	pango_font_description_free(desc);
	pango_layout_set_text(layout, text, -1);
	pango_layout_set_width(layout, width_pango);
	pango_layout_set_alignment(layout, (PangoAlignment)align);
	if (justify) {
		pango_layout_set_justify(layout, TRUE);
	}
	// Line height and tracking are attributes in Pango 1.58: the layout has
	// no setter for them (src/layout.rs:1884-1898). Numeric CSS line-height
	// uses an absolute font-size-scaled value; normal uses Pango's natural
	// logical metric height. The factor attribute instead scales natural height.
	PangoAttrList *attrs = pango_attr_list_new();
	pango_attr_list_insert(attrs, pango_attr_line_height_new_absolute(line_height_absolute));
	if (has_spacing) {
		pango_attr_list_insert(attrs, pango_attr_letter_spacing_new(spacing_pango));
	}
	pango_layout_set_attributes(layout, attrs);
	pango_attr_list_unref(attrs);
	return layout;
}

static void textgo_layout_free(PangoLayout *layout) {
	g_object_unref(layout);
}

static void textgo_font_metrics(PangoContext *ctx, const char *family,
		int size_pango, int weight, int italic,
		int *ascent, int *descent, int *height) {
	PangoFontDescription *desc = pango_font_description_from_string("Sans");
	pango_font_description_set_family(desc, family);
	pango_font_description_set_size(desc, size_pango);
	pango_font_description_set_weight(desc, (PangoWeight)weight);
	pango_font_description_set_style(desc, italic ? PANGO_STYLE_ITALIC : PANGO_STYLE_NORMAL);
	PangoFontMetrics *metrics = pango_context_get_metrics(ctx, desc, pango_language_get_default());
	*ascent = *descent = *height = 0;
	if (metrics) {
		*ascent = pango_font_metrics_get_ascent(metrics);
		*descent = pango_font_metrics_get_descent(metrics);
		*height = textgo_natural_line_height(metrics);
		pango_font_metrics_unref(metrics);
	}
	pango_font_description_free(desc);
}

static void textgo_pixel_size(PangoLayout *layout, int *w, int *h) {
	pango_layout_get_pixel_size(layout, w, h);
}

static int textgo_line_count(PangoLayout *layout) {
	return pango_layout_get_line_count(layout);
}

// One visual line as Pango placed it. Baseline is relative to the line's
// logical top and carries the actual font metrics into inline atom placement.
// The struct crosses cgo by value, so
// no Go pointer ever reaches C. extents/positions follow the oracle's reads
// (src/layout.rs:1702-1719): logical extents in whole pixels, absolute
// position from index_to_pos in Pango units.
typedef struct {
	int x;
	int y;
	int w;
	int h;
	int baseline;
	int start;
	int len;
} textgo_line;

static textgo_line textgo_line_info(PangoLayout *layout, int index) {
	textgo_line out = {0, 0, 0, 0, 0, 0, 0};
	PangoLayoutLine *line = pango_layout_get_line(layout, index);
	if (!line) {
		return out;
	}
	PangoRectangle ink, logical;
	pango_layout_line_get_pixel_extents(line, &ink, &logical);
	// Line extents are relative to the line itself; the position of the
	// line's first character is relative to the layout origin and already
	// includes the alignment offset.
	PangoRectangle pos = {0, 0, 0, 0};
	int start = line->start_index > 0 ? line->start_index : 0;
	int len = line->length > 0 ? line->length : 0;
	pango_layout_index_to_pos(layout, start, &pos);
	PangoLayoutIter *iter = pango_layout_get_iter(layout);
	for (int i = 0; i < index; i++) {
		if (!pango_layout_iter_next_line(iter)) {
			break;
		}
	}
	PangoRectangle iter_ink, iter_logical;
	pango_layout_iter_get_line_extents(iter, &iter_ink, &iter_logical);
	out.baseline = pango_layout_iter_get_baseline(iter) - iter_logical.y;
	pango_layout_iter_free(iter);
	out.x = pos.x;
	out.y = pos.y;
	out.w = logical.width;
	out.h = logical.height;
	out.start = start;
	out.len = len;
	return out;
}

// Whether fontconfig really has this family. FcFontMatch never fails: for
// an unknown family it returns its best guess, which is exactly the silent
// collapse that kept every stack on one font. The match counts only when
// the returned family IS the requested one, compared the way fontconfig
// compares family names (src/layout.rs:2005-2031).
static int textgo_family_available(const char *family) {
	FcPattern *pat = FcPatternCreate();
	if (!pat) {
		return 0;
	}
	FcPatternAddString(pat, FC_FAMILY, (const FcChar8 *)family);
	FcResult res = FcResultNoMatch;
	FcPattern *m = FcFontMatch(NULL, pat, &res);
	FcPatternDestroy(pat);
	if (!m) {
		return 0;
	}
	int ok = 0;
	if (res == FcResultMatch) {
		FcChar8 *val = NULL;
		for (int i = 0; FcPatternGetString(m, FC_FAMILY, i, &val) == FcResultMatch && val; i++) {
			if (FcStrCmpIgnoreCase(val, (const FcChar8 *)family) == 0) {
				ok = 1;
				break;
			}
			val = NULL;
		}
	}
	FcPatternDestroy(m);
	return ok;
}
*/
import "C"

import (
	"strings"
	"sync"
	"unsafe"
)

// PangoScale is Pango's fixed-point scale for font sizes and lengths
// (src/layout.rs:1746).
const PangoScale = 1024.0

// Align is the horizontal alignment of text inside its wrap width,
// mirroring css::TextAlign as the shaper consumes it.
type Align uint8

// Alignment values.
const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
	AlignJustify
)

// Style is the typography a measurement needs. It duplicates the fields
// layout reads from the computed style so this package stays DOM- and
// CSS-free; layout adapts css.Style into it.
type Style struct {
	// FontFamilies is the full computed family list in author order.
	FontFamilies []string
	// FontFamily is the legacy single-family view.
	FontFamily string
	// FontSize in CSS pixels.
	FontSize float64
	// FontWeight: 100-900.
	FontWeight uint32
	// Italic is font-style italic (or oblique).
	Italic bool
	// LineHeight is the unitless CSS ratio for numeric/dimension values.
	LineHeight float64
	// LineHeightNormal selects the Pango font's natural logical line height.
	LineHeightNormal bool
	// TextAlign selects the Pango alignment (Justify keeps left alignment
	// plus the justify flag).
	TextAlign Align
	// LetterSpacing in CSS pixels.
	LetterSpacing float64
	// Color is the text color in normalized RGBA components.
	Color Color
}

// Color is a normalized RGBA text color.
type Color struct{ R, G, B, A float64 }

// Line is one visual line as Pango placed it, relative to the run origin.
type Line struct {
	X, Y, W, H float64
	// Start and End are the byte range of this line in the measured text.
	Start, End int
	// Baseline is Pango's actual baseline offset from this line's logical top.
	Baseline float64
	Text     string
}

// FontMetrics are the resolved Pango font's typographic ascent, descent, and
// recommended natural line height, in CSS pixels.
type FontMetrics struct {
	Ascent, Descent, Height float64
}

// Measured is a wrapped run: its total height plus one entry per visual
// line, mirroring MeasuredText (src/layout.rs:1650-1653).
type Measured struct {
	Height  float64
	Metrics FontMetrics
	Lines   []Line
}

// Span overrides the typography and color over a UTF-8 byte range.
type Span struct {
	Start, End int
	Style      Style
}

// Shaper is a Pango context used only for measuring. It hangs off a scratch
// image surface; Close releases it. It is safe for sequential use (Pango
// contexts are not thread-safe; layout measures on one goroutine).
type Shaper struct {
	ctx *C.textgo_ctx
}

// New builds the scratch measuring context (src/layout.rs:1666).
func New() (*Shaper, error) {
	ctx := C.textgo_new()
	if ctx == nil {
		return nil, errNoContext
	}
	return &Shaper{ctx: ctx}, nil
}

// Close releases the Pango context and its scratch surface.
func (s *Shaper) Close() {
	if s == nil || s.ctx == nil {
		return
	}
	C.textgo_free(s.ctx)
	s.ctx = nil
}

// Measure lays text out inside width and returns the total height plus one
// entry per visual line, mirroring TextShaper::measure
// (src/layout.rs:1682-1729). Only C-allocated strings cross into cgo; no Go
// pointer ever reaches C.
func (s *Shaper) Measure(text string, style Style, width float64) Measured {
	return s.measure(text, style, width, nil)
}

// MeasureStyled measures a text run with inline typography spans.
func (s *Shaper) MeasureStyled(text string, style Style, width float64, spans []Span) Measured {
	return s.measure(text, style, width, spans)
}

func (s *Shaper) measure(text string, style Style, width float64, spans []Span) Measured {
	// NUL would truncate the C string (src/layout.rs:1867-1868).
	text = strings.ReplaceAll(text, "\x00", " ")
	ctext := C.CString(text)
	defer C.free(unsafe.Pointer(ctext))
	cfamily := C.CString(ResolveFamily(familyStack(style)))
	defer C.free(unsafe.Pointer(cfamily))

	w := style.FontWeight
	if w < 100 {
		w = 100
	}
	if w > 900 {
		w = 900
	}
	italic := 0
	if style.Italic {
		italic = 1
	}
	align, justify := 0, 0
	switch style.TextAlign {
	case AlignCenter:
		align = 1
	case AlignRight:
		align = 2
	case AlignJustify:
		justify = 1
	}
	spacing := 0
	hasSpacing := 0
	if style.LetterSpacing != 0 {
		hasSpacing = 1
		spacing = int(style.LetterSpacing * PangoScale)
	}
	size := style.FontSize
	if size < 0 {
		size = 0
	}
	wpx := width
	if wpx < 1 {
		wpx = 1
	}
	layout := C.textgo_layout_new(s.ctx.ctx, ctext, cfamily,
		C.int(size*PangoScale), C.int(w), C.int(italic),
		C.int(wpx*PangoScale), C.int(align), C.int(justify),
		C.int(LineHeightAbsolute(size, style.LineHeight)), C.int(boolInt(style.LineHeightNormal)), C.int(hasSpacing), C.int(spacing))
	if layout == nil {
		return Measured{}
	}
	defer C.textgo_layout_free(layout)
	for _, span := range spans {
		if span.Start < 0 || span.End <= span.Start || span.End > len(text) {
			continue
		}
		spanFamily := C.CString(ResolveFamily(familyStack(span.Style)))
		C.textgo_add_span(layout, C.int(span.Start), C.int(span.End), spanFamily,
			C.int(span.Style.FontSize*PangoScale), C.int(clampWeight(span.Style.FontWeight)),
			C.int(boolInt(span.Style.Italic)), C.int(span.Style.LetterSpacing*PangoScale),
			C.double(span.Style.Color.R), C.double(span.Style.Color.G),
			C.double(span.Style.Color.B), C.double(span.Style.Color.A))
		C.free(unsafe.Pointer(spanFamily))
	}

	var ascent, descent, naturalHeight C.int
	C.textgo_font_metrics(s.ctx.ctx, cfamily, C.int(size*PangoScale), C.int(w), C.int(italic), &ascent, &descent, &naturalHeight)
	var pw, ph C.int
	C.textgo_pixel_size(layout, &pw, &ph)
	out := Measured{
		Height: float64(ph),
		Metrics: FontMetrics{
			Ascent:  float64(ascent) / PangoScale,
			Descent: float64(descent) / PangoScale,
			Height:  float64(naturalHeight) / PangoScale,
		},
	}
	count := int(C.textgo_line_count(layout))
	raw := []byte(text)
	for i := 0; i < count; i++ {
		info := C.textgo_line_info(layout, C.int(i))
		start, length := int(info.start), int(info.len)
		slice := ""
		if start >= 0 && length >= 0 && start+length <= len(raw) {
			slice = string(raw[start : start+length])
		}
		out.Lines = append(out.Lines, Line{
			X:        float64(info.x) / PangoScale,
			Y:        float64(info.y) / PangoScale,
			W:        float64(info.w),
			H:        float64(info.h),
			Start:    start,
			End:      start + length,
			Baseline: float64(info.baseline) / PangoScale,
			Text:     strings.TrimSpace(slice),
		})
	}
	return out
}

func clampWeight(weight uint32) uint32 {
	if weight < 100 {
		return 100
	}
	if weight > 900 {
		return 900
	}
	return weight
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// familyStack mirrors the oracle's stack choice (src/layout.rs:1844-1848):
// the full computed list in author order, or the legacy single family when
// the list is empty.
func familyStack(style Style) []string {
	if len(style.FontFamilies) == 0 {
		return []string{style.FontFamily}
	}
	return style.FontFamilies
}

// familyCache mirrors the oracle's per-process availability cache
// (src/layout.rs:2011-2031): layout asks about the same handful of
// families for every text run.
var familyCache = struct {
	sync.Mutex
	m map[string]bool
}{m: map[string]bool{}}

// familyAvailable reports whether fontconfig really has this family.
func familyAvailable(family string) bool {
	key := strings.ToLower(family)
	familyCache.Lock()
	if known, ok := familyCache.m[key]; ok {
		familyCache.Unlock()
		return known
	}
	familyCache.Unlock()
	cfamily := C.CString(family)
	defer C.free(unsafe.Pointer(cfamily))
	found := C.textgo_family_available(cfamily) != 0
	familyCache.Lock()
	familyCache.m[key] = found
	familyCache.Unlock()
	return found
}
