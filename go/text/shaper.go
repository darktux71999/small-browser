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
// line_height_absolute is the CSS used value (font-size x factor,
// text.LineHeightAbsolute) in Pango units: CSS 2.1 scales the font-size,
// not the font's natural height (S1).
static PangoLayout *textgo_layout_new(PangoContext *ctx, const char *text, const char *family,
		int size_pango, int weight, int italic, int width_pango, int align, int justify,
		int line_height_absolute, int has_spacing, int spacing_pango) {
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
	pango_font_description_free(desc);
	pango_layout_set_text(layout, text, -1);
	pango_layout_set_width(layout, width_pango);
	pango_layout_set_alignment(layout, (PangoAlignment)align);
	if (justify) {
		pango_layout_set_justify(layout, TRUE);
	}
	// Line height and tracking are attributes in Pango 1.58: the layout has
	// no setter for them (src/layout.rs:1884-1898). The line height is the
	// absolute CSS used value in Pango units; the factor variant would
	// scale the font's natural height instead of the font-size (S1).
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

static void textgo_pixel_size(PangoLayout *layout, int *w, int *h) {
	pango_layout_get_pixel_size(layout, w, h);
}

static int textgo_line_count(PangoLayout *layout) {
	return pango_layout_get_line_count(layout);
}

// One visual line as Pango placed it. The struct crosses cgo by value, so
// no Go pointer ever reaches C. extents/positions follow the oracle's reads
// (src/layout.rs:1702-1719): logical extents in whole pixels, absolute
// position from index_to_pos in Pango units.
typedef struct {
	int x;
	int y;
	int w;
	int h;
	int start;
	int len;
} textgo_line;

static textgo_line textgo_line_info(PangoLayout *layout, int index) {
	textgo_line out = {0, 0, 0, 0, 0, 0};
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
	// LineHeight: unitless CSS ratio (go/css resolves normal, inherit and
	// percentages to 1.2). Measure scales the font-size by it into the
	// absolute line box (S1), never the font's natural height.
	LineHeight float64
	// TextAlign selects the Pango alignment (Justify keeps left alignment
	// plus the justify flag).
	TextAlign Align
	// LetterSpacing in CSS pixels.
	LetterSpacing float64
}

// Line is one visual line as Pango placed it, relative to the run origin.
type Line struct {
	X, Y, W, H float64
	Text       string
}

// Measured is a wrapped run: its total height plus one entry per visual
// line, mirroring MeasuredText (src/layout.rs:1650-1653).
type Measured struct {
	Height float64
	Lines  []Line
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
		C.int(LineHeightAbsolute(size, style.LineHeight)), C.int(hasSpacing), C.int(spacing))
	if layout == nil {
		return Measured{}
	}
	defer C.textgo_layout_free(layout)

	var pw, ph C.int
	C.textgo_pixel_size(layout, &pw, &ph)
	out := Measured{Height: float64(ph)}
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
			X:    float64(info.x) / PangoScale,
			Y:    float64(info.y) / PangoScale,
			W:    float64(info.w),
			H:    float64(info.h),
			Text: strings.TrimSpace(slice),
		})
	}
	return out
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
