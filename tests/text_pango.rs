//! Text layout and painting through Pango (S5).
//!
//! These tests use only the public engine API: parse a fixture, compute styles
//! with libcss, lay out, and inspect the produced boxes or the painted frame.
//! Assertions are relative (taller/wider/different ink), never exact glyph
//! widths: this machine only has the JetBrains Mono family installed, so every
//! `font-family` value resolves to the same font through fontconfig.

use std::process::Command;

use browser_lite::css::Style;
use browser_lite::dom::Document;
use browser_lite::layout::{self, Layout, LayoutBox};
use browser_lite::paint;
use browser_lite::css_libcss;

const VIEWPORT: f64 = 1000.0;

fn fixture(name: &str) -> String {
    std::fs::read_to_string(format!(
        "{}/test/data/{name}",
        env!("CARGO_MANIFEST_DIR")
    ))
    .unwrap_or_else(|e| panic!("fixture {name}: {e}"))
}

fn load(name: &str) -> (Document, Vec<Style>) {
    let doc = Document::parse(&fixture(name));
    let sheets = css_libcss::stylesheets(&doc);
    let styles = css_libcss::compute(&doc, &sheets, VIEWPORT);
    (doc, styles)
}

fn block<'a>(layout: &'a Layout, doc: &Document, selector: &str) -> &'a LayoutBox {
    let id = doc
        .query_selector(selector)
        .unwrap_or_else(|| panic!("fixture has no {selector}"));
    layout
        .boxes
        .iter()
        .find(|b| b.node == id)
        .unwrap_or_else(|| panic!("no box for {selector}"))
}

/// Text boxes whose vertical centre falls inside `block`'s box, in document
/// order. Line boxes can be a pixel taller than the block they belong to, so
/// centres are compared instead of raw edges.
fn text_boxes_in<'a>(layout: &'a Layout, block: &LayoutBox) -> Vec<&'a LayoutBox> {
    layout
        .boxes
        .iter()
        .filter(|b| {
            if b.tag != "#text" {
                return false;
            }
            let center = b.y + b.h / 2.0;
            center >= block.y && center < block.y + block.h
        })
        .collect()
}

// ---------------------------------------------------------------------------
// S5: line-height from the cascade changes the block height.
// ---------------------------------------------------------------------------

#[test]
fn line_height_multiplier_makes_blocks_taller() {
    let (doc, styles) = load("text-line-height.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let tight = block(&layout, &doc, ".tight");
    let loose = block(&layout, &doc, ".loose");

    assert!(
        loose.h > tight.h * 1.5,
        "line-height 2 should make the block clearly taller than line-height 1 \
         (tight h={}, loose h={})",
        tight.h,
        loose.h
    );
}

// ---------------------------------------------------------------------------
// S5: wrapping uses real metrics, so nothing is painted past the content box.
// ---------------------------------------------------------------------------

#[test]
fn wrapped_text_stays_inside_the_content_box() {
    let (doc, styles) = load("text-wrap.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);
    let narrow = block(&layout, &doc, "#narrow");

    let lines = text_boxes_in(&layout, narrow);
    assert!(
        lines.len() >= 3,
        "the narrow box should wrap into several lines, got {}",
        lines.len()
    );
    for line in &lines {
        assert!(
            line.x >= narrow.x - 0.5 && line.x + line.w <= narrow.x + narrow.w + 0.5,
            "line {:?} escapes the content box: x={} w={} box=({}..{})",
            line.text,
            line.x,
            line.w,
            narrow.x,
            narrow.x + narrow.w
        );
    }

    // Pixel level: the old character-count wrap painted glyphs past the right
    // edge. Nothing may be inked in the blank strip to the right of the box.
    let frame = paint::render(&layout).expect("render");
    let x0 = (narrow.x + narrow.w) as i32 + 3;
    let x1 = (x0 + 80).min(frame.width);
    let y0 = narrow.y.max(0.0) as i32;
    let y1 = ((narrow.y + narrow.h).ceil() as i32).min(frame.height);
    let mut ink = 0;
    for y in y0..y1 {
        for x in x0..x1 {
            if is_ink(&frame, x, y) {
                ink += 1;
            }
        }
    }
    assert_eq!(
        ink, 0,
        "{ink} inked pixels right of the content box: text is being clipped/overflowing"
    );
}

// ---------------------------------------------------------------------------
// S5: text-align from the cascade (left/center/right/justify).
// ---------------------------------------------------------------------------

#[test]
fn text_align_moves_lines_inside_the_block() {
    let (doc, styles) = load("text-align.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);
    let wide = block(&layout, &doc, "#wide");
    let right_edge = wide.x + wide.w;

    let left_lines = text_boxes_in(&layout, block(&layout, &doc, "#l"));
    let center_lines = text_boxes_in(&layout, block(&layout, &doc, "#c"));
    let right_lines = text_boxes_in(&layout, block(&layout, &doc, "#r"));
    let left = left_lines[0];
    let center = center_lines[0];
    let right = right_lines[0];

    assert!(
        center.x > left.x + 10.0,
        "centred line should start right of the left-aligned one (left x={}, center x={})",
        left.x,
        center.x
    );
    assert!(
        right.x + right.w > right_edge - 1.0,
        "right-aligned line should end at the content edge (end={}, edge={})",
        right.x + right.w,
        right_edge
    );
    assert!(
        left.x + left.w < right_edge - 10.0,
        "the left-aligned control line must not fill the box"
    );
}

#[test]
fn justify_stretches_every_line_but_the_last() {
    let (doc, styles) = load("text-align.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);
    let wide = block(&layout, &doc, "#wide");

    let justified = text_boxes_in(&layout, block(&layout, &doc, "#j"));
    let ragged = text_boxes_in(&layout, block(&layout, &doc, "#jl"));
    assert!(justified.len() >= 2, "justified paragraph should wrap");
    assert_eq!(
        justified.len(),
        ragged.len(),
        "justification stretches lines, it must not change where they break"
    );

    // Every line but the last fills the content box, and at least one of them
    // is visibly wider than the same line left-aligned (Pango stretches the
    // word gaps; a line that already fits keeps its width).
    let mut stretched = 0;
    for (line, control) in justified[..justified.len() - 1]
        .iter()
        .zip(&ragged[..ragged.len() - 1])
    {
        assert!(
            line.w >= wide.w * 0.95,
            "justified line {:?} is {}px wide, content box is {}px",
            line.text,
            line.w,
            wide.w
        );
        if line.w > control.w + 5.0 {
            stretched += 1;
        }
    }
    assert!(
        stretched > 0,
        "justification should stretch at least one line beyond its ragged width"
    );
    assert!(
        ragged
            .iter()
            .any(|line| line.w < wide.w * 0.95),
        "left-aligned control should keep at least one line narrower than the box"
    );
}

// ---------------------------------------------------------------------------
// S5: letter-spacing from the cascade widens the line.
// ---------------------------------------------------------------------------

#[test]
fn letter_spacing_widens_the_line() {
    let (doc, styles) = load("text-letter-spacing.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let plain_lines = text_boxes_in(&layout, block(&layout, &doc, "#plain"));
    let spaced_lines = text_boxes_in(&layout, block(&layout, &doc, "#spaced"));
    let plain = plain_lines[0];
    let spaced = spaced_lines[0];

    assert!(
        spaced.w > plain.w + 50.0,
        "letter-spacing: 5px should widen the line by roughly 5px per character \
         (plain={}, spaced={})",
        plain.w,
        spaced.w
    );
}

// ---------------------------------------------------------------------------
// S5: font-family fallback, weight and italic reach the painted glyphs.
// ---------------------------------------------------------------------------

#[test]
fn italic_paints_different_glyphs_than_normal() {
    let (doc, styles) = load("text-fonts.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);
    let frame = paint::render(&layout).expect("render");

    let normal = ink_mask(&frame, block(&layout, &doc, "#normal"));
    let italic = ink_mask(&frame, block(&layout, &doc, "#italic"));

    assert!(normal.iter().any(|&p| p), "normal text should paint ink");
    assert!(italic.iter().any(|&p| p), "italic text should paint ink");
    let normal_ink = normal.iter().filter(|&&p| p).count();
    let italic_ink = italic.iter().filter(|&&p| p).count();
    assert!(
        normal != italic,
        "italic must not paint the same pixels as normal text \
         (normal ink={normal_ink}, italic ink={italic_ink})"
    );
}

#[test]
fn bold_paints_more_ink_than_normal() {
    let (doc, styles) = load("text-fonts.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);
    let frame = paint::render(&layout).expect("render");

    let normal = ink_mask(&frame, block(&layout, &doc, "#normal"))
        .iter()
        .filter(|&&p| p)
        .count();
    let bold = ink_mask(&frame, block(&layout, &doc, "#bold"))
        .iter()
        .filter(|&&p| p)
        .count();

    assert!(
        bold > normal,
        "bold should paint thicker glyphs (normal={normal}, bold={bold})"
    );
}

#[test]
fn missing_font_family_falls_back_and_still_paints() {
    let (doc, styles) = load("text-fonts.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);
    let frame = paint::render(&layout).expect("render");

    for selector in ["#normal", "#serif"] {
        let mask = ink_mask(&frame, block(&layout, &doc, selector));
        assert!(
            mask.iter().any(|&p| p),
            "{selector} should still paint glyphs through the font stack"
        );
    }
}

// ---------------------------------------------------------------------------
// PRESERVE: behaviour that already existed and must keep working.
// ---------------------------------------------------------------------------

#[test]
fn blocks_still_stack_in_document_order() {
    let (doc, styles) = load("text-align.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let order = ["#l", "#c", "#r", "#j", "#jl"];
    let mut previous = f64::NEG_INFINITY;
    for selector in order {
        let b = block(&layout, &doc, selector);
        assert_eq!(b.tag, "p", "{selector} keeps its block box label");
        assert!(
            b.y > previous,
            "{selector} should stack below the previous block (y={}, previous={previous})",
            b.y
        );
        previous = b.y;
    }
    assert!(
        layout.boxes.iter().any(|b| b.tag == "#text"),
        "text boxes keep their #text label"
    );
}

#[test]
fn backgrounds_still_paint_behind_text() {
    let doc = Document::parse(
        "<body style=\"margin:0\"><div style=\"width:100px;height:50px;background:red\"></div></body>",
    );
    let sheets = css_libcss::stylesheets(&doc);
    let styles = css_libcss::compute(&doc, &sheets, VIEWPORT);
    let layout = layout::layout(&doc, &styles, VIEWPORT);
    let frame = paint::render(&layout).expect("render");

    let [b, g, r, _] = pixel(&frame, 50, 25);
    assert!(
        r > 200 && g < 60 && b < 60,
        "background should be red, got rgb({r},{g},{b})"
    );
}

#[test]
fn png_output_keeps_the_layout_size() {
    let (doc, styles) = load("text-line-height.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let path = std::env::temp_dir().join(format!(
        "browser-lite-pango-{}.png",
        std::process::id()
    ));
    paint::render_to_png(&layout, &path).expect("render_to_png");
    let mut file = std::fs::File::open(&path).expect("png exists");
    let png = cairo::ImageSurface::create_from_png(&mut file).expect("valid png");
    let _ = std::fs::remove_file(&path);

    assert_eq!(png.width(), layout.width as i32);
    assert_eq!(png.height(), layout.height.ceil() as i32);
}

#[test]
fn png_cli_writes_a_valid_image() {
    let path = std::env::temp_dir().join(format!(
        "browser-lite-pango-cli-{}.png",
        std::process::id()
    ));
    let output = Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .arg("--png")
        .arg(&path)
        .arg(format!("{}/test/data/hello.html", env!("CARGO_MANIFEST_DIR")))
        .output()
        .expect("run browser-lite --png");

    assert!(
        output.status.success(),
        "--png exit: {:?} stderr: {}",
        output.status.code(),
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(
        stdout.starts_with("wrote ") && stdout.contains("(1000x"),
        "--png should report the written path and page size, got {stdout:?}"
    );

    let mut file = std::fs::File::open(&path).expect("png file exists");
    let png = cairo::ImageSurface::create_from_png(&mut file).expect("valid png");
    let _ = std::fs::remove_file(&path);
    assert_eq!(png.width(), VIEWPORT as i32);
    assert!(png.height() > 0);
}

#[test]
fn dump_keeps_its_format_and_exit_code() {
    let output = Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .arg("--dump")
        .arg(format!("{}/test/data/hello.html", env!("CARGO_MANIFEST_DIR")))
        .output()
        .expect("run browser-lite --dump");

    assert!(
        output.status.success(),
        "--dump exit: {:?} stderr: {}",
        output.status.code(),
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.starts_with("DOM\n"), "dump starts with the DOM tree");
    let layout_line = stdout
        .lines()
        .find(|l| l.starts_with("LAYOUT "))
        .expect("LAYOUT header");
    let (w, h) = layout_line
        .trim_start_matches("LAYOUT ")
        .split_once('x')
        .expect("LAYOUT <w>x<h>");
    w.parse::<f64>().expect("layout width");
    h.parse::<f64>().expect("layout height");

    let box_lines: Vec<&str> = stdout
        .lines()
        .filter(|l| l.starts_with("  ["))
        .collect();
    assert!(!box_lines.is_empty(), "dump lists layout boxes");
    for line in box_lines {
        let rest = line
            .strip_prefix("  [")
            .and_then(|l| l.split_once(']'))
            .map(|(rect, _)| rect)
            .expect("box line keeps its [x y w h] rect");
        let fields: Vec<&str> = rest.split_whitespace().collect();
        assert_eq!(fields.len(), 4, "box rect keeps four numbers: {line:?}");
        for field in fields {
            field.parse::<f64>().expect("numeric box coordinate");
        }
    }
    assert!(
        stdout.contains("\ntitle: hello\n"),
        "dump still ends with the title line"
    );
}

// ---------------------------------------------------------------------------
// Pixel helpers (ARGB32 native-endian: [B, G, R, A] on little endian).
// ---------------------------------------------------------------------------

fn pixel(frame: &paint::Frame, x: i32, y: i32) -> [u8; 4] {
    let offset = ((y * frame.width + x) * 4) as usize;
    let p = &frame.pixels[offset..offset + 4];
    [p[0], p[1], p[2], p[3]]
}

fn is_ink(frame: &paint::Frame, x: i32, y: i32) -> bool {
    let [b, g, r, _] = pixel(frame, x, y);
    r < 128 && g < 128 && b < 128
}

/// Ink presence bitmap of a block's box, normalised to the box origin.
fn ink_mask(frame: &paint::Frame, block: &LayoutBox) -> Vec<bool> {
    let x0 = block.x.max(0.0) as i32;
    let y0 = block.y.max(0.0) as i32;
    let x1 = ((block.x + block.w) as i32).min(frame.width);
    let y1 = ((block.y + block.h).ceil() as i32).min(frame.height);
    let mut mask = Vec::new();
    for y in y0..y1 {
        for x in x0..x1 {
            mask.push(is_ink(frame, x, y));
        }
    }
    mask
}
