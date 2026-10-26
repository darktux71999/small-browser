// Package paint renders a laid-out page to pixels through Cairo via cgo,
// mirroring the Rust oracle (src/paint.rs) op for op: the same C library
// draws the same boxes, so the bytes match.
//
// The paint order mirrors draw(): a white page, then per box the background
// colour, the tiled background image, and the border ring (clipped to the
// rounded shape when the box has a radius), then the scaled <img> pixels,
// then the Pango text runs at the same 72dpi the measuring shaper uses.
//
// Frame pixels are straight (non-premultiplied) RGBA8, row-major, top-down:
// Cairo's premultiplied native-endian words are converted on the way out so
// tests and texture uploads see plain RGBA.
package paint

/*
#cgo pkg-config: cairo pangocairo pango cairo gobject-2.0 fontconfig
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include <cairo.h>
#include <pango/pangocairo.h>

typedef struct {
	cairo_surface_t *surface;
	cairo_t *cr;
} paint_t;

static paint_t *paint_new(int width, int height) {
	paint_t *p = (paint_t *)calloc(1, sizeof *p);
	if (!p) {
		return NULL;
	}
	p->surface = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, width, height);
	if (cairo_surface_status(p->surface) != CAIRO_STATUS_SUCCESS) {
		free(p);
		return NULL;
	}
	p->cr = cairo_create(p->surface);
	if (cairo_status(p->cr) != CAIRO_STATUS_SUCCESS) {
		cairo_surface_destroy(p->surface);
		free(p);
		return NULL;
	}
	return p;
}

static void paint_free(paint_t *p) {
	if (!p) {
		return;
	}
	cairo_destroy(p->cr);
	cairo_surface_destroy(p->surface);
	free(p);
}

static void paint_white(paint_t *p) {
	cairo_set_source_rgb(p->cr, 1.0, 1.0, 1.0);
	cairo_paint(p->cr);
}

static void paint_clip_viewport(paint_t *p, int width, int height) {
	cairo_rectangle(p->cr, 0.0, 0.0, (double)width, (double)height);
	cairo_clip(p->cr);
}

static void paint_translate(paint_t *p, double dx, double dy) {
	cairo_translate(p->cr, dx, dy);
}

static void paint_fill_rect(paint_t *p,
		double r, double g, double b, double a,
		double x, double y, double w, double h) {
	cairo_set_source_rgba(p->cr, r, g, b, a);
	cairo_rectangle(p->cr, x, y, w, h);
	cairo_fill(p->cr);
}

static void paint_save(paint_t *p) {
	cairo_save(p->cr);
}

static void paint_restore(paint_t *p) {
	cairo_restore(p->cr);
}

static void paint_clip_rect(paint_t *p, double x, double y, double w, double h) {
	cairo_rectangle(p->cr, x, y, w, h);
	cairo_clip(p->cr);
}

// Rounded-rectangle subpath for the box, exactly like the oracle's
// rounded_rect_path: adjacent radii that overflow a side are scaled down
// together, per CSS.
static void paint_rounded_path(paint_t *p,
		double x, double y, double w, double h,
		double tl, double tr, double br, double bl) {
	if (w <= 0.0 || h <= 0.0) {
		return;
	}
	double r[4] = {tl, tr, br, bl};
	for (int i = 0; i < 4; i++) {
		if (r[i] < 0.0) {
			r[i] = 0.0;
		}
	}
	double sums[4] = {r[0] + r[1], r[2] + r[3], r[0] + r[3], r[1] + r[2]};
	double sides[4] = {w, w, h, h};
	double scale = 1.0;
	for (int i = 0; i < 4; i++) {
		if (sums[i] > 0.0 && sides[i] / sums[i] < scale) {
			scale = sides[i] / sums[i];
		}
	}
	if (scale < 1.0) {
		for (int i = 0; i < 4; i++) {
			r[i] *= scale;
		}
	}
	tl = r[0];
	tr = r[1];
	br = r[2];
	bl = r[3];
	cairo_new_path(p->cr);
	cairo_move_to(p->cr, x + tl, y);
	cairo_line_to(p->cr, x + w - tr, y);
	if (tr > 0.0) {
		cairo_arc(p->cr, x + w - tr, y + tr, tr, -M_PI_2, 0.0);
	}
	cairo_line_to(p->cr, x + w, y + h - br);
	if (br > 0.0) {
		cairo_arc(p->cr, x + w - br, y + h - br, br, 0.0, M_PI_2);
	}
	cairo_line_to(p->cr, x + bl, y + h);
	if (bl > 0.0) {
		cairo_arc(p->cr, x + bl, y + h - bl, bl, M_PI_2, M_PI);
	}
	cairo_line_to(p->cr, x, y + tl);
	if (tl > 0.0) {
		cairo_arc(p->cr, x + tl, y + tl, tl, M_PI, 3.0 * M_PI_2);
	}
	cairo_close_path(p->cr);
}

static void paint_clip_rounded(paint_t *p,
		double x, double y, double w, double h,
		double tl, double tr, double br, double bl) {
	paint_rounded_path(p, x, y, w, h, tl, tr, br, bl);
	cairo_clip(p->cr);
}

// Straight RGBA8 to a premultiplied ARGB32 surface, the inverse of the
// frame copy below (mirrors the oracle's rgba_surface, truncating like its
// integer division). Takes ownership of rgba and frees it.
static cairo_surface_t *paint_rgba_surface(unsigned char *rgba, int width, int height) {
	cairo_surface_t *s = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, width, height);
	if (cairo_surface_status(s) != CAIRO_STATUS_SUCCESS) {
		free(rgba);
		return NULL;
	}
	unsigned char *dst = cairo_image_surface_get_data(s);
	int stride = cairo_image_surface_get_stride(s);
	for (int y = 0; y < height; y++) {
		uint32_t *row = (uint32_t *)(dst + y * stride);
		for (int x = 0; x < width; x++) {
			unsigned char *px = rgba + (y * width + x) * 4;
			unsigned a = px[3];
			uint32_t word = (a << 24)
				| ((px[0] * a / 255) << 16)
				| ((px[1] * a / 255) << 8)
				| (px[2] * a / 255);
			row[x] = word;
		}
	}
	cairo_surface_mark_dirty(s);
	free(rgba);
	return s;
}

static void paint_surface_destroy(cairo_surface_t *s) {
	cairo_surface_destroy(s);
}

// The background-image tile (S10): the image is placed at the box's origin
// plus the background-position offsets (ox/oy, CSS Backgrounds 3 §3.4
// position computation), clipped to the box, over the background colour.
// mode 0/1/2 repeat along both axes / x only / y only (the repeat-x and
// repeat-y bands stay inside the image's other extent, §4.2); mode 3 is
// no-repeat, one anchored instance (§4.1). Nearest keeps exact pixels, like
// the oracle's Filter::Nearest.
// iw x ih is the image's CSS size; the surface may be a finer raster of it
// (an SVG), in which case the pattern scales the surface down to CSS units.
static void paint_bg_image(paint_t *p, cairo_surface_t *img,
		double x, double y, double w, double h,
		double ox, double oy, int mode, double iw, double ih) {
	double sx = cairo_image_surface_get_width(img) / iw;
	double sy = cairo_image_surface_get_height(img) / ih;
	cairo_save(p->cr);
	cairo_rectangle(p->cr, x, y, w, h);
	cairo_clip(p->cr);
	if (mode == 1) {
		// repeat-x: paint only the image's vertical band.
		cairo_rectangle(p->cr, x, y + oy, w, ih);
		cairo_clip(p->cr);
	} else if (mode == 2) {
		// repeat-y: paint only the image's horizontal band.
		cairo_rectangle(p->cr, x + ox, y, iw, h);
		cairo_clip(p->cr);
	}
	cairo_translate(p->cr, x + ox, y + oy);
	cairo_pattern_t *pat = cairo_pattern_create_for_surface(img);
	cairo_matrix_t m;
	cairo_matrix_init_scale(&m, sx, sy);
	cairo_pattern_set_matrix(pat, &m);
	// Nearest keeps raster backgrounds pixel-exact; a scaled SVG raster is
	// downsampled, which needs the filter.
	cairo_pattern_set_filter(pat, sx == 1.0 && sy == 1.0 ? CAIRO_FILTER_NEAREST : CAIRO_FILTER_GOOD);
	cairo_pattern_set_extend(pat, mode == 3 ? CAIRO_EXTEND_NONE : CAIRO_EXTEND_REPEAT);
	cairo_set_source(p->cr, pat);
	cairo_paint(p->cr);
	cairo_pattern_destroy(pat);
	cairo_restore(p->cr);
}

// The two-stop vertical linear-gradient (S10, CSS Images 3 §3.3): a linear
// pattern from the box's top to its bottom with the two stop colours,
// painted over the background colour and clipped to the box like the image
// tile. Only the vertical subset (to bottom / legacy top) reaches this
// helper; the stop positions are dropped (documented approximation of
// §3.4 colour-stop positions).
static void paint_background_gradient(paint_t *p,
		double x, double y, double w, double h,
		double r0, double g0, double b0, double a0,
		double r1, double g1, double b1, double a1) {
	cairo_pattern_t *pat = cairo_pattern_create_linear(x, y, x, y + h);
	cairo_pattern_add_color_stop_rgba(pat, 0.0, r0, g0, b0, a0);
	cairo_pattern_add_color_stop_rgba(pat, 1.0, r1, g1, b1, a1);
	cairo_save(p->cr);
	cairo_rectangle(p->cr, x, y, w, h);
	cairo_clip(p->cr);
	cairo_set_source(p->cr, pat);
	cairo_paint(p->cr);
	cairo_restore(p->cr);
	cairo_pattern_destroy(pat);
}

// One <img> box: the image scaled into the layout box, clipped to the
// rounded shape (the outset ring itself stays square, like the oracle).
static void paint_draw_image(paint_t *p, cairo_surface_t *img,
		double x, double y, double w, double h,
		double iw, double ih,
		double tl, double tr, double br, double bl) {
	cairo_pattern_t *pat = cairo_pattern_create_for_surface(img);
	cairo_pattern_set_filter(pat, CAIRO_FILTER_GOOD);
	cairo_pattern_set_extend(pat, CAIRO_EXTEND_PAD);
	cairo_save(p->cr);
	if (tl > 0.0 || tr > 0.0 || br > 0.0 || bl > 0.0) {
		paint_rounded_path(p, x, y, w, h, tl, tr, br, bl);
	} else {
		cairo_rectangle(p->cr, x, y, w, h);
	}
	cairo_clip(p->cr);
	cairo_translate(p->cr, x, y);
	cairo_scale(p->cr, w / iw, h / ih);
	cairo_set_source(p->cr, pat);
	cairo_paint(p->cr);
	cairo_restore(p->cr);
	cairo_pattern_destroy(pat);
}

// One text run, rebuilt from the run's typography exactly like the measuring
// shaper builds it (72dpi, same family/weight/style/width/alignment/line
// height/tracking): the painted lines land where the boxes measured them.
// line_height_absolute is the numeric CSS used value (font-size x factor,
// text.LineHeightAbsolute) in Pango units. The separate normal flag selects
// the same metric-derived natural height as the measuring shaper.
typedef struct {
	const char *family;
	int size, weight, italic, spacing, start, end;
	double r, g, b, a;
} paint_span;

static void paint_show_text(paint_t *p, PangoContext *ctx,
		const char *text, const char *family,
		int size_pango, int weight, int italic,
		int width_pango, int align, int justify,
		int line_height_absolute, int line_height_normal, int has_spacing, int spacing_pango,
		const paint_span *spans, int span_count,
		double x, double y,
		double r, double g, double b, double a) {
	PangoLayout *layout = pango_layout_new(ctx);
	if (!layout) {
		return;
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
			int natural_height = pango_font_metrics_get_height(metrics);
			int extents = pango_font_metrics_get_ascent(metrics) + pango_font_metrics_get_descent(metrics);
			// Match the shaper when font metric rounding puts height below extents.
			line_height_absolute = natural_height > extents ? natural_height : extents;
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
	PangoAttrList *attrs = pango_attr_list_new();
	pango_attr_list_insert(attrs, pango_attr_line_height_new_absolute(line_height_absolute));
	if (has_spacing) {
		pango_attr_list_insert(attrs, pango_attr_letter_spacing_new(spacing_pango));
	}
	for (int i = 0; i < span_count; i++) {
		const paint_span *span = &spans[i];
		PangoFontDescription *span_desc = pango_font_description_new();
		pango_font_description_set_family(span_desc, span->family);
		pango_font_description_set_size(span_desc, span->size);
		pango_font_description_set_weight(span_desc, (PangoWeight)span->weight);
		pango_font_description_set_style(span_desc, span->italic ? PANGO_STYLE_ITALIC : PANGO_STYLE_NORMAL);
		PangoAttribute *font = pango_attr_font_desc_new(span_desc);
		pango_font_description_free(span_desc);
		font->start_index = (guint)span->start;
		font->end_index = (guint)span->end;
		pango_attr_list_insert(attrs, font);
		PangoAttribute *spacing = pango_attr_letter_spacing_new(span->spacing);
		spacing->start_index = (guint)span->start;
		spacing->end_index = (guint)span->end;
		pango_attr_list_insert(attrs, spacing);
		PangoAttribute *foreground = pango_attr_foreground_new(
			(guint16)(span->r * 65535.0), (guint16)(span->g * 65535.0), (guint16)(span->b * 65535.0));
		foreground->start_index = (guint)span->start;
		foreground->end_index = (guint)span->end;
		pango_attr_list_insert(attrs, foreground);
		PangoAttribute *alpha = pango_attr_foreground_alpha_new((guint16)(span->a * 65535.0));
		alpha->start_index = (guint)span->start;
		alpha->end_index = (guint)span->end;
		pango_attr_list_insert(attrs, alpha);
	}
	pango_layout_set_attributes(layout, attrs);
	pango_attr_list_unref(attrs);

	cairo_set_source_rgba(p->cr, r, g, b, a);
	cairo_move_to(p->cr, x, y);
	pango_cairo_update_layout(p->cr, layout);
	pango_cairo_show_layout(p->cr, layout);
	g_object_unref(layout);
}

static PangoContext *paint_text_context(paint_t *p) {
	PangoContext *ctx = pango_cairo_create_context(p->cr);
	if (ctx) {
		pango_cairo_context_set_resolution(ctx, 72.0);
	}
	return ctx;
}

static void paint_text_context_free(PangoContext *ctx) {
	g_object_unref(ctx);
}

static int paint_write_png(paint_t *p, const char *path, const char **msg) {
	cairo_status_t status = cairo_surface_write_to_png(p->surface, path);
	if (msg) {
		*msg = cairo_status_to_string(status);
	}
	return status == CAIRO_STATUS_SUCCESS;
}
*/
import "C"

import (
	"errors"
	"math"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"browser-lite/css"
	"browser-lite/layout"
	"browser-lite/text"
)

// ViewportWidth and ViewportHeight are the fixed interactive window size in
// CSS pixels (the shell's VIEWPORT_WIDTH/VIEWPORT_HEIGHT, S9): a scrolled
// render paints exactly this viewport, translated by the scroll offset.
const (
	ViewportWidth  = 1000
	ViewportHeight = 760
)

// Frame is one painted frame: straight (non-premultiplied) RGBA8,
// row-major, top-down, Width*Height*4 bytes.
type Frame struct {
	Pixels []byte
	Width  int
	Height int
}

// Render paints the full document (scroll == nil) or the fixed viewport
// translated up by *scroll (the T8 interactive semantics, used later by the
// shell). It mirrors paint::render / paint::render_viewport.
func Render(l *layout.Layout, scroll *float64) (Frame, error) {
	width, height := docSize(l)
	if scroll != nil {
		width, height = ViewportWidth, ViewportHeight
	}
	p := C.paint_new(C.int(width), C.int(height))
	if p == nil {
		return Frame{}, errors.New("paint: cannot create the cairo surface")
	}
	defer C.paint_free(p)
	draw(p, l, scroll)
	return frameFrom(p, width, height), nil
}

// RenderPNG paints the full document to a PNG file, mirroring
// paint::render_to_png (the --png path).
func RenderPNG(l *layout.Layout, path string) error {
	width, height := docSize(l)
	p := C.paint_new(C.int(width), C.int(height))
	if p == nil {
		return errors.New("paint: cannot create the cairo surface")
	}
	defer C.paint_free(p)
	draw(p, l, nil)
	// The file is created first, so an unwritable path reports the OS
	// error exactly like the oracle's File::create does; cairo then
	// overwrites the empty file with the PNG bytes.
	if f, err := os.Create(path); err != nil {
		return osError(err)
	} else {
		f.Close()
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var msg *C.char
	if C.paint_write_png(p, cpath, &msg) == 0 {
		return errors.New(C.GoString(msg))
	}
	return nil
}

// docSize is the full-document surface size: the layout width (floored at
// 1px) by the ceiled content height, like the oracle's render.
func docSize(l *layout.Layout) (int, int) {
	w := math.Max(l.Width, 1)
	h := math.Ceil(math.Max(l.Height, 1))
	return int(w), int(h)
}

// draw paints every box and run onto p. Scroll is nil for full-document
// renders and the offset for the interactive viewport: clip first in device
// space, then move the document.
func draw(p *C.paint_t, l *layout.Layout, scroll *float64) {
	if scroll != nil {
		C.paint_clip_viewport(p, C.int(ViewportWidth), C.int(ViewportHeight))
		C.paint_translate(p, 0, C.double(-*scroll))
	}
	C.paint_white(p)

	for i := range l.Boxes {
		paintBox(p, &l.Boxes[i])
	}
	for i := range l.Boxes {
		if l.Boxes[i].Image == nil {
			continue
		}
		drawImage(p, &l.Boxes[i])
	}

	ctx := C.paint_text_context(p)
	if ctx == nil {
		return
	}
	defer C.paint_text_context_free(ctx)
	for i := range l.Runs {
		showRun(p, ctx, &l.Runs[i])
	}
}

// paintBox paints one box's background colour, background image and border
// ring, clipped to the rounded shape when the box has a radius.
func paintBox(p *C.paint_t, b *layout.LayoutBox) {
	radii := resolveRadius(b)
	rounded := radii[0] > 0 || radii[1] > 0 || radii[2] > 0 || radii[3] > 0
	if rounded {
		C.paint_save(p)
		C.paint_clip_rounded(p, C.double(b.X), C.double(b.Y), C.double(b.W), C.double(b.H),
			C.double(radii[0]), C.double(radii[1]), C.double(radii[2]), C.double(radii[3]))
	}
	if b.Background != nil {
		c := b.Background
		C.paint_fill_rect(p, C.double(c.R), C.double(c.G), C.double(c.B), C.double(c.A),
			C.double(b.X), C.double(b.Y), C.double(b.W), C.double(b.H))
	}
	if b.BackgroundGradient != nil {
		drawBackgroundGradient(p, b)
	} else if b.BackgroundImage != nil {
		drawBackgroundImage(p, b)
	}
	if borderVisible(b) {
		if b.Image != nil {
			drawOutsetRing(p, b)
		} else {
			drawInsetRing(p, b)
		}
	}
	if rounded {
		C.paint_restore(p)
	}
}

// borderVisible reports whether the box paints a border ring: a side paints
// only when its width is positive and its style is solid (none paints
// nothing; every other CSS style degrades to solid).
func borderVisible(b *layout.LayoutBox) bool {
	for i := 0; i < 4; i++ {
		if b.BorderWidth[i] > 0 && b.BorderStyle[i] == css.BorderSolid {
			return true
		}
	}
	return false
}

// resolveRadius is one corner radius in CSS pixels: lengths as specified,
// percentages of the smaller box side.
func resolveRadius(b *layout.LayoutBox) [4]float64 {
	base := math.Min(b.W, b.H)
	var out [4]float64
	for i, r := range b.BorderRadius {
		v := math.Max(r.Value, 0)
		if r.Percent {
			v = v / 100 * base
		}
		out[i] = v
	}
	return out
}

// drawInsetRing paints the border ring of an ordinary box inside the box
// edge, over the background: one rectangle per visible side.
func drawInsetRing(p *C.paint_t, b *layout.LayoutBox) {
	bt, br, bb, bl := b.BorderWidth[0], b.BorderWidth[1], b.BorderWidth[2], b.BorderWidth[3]
	fill := func(i int, x, y, w, h float64) {
		if b.BorderWidth[i] <= 0 || b.BorderStyle[i] != css.BorderSolid {
			return
		}
		c := b.BorderColor[i]
		C.paint_fill_rect(p, C.double(c.R), C.double(c.G), C.double(c.B), C.double(c.A),
			C.double(x), C.double(y), C.double(w), C.double(h))
	}
	fill(0, b.X, b.Y, b.W, bt)
	fill(2, b.X, b.Y+b.H-bb, b.W, bb)
	if b.H-bt-bb > 0 {
		fill(3, b.X, b.Y+bt, bl, b.H-bt-bb)
		fill(1, b.X+b.W-br, b.Y+bt, br, b.H-bt-bb)
	}
}

// drawOutsetRing paints the border ring of a replaced <img> box outside the
// box: the box itself is content-sized, so an inset ring would eat the image
// pixels.
func drawOutsetRing(p *C.paint_t, b *layout.LayoutBox) {
	bt, br, bb, bl := b.BorderWidth[0], b.BorderWidth[1], b.BorderWidth[2], b.BorderWidth[3]
	fill := func(i int, x, y, w, h float64) {
		if b.BorderWidth[i] <= 0 || b.BorderStyle[i] != css.BorderSolid {
			return
		}
		c := b.BorderColor[i]
		C.paint_fill_rect(p, C.double(c.R), C.double(c.G), C.double(c.B), C.double(c.A),
			C.double(x), C.double(y), C.double(w), C.double(h))
	}
	fill(0, b.X-bl, b.Y-bt, b.W+bl+br, bt)
	fill(2, b.X-bl, b.Y+b.H, b.W+bl+br, bb)
	if b.H > 0 {
		fill(3, b.X-bl, b.Y, bl, b.H)
		fill(1, b.X+b.W, b.Y, br, b.H)
	}
}

// drawBackgroundImage places the box's background image per the computed
// repeat and position (CSS Backgrounds 3 §4.4): a percentage position
// aligns the image's p% point with the box's p% point of the free space —
// offset = p/100 * (box - image) — and a plain length offsets the image
// from the box's top-left corner (CSS 2.1 §8.14.1). The zero values
// (repeat, 0%, 0%) tile from the box origin exactly like the
// pre-placement engine.
func drawBackgroundImage(p *C.paint_t, b *layout.LayoutBox) {
	image := b.BackgroundImage
	if b.W <= 0 || b.H <= 0 || image.Width == 0 || image.Height == 0 {
		return
	}
	iw, ih := float64(image.Width), float64(image.Height)
	ox := backgroundOffset(b.BackgroundPositionX, b.W, iw)
	oy := backgroundOffset(b.BackgroundPositionY, b.H, ih)
	mode := 0
	switch b.BackgroundRepeat {
	case css.BackgroundRepeatRepeatX:
		mode = 1
	case css.BackgroundRepeatRepeatY:
		mode = 2
	case css.BackgroundRepeatNoRepeat:
		mode = 3
	}
	rw, rh := image.RasterSize()
	s := rgbaSurface(image.RGBA, int(rw), int(rh))
	if s == nil {
		return
	}
	defer C.paint_surface_destroy(s)
	C.paint_bg_image(p, s, C.double(b.X), C.double(b.Y), C.double(b.W), C.double(b.H),
		C.double(ox), C.double(oy), C.int(mode), C.double(iw), C.double(ih))
}

// backgroundOffset is one axis of the computed placement: a percentage of
// the free space (box - image), or a plain length.
func backgroundOffset(pos css.BackgroundPosition, box, image float64) float64 {
	if pos.Percent {
		return pos.Value / 100 * (box - image)
	}
	return pos.Value
}

// drawBackgroundGradient paints the two-stop vertical ramp over the
// background colour: the image slot the manual cascade filled with a
// linear-gradient libcss discarded (the codeberg cards' teal ramp).
func drawBackgroundGradient(p *C.paint_t, b *layout.LayoutBox) {
	g := b.BackgroundGradient
	if g == nil || b.W <= 0 || b.H <= 0 {
		return
	}
	C.paint_background_gradient(p,
		C.double(b.X), C.double(b.Y), C.double(b.W), C.double(b.H),
		C.double(g.From.R), C.double(g.From.G), C.double(g.From.B), C.double(g.From.A),
		C.double(g.To.R), C.double(g.To.G), C.double(g.To.B), C.double(g.To.A))
}

// drawImage paints one <img> box's decoded pixels, scaled into the box. A
// KeepAspect image (SVG) is fitted into the box instead of stretched.
func drawImage(p *C.paint_t, b *layout.LayoutBox) {
	image := b.Image
	rw, rh := image.RasterSize()
	if b.W <= 0 || b.H <= 0 || rw == 0 || rh == 0 {
		return
	}
	s := rgbaSurface(image.RGBA, int(rw), int(rh))
	if s == nil {
		return
	}
	defer C.paint_surface_destroy(s)
	x, y, w, h := b.X, b.Y, b.W, b.H
	if image.KeepAspect {
		x, y, w, h = fitRect(x, y, w, h, rw, rh)
	}
	radii := resolveRadius(b)
	C.paint_draw_image(p, s,
		C.double(x), C.double(y), C.double(w), C.double(h),
		C.double(rw), C.double(rh),
		C.double(radii[0]), C.double(radii[1]), C.double(radii[2]), C.double(radii[3]))
}

// fitRect is the xMidYMid meet placement of a iw x ih content box inside the
// box x,y,w,h: the largest rectangle with the content's aspect ratio that
// fits, centred on the box.
func fitRect(x, y, w, h float64, iw, ih uint32) (float64, float64, float64, float64) {
	if iw == 0 || ih == 0 {
		return x, y, w, h
	}
	scale := math.Min(w/float64(iw), h/float64(ih))
	fw, fh := float64(iw)*scale, float64(ih)*scale
	return x + (w-fw)/2, y + (h-fh)/2, fw, fh
}

// rgbaSurface converts straight RGBA8 pixels to a Cairo ARGB32 surface,
// taking a copy that C owns and frees: no Go pointer ever reaches C.
func rgbaSurface(rgba []byte, width, height int) *C.cairo_surface_t {
	if len(rgba) < width*height*4 {
		return nil
	}
	dup := C.CBytes(rgba[:width*height*4])
	s := C.paint_rgba_surface((*C.uchar)(dup), C.int(width), C.int(height))
	if s == nil {
		return nil
	}
	return s
}

// showRun paints one text run, rebuilding the Pango layout the shaper
// measured so the painted lines match the computed boxes.
func showRun(p *C.paint_t, ctx *C.PangoContext, run *layout.TextRun) {
	style := adaptStyle(&run.Style)
	family := text.ResolveFamily(style.FontFamilies)
	ctext := C.CString(strings.ReplaceAll(run.Text, "\x00", " "))
	defer C.free(unsafe.Pointer(ctext))
	cfamily := C.CString(family)
	defer C.free(unsafe.Pointer(cfamily))

	weight := style.FontWeight
	if weight < 100 {
		weight = 100
	}
	if weight > 900 {
		weight = 900
	}
	italic := 0
	if style.Italic {
		italic = 1
	}
	align, justify := 0, 0
	switch style.TextAlign {
	case text.AlignCenter:
		align = 1
	case text.AlignRight:
		align = 2
	case text.AlignJustify:
		justify = 1
	}
	spacing := 0
	hasSpacing := 0
	if style.LetterSpacing != 0 {
		hasSpacing = 1
		spacing = int(style.LetterSpacing * text.PangoScale)
	}
	var spanMem unsafe.Pointer
	var spanFamilies []*C.char
	var spans []C.paint_span
	if len(run.Spans) > 0 {
		spanMem = C.malloc(C.size_t(len(run.Spans)) * C.size_t(C.sizeof_paint_span))
		if spanMem != nil {
			defer C.free(spanMem)
			spans = unsafe.Slice((*C.paint_span)(spanMem), len(run.Spans))
			spanFamilies = make([]*C.char, len(run.Spans))
			for i, span := range run.Spans {
				spanFamilies[i] = C.CString(text.ResolveFamily(span.Style.FontFamilies))
				defer C.free(unsafe.Pointer(spanFamilies[i]))
				spans[i].family = spanFamilies[i]
				spans[i].size = C.int(span.Style.FontSize * text.PangoScale)
				spans[i].weight = C.int(span.Style.FontWeight)
				if spans[i].weight < 100 {
					spans[i].weight = 100
				}
				if spans[i].weight > 900 {
					spans[i].weight = 900
				}
				if span.Style.Italic {
					spans[i].italic = 1
				}
				spans[i].spacing = C.int(span.Style.LetterSpacing * text.PangoScale)
				spans[i].start, spans[i].end = C.int(span.Start), C.int(span.End)
				spans[i].r, spans[i].g = C.double(span.Style.Color.R), C.double(span.Style.Color.G)
				spans[i].b, spans[i].a = C.double(span.Style.Color.B), C.double(span.Style.Color.A)
			}
		}
	}
	size := style.FontSize
	if size < 0 {
		size = 0
	}
	naturalLineHeight := 0
	if style.LineHeightNormal {
		naturalLineHeight = 1
	}
	width := run.Width
	if width < 1 {
		width = 1
	}
	c := style.Color
	C.paint_show_text(p, ctx, ctext, cfamily,
		C.int(size*text.PangoScale), C.int(weight), C.int(italic),
		C.int(width*text.PangoScale), C.int(align), C.int(justify),
		C.int(text.LineHeightAbsolute(size, style.LineHeight)), C.int(naturalLineHeight), C.int(hasSpacing), C.int(spacing),
		(*C.paint_span)(spanMem), C.int(len(spans)),
		C.double(run.X), C.double(run.Y),
		C.double(c.R), C.double(c.G), C.double(c.B), C.double(c.A))
}

// adaptedStyle is the typography a run paints with: the same projection the
// layout shaper consumes (the layout package's private adaptStyle, field
// for field).
type adaptedStyle struct {
	FontFamilies     []string
	FontSize         float64
	FontWeight       uint32
	Italic           bool
	LineHeight       float64
	LineHeightNormal bool
	TextAlign        text.Align
	LetterSpacing    float64
	Color            css.Color
}

// adaptStyle projects the computed style onto the painter's typography.
func adaptStyle(style *css.Style) adaptedStyle {
	families := style.FontFamilies
	if len(families) == 0 {
		families = []string{style.FontFamily}
	}
	align := text.AlignLeft
	switch style.TextAlign {
	case css.TextAlignCenter:
		align = text.AlignCenter
	case css.TextAlignRight:
		align = text.AlignRight
	case css.TextAlignJustify:
		align = text.AlignJustify
	}
	return adaptedStyle{
		FontFamilies:     families,
		FontSize:         style.FontSize,
		FontWeight:       style.FontWeight,
		Italic:           style.Italic,
		LineHeight:       style.LineHeight,
		LineHeightNormal: style.LineHeightNormal,
		TextAlign:        align,
		LetterSpacing:    style.LetterSpacing,
		Color:            style.Color,
	}
}

// frameFrom copies the surface rows (stride aware) into a tightly packed
// straight-RGBA frame.
func frameFrom(p *C.paint_t, width, height int) Frame {
	surface := p.surface
	stride := int(C.cairo_image_surface_get_stride(surface))
	C.cairo_surface_flush(surface)
	data := C.cairo_image_surface_get_data(surface)
	raw := C.GoBytes(unsafe.Pointer(data), C.int(stride*height))
	out := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		row := raw[y*stride : y*stride+width*4]
		dst := out[y*width*4 : (y+1)*width*4]
		for x := 0; x < width; x++ {
			// ARGB32 words are native-endian: bytes hold B, G, R, A.
			b, g, r, a := row[x*4+0], row[x*4+1], row[x*4+2], uint32(row[x*4+3])
			if a == 0 {
				continue
			}
			if a == 255 {
				dst[x*4+0] = r
				dst[x*4+1] = g
				dst[x*4+2] = b
				dst[x*4+3] = 255
				continue
			}
			dst[x*4+0] = unpremultiply(r, a)
			dst[x*4+1] = unpremultiply(g, a)
			dst[x*4+2] = unpremultiply(b, a)
			dst[x*4+3] = uint8(a)
		}
	}
	return Frame{Pixels: out, Width: width, Height: height}
}

// osError renders an OS failure the way the Rust oracle's io::Error
// displays ("No such file or directory (os error 2)").
func osError(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errors.New(upperASCII(errno.Error()) + " (os error " + itoa(uint(errno)) + ")")
	}
	return err
}

// upperASCII upper-cases the first ASCII letter, like strerror's wording.
func upperASCII(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if 'a' <= b[0] && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}

func itoa(v uint) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for n := v; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

// unpremultiply undoes Cairo's premultiplication for one 8-bit channel.
func unpremultiply(value byte, alpha uint32) byte {
	v := (uint32(value)*255 + alpha/2) / alpha
	if v > 255 {
		v = 255
	}
	return byte(v)
}
