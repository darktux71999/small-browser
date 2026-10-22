//! Renderer del spike: modelo de cajas de bloque + texto vía Pango, pintura cairo.
//!
//! Simplificaciones deliberadas (iguales para ambos proveedores, S2):
//! - bloque/inline se decide por etiqueta (el modelo UA no es parte de la comparación);
//! - `display:none` sí se respeta desde el estilo computado;
//! - los elementos inline se pintan como su propio grupo de líneas, sin mezcla
//!   inline profunda;
//! - sin imágenes, flotados ni posicionamiento.

use std::ffi::CString;

use browser_lite::dom::{Document, NodeId};

use crate::ffi;
use crate::style::{Display, Rgba, SpikeStyle, TextAlign};

pub struct RenderInfo {
    pub height: f64,
    pub ops: usize,
}

enum Op {
    Fill {
        x: f64,
        y: f64,
        w: f64,
        h: f64,
        color: Rgba,
    },
    Text {
        x: f64,
        y: f64,
        w: f64,
        text: String,
        style: SpikeStyle,
    },
}

const PAGE_BG: Rgba = (1.0, 1.0, 1.0, 1.0);
const SKIP: &[&str] = &["head", "style", "script", "title", "meta", "link", "noscript"];

fn is_block_tag(tag: &str) -> bool {
    matches!(
        tag,
        "html" | "body" | "div" | "p" | "h1" | "h2" | "h3" | "h4" | "h5" | "h6" | "ul" | "ol"
            | "li" | "blockquote" | "pre" | "section" | "article" | "header" | "footer" | "nav"
            | "figure" | "figcaption" | "hr" | "table" | "tr" | "td" | "th" | "dl" | "dt" | "dd"
    )
}

fn normalize_ws(text: &str) -> String {
    text.split_whitespace().collect::<Vec<_>>().join(" ")
}

/// Layout en dos pasadas: mide con Pango y acumula ops; luego pinta. Devuelve la
/// altura total de la página y el número de ops.
pub fn render_to_png(
    doc: &Document,
    styles: &[SpikeStyle],
    out: &str,
    width: f64,
) -> Result<RenderInfo, String> {
    let mut ops: Vec<Op> = Vec::new();
    let start = doc
        .query_selector("body")
        .filter(|&id| styles.len() > id)
        .unwrap_or(doc.root);
    let height = block(doc, styles, start, 0.0, 0.0, width, &mut ops);
    let page_h = (height + 24.0).max(64.0);
    let page_w = width.max(64.0);

    let surface =
        unsafe { ffi::cairo_image_surface_create(ffi::CAIRO_FORMAT_ARGB32, page_w as i32, page_h as i32) };
    if surface.is_null() {
        return Err("cairo: surface create failed".into());
    }
    let cr = unsafe { ffi::cairo_create(surface) };
    if cr.is_null() {
        return Err("cairo: create failed".into());
    }
    let ctx = unsafe { ffi::pango_cairo_create_context(cr) };
    unsafe { ffi::pango_cairo_context_set_resolution(ctx, 72.0) };

    unsafe {
        ffi::cairo_set_source_rgba(cr, PAGE_BG.0, PAGE_BG.1, PAGE_BG.2, PAGE_BG.3);
        ffi::cairo_paint(cr);
    }

    for op in &ops {
        match op {
            Op::Fill { x, y, w, h, color } => unsafe {
                ffi::cairo_set_source_rgba(cr, color.0, color.1, color.2, color.3);
                ffi::cairo_rectangle(cr, *x, *y, *w, *h);
                ffi::cairo_fill(cr);
            },
            Op::Text { x, y, w, text, style } => {
                let layout = make_layout(ctx, style, text, *w);
                unsafe {
                    // El color del texto se fija aquí: si no, los glifos salen con
                    // el último color de relleno usado (bug encontrado en T3).
                    ffi::cairo_set_source_rgba(cr, style.color.0, style.color.1, style.color.2, style.color.3);
                    ffi::cairo_move_to(cr, *x, *y);
                    ffi::pango_cairo_update_layout(cr, layout);
                    ffi::pango_cairo_show_layout(cr, layout);
                    ffi::g_object_unref(layout);
                }
            }
        }
    }

    let out_c = CString::new(out).map_err(|e| e.to_string())?;
    let status = unsafe { ffi::cairo_surface_write_to_png(surface, out_c.as_ptr()) };
    unsafe {
        ffi::g_object_unref(ctx);
        ffi::cairo_destroy(cr);
        ffi::cairo_surface_destroy(surface);
    }
    if status != 0 {
        return Err(format!("cairo: write_to_png status {status}"));
    }
    Ok(RenderInfo {
        height,
        ops: ops.len(),
    })
}

/// Devuelve la altura exterior (márgenes incluidos) que consume el subárbol.
fn block(
    doc: &Document,
    styles: &[SpikeStyle],
    id: NodeId,
    x: f64,
    y: f64,
    content_w: f64,
    ops: &mut Vec<Op>,
) -> f64 {
    let node = &doc.nodes[id];
    let st = &styles[id];
    if st.display == Display::None {
        return 0.0;
    }
    let [mt, mr, mb, ml] = st.margin;
    let [pt, pr, pb, pl] = st.padding;
    let inner_w = st
        .width
        .unwrap_or(content_w - ml - mr - pl - pr)
        .max(8.0);
    let bx = x + ml;
    let by = y + mt;
    let ix = bx + pl;
    let iy = by + pt;

    let mut child_ops: Vec<Op> = Vec::new();
    let mut cy = iy;
    for &c in &node.children {
        let cn = &doc.nodes[c];
        if cn.is_text {
            let text = normalize_ws(&cn.text);
            if text.is_empty() {
                continue;
            }
            cy += text_op(&mut child_ops, text, st, ix, cy, inner_w);
        } else {
            let tag = cn.tag.as_str();
            if SKIP.contains(&tag) {
                continue;
            }
            let cst = &styles[c];
            if cst.display == Display::None {
                continue;
            }
            if is_block_tag(tag) {
                cy += block(doc, styles, c, ix, cy, inner_w, &mut child_ops);
            } else {
                let text = normalize_ws(&doc.text_content(c));
                if text.is_empty() {
                    continue;
                }
                cy += text_op(&mut child_ops, text, cst, ix, cy, inner_w);
            }
        }
    }
    let content_h = cy - iy;

    if let Some(bg) = st.background {
        ops.push(Op::Fill {
            x: bx,
            y: by,
            w: inner_w + pl + pr,
            h: content_h + pt + pb,
            color: bg,
        });
    }
    ops.extend(child_ops);
    content_h + pt + pb + mt + mb
}

/// Crea el run de texto, mide su altura con Pango y apila la op de pintura.
fn text_op(
    ops: &mut Vec<Op>,
    text: String,
    style: &SpikeStyle,
    x: f64,
    y: f64,
    w: f64,
) -> f64 {
    // Medida con el mismo layout que se pintará: contexto temporal de un surface mínimo.
    let scratch = unsafe { ffi::cairo_image_surface_create(ffi::CAIRO_FORMAT_ARGB32, 8, 8) };
    let cr = unsafe { ffi::cairo_create(scratch) };
    let ctx = unsafe { ffi::pango_cairo_create_context(cr) };
    unsafe { ffi::pango_cairo_context_set_resolution(ctx, 72.0) };
    let layout = make_layout(ctx, style, &text, w);
    let mut lw = 0i32;
    let mut lh = 0i32;
    unsafe {
        ffi::pango_layout_get_pixel_size(layout, &mut lw, &mut lh);
        ffi::g_object_unref(layout);
        ffi::g_object_unref(ctx);
        ffi::cairo_destroy(cr);
        ffi::cairo_surface_destroy(scratch);
    }
    let height = lh as f64;
    ops.push(Op::Text {
        x,
        y,
        w,
        text,
        style: style.clone(),
    });
    height.max(1.0)
}

fn make_layout(ctx: *mut std::os::raw::c_void, style: &SpikeStyle, text: &str, w: f64) -> *mut std::os::raw::c_void {
    unsafe {
        let layout = ffi::pango_layout_new(ctx);
        let desc = ffi::pango_font_description_from_string(b"Sans\0".as_ptr() as *const _);
        let family = CString::new(style.font_family.as_str()).unwrap_or_default();
        ffi::pango_font_description_set_family(desc, family.as_ptr());
        ffi::pango_font_description_set_size(desc, (style.font_size * ffi::PANGO_SCALE) as i32);
        ffi::pango_font_description_set_weight(desc, style.font_weight as i32);
        ffi::pango_font_description_set_style(
            desc,
            if style.italic {
                ffi::PANGO_STYLE_ITALIC
            } else {
                ffi::PANGO_STYLE_NORMAL
            },
        );
        ffi::pango_layout_set_font_description(layout, desc);
        ffi::pango_font_description_free(desc);

        let text_c = CString::new(text).unwrap_or_default();
        ffi::pango_layout_set_text(layout, text_c.as_ptr(), text_c.as_bytes().len() as i32);
        ffi::pango_layout_set_width(layout, (w * ffi::PANGO_SCALE) as i32);
        ffi::pango_layout_set_alignment(
            layout,
            match style.text_align {
                TextAlign::Left => ffi::PANGO_ALIGN_LEFT,
                TextAlign::Center => ffi::PANGO_ALIGN_CENTER,
                TextAlign::Right => ffi::PANGO_ALIGN_RIGHT,
                TextAlign::Justify => ffi::PANGO_ALIGN_LEFT,
            },
        );
        if style.text_align == TextAlign::Justify {
            ffi::pango_layout_set_justify(layout, 1);
        }
        // Interlineado y tracking son atributos de Pango, no setters del layout.
        let attrs = ffi::pango_attr_list_new();
        ffi::pango_attr_list_insert(attrs, ffi::pango_attr_line_height_new(style.line_height));
        if style.letter_spacing != 0.0 {
            let attr = ffi::pango_attr_letter_spacing_new((style.letter_spacing * ffi::PANGO_SCALE) as i32);
            ffi::pango_attr_list_insert(attrs, attr);
        }
        ffi::pango_layout_set_attributes(layout, attrs);
        ffi::pango_attr_list_unref(attrs);
        layout
    }
}
