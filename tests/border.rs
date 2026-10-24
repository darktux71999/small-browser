//! S10 (T11) — `border` (per-side width/style/color) and `border-radius`.
//!
//! The tests drive the public engine API only: parse a fixture, compute its
//! styles through libcss, lay out with subresources, and inspect the boxes or
//! the painted frame/PNG pixels. The box model contract under test: the
//! border sits INSIDE the reported border box (the `LayoutBox` w/h keep their
//! meaning, so unbordered pages are byte-identical), the content shrinks by
//! the border widths, and rounded corners clip the background fill too.

use browser_lite::css::{self, BorderRadius, BorderStyle, Style};
use browser_lite::css_libcss;
use browser_lite::dom::{Document, NodeId};
use browser_lite::img::ImageSet;
use browser_lite::layout::{self, Layout, LayoutBox};
use browser_lite::paint;

const VIEWPORT: f64 = 1000.0;

fn fixture(name: &str) -> String {
    std::fs::read_to_string(format!(
        "{}/test/data/{name}",
        env!("CARGO_MANIFEST_DIR")
    ))
    .unwrap_or_else(|e| panic!("fixture {name}: {e}"))
}

/// The base for the local fixtures: their containing directory, exactly what
/// `main` hands the engine for a local page.
fn fixture_base() -> String {
    format!("{}/test/data/", env!("CARGO_MANIFEST_DIR"))
}

fn load(name: &str) -> (Document, Vec<Style>) {
    let doc = Document::parse(&fixture(name));
    let styles = css_libcss::compute_with_subresources(&doc, &fixture_base(), VIEWPORT);
    (doc, styles)
}

fn node(doc: &Document, selector: &str) -> NodeId {
    doc.query_selector(selector)
        .unwrap_or_else(|| panic!("fixture has no {selector}"))
}

fn box_of<'a>(layout: &'a Layout, doc: &Document, selector: &str) -> &'a LayoutBox {
    let id = node(doc, selector);
    layout
        .boxes
        .iter()
        .find(|b| b.node == id)
        .unwrap_or_else(|| panic!("no box for {selector}"))
}

/// ARGB32 native-endian: [B, G, R, A] on little endian, like the frame path
/// in `tests/images.rs`.
fn pixel(frame: &paint::Frame, x: i32, y: i32) -> [u8; 4] {
    let offset = ((y * frame.width + x) * 4) as usize;
    let p = &frame.pixels[offset..offset + 4];
    [p[0], p[1], p[2], p[3]]
}

fn is_red([b, g, r, _]: [u8; 4]) -> bool {
    r > 200 && g < 60 && b < 60
}

fn is_blue([b, g, r, _]: [u8; 4]) -> bool {
    b > 200 && r < 60 && g < 60
}

fn is_lime([b, g, r, _]: [u8; 4]) -> bool {
    g > 200 && r < 60 && b < 60
}

fn is_black([b, g, r, _]: [u8; 4]) -> bool {
    r < 80 && g < 80 && b < 80
}

fn is_white([b, g, r, a]: [u8; 4]) -> bool {
    a == 255 && r > 240 && g > 240 && b > 240
}

// ---------------------------------------------------------------------------
// Computed style: the libcss cascade carries per-side width/style/color
// ---------------------------------------------------------------------------

#[test]
fn computed_border_shorthand_sets_all_sides() {
    let (doc, styles) = load("border-basic.html");
    let style = &styles[node(&doc, ".box")];
    assert_eq!(
        style.border_width,
        [8.0, 8.0, 8.0, 8.0],
        "border: 8px sets every side, got {:?}",
        style.border_width
    );
    assert!(
        style.border_style.iter().all(|s| *s == BorderStyle::Solid),
        "border: solid sets every side, got {:?}",
        style.border_style
    );
    for (i, color) in style.border_color.iter().enumerate() {
        assert!(
            color.r > 0.9 && color.g < 0.1 && color.b < 0.1,
            "side {i} should be red, got {color:?}"
        );
    }
}

#[test]
fn computed_border_longhands_differ_per_side() {
    let (doc, styles) = load("border-sides.html");
    let style = &styles[node(&doc, ".box")];
    assert_eq!(
        style.border_width,
        [4.0, 6.0, 0.0, 10.0],
        "top/right/bottom/left widths, got {:?}",
        style.border_width
    );
    assert_eq!(
        style.border_style,
        [
            BorderStyle::Solid,
            BorderStyle::Solid,
            BorderStyle::None,
            BorderStyle::Solid
        ],
        "the bottom side has no style, got {:?}",
        style.border_style
    );
    assert!(
        style.border_color[3].r > 0.9,
        "the left border is red, got {:?}",
        style.border_color[3]
    );
    assert!(
        style.border_color[0].b > 0.9,
        "the top border is blue, got {:?}",
        style.border_color[0]
    );
}

#[test]
fn computed_border_without_color_uses_currentcolor() {
    let doc = Document::parse(
        "<style>.box { color: blue; border: 2px solid; }</style><body><div class=\"box\">x</div></body>",
    );
    let styles = css_libcss::compute_with_subresources(&doc, &fixture_base(), VIEWPORT);
    let style = &styles[node(&doc, ".box")];
    assert_eq!(style.border_width, [2.0, 2.0, 2.0, 2.0]);
    for (i, color) in style.border_color.iter().enumerate() {
        assert!(
            color.b > 0.9 && color.r < 0.1,
            "side {i} follows the text color, got {color:?}"
        );
    }
}

#[test]
fn computed_border_radius_is_uniform_by_default() {
    let (doc, styles) = load("border-radius.html");
    let plain = &styles[node(&doc, ".box")];
    assert!(
        plain
            .border_radius
            .iter()
            .all(|r| *r == BorderRadius::Px(20.0)),
        "border-radius: 20px sets every corner, got {:?}",
        plain.border_radius
    );
    let ring = &styles[node(&doc, ".ring")];
    assert!(
        ring.border_radius
            .iter()
            .all(|r| *r == BorderRadius::Px(20.0)),
        "got {:?}",
        ring.border_radius
    );
    assert_eq!(
        ring.border_width,
        [6.0, 6.0, 6.0, 6.0],
        "the ring keeps its border, got {:?}",
        ring.border_width
    );
    let circle = &styles[node(&doc, ".circle")];
    assert!(
        circle
            .border_radius
            .iter()
            .all(|r| *r == BorderRadius::Pct(50.0)),
        "percentages stay specified, got {:?}",
        circle.border_radius
    );
}

// ---------------------------------------------------------------------------
// Legacy cascade: `css::Stylesheet` parses the same declarations
// ---------------------------------------------------------------------------

#[test]
fn legacy_cascade_parses_border_shorthand_and_longhands() {
    let parse = |rule: &str| {
        let doc = Document::parse("<div class=\"box\">x</div>");
        let sheet = css::Stylesheet::parse(&format!(".box {{ {rule} }}"));
        css::compute(&doc, &sheet)[node(&doc, ".box")].clone()
    };

    let style = parse("border: 8px solid red");
    assert_eq!(style.border_width, [8.0, 8.0, 8.0, 8.0]);
    assert!(style.border_style.iter().all(|s| *s == BorderStyle::Solid));

    let style = parse("border-width: 1px 2px 3px 4px; border-style: solid");
    assert_eq!(style.border_width, [1.0, 2.0, 3.0, 4.0]);

    let style = parse("border-left: 10px solid red");
    assert_eq!(style.border_width[3], 10.0);
    assert_eq!(style.border_width[0], 0.0);
    assert_eq!(style.border_style[3], BorderStyle::Solid);
    assert_eq!(style.border_style[0], BorderStyle::None);

    let style = parse("border-top-width: 5px; border-top-style: solid; border-top-color: blue");
    assert_eq!(style.border_width[0], 5.0);
    assert!(style.border_color[0].b > 0.9);

    // `none` paints nothing even with a width.
    let style = parse("border: 8px none red");
    assert!(style.border_style.iter().all(|s| *s == BorderStyle::None));
}

#[test]
fn legacy_cascade_parses_border_radius() {
    let parse = |rule: &str| {
        let doc = Document::parse("<div class=\"box\">x</div>");
        let sheet = css::Stylesheet::parse(&format!(".box {{ {rule} }}"));
        css::compute(&doc, &sheet)[node(&doc, ".box")].border_radius
    };

    assert_eq!(
        parse("border-radius: 8px"),
        [BorderRadius::Px(8.0); 4]
    );
    assert_eq!(
        parse("border-radius: 8px 4px"),
        [
            BorderRadius::Px(8.0),
            BorderRadius::Px(4.0),
            BorderRadius::Px(8.0),
            BorderRadius::Px(4.0)
        ]
    );
    assert_eq!(
        parse("border-radius: 1px 2px 3px 4px"),
        [
            BorderRadius::Px(1.0),
            BorderRadius::Px(2.0),
            BorderRadius::Px(3.0),
            BorderRadius::Px(4.0)
        ]
    );
    // Elliptical radii degrade to the horizontal radius.
    assert_eq!(
        parse("border-radius: 8px / 4px"),
        [BorderRadius::Px(8.0); 4]
    );
    assert_eq!(parse("border-radius: 50%"), [BorderRadius::Pct(50.0); 4]);
    // Longhands win over the shorthand.
    let doc = Document::parse("<div class=\"box\">x</div>");
    let sheet = css::Stylesheet::parse(".box { border-radius: 8px; border-top-left-radius: 2px; }");
    let style = css::compute(&doc, &sheet)[node(&doc, ".box")].clone();
    assert_eq!(style.border_radius[0], BorderRadius::Px(2.0));
    assert_eq!(style.border_radius[1], BorderRadius::Px(8.0));
}

// ---------------------------------------------------------------------------
// Paint: the border sits inside the box edge, the content starts inside it
// ---------------------------------------------------------------------------

#[test]
fn border_paints_inside_the_box_edge() {
    let (doc, styles) = load("border-basic.html");
    let images = ImageSet::load(&doc, &fixture_base()).load_background_images(
        &styles,
        &fixture_base(),
    );
    assert!(images.warnings().is_empty(), "{:?}", images.warnings());
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let frame = paint::render(&layout).expect("render");

    let b = box_of(&layout, &doc, ".box");
    // The reported box keeps its width-driven geometry: `width: 200px` is the
    // content width, so the border box is 200 + 2*10 padding = 220 with the
    // 8px ring living inside it instead of growing it.
    assert!(
        (b.w - 220.0).abs() < 0.01,
        "the border sits inside the padding box, got w={}",
        b.w
    );
    let (bx, by) = (b.x as i32, b.y as i32);

    // The 8px ring is red on every edge, sampled below the text zone.
    let bottom = by + b.h as i32;
    assert!(is_red(pixel(&frame, bx + 4, by + 4)), "top-left of the ring");
    assert!(
        is_red(pixel(&frame, bx + 100, by + 4)),
        "top edge of the ring"
    );
    assert!(
        is_red(pixel(&frame, bx + 4, bottom - 4)),
        "bottom-left of the ring"
    );
    assert!(
        is_red(pixel(&frame, bx + b.w as i32 - 4, by + b.h as i32 / 2)),
        "right edge of the ring"
    );
    // Just inside the ring the lime background shows (the padding zone).
    assert!(
        is_lime(pixel(&frame, bx + 100, bottom - 12)),
        "the background starts inside the border, got {:?}",
        pixel(&frame, bx + 100, bottom - 12)
    );
    // The text starts inside border + padding.
    let texts: Vec<&LayoutBox> = layout
        .boxes
        .iter()
        .filter(|t| {
            t.text.is_some() && t.y >= b.y && t.y < b.y + b.h && t.x >= b.x
        })
        .collect();
    assert!(!texts.is_empty(), "the box text is laid out");
    for t in &texts {
        assert!(
            (t.x - (b.x + 18.0)).abs() < 2.0,
            "text starts after the 8px border + 10px padding, got x={}",
            t.x
        );
        assert!(
            t.y >= b.y + 17.0,
            "text starts below the border + padding, got y={}",
            t.y
        );
    }
}

#[test]
fn per_side_borders_differ() {
    let (doc, styles) = load("border-sides.html");
    let images = ImageSet::load(&doc, &fixture_base()).load_background_images(
        &styles,
        &fixture_base(),
    );
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let frame = paint::render(&layout).expect("render");

    let b = box_of(&layout, &doc, ".box");
    let (bx, by) = (b.x as i32, b.y as i32);
    let (bw, bh) = (b.w as i32, b.h as i32);

    // Sampled near the bottom, below the short "Hi" text.
    assert!(
        is_red(pixel(&frame, bx + 3, by + bh - 6)),
        "the 10px left border is red, got {:?}",
        pixel(&frame, bx + 3, by + bh - 6)
    );
    assert!(
        is_blue(pixel(&frame, bx + bw / 2, by + 2)),
        "the 4px top border is blue, got {:?}",
        pixel(&frame, bx + bw / 2, by + 2)
    );
    assert!(
        is_black(pixel(&frame, bx + bw - 3, by + bh - 6)),
        "the 6px right border is black, got {:?}",
        pixel(&frame, bx + bw - 3, by + bh - 6)
    );
    // No bottom border: the page white shows at the box edge.
    assert!(
        is_white(pixel(&frame, bx + bw / 2, by + bh - 1)),
        "no bottom border, got {:?}",
        pixel(&frame, bx + bw / 2, by + bh - 1)
    );
}

// ---------------------------------------------------------------------------
// Paint: rounded corners clip the background fill too
// ---------------------------------------------------------------------------

#[test]
fn border_radius_clips_the_corners() {
    let (doc, styles) = load("border-radius.html");
    let images = ImageSet::load(&doc, &fixture_base()).load_background_images(
        &styles,
        &fixture_base(),
    );
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let frame = paint::render(&layout).expect("render");

    // A square background would paint the corner red; the rounded one leaves
    // the page white there.
    let b = box_of(&layout, &doc, ".box");
    let (bx, by) = (b.x as i32, b.y as i32);
    assert!(
        is_white(pixel(&frame, bx + 1, by + 1)),
        "the corner stays page-white, got {:?}",
        pixel(&frame, bx + 1, by + 1)
    );
    assert!(
        is_white(pixel(&frame, bx + b.w as i32 - 2, by + 1)),
        "top-right corner, got {:?}",
        pixel(&frame, bx + b.w as i32 - 2, by + 1)
    );
    assert!(
        is_red(pixel(&frame, bx + 100, by + 40)),
        "the centre keeps the background, got {:?}",
        pixel(&frame, bx + 100, by + 40)
    );
    assert!(
        is_red(pixel(&frame, bx + 100, by + 1)),
        "the top edge midpoint keeps the background, got {:?}",
        pixel(&frame, bx + 100, by + 1)
    );

    // The bordered ring follows the same clip: blue on the edges, white in
    // the corner outside the curve.
    let r = box_of(&layout, &doc, ".ring");
    let (rx, ry) = (r.x as i32, r.y as i32);
    assert!(
        is_white(pixel(&frame, rx + 1, ry + 1)),
        "the ring corner stays page-white, got {:?}",
        pixel(&frame, rx + 1, ry + 1)
    );
    assert!(
        is_blue(pixel(&frame, rx + 100, ry + 2)),
        "the ring top edge is blue, got {:?}",
        pixel(&frame, rx + 100, ry + 2)
    );
    assert!(
        is_white(pixel(&frame, rx + 100, ry + 40)),
        "the ring interior keeps the white background, got {:?}",
        pixel(&frame, rx + 100, ry + 40)
    );

    // A 50% radius on a square box is a circle: the corners stay white deep
    // into the box, the edge midpoints and centre keep the fill.
    let c = box_of(&layout, &doc, ".circle");
    let (cx, cy) = (c.x as i32, c.y as i32);
    assert!(
        is_white(pixel(&frame, cx + 5, cy + 5)),
        "the circle corner stays page-white, got {:?}",
        pixel(&frame, cx + 5, cy + 5)
    );
    assert!(
        is_red(pixel(&frame, cx + 50, cy + 2)),
        "the circle top midpoint keeps the fill, got {:?}",
        pixel(&frame, cx + 50, cy + 2)
    );
    assert!(
        is_red(pixel(&frame, cx + 50, cy + 50)),
        "the circle centre keeps the fill, got {:?}",
        pixel(&frame, cx + 50, cy + 50)
    );
}

// ---------------------------------------------------------------------------
// Regression pin + the real CLI end to end
// ---------------------------------------------------------------------------

#[test]
fn page_without_borders_paints_unchanged() {
    let (doc, styles) = load("border-none.html");
    let style = &styles[node(&doc, ".box")];
    assert_eq!(style.border_width, [0.0; 4]);
    assert!(style.border_style.iter().all(|s| *s == BorderStyle::None));
    assert_eq!(style.border_radius, [BorderRadius::Px(0.0); 4]);

    let images = ImageSet::load(&doc, &fixture_base()).load_background_images(
        &styles,
        &fixture_base(),
    );
    assert!(images.warnings().is_empty(), "{:?}", images.warnings());
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let frame = paint::render(&layout).expect("render");

    let b = box_of(&layout, &doc, ".box");
    let [bl, g, r, _] = pixel(&frame, b.x as i32 + 60, b.y as i32 + 10);
    assert!(
        g > 200 && r < 60 && bl < 60,
        "the background colour still paints, got rgb({r},{g},{bl})"
    );

    // Border colours appear nowhere: no stray ring painted.
    let mut stray = 0usize;
    for y in 0..frame.height {
        for x in 0..frame.width {
            let px = pixel(&frame, x, y);
            stray += usize::from(is_red(px) || is_blue(px));
        }
    }
    assert_eq!(stray, 0, "no border pixels on a page without borders");
}

#[test]
fn cli_paints_borders() {
    let out = std::env::temp_dir().join(format!(
        "browser-lite-border-cli-{}.png",
        std::process::id()
    ));
    let _ = std::fs::remove_file(&out);
    let fixture_path = format!("{}/test/data/border-basic.html", env!("CARGO_MANIFEST_DIR"));
    let status = std::process::Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .args(["--png", out.to_str().unwrap(), &fixture_path])
        .status()
        .expect("run browser-lite");
    assert!(status.success(), "browser-lite exited with {status}");

    let bytes = std::fs::read(&out).expect("PNG written");
    let img = browser_lite::img::decode(&bytes).expect("output PNG decodes");
    let red = img
        .rgba
        .chunks_exact(4)
        .filter(|px| px[0] > 200 && px[1] < 60 && px[2] < 60)
        .count();
    // An 8px ring around a ~200px box is thousands of red pixels.
    assert!(red >= 1000, "expected the border painted, got {red} red px");
    let _ = std::fs::remove_file(&out);
}

