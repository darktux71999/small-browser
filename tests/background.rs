//! S10 (T9) — `background-image`: the `url(...)` is parsed into the style,
//! fetched and decoded through the existing image machinery, and painted as
//! the box background — behind the content, clipped to the box, tiled at
//! natural size from the box's top-left corner (the CSS `repeat` default).
//!
//! The tests drive the public engine API only: parse a fixture, compute its
//! styles, lay out with subresources, and inspect the boxes or the painted
//! frame/PNG pixels. The tiling fixture (`background-tile.png`, a 4x4
//! checkerboard of 2x2 red/blue quadrants) makes natural-size repeat
//! observable, which a solid-coloured image could not.

use browser_lite::css::{self, Style};
use browser_lite::css_libcss;
use browser_lite::dom::{Document, NodeId};
use browser_lite::img::ImageSet;
use browser_lite::layout::{self, Layout, LayoutBox};
use browser_lite::paint;

const VIEWPORT: f64 = 1000.0;

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

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

/// The 4x4 tile painted at natural size from the box origin is red exactly at
/// box offsets where `(x % 4 < 2) == (y % 4 < 2)` (see `background-tile.png`).
fn tile_is_red(dx: i32, dy: i32) -> bool {
    (dx.rem_euclid(4) < 2) == (dy.rem_euclid(4) < 2)
}

// ---------------------------------------------------------------------------
// url() parsing: the libcss cascade and the legacy cascade
// ---------------------------------------------------------------------------

#[test]
fn computed_style_carries_the_background_url() {
    let (doc, styles) = load("background-image.html");
    // libcss resolves the url() against the sheet's own URL at parse time
    // (the `resolve` callback), so the computed URL is absolute — here the
    // fixture's path on disk. Resolving it again against the base is
    // idempotent for absolute URLs.
    let url = styles[node(&doc, ".hero")]
        .background_image
        .clone()
        .expect("the hero declares background-image");
    assert!(
        url.ends_with("test/data/background-tile.png"),
        "the url is resolved against the sheet base, got {url:?}"
    );
    assert_eq!(
        styles[node(&doc, "p")].background_image, None,
        "elements without the declaration keep None"
    );

    let (doc, styles) = load("background-alpha.html");
    let url = styles[node(&doc, ".hero")]
        .background_image
        .clone()
        .expect("the hero declares background-image");
    assert!(
        url.ends_with("test/data/img-alpha.png"),
        "got {url:?}"
    );
}

#[test]
fn legacy_cascade_parses_background_urls() {
    let parse = |rule: &str| {
        let doc = Document::parse("<div class=\"box\">x</div>");
        let sheet = css::Stylesheet::parse(&format!(".box {{ {rule} }}"));
        let styles = css::compute(&doc, &sheet);
        styles[node(&doc, ".box")].clone()
    };

    let style = parse("background-image: url(tile.png)");
    assert_eq!(style.background_image.as_deref(), Some("tile.png"));

    let style = parse("background: url(other.png)");
    assert_eq!(
        style.background_image.as_deref(),
        Some("other.png"),
        "the shorthand carries its url() too"
    );

    let style = parse("background-image: url(\"quoted.png\")");
    assert_eq!(style.background_image.as_deref(), Some("quoted.png"));

    let style = parse("background-image: none; background: #00ff00");
    assert_eq!(style.background_image, None, "`none` clears the image");
    assert!(style.background.is_some(), "the colour still applies");

    let style = parse("background: #00ff00");
    assert_eq!(
        style.background_image, None,
        "a colour-only background declares no image"
    );
}

// ---------------------------------------------------------------------------
// Paint: tiled behind the content, clipped to the box
// ---------------------------------------------------------------------------

#[test]
fn background_image_tiles_at_natural_size_from_the_box_origin() {
    let (doc, styles) = load("background-image.html");
    let images = ImageSet::load(&doc, &fixture_base()).load_background_images(
        &styles,
        &fixture_base(),
    );
    assert!(images.warnings().is_empty(), "{:?}", images.warnings());
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let frame = paint::render(&layout).expect("render");

    let b = box_of(&layout, &doc, ".hero");
    let bx = b.x as i32;
    let by = b.y as i32;

    // The tile is 4x4, the box 120x100: sample rows below the text zone.
    let expect = [
        // Natural size: the quadrant flips inside one 4px tile.
        (1, 51),
        (3, 51),
        // Horizontal repeat: same colours 4px further right.
        (5, 51),
        (7, 51),
        // The second tile row flips the quadrants again.
        (1, 53),
        (3, 53),
        // Bottom of the box, behind/below the content.
        (11, 91),
    ];
    for (dx, dy) in expect {
        let px = pixel(&frame, bx + dx, by + dy);
        if tile_is_red(dx, dy) {
            assert!(is_red(px), "box offset ({dx},{dy}) should be red, got {px:?}");
        } else {
            assert!(is_blue(px), "box offset ({dx},{dy}) should be blue, got {px:?}");
        }
    }

    // Clipped to the box: the pixels beside and below it stay the page white.
    assert_eq!(
        pixel(&frame, bx - 5, by + 30),
        [255, 255, 255, 255],
        "nothing left of the box"
    );
    assert_eq!(
        pixel(&frame, bx + b.w as i32 + 5, by + 30),
        [255, 255, 255, 255],
        "nothing right of the box"
    );
    assert_eq!(
        pixel(&frame, bx + 10, by + b.h as i32 + 5),
        [255, 255, 255, 255],
        "nothing below the box"
    );

    // The text is painted over the background: dark glyph pixels inside the
    // box, where the tile alone would be red or blue.
    let dark = (by + 2..by + 45)
        .flat_map(|y| (bx..bx + b.w as i32).map(move |x| (x, y)))
        .filter(|&(x, y)| {
            let [b_, g, r, _] = pixel(&frame, x, y);
            r < 80 && g < 80 && b_ < 80
        })
        .count();
    assert!(
        dark >= 20,
        "the hero text should paint over the tile, got {dark} dark pixels"
    );
}

#[test]
fn background_image_alpha_composites_over_the_page() {
    let (doc, styles) = load("background-alpha.html");
    let images = ImageSet::load(&doc, &fixture_base()).load_background_images(
        &styles,
        &fixture_base(),
    );
    assert!(images.warnings().is_empty(), "{:?}", images.warnings());
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let frame = paint::render(&layout).expect("render");

    let b = box_of(&layout, &doc, ".hero");
    // img-alpha.png is 50% red; over the white page it lands mid-way, like
    // the `<img>` alpha path in tests/images.rs.
    let [b_, g, r, _] = pixel(&frame, b.x as i32 + 70, b.y as i32 + 50);
    assert!(
        r > 240 && (110..=150).contains(&g) && (110..=150).contains(&b_),
        "half-transparent red over white, got rgb({r},{g},{b_})"
    );
}

// ---------------------------------------------------------------------------
// Regression pin: a page without background-image paints as before
// ---------------------------------------------------------------------------

#[test]
fn page_without_background_image_paints_unchanged() {
    let (doc, styles) = load("background-none.html");
    assert_eq!(
        styles[node(&doc, ".box")].background_image, None,
        "no url() was declared"
    );

    // No background URL means no extra fetch, no warning: the loader is a
    // no-op for this page.
    let images = ImageSet::load(&doc, &fixture_base()).load_background_images(
        &styles,
        &fixture_base(),
    );
    assert_eq!(images.fetched(), 0, "nothing new to fetch");
    assert!(images.warnings().is_empty(), "{:?}", images.warnings());

    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let frame = paint::render(&layout).expect("render");

    let b = box_of(&layout, &doc, ".box");
    let [b_, g, r, _] = pixel(&frame, b.x as i32 + 90, b.y as i32 + 25);
    assert!(
        g > 200 && r < 60 && b_ < 60,
        "the background colour still paints, got rgb({r},{g},{b_})"
    );

    // The tile colours appear nowhere: no stray image painted.
    let mut red = 0usize;
    let mut blue = 0usize;
    for y in 0..frame.height {
        for x in 0..frame.width {
            let px = pixel(&frame, x, y);
            red += usize::from(is_red(px));
            blue += usize::from(is_blue(px));
        }
    }
    assert_eq!((red, blue), (0, 0), "no image pixels on a page without one");
}

// ---------------------------------------------------------------------------
// The real CLI paints the background image end to end
// ---------------------------------------------------------------------------

#[test]
fn cli_paints_background_image() {
    let out = std::env::temp_dir().join(format!(
        "browser-lite-bg-cli-{}.png",
        std::process::id()
    ));
    let _ = std::fs::remove_file(&out);
    let fixture_path = format!("{}/test/data/background-image.html", env!("CARGO_MANIFEST_DIR"));
    let status = std::process::Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .args(["--png", out.to_str().unwrap(), &fixture_path])
        .status()
        .expect("run browser-lite");
    assert!(status.success(), "browser-lite exited with {status}");

    let bytes = std::fs::read(&out).expect("PNG written");
    let img = browser_lite::img::decode(&bytes).expect("output PNG decodes");
    let mut red = 0usize;
    let mut blue = 0usize;
    for px in img.rgba.chunks_exact(4) {
        red += usize::from(is_red([px[0], px[1], px[2], px[3]]));
        blue += usize::from(is_blue([px[0], px[1], px[2], px[3]]));
    }
    // A 120x100 box of 4x4 tiles is thousands of pixels of each colour.
    assert!(red >= 500, "expected the tile painted, got {red} red px");
    assert!(blue >= 500, "expected the tile painted, got {blue} blue px");
    let _ = std::fs::remove_file(&out);
}
