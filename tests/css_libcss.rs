//! Computed-style tests for the libcss-backed engine path (T2, S3/S4).
//!
//! These tests talk to the public engine API only: parse a document, collect
//! its `<style>` elements and compute styles at a given viewport width. The
//! expected values come from the CSS spec and from the engine's documented UA
//! sheet, never from an implementation detail.

use browser_lite::css::{Display, Style, TextAlign};
use browser_lite::css_libcss;
use browser_lite::dom::{Document, NodeId};

fn compute(html: &str, viewport_width: f64) -> (Document, Vec<Style>) {
    let doc = Document::parse(html);
    let sheets = css_libcss::stylesheets(&doc);
    let styles = css_libcss::compute(&doc, &sheets, viewport_width);
    (doc, styles)
}

fn fixture(name: &str) -> String {
    std::fs::read_to_string(format!(
        "{}/test/data/{name}",
        env!("CARGO_MANIFEST_DIR")
    ))
    .unwrap_or_else(|e| panic!("fixture {name}: {e}"))
}

fn near(a: f64, b: f64) -> bool {
    (a - b).abs() < 0.01
}

fn color_near(actual: browser_lite::css::Color, r: f64, g: f64, b: f64) -> bool {
    near(actual.r, r) && near(actual.g, g) && near(actual.b, b) && near(actual.a, 1.0)
}

fn by_class(doc: &Document, class: &str) -> NodeId {
    doc.query_selector(&format!(".{class}"))
        .unwrap_or_else(|| panic!("fixture has no .{class}"))
}

#[test]
fn specificity_winner_beats_class_and_tag() {
    let (doc, styles) = compute(&fixture("css-specificity.html"), 800.0);

    let destino = doc.query_selector("#destino").expect("#destino");
    let s = &styles[destino];
    assert!(
        color_near(s.color, 0.8, 0.0, 0.0),
        "#destino colour: {:?}",
        s.color
    );
    let bg = s.background.expect("#destino background");
    assert!(
        color_near(bg, 1.0, 0.8, 0.8),
        "#destino background: {:?}",
        bg
    );
    assert!(near(s.margin[0], 12.0) && near(s.margin[3], 12.0));
    assert!(near(s.padding[0], 12.0) && near(s.padding[3], 12.0));
    assert!(near(s.font_size, 22.0), "font-size: {}", s.font_size);

    // The class rule beats the bare tag rule.
    let cajas = doc.query_selector_all(".caja");
    let clase = cajas[1];
    let s = &styles[clase];
    assert!(
        color_near(s.color, 0.0, 0.0, 1.0),
        ".caja colour: {:?}",
        s.color
    );
    let grey = 238.0 / 255.0;
    let bg = s.background.expect(".caja background");
    assert!(color_near(bg, grey, grey, grey));
    assert!(near(s.font_size, 20.0));
}

#[test]
fn author_rules_beat_the_ua_stylesheet() {
    let (doc, styles) = compute(
        "<style>p { display: inline; margin: 3px; }</style><p id=x>x</p>",
        800.0,
    );
    let s = &styles[doc.query_selector("#x").unwrap()];
    assert_eq!(s.display, Display::Inline);
    assert!(near(s.margin[0], 3.0), "margin: {:?}", s.margin);
}

#[test]
fn ua_stylesheet_supplies_display_margins_and_heading_sizes() {
    let (doc, styles) = compute(
        "<head><style></style><script></script></head><body><p>x</p><span>y</span><h1>z</h1></body>",
        800.0,
    );

    let none = |selector: &str| {
        let id = doc.query_selector(selector).unwrap_or_else(|| panic!("{selector}"));
        assert_eq!(styles[id].display, Display::None, "{selector} display");
    };
    none("head");
    none("style");
    none("script");

    let p = &styles[doc.query_selector("p").unwrap()];
    assert_eq!(p.display, Display::Block);
    assert!(near(p.margin[0], 16.0) && near(p.margin[2], 16.0));
    assert_eq!(styles[doc.query_selector("span").unwrap()].display, Display::Inline);

    let body = &styles[doc.query_selector("body").unwrap()];
    assert_eq!(body.display, Display::Block);
    assert!(body.margin.iter().all(|m| near(*m, 8.0)), "body margin: {:?}", body.margin);

    let h1 = &styles[doc.query_selector("h1").unwrap()];
    assert!(near(h1.font_size, 32.0), "h1 font-size: {}", h1.font_size);
    assert_eq!(h1.font_weight, 700);
    assert!(h1.bold);
}

#[test]
fn computed_style_inherits_and_resolves_relative_font_sizes() {
    let (doc, styles) = compute(&fixture("css-inheritance.html"), 800.0);

    let body = &styles[doc.query_selector("body").unwrap()];
    assert!(near(body.font_size, 20.0), "body font-size: {}", body.font_size);

    let level3 = by_class(&doc, "nivel3");
    let s = &styles[level3];
    assert!(
        color_near(s.color, 0.0, 68.0 / 255.0, 170.0 / 255.0),
        "inherited colour: {:?}",
        s.color
    );
    assert!(near(s.font_size, 15.0), "font-size: {}", s.font_size);
    assert_eq!(s.font_family, "Serif");
    assert!(near(s.line_height, 2.0), "line-height: {}", s.line_height);
    assert_eq!(s.font_weight, 700);
    assert!(s.bold);
}

#[test]
fn media_queries_resolve_against_the_viewport_width() {
    let html = fixture("css-media.html");

    let (doc, wide) = compute(&html, 800.0);
    let base = doc.query_selector("#base").unwrap();
    assert!(
        color_near(wide[base].color, 0.0, 0.2, 0.8),
        "screen rule colour: {:?}",
        wide[base].color
    );
    let bg = wide[base].background.expect("wide background");
    assert!(color_near(bg, 1.0, 1.0, 0.8), "wide background: {:?}", bg);
    assert!(near(wide[base].font_size, 18.0));

    let (doc, narrow) = compute(&html, 400.0);
    let base = doc.query_selector("#base").unwrap();
    assert!(color_near(narrow[base].color, 0.0, 0.2, 0.8));
    let bg = narrow[base].background.expect("narrow background");
    assert!(color_near(bg, 0.8, 1.0, 0.8), "narrow background: {:?}", bg);
    assert!(near(narrow[base].font_size, 12.0));
}

#[test]
fn typography_fields_come_from_the_cascade() {
    let (doc, styles) = compute(&fixture("css-typography.html"), 800.0);

    let h1 = &styles[doc.query_selector("h1").unwrap()];
    assert_eq!(h1.font_family, "T2 Mono");
    assert!(near(h1.font_size, 28.0));
    assert_eq!(h1.font_weight, 800);
    assert!(!h1.italic);
    assert!(near(h1.line_height, 1.5), "line-height: {}", h1.line_height);
    assert_eq!(h1.text_align, TextAlign::Center);
    assert!(near(h1.letter_spacing, 2.0));

    let p = &styles[by_class(&doc, "largo")];
    assert_eq!(p.font_family, "Serif");
    assert!(near(p.font_size, 16.0));
    assert_eq!(p.font_weight, 300);
    assert!(p.italic);
    assert!(near(p.line_height, 1.8));
    assert_eq!(p.text_align, TextAlign::Justify);
    assert!(near(p.letter_spacing, 0.5));
}

#[test]
fn inline_style_attribute_wins_over_author_rules() {
    let (doc, styles) = compute(
        "<style>p { color: blue; }</style><p id=x style=\"color: #00ff00; font-weight: 700\">x</p>",
        800.0,
    );
    let s = &styles[doc.query_selector("#x").unwrap()];
    assert!(color_near(s.color, 0.0, 1.0, 0.0), "inline colour: {:?}", s.color);
    assert_eq!(s.font_weight, 700);
}
