//! Regression probes for the libcss style layer (S3/S4) and for the
//! process-wide serialisation of `css_libcss::compute`.
//!
//! libwapcaplet interns strings in a process-wide dictionary with non-atomic
//! refcounts, so two concurrent selections must never overlap. These tests also
//! pin the viewport-driven `@media` resolution, the UA-origin defaults and the
//! additive typography fields of `css::Style`.
//!
//! Written by the independent verifier of commit cfafc50 (odd/tasks/css-libcss.md).

use browser_lite::css::{self, Color, Display, Style, TextAlign};
use browser_lite::css_libcss;
use browser_lite::dom::Document;

fn compute(html: &str, viewport_width: f64) -> (Document, Vec<Style>) {
    let doc = Document::parse(html);
    let sheets = css_libcss::stylesheets(&doc);
    let styles = css_libcss::compute(&doc, &sheets, viewport_width);
    (doc, styles)
}

fn style_of(html: &str, viewport_width: f64, selector: &str) -> Style {
    let (doc, styles) = compute(html, viewport_width);
    let id = doc
        .query_selector(selector)
        .unwrap_or_else(|| panic!("no {selector}"));
    styles[id].clone()
}

fn near(a: f64, b: f64) -> bool {
    (a - b).abs() < 0.02
}

fn rgb(c: Color) -> (i32, i32, i32) {
    (
        (c.r * 255.0).round() as i32,
        (c.g * 255.0).round() as i32,
        (c.b * 255.0).round() as i32,
    )
}

/// S4: the legacy subset API keeps its signature and its behaviour, because
/// `spikes/css` still drives it.
#[test]
fn legacy_parse_and_compute_keep_their_signatures() {
    let _: fn(&str) -> css::Stylesheet = css::Stylesheet::parse;
    let _: fn(&Document, &css::Stylesheet) -> Vec<Style> = css::compute;

    let doc = Document::parse("<p id=x>x</p>");
    let sheet = css::Stylesheet::parse("p { color: #ff0000; margin: 4px }");
    let styles = css::compute(&doc, &sheet);
    let s = &styles[doc.query_selector("#x").unwrap()];
    assert_eq!(rgb(s.color), (255, 0, 0));
    assert!(near(s.margin[0], 4.0));
}

/// S3: `@media` width features resolve against the requested viewport, and the
/// boundary is inclusive in both directions.
#[test]
fn media_width_features_track_the_viewport() {
    let html = "<style>\
        @media (min-width: 1000px) { #x { color: #ff0000 } }\
        @media (max-width: 999px) { #x { color: #0000ff } }\
        </style><p id=x>x</p>";
    assert_eq!(rgb(style_of(html, 1000.0, "#x").color), (255, 0, 0));
    assert_eq!(rgb(style_of(html, 999.0, "#x").color), (0, 0, 255));
    assert_eq!(rgb(style_of(html, 2000.0, "#x").color), (255, 0, 0));

    let print = "<style>@media print { #x { color: #ff0000 } }\
                 @media screen { #x { color: #00ff00 } }</style><p id=x>x</p>";
    assert_eq!(rgb(style_of(print, 800.0, "#x").color), (0, 255, 0));
}

/// S3: the UA sheet fills display/margins/heading sizes at the UA origin, so
/// every author rule wins but unstyled tags still lay out as a document.
#[test]
fn ua_sheet_defaults_lose_to_author_rules() {
    let ua = style_of("<h1>x</h1>", 800.0, "h1");
    assert_eq!(ua.display, Display::Block);
    assert!(near(ua.font_size, 32.0), "h1 size: {}", ua.font_size);
    assert!(near(ua.margin[0], 21.0), "h1 margin: {:?}", ua.margin);
    assert_eq!(ua.font_weight, 700);
    assert_eq!(
        style_of("<style>p{}</style><p>x</p>", 800.0, "style").display,
        Display::None
    );
    assert_eq!(style_of("<p>x</p>", 800.0, "p").display, Display::Block);

    let author = style_of(
        "<style>h1 { display: inline; margin: 0; font-size: 10px }</style><h1>x</h1>",
        800.0,
        "h1",
    );
    assert_eq!(author.display, Display::Inline);
    assert!(near(author.font_size, 10.0));
    assert!(near(author.margin[0], 0.0) && near(author.margin[2], 0.0));
}

/// S4: typography comes from the cascade, inherits, and the coarse layout/paint
/// flags (`bold`, `inline_align`) mirror the new fields.
#[test]
fn typography_fields_cascade_inherit_and_mirror_flags() {
    let html = "<style>div { font-family: \"No Such Font\", serif; font-weight: 300;\
        font-style: italic; line-height: 24px; text-align: right; letter-spacing: 1.5px }\
        </style><div><span id=x>t</span></div>";
    let div = style_of(html, 800.0, "div");
    assert_eq!(div.font_family, "No Such Font");
    assert_eq!(div.font_weight, 300);
    assert!(div.italic);
    assert!(near(div.line_height, 1.5), "24px/16px: {}", div.line_height);
    assert_eq!(div.text_align, TextAlign::Right);
    assert!(near(div.letter_spacing, 1.5));

    let span = style_of(html, 800.0, "#x");
    assert_eq!(span.font_family, div.font_family);
    assert_eq!(span.font_weight, 300);
    assert!(span.italic);
    assert_eq!(span.text_align, TextAlign::Right);
    assert!(near(span.letter_spacing, 1.5));
    assert!(near(span.line_height, 1.5));

    let plain = style_of("<p id=x>x</p>", 800.0, "#x");
    assert_eq!(plain.font_family, "Sans");
    assert_eq!(plain.font_weight, 400);
    assert!(!plain.italic && !plain.bold && !plain.inline_align);
    assert_eq!(plain.text_align, TextAlign::Left);
    assert!(near(plain.line_height, 1.2) && near(plain.letter_spacing, 0.0));

    let bold = style_of("<style>p { font-weight: 700 }</style><p id=x>x</p>", 800.0, "#x");
    assert_eq!(bold.font_weight, 700);
    assert!(bold.bold, "bold flag mirrors weight >= 600");
    let centered = style_of(
        "<style>p { text-align: center }</style><p id=x>x</p>",
        800.0,
        "#x",
    );
    assert_eq!(centered.text_align, TextAlign::Center);
    assert!(centered.inline_align);
}

/// S3: one style per DOM node; text and synthetic document nodes keep defaults.
#[test]
fn every_node_gets_a_style_and_defaults_are_stable() {
    let (doc, styles) = compute("<p>hola</p>", 800.0);
    assert_eq!(styles.len(), doc.nodes.len());
    assert_eq!(styles[doc.root].display, Display::Block);
    let text: browser_lite::dom::NodeId = doc.nodes.iter().position(|n| n.is_text).unwrap();
    assert_eq!(styles[text].display, Display::Inline);
    assert!(near(styles[text].font_size, 16.0));
}

/// S3: malformed or unsupported CSS must not abort the engine.
#[test]
fn malformed_stylesheets_are_survivable() {
    for html in [
        "<p id=x>x</p>",
        "<style></style><p id=x>x</p>",
        "<style>p { color: }</style><p id=x>x</p>",
        "<style>@import url(nope.css); p { color: #ff0000 }</style><p id=x>x</p>",
        "<style>p { color: #ff0000 </style><p id=x>x</p>",
        "<style>* { margin: 1px }</style><p id=x style=\"color: blue\">x</p>",
        "<style>div p + span ~ b { color: red }</style><div><p></p><span></span><b id=x></b></div>",
    ] {
        let (doc, styles) = compute(html, 800.0);
        assert_eq!(styles.len(), doc.nodes.len(), "input: {html}");
    }
    let attr = style_of(
        "<style>p[data-x=\"1\"] { color: #ff0000 }</style><p id=x data-x=1>x</p>",
        800.0,
        "#x",
    );
    assert_eq!(rgb(attr.color), (255, 0, 0));
    let link = style_of(
        "<style>a:link { color: #00ff00 }</style><a id=x href=#>x</a>",
        800.0,
        "#x",
    );
    assert_eq!(rgb(link.color), (0, 255, 0));
}

/// Risk: `compute` is serialised because libwapcaplet refcounts are not atomic.
/// Eight threads, distinct documents and two viewport widths, must keep their
/// own values and never panic or hang. Removing the lock makes this SIGSEGV.
#[test]
fn parallel_compute_keeps_values_and_does_not_race() {
    let mut handles = Vec::new();
    for t in 0..8 {
        handles.push(std::thread::spawn(move || {
            for i in 0..40 {
                let n = t * 40 + i;
                let html = format!(
                    "<style>@media (min-width: 500px) {{ .c{n} {{ color: rgb({}, 0, 0); font-size: {}px }} }}</style>\
                     <div class=\"c{n}\" id=\"n{n}\">x</div>",
                    (n * 7) % 255,
                    10 + n
                );
                let (doc, styles) = compute(&html, 800.0);
                let s = &styles[doc.query_selector(&format!("#n{n}")).unwrap()];
                let want_r = ((n * 7) % 255) as f64 / 255.0;
                assert!((s.color.r - want_r).abs() < 0.02, "colour {:?}", s.color);
                assert_eq!(s.font_size, (10 + n) as f64);
            }
        }));
    }
    for h in handles {
        h.join().expect("compute panicked under concurrency");
    }
}

/// Risk: the libcss/class-array/node-data teardown must not leak per call.
#[test]
fn repeated_compute_does_not_grow_rss() {
    fn rss_kb() -> u64 {
        std::fs::read_to_string("/proc/self/status")
            .ok()
            .and_then(|s| {
                s.lines().find_map(|l| {
                    l.strip_prefix("VmRSS:")
                        .map(|r| r.trim().trim_end_matches(" kB").trim().to_owned())
                })
            })
            .and_then(|v| v.parse().ok())
            .unwrap_or(0)
    }
    let html = "<style>.a { color: #123456; font-size: 13px } .b.a { margin: 2px }</style>\
                <div class=\"a b\" id=\"x\"><span class=\"a\">y</span></div>";
    for _ in 0..200 {
        let _ = compute(html, 800.0);
    }
    if rss_kb() == 0 {
        return; // not Linux: no /proc
    }
    let before = rss_kb();
    for _ in 0..2000 {
        let _ = compute(html, 800.0);
    }
    let after = rss_kb();
    assert!(
        after < before + 32_768,
        "RSS grew by {} kB",
        after as i64 - before as i64
    );
}

/// ADVISORY (fails today): libcss 0.9.2 maps `vw` to the viewport height and
/// `vh` to the viewport width (upstream bug), and the engine leaves
/// `css_unit_ctx.root_style` NULL so `rem` falls back to the 16px client default
/// instead of the root element's font-size. Enable once both are resolved.
#[test]
#[ignore = "libcss 0.9.2 resolves vw/vh against the wrong axis; rem ignores the root font-size"]
fn viewport_units_and_rem_follow_css() {
    let vw = style_of("<div id=x style=\"width: 50vw\">x</div>", 800.0, "#x");
    assert!(near(vw.width.unwrap_or(-1.0), 400.0), "50vw at 800px: {:?}", vw.width);
    let vh = style_of("<div id=x style=\"height: 50vh\">x</div>", 800.0, "#x");
    assert!(near(vh.height.unwrap_or(-1.0), 300.0), "50vh at 600px: {:?}", vh.height);
    let rem = style_of(
        "<style>html { font-size: 20px } #x { font-size: 2rem }</style><div id=x>x</div>",
        800.0,
        "#x",
    );
    assert!(near(rem.font_size, 40.0), "2rem against a 20px root: {}", rem.font_size);
}
