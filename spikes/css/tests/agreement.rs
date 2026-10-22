//! Test de acuerdo entre proveedores (odd/tasks/css-spike.md, T3, S2).
//!
//! El resultado esperado viene de la especificación de CSS, no de un proveedor:
//! ambos caminos tienen que llegar al mismo estilo computado en el terreno común
//! (fixture 01: especificidad sobre color, fondo, caja y font-size). Antes de
//! implementar el proveedor B este test debe verse en RED.

use browser_lite::dom::Document;
use css_spike::provider::{stylesheets, CssRsProvider, LibcssProvider, Provider};
use css_spike::style::SpikeStyle;

fn fixture(name: &str) -> String {
    std::fs::read_to_string(format!("{}/fixtures/{name}", env!("CARGO_MANIFEST_DIR")))
        .unwrap_or_else(|e| panic!("fixture {name}: {e}"))
}

#[test]
fn especificidad_proveedores_llegan_al_resultado_spec() {
    let html = fixture("01-especificidad.html");
    let doc = Document::parse(&html);
    let sheets = stylesheets(&doc);

    let providers: Vec<Box<dyn Provider>> =
        vec![Box::new(CssRsProvider), Box::new(LibcssProvider)];
    for provider in providers {
        let styles = provider.compute(&doc, &sheets);
        let name = provider.name();

        // #destino gana a .caja gana a div: fondo #ffcccc, texto #cc0000,
        // margen 12, padding 12, font-size 22.
        let destino = doc.query_selector("#destino").expect("fixture sin #destino");
        let s = &styles[destino];
        assert!(
            SpikeStyle::color_eq(s.background, Some((1.0, 0.8, 0.8, 1.0))),
            "[{name}] background de #destino: {:?}",
            s.background
        );
        assert!(
            SpikeStyle::color_eq(Some(s.color), Some((0.8, 0.0, 0.0, 1.0))),
            "[{name}] color de #destino: {:?}",
            s.color
        );
        assert!(SpikeStyle::near(s.font_size, 22.0), "[{name}] font-size: {}", s.font_size);
        assert!(
            SpikeStyle::near(s.margin[0], 12.0) && SpikeStyle::near(s.margin[3], 12.0),
            "[{name}] margin de #destino: {:?}",
            s.margin
        );
        assert!(
            SpikeStyle::near(s.padding[0], 12.0),
            "[{name}] padding de #destino: {:?}",
            s.padding
        );

        // .caja gana a div: fondo #dddddd, texto #0000ff, margen 8, font-size 18.
        let cajas = doc.query_selector_all(".caja");
        let clase = *cajas.get(1).expect("fixture sin segundo .caja");
        let s = &styles[clase];
        let gris = 221.0 / 255.0;
        assert!(
            SpikeStyle::color_eq(s.background, Some((gris, gris, gris, 1.0))),
            "[{name}] background de .caja: {:?}",
            s.background
        );
        assert!(
            SpikeStyle::color_eq(Some(s.color), Some((0.0, 0.0, 1.0, 1.0))),
            "[{name}] color de .caja: {:?}",
            s.color
        );
        assert!(SpikeStyle::near(s.font_size, 18.0), "[{name}] font-size: {}", s.font_size);
    }
}
