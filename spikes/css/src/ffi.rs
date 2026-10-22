//! FFI fino de Pango/PangoCairo/Cairo + GObject (sin crates intermedios).
//! Firmas contra pango 1.58 / cairo 1.18 del sistema.

use std::os::raw::{c_char, c_double, c_int, c_void};

pub const PANGO_SCALE: f64 = 1024.0;
pub const CAIRO_FORMAT_ARGB32: c_int = 0;

pub const PANGO_ALIGN_LEFT: c_int = 0;
pub const PANGO_ALIGN_CENTER: c_int = 1;
pub const PANGO_ALIGN_RIGHT: c_int = 2;

pub const PANGO_STYLE_NORMAL: c_int = 0;
pub const PANGO_STYLE_ITALIC: c_int = 2;

pub const PANGO_WEIGHT_NORMAL: c_int = 400;
#[allow(dead_code)]
pub const PANGO_WEIGHT_BOLD: c_int = 700;

#[repr(C)]
pub struct cairo_t {
    _private: [u8; 0],
}
#[repr(C)]
pub struct cairo_surface_t {
    _private: [u8; 0],
}

#[link(name = "cairo")]
extern "C" {
    pub fn cairo_image_surface_create(format: c_int, width: c_int, height: c_int)
        -> *mut cairo_surface_t;
    pub fn cairo_create(target: *mut cairo_surface_t) -> *mut cairo_t;
    pub fn cairo_destroy(cr: *mut cairo_t);
    pub fn cairo_surface_destroy(surface: *mut cairo_surface_t);
    pub fn cairo_surface_status(surface: *mut cairo_surface_t) -> c_int;
    pub fn cairo_surface_write_to_png(
        surface: *mut cairo_surface_t,
        filename: *const c_char,
    ) -> c_int;
    pub fn cairo_set_source_rgb(cr: *mut cairo_t, r: c_double, g: c_double, b: c_double);
    pub fn cairo_set_source_rgba(
        cr: *mut cairo_t,
        r: c_double,
        g: c_double,
        b: c_double,
        a: c_double,
    );
    pub fn cairo_paint(cr: *mut cairo_t);
    pub fn cairo_rectangle(
        cr: *mut cairo_t,
        x: c_double,
        y: c_double,
        w: c_double,
        h: c_double,
    );
    pub fn cairo_fill(cr: *mut cairo_t);
    pub fn cairo_move_to(cr: *mut cairo_t, x: c_double, y: c_double);
}

#[link(name = "pangocairo-1.0")]
extern "C" {
    pub fn pango_cairo_create_context(cr: *mut cairo_t) -> *mut c_void;
    pub fn pango_cairo_update_context(cr: *mut cairo_t, context: *mut c_void);
    pub fn pango_cairo_update_layout(cr: *mut cairo_t, layout: *mut c_void);
    pub fn pango_cairo_show_layout(cr: *mut cairo_t, layout: *mut c_void);
    pub fn pango_cairo_context_set_resolution(context: *mut c_void, dpi: c_double);
}

#[link(name = "pango-1.0")]
extern "C" {
    pub fn pango_layout_new(context: *mut c_void) -> *mut c_void;
    pub fn pango_layout_set_text(layout: *mut c_void, text: *const c_char, length: c_int);
    pub fn pango_layout_set_font_description(layout: *mut c_void, desc: *const c_void);
    pub fn pango_layout_set_width(layout: *mut c_void, width: c_int);
    pub fn pango_layout_set_alignment(layout: *mut c_void, alignment: c_int);
    pub fn pango_layout_set_justify(layout: *mut c_void, justify: c_int);
    pub fn pango_layout_set_attributes(layout: *mut c_void, attrs: *mut c_void);
    pub fn pango_layout_get_pixel_size(layout: *mut c_void, width: *mut c_int, height: *mut c_int);
    pub fn pango_font_description_from_string(str: *const c_char) -> *mut c_void;
    pub fn pango_font_description_free(desc: *mut c_void);
    pub fn pango_font_description_set_family(desc: *mut c_void, family: *const c_char);
    pub fn pango_font_description_set_size(desc: *mut c_void, size: c_int);
    pub fn pango_font_description_set_weight(desc: *mut c_void, weight: c_int);
    pub fn pango_font_description_set_style(desc: *mut c_void, style: c_int);
    pub fn pango_attr_list_new() -> *mut c_void;
    pub fn pango_attr_list_unref(list: *mut c_void);
    pub fn pango_attr_list_insert(list: *mut c_void, attr: *mut c_void);
    pub fn pango_attr_letter_spacing_new(spacing: c_int) -> *mut c_void;
    pub fn pango_attr_line_height_new(factor: c_double) -> *mut c_void;
}

#[link(name = "gobject-2.0")]
extern "C" {
    pub fn g_object_unref(obj: *mut c_void);
}
