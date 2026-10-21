//! Cairo paint backend: layout boxes in, ARGB pixels or PNG out.
//! Software rendering now; the same box list can feed a GL/SDL texture later.

use std::fs::File;
use std::path::Path;

use cairo::{Context, FontSlant, FontWeight, Format, ImageSurface};

use crate::layout::Layout;

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

    for b in &layout.boxes {
        let Some(text) = &b.text else { continue };
        cr.set_source_rgba(b.color.r, b.color.g, b.color.b, b.color.a);
        cr.select_font_face(
            "Sans",
            FontSlant::Normal,
            if b.bold {
                FontWeight::Bold
            } else {
                FontWeight::Normal
            },
        );
        cr.set_font_size(b.font_size);
        cr.move_to(b.x, b.y + b.font_size);
        cr.show_text(text).map_err(|e| e.to_string())?;
    }
    Ok(())
}
