//! Block-flow layout for the MVP subset.
//! Produces absolute boxes consumed by both the PNG painter and `--dump`.
//!
//! Text is measured, wrapped, aligned and justified by Pango: the run is laid
//! out once with the real font metrics inside the content box, and the same
//! layout is rebuilt at paint time from [`TextRun`]. Nothing here estimates
//! advances from the character count.

use std::os::raw::{c_int, c_void};
use std::rc::Rc;

use crate::css::{Display, Style};
use crate::dom::{Document, NodeId};
use crate::img::{Image, ImageSet};

#[derive(Debug, Clone)]
pub struct LayoutBox {
    pub node: NodeId,
    pub tag: String,
    pub x: f64,
    pub y: f64,
    pub w: f64,
    pub h: f64,
    pub text: Option<String>,
    pub color: crate::css::Color,
    pub background: Option<crate::css::Color>,
    pub font_size: f64,
    pub bold: bool,
    pub centered: bool,
    /// Decoded pixels for a replaced `<img>`; `None` for text/block boxes and
    /// for images that failed to load.
    pub image: Option<Rc<Image>>,
}

/// A text run as Pango sees it: the block's text plus the typography and the
/// wrap width. Paint rebuilds the layout from this instead of re-wrapping.
#[derive(Debug, Clone)]
pub struct TextRun {
    pub x: f64,
    pub y: f64,
    /// Wrap width, i.e. the content box the run lives in.
    pub width: f64,
    pub text: String,
    pub style: Style,
}

pub struct Layout {
    pub boxes: Vec<LayoutBox>,
    pub runs: Vec<TextRun>,
    pub width: f64,
    pub height: f64,
}

pub fn layout(doc: &Document, styles: &[Style], viewport_width: f64) -> Layout {
    layout_with_images(doc, styles, viewport_width, &ImageSet::empty())
}

/// Lays `doc` out with its `<img>` subresources fetched and decoded.
///
/// `base` resolves relative `src` like the stylesheet loader does: the final
/// document URL for http(s) pages, the containing directory for local files.
/// Failures degrade to empty boxes and warnings; they never abort the render.
pub fn layout_with_subresources(
    doc: &Document,
    styles: &[Style],
    viewport_width: f64,
    base: &str,
) -> Layout {
    let images = ImageSet::load(doc, base);
    layout_with_images(doc, styles, viewport_width, &images)
}

/// Lays `doc` out against an already loaded [`ImageSet`], so the caller keeps
/// the decoded images (and their warnings) for the rest of the run.
pub fn layout_with_images(
    doc: &Document,
    styles: &[Style],
    viewport_width: f64,
    images: &ImageSet,
) -> Layout {
    let shaper = TextShaper::new().expect("pango: cannot create a text measuring context");
    let mut boxes = Vec::new();
    let mut runs = Vec::new();
    let mut cursor = 0.0;

    for &child in &doc.nodes[doc.root].children {
        if styles[child].display == Display::None {
            continue;
        }
        cursor += layout_node(
            doc,
            styles,
            child,
            0.0,
            cursor,
            viewport_width,
            &shaper,
            images,
            &mut boxes,
            &mut runs,
        );
    }

    Layout {
        boxes,
        runs,
        width: viewport_width,
        height: cursor.max(1.0),
    }
}

fn layout_node(
    doc: &Document,
    styles: &[Style],
    id: NodeId,
    origin_x: f64,
    origin_y: f64,
    available_width: f64,
    shaper: &TextShaper,
    images: &ImageSet,
    out: &mut Vec<LayoutBox>,
    runs: &mut Vec<TextRun>,
) -> f64 {
    let style = &styles[id];
    if style.display == Display::None {
        return 0.0;
    }
    if is_image(doc, id) {
        return layout_image(doc, styles, id, origin_x, origin_y, available_width, images, out);
    }
    if style.display == Display::Inline {
        return 0.0; // inline runs are flushed by their block container
    }

    let content = content_box(style, origin_x, available_width);
    let border_x = content.border_x;
    let border_w = content.border_w;
    let content_x = content.x;
    let content_w = content.w;

    let border_y = origin_y + style.margin[0];
    let mut cursor_y = border_y + style.padding[0];

    // Placeholder so children find a stable index; height filled after flow.
    let box_index = out.len();
    out.push(LayoutBox {
        node: id,
        tag: doc.nodes[id].tag.clone(),
        x: border_x,
        y: border_y,
        w: border_w,
        h: 0.0,
        text: None,
        color: style.color,
        background: style.background,
        font_size: style.font_size,
        bold: style.bold,
        centered: style.inline_align,
        image: None,
    });

    let mut run = String::new();
    for &child in &doc.nodes[id].children {
        let child_style = &styles[child];
        match child_style.display {
            Display::None => {}
            _ if is_image(doc, child) => {
                // A replaced element takes its own line in this block flow, so
                // the text collected so far is flushed above it.
                if !run.trim().is_empty() {
                    cursor_y += flush_text(
                        shaper, style, content_x, cursor_y, content_w, &mut run, out, runs,
                    );
                }
                run.clear();
                cursor_y += layout_node(
                    doc, styles, child, content_x, cursor_y, content_w, shaper, images, out, runs,
                );
            }
            Display::Block => {
                if !run.trim().is_empty() {
                    cursor_y += flush_text(
                        shaper, style, content_x, cursor_y, content_w, &mut run, out, runs,
                    );
                }
                run.clear();
                cursor_y += layout_node(
                    doc, styles, child, content_x, cursor_y, content_w, shaper, images, out, runs,
                );
            }
            Display::Inline => collect_inline_text(doc, child, &mut run),
        }
    }
    if !run.trim().is_empty() {
        cursor_y +=
            flush_text(shaper, style, content_x, cursor_y, content_w, &mut run, out, runs);
    }

    let mut border_h = cursor_y - border_y + style.padding[2];
    if let Some(h) = style.height {
        border_h = border_h.max(h);
    }
    out[box_index].h = border_h.max(0.0);

    border_h + style.margin[0] + style.margin[2]
}

/// The content box of a block-level element: the rectangle its children (and a
/// replaced image) live in.
struct ContentBox {
    /// Left edge of the border box.
    border_x: f64,
    /// Width of the border box.
    border_w: f64,
    /// Left edge of the content box.
    x: f64,
    /// Width of the content box.
    w: f64,
}

fn content_box(style: &Style, origin_x: f64, available_width: f64) -> ContentBox {
    let border_x = origin_x + style.margin[3];
    let border_w = style
        .width
        .map(|w| w + style.padding[1] + style.padding[3])
        .unwrap_or((available_width - style.margin[1] - style.margin[3]).max(1.0));
    ContentBox {
        border_x,
        border_w,
        x: border_x + style.padding[3],
        w: (border_w - style.padding[1] - style.padding[3]).max(1.0),
    }
}

fn is_image(doc: &Document, id: NodeId) -> bool {
    !doc.nodes[id].is_text && doc.nodes[id].tag.eq_ignore_ascii_case("img")
}

/// Lays out a replaced `<img>`: one box holding the decoded pixels, sized from
/// its intrinsic dimensions (or the author's width/height) and never wider than
/// the content box. Returns the space it takes in the flow.
fn layout_image(
    doc: &Document,
    styles: &[Style],
    id: NodeId,
    origin_x: f64,
    origin_y: f64,
    available_width: f64,
    images: &ImageSet,
    out: &mut Vec<LayoutBox>,
) -> f64 {
    let style = &styles[id];
    let content = content_box(style, origin_x, available_width);
    let image = images.image(id);
    let (w, h) = image_size(&doc.nodes[id], style, image.as_deref(), content.w);
    let y = origin_y + style.margin[0] + style.padding[0];

    out.push(LayoutBox {
        node: id,
        tag: doc.nodes[id].tag.clone(),
        x: content.x,
        y,
        w,
        h,
        text: None,
        color: style.color,
        background: style.background,
        font_size: style.font_size,
        bold: style.bold,
        centered: style.inline_align,
        image,
    });

    h + style.margin[0] + style.margin[2]
}

/// The painted size of an image box.
///
/// CSS `width`/`height` win over the HTML `width`/`height` attributes, and both
/// win over the intrinsic pixel size. A single dimension keeps the intrinsic
/// aspect ratio; without pixels (failed or missing image) it stays 0. The
/// result never exceeds the content box: a wider image is scaled down on both
/// axes instead of overflowing its container.
fn image_size(
    node: &crate::dom::Node,
    style: &Style,
    image: Option<&Image>,
    max_width: f64,
) -> (f64, f64) {
    let (intrinsic_w, intrinsic_h) = image
        .map(|image| (image.width as f64, image.height as f64))
        .unwrap_or((0.0, 0.0));

    let width_hint = style
        .width
        .or_else(|| node.attr("width").and_then(length_px));
    let height_hint = style
        .height
        .or_else(|| node.attr("height").and_then(length_px));

    let (mut w, mut h) = match (width_hint, height_hint) {
        (Some(w), Some(h)) => (w, h),
        (Some(w), None) => (w, scale_by(intrinsic_w, intrinsic_h, w)),
        (None, Some(h)) => (scale_by(intrinsic_h, intrinsic_w, h), h),
        (None, None) => (intrinsic_w, intrinsic_h),
    };

    if w > max_width {
        let scale = max_width / w;
        w = max_width;
        h *= scale;
    }
    (w.max(0.0), h.max(0.0))
}

/// `known * wanted / basis`, i.e. the missing side of an aspect ratio. A zero
/// basis (no pixels decoded) has no ratio to preserve.
fn scale_by(basis: f64, other: f64, wanted: f64) -> f64 {
    if basis > 0.0 {
        other * wanted / basis
    } else {
        0.0
    }
}

/// An HTML length attribute (`width="30"`, `height="30px"`) in CSS pixels.
/// Percentages and unparsable values are ignored: they do not size the box.
fn length_px(value: &str) -> Option<f64> {
    let value = value.trim();
    let value = value
        .strip_suffix("px")
        .or_else(|| value.strip_suffix("PX"))
        .unwrap_or(value)
        .trim();
    let px: f64 = value.parse().ok()?;
    (px.is_finite() && px >= 0.0).then_some(px)
}

/// Concatenates the inline descendants of a block into a single text run.
fn collect_inline_text(doc: &Document, id: NodeId, out: &mut String) {
    let node = &doc.nodes[id];
    if node.is_text {
        out.push_str(&node.text);
        return;
    }
    if matches!(node.tag.as_str(), "script" | "style" | "head" | "title") {
        return;
    }
    for &child in &node.children {
        collect_inline_text(doc, child, out);
    }
}

/// Lays a run out with Pango inside `width` and pushes one `#text` box per
/// visual line, plus the run itself for the painter. Returns the run height.
fn flush_text(
    shaper: &TextShaper,
    style: &Style,
    x: f64,
    y: f64,
    width: f64,
    run: &mut String,
    out: &mut Vec<LayoutBox>,
    runs: &mut Vec<TextRun>,
) -> f64 {
    let text = run.split_whitespace().collect::<Vec<_>>().join(" ");
    run.clear();
    if text.is_empty() {
        return 0.0;
    }

    let measured = shaper.measure(&text, style, width);
    for line in &measured.lines {
        if line.text.is_empty() {
            continue;
        }
        out.push(LayoutBox {
            node: usize::MAX,
            tag: "#text".to_string(),
            x: x + line.x,
            y: y + line.y,
            w: line.w,
            h: line.h,
            text: Some(line.text.clone()),
            color: style.color,
            background: None,
            font_size: style.font_size,
            bold: style.bold,
            centered: false,
            image: None,
        });
    }

    runs.push(TextRun {
        x,
        y,
        width,
        text,
        style: style.clone(),
    });
    measured.height
}

/// One visual line as Pango placed it, relative to the run origin.
struct MeasuredLine {
    x: f64,
    y: f64,
    w: f64,
    h: f64,
    text: String,
}

struct MeasuredText {
    height: f64,
    lines: Vec<MeasuredLine>,
}

/// A Pango context used only for measuring. It hangs off a scratch image
/// surface; the real painting context is built by `paint` from its own cairo
/// target, using the same 72 dpi so both agree on metrics.
struct TextShaper {
    ctx: *mut c_void,
    // Keep the scratch target alive: the PangoContext references it.
    _surface: cairo::ImageSurface,
    _cr: cairo::Context,
}

impl TextShaper {
    fn new() -> Option<TextShaper> {
        let surface = cairo::ImageSurface::create(cairo::Format::ARgb32, 8, 8).ok()?;
        let cr = cairo::Context::new(&surface).ok()?;
        let ctx = unsafe { pango::pango_cairo_create_context(cr.to_raw_none() as *mut c_void) };
        if ctx.is_null() {
            return None;
        }
        // 72 dpi: 1pt == 1px, so CSS px sizes map 1:1 to Pango font sizes.
        unsafe { pango::pango_cairo_context_set_resolution(ctx, 72.0) };
        Some(TextShaper {
            ctx,
            _surface: surface,
            _cr: cr,
        })
    }

    fn measure(&self, text: &str, style: &Style, width: f64) -> MeasuredText {
        let layout = pango::create_layout(self.ctx, text, style, width);
        if layout.is_null() {
            return MeasuredText {
                height: 0.0,
                lines: Vec::new(),
            };
        }

        let mut pixel_width = 0;
        let mut pixel_height = 0;
        let mut lines = Vec::new();
        unsafe {
            pango::pango_layout_get_pixel_size(layout, &mut pixel_width, &mut pixel_height);
            let count = pango::pango_layout_get_line_count(layout);
            for index in 0..count {
                let line = pango::pango_layout_get_line(layout, index);
                if line.is_null() {
                    continue;
                }
                let mut ink = pango::PangoRectangle::default();
                let mut logical = pango::PangoRectangle::default();
                pango::pango_layout_line_get_pixel_extents(line, &mut ink, &mut logical);
                let start = (*line).start_index.max(0) as usize;
                let len = (*line).length.max(0) as usize;
                // Line extents are relative to the line itself; the position of
                // the line's first character is relative to the layout origin
                // and already includes the alignment offset.
                let mut pos = pango::PangoRectangle::default();
                pango::pango_layout_index_to_pos(layout, start as c_int, &mut pos);
                let slice = text.get(start..start.saturating_add(len)).unwrap_or("");
                lines.push(MeasuredLine {
                    x: pos.x as f64 / pango::PANGO_SCALE,
                    y: pos.y as f64 / pango::PANGO_SCALE,
                    w: logical.width as f64,
                    h: logical.height as f64,
                    text: slice.trim().to_string(),
                });
            }
            pango::g_object_unref(layout);
        }

        MeasuredText {
            height: pixel_height as f64,
            lines,
        }
    }
}

impl Drop for TextShaper {
    fn drop(&mut self) {
        unsafe { pango::g_object_unref(self.ctx) };
    }
}

/// Hand-written Pango/PangoCairo FFI, in the same style as `src/sys.rs`.
/// Signatures follow the system Pango 1.58 headers; no intermediate crates.
pub(crate) mod pango {
    use std::ffi::CString;
    use std::os::raw::{c_char, c_double, c_int, c_void};

    use crate::css::{Style, TextAlign};

    /// Pango's fixed-point scale for font sizes and lengths.
    pub const PANGO_SCALE: f64 = 1024.0;

    const PANGO_ALIGN_LEFT: c_int = 0;
    const PANGO_ALIGN_CENTER: c_int = 1;
    const PANGO_ALIGN_RIGHT: c_int = 2;

    const PANGO_STYLE_NORMAL: c_int = 0;
    const PANGO_STYLE_ITALIC: c_int = 2;

    #[repr(C)]
    #[derive(Default)]
    pub struct PangoRectangle {
        pub x: c_int,
        pub y: c_int,
        pub width: c_int,
        pub height: c_int,
    }

    /// `PangoLayoutLine` is a public struct in the pango headers: reading
    /// `start_index`/`length` gives the bytes of each visual line.
    #[repr(C)]
    pub struct PangoLayoutLine {
        pub layout: *mut c_void,
        pub start_index: c_int,
        pub length: c_int,
        pub runs: *mut c_void,
    }

    #[link(name = "pangocairo-1.0")]
    extern "C" {
        pub fn pango_cairo_create_context(cr: *mut c_void) -> *mut c_void;
        pub fn pango_cairo_context_set_resolution(context: *mut c_void, dpi: c_double);
        pub fn pango_cairo_update_layout(cr: *mut c_void, layout: *mut c_void);
        pub fn pango_cairo_show_layout(cr: *mut c_void, layout: *mut c_void);
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
        pub fn pango_layout_get_pixel_size(
            layout: *mut c_void,
            width: *mut c_int,
            height: *mut c_int,
        );
        pub fn pango_layout_get_line_count(layout: *mut c_void) -> c_int;
        pub fn pango_layout_get_line(layout: *mut c_void, line: c_int) -> *mut PangoLayoutLine;
        pub fn pango_layout_index_to_pos(
            layout: *mut c_void,
            index: c_int,
            pos: *mut PangoRectangle,
        );
        pub fn pango_layout_line_get_pixel_extents(
            line: *mut PangoLayoutLine,
            ink_rect: *mut PangoRectangle,
            logical_rect: *mut PangoRectangle,
        );
        pub fn pango_font_description_from_string(str: *const c_char) -> *mut c_void;
        pub fn pango_font_description_free(desc: *mut c_void);
        pub fn pango_font_description_set_family(desc: *mut c_void, family: *const c_char);
        pub fn pango_font_description_set_size(desc: *mut c_void, size: c_int);
        pub fn pango_font_description_set_weight(desc: *mut c_void, weight: c_int);
        pub fn pango_font_description_set_style(desc: *mut c_void, style: c_int);
        pub fn pango_attr_list_new() -> *mut c_void;
        pub fn pango_attr_list_insert(list: *mut c_void, attr: *mut c_void);
        pub fn pango_attr_list_unref(list: *mut c_void);
        pub fn pango_attr_letter_spacing_new(spacing: c_int) -> *mut c_void;
        pub fn pango_attr_line_height_new(factor: c_double) -> *mut c_void;
    }

    #[link(name = "gobject-2.0")]
    extern "C" {
        pub fn g_object_unref(obj: *mut c_void);
    }

    /// Builds the Pango layout shared by measurement and painting, so both see
    /// the same font, width, alignment, line height and tracking.
    pub fn create_layout(
        ctx: *mut c_void,
        text: &str,
        style: &Style,
        width: f64,
    ) -> *mut c_void {
        unsafe {
            let layout = pango_layout_new(ctx);
            if layout.is_null() {
                return layout;
            }

            let desc = pango_font_description_from_string(b"Sans\0".as_ptr() as *const c_char);
            let family = CString::new(family_name(&style.font_family)).unwrap_or_default();
            pango_font_description_set_family(desc, family.as_ptr());
            pango_font_description_set_size(
                desc,
                (style.font_size.max(0.0) * PANGO_SCALE) as c_int,
            );
            pango_font_description_set_weight(desc, style.font_weight.clamp(100, 900) as c_int);
            pango_font_description_set_style(
                desc,
                if style.italic {
                    PANGO_STYLE_ITALIC
                } else {
                    PANGO_STYLE_NORMAL
                },
            );
            pango_layout_set_font_description(layout, desc);
            pango_font_description_free(desc);

            // NUL would truncate the CString; Pango takes an explicit length.
            let text = text.replace('\0', " ");
            let text_c = CString::new(text).unwrap_or_default();
            pango_layout_set_text(layout, text_c.as_ptr(), text_c.as_bytes().len() as c_int);
            pango_layout_set_width(layout, (width.max(1.0) * PANGO_SCALE) as c_int);
            pango_layout_set_alignment(
                layout,
                match style.text_align {
                    TextAlign::Left | TextAlign::Justify => PANGO_ALIGN_LEFT,
                    TextAlign::Center => PANGO_ALIGN_CENTER,
                    TextAlign::Right => PANGO_ALIGN_RIGHT,
                },
            );
            if style.text_align == TextAlign::Justify {
                pango_layout_set_justify(layout, 1);
            }

            // Line height and tracking are attributes in Pango 1.58: the layout
            // has no setter for them.
            let attrs = pango_attr_list_new();
            pango_attr_list_insert(
                attrs,
                pango_attr_line_height_new(style.line_height.max(0.0)),
            );
            if style.letter_spacing != 0.0 {
                pango_attr_list_insert(
                    attrs,
                    pango_attr_letter_spacing_new((style.letter_spacing * PANGO_SCALE) as c_int),
                );
            }
            pango_layout_set_attributes(layout, attrs);
            pango_attr_list_unref(attrs);

            layout
        }
    }

    /// libcss keeps the family as written; strip the CSS quotes and fall back
    /// to Pango's default family when it is empty.
    fn family_name(family: &str) -> String {
        let trimmed = family.trim().trim_matches(['"', '\'']).trim();
        if trimmed.is_empty() {
            "Sans".to_string()
        } else {
            trimmed.to_string()
        }
    }
}

