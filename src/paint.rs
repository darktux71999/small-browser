//! Cairo paint backend: layout boxes in, ARGB pixels or PNG out.
//! Software rendering now; the same box list can feed a GL/SDL texture later.
//!
//! Text is drawn by Pango, rebuilding the layout that `layout` measured, so the
//! painted lines match the computed boxes (wrapping, alignment, line height,
//! tracking, family/weight/style).

use std::fs::File;
use std::os::raw::c_void;
use std::path::Path;

use cairo::{Context, Format, ImageSurface};

use crate::img::Image;
use crate::layout::{pango, Layout, LayoutBox};
use crate::shell::{VIEWPORT_HEIGHT, VIEWPORT_WIDTH};

#[derive(Clone)]
pub struct Frame {
    pub pixels: Vec<u8>,
    pub width: i32,
    pub height: i32,
}

/// Full-document frame: every box of `layout`, untouched. This is the
/// measuring path behind `--png`, so it must stay byte-identical.
pub fn render(layout: &Layout) -> Result<Frame, String> {
    let width = layout.width.max(1.0) as i32;
    let height = layout.height.ceil().max(1.0) as i32;

    let mut surface =
        ImageSurface::create(Format::ARgb32, width, height).map_err(|e| e.to_string())?;
    draw(&surface, layout, None)?;

    frame_from_surface(&mut surface, width, height)
}

/// Interactive frame (S9): exactly [`VIEWPORT_WIDTH`] x [`VIEWPORT_HEIGHT`]
/// pixels of `layout`, clipped to the viewport, with the document translated
/// up by `scroll_offset`. Nothing is truncated: the caller passes the full
/// layout and the offset, the painter only decides what is visible.
pub fn render_viewport(layout: &Layout, scroll_offset: f64) -> Result<Frame, String> {
    let width = VIEWPORT_WIDTH;
    let height = VIEWPORT_HEIGHT;

    let mut surface =
        ImageSurface::create(Format::ARgb32, width, height).map_err(|e| e.to_string())?;
    draw(&surface, layout, Some(scroll_offset))?;

    frame_from_surface(&mut surface, width, height)
}

pub fn render_to_png(layout: &Layout, path: &Path) -> Result<(), String> {
    let width = layout.width.max(1.0) as i32;
    let height = layout.height.ceil().max(1.0) as i32;

    let surface =
        ImageSurface::create(Format::ARgb32, width, height).map_err(|e| e.to_string())?;
    draw(&surface, layout, None)?;

    let mut file = File::create(path).map_err(|e| e.to_string())?;
    surface.write_to_png(&mut file).map_err(|e| e.to_string())?;
    Ok(())
}

/// `scroll` is `None` for the full-document renders (`render`,
/// `render_to_png`, and therefore `--png`) and `Some(offset)` for the
/// interactive viewport: clip first in device space, then move the document.
fn draw(surface: &ImageSurface, layout: &Layout, scroll: Option<f64>) -> Result<(), String> {
    let cr = Context::new(surface).map_err(|e| e.to_string())?;
    if let Some(offset) = scroll {
        // A fresh context has an identity matrix, so this clip is in device
        // space (the viewport). The translation afterwards is applied to every
        // box and every Pango run, moving document row `offset` to the top.
        cr.rectangle(
            0.0,
            0.0,
            f64::from(surface.width()),
            f64::from(surface.height()),
        );
        cr.clip();
        cr.translate(0.0, -offset);
    }
    cr.set_source_rgb(1.0, 1.0, 1.0);
    cr.paint().map_err(|e| e.to_string())?;

    for b in &layout.boxes {
        if let Some(bg) = b.background {
            cr.set_source_rgba(bg.r, bg.g, bg.b, bg.a);
            cr.rectangle(b.x, b.y, b.w, b.h);
            cr.fill().map_err(|e| e.to_string())?;
        }
        // `background-image` (S10) goes over the background colour, clipped
        // to the same box, still below every piece of content.
        if let Some(image) = &b.background_image {
            draw_background_image(&cr, b, image)?;
        }
    }

    // Decoded `<img>` pixels, scaled into their layout box.
    for b in &layout.boxes {
        if let Some(image) = &b.image {
            draw_image(&cr, b, image)?;
        }
    }

    // 72 dpi, exactly like the measuring context in `layout`: 1pt == 1px.
    let cr_ptr = cr.to_raw_none() as *mut c_void;
    let ctx = unsafe { pango::pango_cairo_create_context(cr_ptr) };
    if ctx.is_null() {
        return Err("pango: cannot create a context for the surface".to_string());
    }
    unsafe { pango::pango_cairo_context_set_resolution(ctx, 72.0) };

    for run in &layout.runs {
        let text_layout = pango::create_layout(ctx, &run.text, &run.style, run.width);
        if text_layout.is_null() {
            continue;
        }
        let color = run.style.color;
        cr.set_source_rgba(color.r, color.g, color.b, color.a);
        cr.move_to(run.x, run.y);
        unsafe {
            pango::pango_cairo_update_layout(cr_ptr, text_layout);
            pango::pango_cairo_show_layout(cr_ptr, text_layout);
            pango::g_object_unref(text_layout);
        }
    }

    unsafe { pango::g_object_unref(ctx) };
    Ok(())
}

/// Draws one image inside its layout box, scaling it when the box differs from
/// the intrinsic size (author `width`/`height`, or the content-box clamp).
fn draw_image(cr: &Context, b: &LayoutBox, image: &Image) -> Result<(), String> {
    if b.w <= 0.0 || b.h <= 0.0 || image.width == 0 || image.height == 0 {
        return Ok(());
    }
    let surface = rgba_surface(image)?;
    let pattern = cairo::SurfacePattern::create(&surface);
    // Boxes are usually scaled up from a small logo, so spend the filter.
    pattern.set_filter(cairo::Filter::Good);
    pattern.set_extend(cairo::Extend::Pad);

    cr.save().map_err(|e| e.to_string())?;
    cr.rectangle(b.x, b.y, b.w, b.h);
    cr.clip();
    cr.translate(b.x, b.y);
    cr.scale(b.w / f64::from(image.width), b.h / f64::from(image.height));
    cr.set_source(&pattern).map_err(|e| e.to_string())?;
    cr.paint().map_err(|e| e.to_string())?;
    cr.restore().map_err(|e| e.to_string())?;
    Ok(())
}

/// Draws one `background-image` (S10): the image repeats at its natural size
/// from the box's top-left corner (the CSS `repeat` default), clipped to the
/// box and composited over the background colour. `background-position`,
/// `background-size` and the non-repeating values are not part of the minimal
/// slice.
fn draw_background_image(cr: &Context, b: &LayoutBox, image: &Image) -> Result<(), String> {
    if b.w <= 0.0 || b.h <= 0.0 || image.width == 0 || image.height == 0 {
        return Ok(());
    }
    let surface = rgba_surface(image)?;
    let pattern = cairo::SurfacePattern::create(&surface);
    // Tiles land at natural size (no scaling), so nearest keeps exact pixels.
    pattern.set_filter(cairo::Filter::Nearest);
    pattern.set_extend(cairo::Extend::Repeat);

    cr.save().map_err(|e| e.to_string())?;
    cr.rectangle(b.x, b.y, b.w, b.h);
    cr.clip();
    cr.translate(b.x, b.y);
    cr.set_source(&pattern).map_err(|e| e.to_string())?;
    cr.paint().map_err(|e| e.to_string())?;
    cr.restore().map_err(|e| e.to_string())?;
    Ok(())
}

/// Cairo's ARGB32 is premultiplied, native-endian; [`Image`] is
/// non-premultiplied RGBA8, so every pixel is converted on the way in.
fn rgba_surface(image: &Image) -> Result<ImageSurface, String> {
    let width = image.width as i32;
    let height = image.height as i32;
    let mut surface = ImageSurface::create(Format::ARgb32, width, height)
        .map_err(|e| format!("image surface: {e}"))?;
    let stride = surface.stride() as usize;
    {
        let mut data = surface.data().map_err(|e| e.to_string())?;
        for y in 0..image.height as usize {
            let row = y * stride;
            for x in 0..image.width as usize {
                let src = (y * image.width as usize + x) * 4;
                let alpha = u32::from(image.rgba[src + 3]);
                let premultiply = |channel: u8| u32::from(channel) * alpha / 255;
                // 0xAARRGGBB, the ARGB32 word cairo expects.
                let word = (alpha << 24)
                    | (premultiply(image.rgba[src]) << 16)
                    | (premultiply(image.rgba[src + 1]) << 8)
                    | premultiply(image.rgba[src + 2]);
                let dst = row + x * 4;
                data[dst..dst + 4].copy_from_slice(&word.to_ne_bytes());
            }
        }
    }
    Ok(surface)
}

/// Copies the surface rows (stride aware) into a tightly packed ARGB frame.
fn frame_from_surface(
    surface: &mut ImageSurface,
    width: i32,
    height: i32,
) -> Result<Frame, String> {
    let stride = surface.stride() as usize;
    let mut pixels = Vec::with_capacity((width * height * 4) as usize);
    {
        let data = surface.data().map_err(|e| e.to_string())?;
        for row in 0..height as usize {
            let start = row * stride;
            pixels.extend_from_slice(&data[start..start + width as usize * 4]);
        }
    }

    Ok(Frame {
        pixels,
        width,
        height,
    })
}
