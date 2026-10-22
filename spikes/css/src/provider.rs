//! Proveedores de estilo: la única pieza que cambia entre las dos rutas (S2).

use browser_lite::dom::Document;

use crate::style::{Display, SpikeStyle};

pub trait Provider {
    fn name(&self) -> &'static str;
    fn compute(&self, doc: &Document, sheets: &[String]) -> Vec<SpikeStyle>;
}

/// Bloques `<style>` del documento, en orden.
pub fn stylesheets(doc: &Document) -> Vec<String> {
    doc.query_selector_all("style")
        .into_iter()
        .map(|id| doc.text_content(id))
        .filter(|s| !s.trim().is_empty())
        .collect()
}

/// Ruta A: el motor propio (`css.rs`), tal cual existe hoy.
pub struct CssRsProvider;

impl Provider for CssRsProvider {
    fn name(&self) -> &'static str {
        "cssrs"
    }

    fn compute(&self, doc: &Document, sheets: &[String]) -> Vec<SpikeStyle> {
        let mut source = String::new();
        for s in sheets {
            source.push_str(s);
            source.push('\n');
        }
        let sheet = browser_lite::css::Stylesheet::parse(&source);
        browser_lite::css::compute(doc, &sheet)
            .iter()
            .map(from_engine_style)
            .collect()
    }
}

fn from_engine_style(s: &browser_lite::css::Style) -> SpikeStyle {
    let mut out = SpikeStyle::default();
    out.display = match s.display {
        browser_lite::css::Display::Block => Display::Block,
        browser_lite::css::Display::Inline => Display::Inline,
        browser_lite::css::Display::None => Display::None,
    };
    out.width = s.width;
    out.height = s.height;
    out.margin = s.margin;
    out.padding = s.padding;
    out.color = (s.color.r, s.color.g, s.color.b, s.color.a);
    out.background = s
        .background
        .map(|c| (c.r, c.g, c.b, c.a));
    out.font_size = s.font_size;
    if s.bold {
        out.font_weight = 700;
    }
    out
}

/// Ruta B: libcss 0.9.2 vía el FFI de `libcss_ffi` (T3).
pub struct LibcssProvider;

impl Provider for LibcssProvider {
    fn name(&self) -> &'static str {
        "libcss"
    }

    fn compute(&self, doc: &Document, sheets: &[String]) -> Vec<SpikeStyle> {
        crate::libcss_ffi::compute(doc, sheets)
    }
}
