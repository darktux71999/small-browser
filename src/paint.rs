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

use crate::layout::{pango, Layout};

#[derive(Clone)]
pub struct Frame {
    pub pixels: Vec<u8>,
    pub width: i32,
    pub height: i32,
}

pub fn render(layout: &Layout) -> Result<Frame, String> {
    let width = layout.width.max(1.0) as i32;
    let height = layout.height.ceil().max(1.0) as i32;

    let mut surface =
        ImageSurface::create(Format::ARgb32, width, height).map_err(|e| e.to_string())?;
    draw(&surface, layout)?;

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

pub fn render_to_png(layout: &Layout, path: &Path) -> Result<(), String> {
    let width = layout.width.max(1.0) as i32;
    let height = layout.height.ceil().max(1.0) as i32;

    let surface =
        ImageSurface::create(Format::ARgb32, width, height).map_err(|e| e.to_string())?;
    draw(&surface, layout)?;

    let mut file = File::create(path).map_err(|e| e.to_string())?;
    surface.write_to_png(&mut file).map_err(|e| e.to_string())?;
    Ok(())
}

fn draw(surface: &ImageSurface, layout: &Layout) -> Result<(), String> {
    let cr = Context::new(surface).map_err(|e| e.to_string())?;
    cr.set_source_rgb(1.0, 1.0, 1.0);
    cr.paint().map_err(|e| e.to_string())?;

    for b in &layout.boxes {
        if let Some(bg) = b.background {
            cr.set_source_rgba(bg.r, bg.g, bg.b, bg.a);
            cr.rectangle(b.x, b.y, b.w, b.h);
            cr.fill().map_err(|e| e.to_string())?;
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
