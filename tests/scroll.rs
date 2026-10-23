//! Scroll model, keyboard mapping and viewport painting (S9, task T8).
//!
//! The scroll state is a pure unit: offsets, clamping and step sizes are
//! asserted as numbers through the public API, never through the SDL loop
//! (CI has no display or keyboard). The painting tests prove that the
//! interactive path clips to the fixed 1000x760 viewport and translates by the
//! scroll offset, while `render`/`render_to_png` and the `--png`/`--dump`
//! commands keep painting and measuring the full document untouched.
//!
//! SDL keycodes are ABI stable, so the literals below pin the real SDL3
//! values (headers `/usr/include/SDL3/SDL_keycode.h`, SDL 3.4.18): letter
//! keycodes are ASCII (`'g'` = 0x67), arrow/page keys are
//! `SDL_SCANCODE_TO_KEYCODE(scancode)`, and `SDL_KMOD_SHIFT` is 0x3.

use std::process::Command;

use browser_lite::css_libcss;
use browser_lite::dom::Document;
use browser_lite::img;
use browser_lite::layout::{self, Layout};
use browser_lite::paint::{self, Frame};
use browser_lite::shell::{self, ScrollAction, ScrollState};

const FIXTURE_WIDTH: f64 = 1000.0;
/// `test/data/long.html`: 8 sections of 400px laid out in one column.
const FIXTURE_HEIGHT: f64 = 3200.0;

// SDL3 keycodes (SDL 3.4.18).
const SDLK_UP: u32 = 0x4000_0052;
const SDLK_DOWN: u32 = 0x4000_0051;
const SDLK_PAGEUP: u32 = 0x4000_004B;
const SDLK_PAGEDOWN: u32 = 0x4000_004E;
const SDLK_HOME: u32 = 0x4000_004A;
const SDLK_END: u32 = 0x4000_004D;
const KEY_J: u32 = 0x0000_006A;
const KEY_K: u32 = 0x0000_006B;
const KEY_G_LOWER: u32 = 0x0000_0067;
const KEY_G_UPPER: u32 = 0x0000_0047;
const KEY_Q: u32 = 0x0000_0071;
const SDL_KMOD_SHIFT: u16 = 0x0003;

// ---------------------------------------------------------------------------
// The scroll model (pure unit).
// ---------------------------------------------------------------------------

#[test]
fn the_viewport_is_a_fixed_practical_size() {
    // The window must not open at document height; both dimensions are fixed.
    assert_eq!(shell::VIEWPORT_WIDTH, 1000);
    assert_eq!(shell::VIEWPORT_HEIGHT, 760);

    let scroll = ScrollState::new(FIXTURE_HEIGHT, f64::from(shell::VIEWPORT_HEIGHT));
    assert_eq!(scroll.offset(), 0.0, "a fresh state starts at the top");
    assert_eq!(
        scroll.max_offset(),
        2440.0,
        "max offset is document_height - viewport_height"
    );
}

#[test]
fn the_offset_clamps_between_zero_and_the_document_end() {
    let mut scroll = ScrollState::new(FIXTURE_HEIGHT, 760.0);
    assert_eq!(scroll.max_offset(), 2440.0);

    scroll.set_offset(10_000.0);
    assert_eq!(scroll.offset(), 2440.0, "past the end clamps to the end");

    scroll.set_offset(-50.0);
    assert_eq!(scroll.offset(), 0.0, "before the top clamps to the top");

    scroll.set_offset(1_234.5);
    assert_eq!(scroll.offset(), 1_234.5, "in range keeps the value");
}

#[test]
fn a_document_shorter_than_the_viewport_never_scrolls() {
    for document_height in [500.0, 760.0] {
        let mut scroll = ScrollState::new(document_height, 760.0);
        assert_eq!(scroll.max_offset(), 0.0, "doc {document_height}");
        for action in [
            ScrollAction::LineDown,
            ScrollAction::PageDown,
            ScrollAction::Bottom,
        ] {
            scroll.apply(action);
            assert_eq!(
                scroll.offset(),
                0.0,
                "{action:?} must not scroll a {document_height}px document"
            );
        }
    }
}

#[test]
fn line_and_page_steps_have_the_pinned_sizes() {
    let mut scroll = ScrollState::new(FIXTURE_HEIGHT, 760.0);

    scroll.apply(ScrollAction::LineDown);
    scroll.apply(ScrollAction::LineDown);
    scroll.apply(ScrollAction::LineDown);
    assert_eq!(scroll.offset(), 120.0, "a line step is 40px");

    scroll.apply(ScrollAction::LineUp);
    assert_eq!(scroll.offset(), 80.0, "LineUp is the mirrored step");

    scroll.apply(ScrollAction::PageDown);
    assert_eq!(
        scroll.offset(),
        780.0,
        "a page step is the viewport (760) minus a 60px overlap"
    );
    scroll.apply(ScrollAction::PageUp);
    assert_eq!(scroll.offset(), 80.0, "PageUp is the mirrored step");
}

#[test]
fn repeated_paging_clamps_at_both_ends() {
    let mut scroll = ScrollState::new(FIXTURE_HEIGHT, 760.0);
    for _ in 0..100 {
        scroll.apply(ScrollAction::PageDown);
    }
    assert_eq!(scroll.offset(), 2440.0, "paging down stops at the end");

    for _ in 0..100 {
        scroll.apply(ScrollAction::PageUp);
    }
    assert_eq!(scroll.offset(), 0.0, "paging up stops at the top");

    scroll.set_offset(2_400.0);
    scroll.apply(ScrollAction::PageDown);
    assert_eq!(
        scroll.offset(),
        2440.0,
        "the last page lands exactly on the end, never past it"
    );
}

/// Home/End and vi `g` (top) / `G` (bottom, sent as shifted `g`).
#[test]
fn home_end_and_vi_g_g_jump_to_the_ends() {
    let mut scroll = ScrollState::new(FIXTURE_HEIGHT, 760.0);
    scroll.set_offset(1_000.0);

    scroll.apply(ScrollAction::Top);
    assert_eq!(scroll.offset(), 0.0, "Home / g jump to the top");

    scroll.apply(ScrollAction::Bottom);
    assert_eq!(scroll.offset(), 2440.0, "End / G jump to the bottom");

    scroll.apply(ScrollAction::Top);
    assert_eq!(scroll.offset(), 0.0, "and back again");
}

// ---------------------------------------------------------------------------
// Keyboard mapping: SDL3 keycodes -> scroll actions.
// ---------------------------------------------------------------------------

#[test]
fn scroll_keys_map_from_sdl_keycodes() {
    let cases: &[(u32, u16, Option<ScrollAction>)] = &[
        (SDLK_UP, 0, Some(ScrollAction::LineUp)),
        (SDLK_DOWN, 0, Some(ScrollAction::LineDown)),
        (SDLK_PAGEUP, 0, Some(ScrollAction::PageUp)),
        (SDLK_PAGEDOWN, 0, Some(ScrollAction::PageDown)),
        (SDLK_HOME, 0, Some(ScrollAction::Top)),
        (SDLK_END, 0, Some(ScrollAction::Bottom)),
        (KEY_J, 0, Some(ScrollAction::LineDown)),
        (KEY_K, 0, Some(ScrollAction::LineUp)),
        (KEY_J, SDL_KMOD_SHIFT, Some(ScrollAction::LineDown)),
        (KEY_K, SDL_KMOD_SHIFT, Some(ScrollAction::LineUp)),
        (KEY_G_LOWER, 0, Some(ScrollAction::Top)),
        // Shift+g is `G` in vi: jump to the bottom, not to the top.
        (KEY_G_LOWER, SDL_KMOD_SHIFT, Some(ScrollAction::Bottom)),
        (KEY_G_UPPER, 0, Some(ScrollAction::Bottom)),
        // Keys with no vertical meaning stay unmapped.
        (KEY_Q, 0, None),
        (0x0000_0068, 0, None), // 'h': no horizontal scrolling yet
        (0x0000_006C, 0, None), // 'l'
        (0x4000_0029, 0, None), // Escape
    ];
    for &(key, modifiers, expected) in cases {
        assert_eq!(
            shell::scroll_action_from_key(key, modifiers),
            expected,
            "key {key:#x} mod {modifiers:#x}"
        );
    }
}

#[test]
fn a_mapped_key_actually_moves_the_scroll_state() {
    // End to end: the keycode the shell parses drives the pure scroll model.
    let mut scroll = ScrollState::new(FIXTURE_HEIGHT, 760.0);
    let action = shell::scroll_action_from_key(KEY_J, 0).expect("'j' scrolls");
    scroll.apply(action);
    assert_eq!(scroll.offset(), 40.0);

    let action = shell::scroll_action_from_key(KEY_G_LOWER, 0).expect("'g' scrolls");
    scroll.apply(action);
    assert_eq!(scroll.offset(), 0.0);
}

// ---------------------------------------------------------------------------
// Painting: fixed viewport, clipped and translated by the scroll offset.
// ---------------------------------------------------------------------------

fn fixture(name: &str) -> String {
    std::fs::read_to_string(format!("{}/test/data/{name}", env!("CARGO_MANIFEST_DIR")))
        .unwrap_or_else(|e| panic!("fixture {name}: {e}"))
}

fn long_layout() -> Layout {
    let doc = Document::parse(&fixture("long.html"));
    let sheets = css_libcss::stylesheets(&doc);
    let styles = css_libcss::compute(&doc, &sheets, FIXTURE_WIDTH);
    layout::layout(&doc, &styles, FIXTURE_WIDTH)
}

/// ARGB32 native-endian: [B, G, R, A] on little endian.
fn pixel(frame: &Frame, x: i32, y: i32) -> [u8; 4] {
    let offset = ((y * frame.width + x) * 4) as usize;
    let p = &frame.pixels[offset..offset + 4];
    [p[0], p[1], p[2], p[3]]
}

/// Section colours of `test/data/long.html`, sampled away from the centred
/// label: index i is `hsl(40i, 60%, 45%)`.
fn assert_section(frame: &Frame, x: i32, y: i32, section: usize, where_: &str) {
    let [b, g, r, _] = pixel(frame, x, y);
    let (min_r, max_r, min_g, max_g, min_b, max_b) = match section {
        0 => (150, 255, 0, 90, 0, 90),    // red
        1 => (150, 255, 100, 190, 0, 90), // orange
        6 => (0, 90, 0, 90, 150, 255),    // blue
        7 => (110, 175, 0, 90, 150, 255), // purple
        other => panic!("test has no colour expectation for section {other}"),
    };
    let ok = (min_r..=max_r).contains(&r)
        && (min_g..=max_g).contains(&g)
        && (min_b..=max_b).contains(&b);
    assert!(
        ok,
        "{where_}: expected section {section} colour, got rgb({r},{g},{b})"
    );
}

#[test]
fn the_layout_still_measures_the_whole_document() {
    let layout = long_layout();
    assert_eq!(layout.width as i32, shell::VIEWPORT_WIDTH);
    assert_eq!(
        layout.height, FIXTURE_HEIGHT,
        "layout measures the full document, never a truncated one"
    );
}

#[test]
fn the_paint_render_still_produces_the_full_document_frame() {
    let layout = long_layout();
    let frame = paint::render(&layout).expect("render");
    assert_eq!(frame.width, 1000, "full render width is the document width");
    assert_eq!(
        frame.height, 3200,
        "full render height is the document height, not the viewport"
    );
    assert_section(&frame, 50, 20, 0, "full render top");
    assert_section(&frame, 50, 3140, 7, "full render bottom");
}

#[test]
fn the_viewport_frame_is_clipped_to_the_fixed_size() {
    let layout = long_layout();
    let top = paint::render_viewport(&layout, 0.0).expect("render_viewport");
    assert_eq!(top.width, 1000, "viewport width is fixed");
    assert_eq!(
        top.height, 760,
        "viewport height is fixed, independent of the 3200px document"
    );
    assert_section(&top, 50, 20, 0, "viewport top");
    assert_section(&top, 50, 420, 1, "viewport at y=420");
}

#[test]
fn the_unscrolled_viewport_matches_the_top_of_the_full_render() {
    // Clipping and translating must be the ONLY difference: at offset 0 the
    // viewport rows are byte-identical to the first 760 rows of the full render.
    let layout = long_layout();
    let full = paint::render(&layout).expect("render");
    let top = paint::render_viewport(&layout, 0.0).expect("render_viewport");

    let row_bytes = (top.width * 4) as usize;
    for row in 0..top.height as usize {
        let view_row = &top.pixels[row * row_bytes..(row + 1) * row_bytes];
        let full_row = &full.pixels[row * row_bytes..(row + 1) * row_bytes];
        assert_eq!(
            view_row, full_row,
            "viewport row {row} differs from the full render"
        );
    }
}

#[test]
fn scrolling_translates_the_document_window() {
    let layout = long_layout();
    let full = paint::render(&layout).expect("render");
    let offset = ScrollState::new(layout.height, 760.0).max_offset();
    assert_eq!(offset, 2440.0);

    let view = paint::render_viewport(&layout, offset).expect("render_viewport");
    assert_eq!((view.width, view.height), (1000, 760));

    // What the window shows at the bottom of the scroll is the bottom of the
    // document: section 6 (blue) at the top of the view, section 7 (purple)
    // near its end.
    assert_section(&view, 50, 20, 6, "scrolled view top");
    assert_section(&view, 50, 700, 7, "scrolled view bottom");

    // Same pixels at the same document coordinates as the full render: the
    // view is the document shifted by exactly `offset`, nothing else.
    assert_eq!(
        pixel(&view, 50, 20),
        pixel(&full, 50, offset as i32 + 20),
        "scrolled view must equal the full render at y+offset"
    );
    assert_eq!(
        pixel(&view, 50, 700),
        pixel(&full, 50, offset as i32 + 700),
        "scrolled view must equal the full render at y+offset"
    );
}

#[test]
fn painting_below_the_document_keeps_the_background() {
    // The offset is clamped by the scroll model, not by the painter: an
    // out-of-range offset must still paint only background, never stray boxes
    // from elsewhere in the document.
    let layout = long_layout();
    let below = paint::render_viewport(&layout, 1_000_000.0).expect("render_viewport");
    for &(x, y) in &[(50, 20), (50, 400), (50, 700)] {
        let [b, g, r, _] = pixel(&below, x, y);
        assert!(
            r > 240 && g > 240 && b > 240,
            "pixel ({x},{y}) past the document should be white, got rgb({r},{g},{b})"
        );
    }
}

// ---------------------------------------------------------------------------
// Regressions: --png and --dump stay full-document measurement tools.
// ---------------------------------------------------------------------------

#[test]
fn png_mode_still_renders_the_full_1000x3200_document() {
    let path = std::env::temp_dir().join(format!(
        "browser-lite-scroll-long-{}.png",
        std::process::id()
    ));
    let _ = std::fs::remove_file(&path);
    let fixture = format!("{}/test/data/long.html", env!("CARGO_MANIFEST_DIR"));

    let output = Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .arg("--png")
        .arg(&path)
        .arg(&fixture)
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
        stdout.contains("(1000x3200)"),
        "--png must report the full document size, got {stdout:?}"
    );

    let bytes = std::fs::read(&path).expect("png written");
    let image = img::decode(&bytes).expect("output png decodes");
    let _ = std::fs::remove_file(&path);
    assert_eq!(
        (image.width, image.height),
        (1000, 3200),
        "--png writes the whole document, not a viewport crop"
    );

    // The bottom of the document is still painted in the PNG.
    let at = |x: u32, y: u32| -> [u8; 4] {
        let i = ((y * image.width + x) * 4) as usize;
        [
            image.rgba[i],
            image.rgba[i + 1],
            image.rgba[i + 2],
            image.rgba[i + 3],
        ]
    };
    let [r, g, b, _] = at(50, 3140);
    assert!(
        b > 150 && (110..=175).contains(&r) && g < 90,
        "the last section must be painted, got rgb({r},{g},{b})"
    );
}

#[test]
fn dump_mode_still_measures_the_full_1000x3200_document() {
    let fixture = format!("{}/test/data/long.html", env!("CARGO_MANIFEST_DIR"));
    let output = Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .arg("--dump")
        .arg(&fixture)
        .output()
        .expect("run browser-lite --dump");
    assert!(
        output.status.success(),
        "--dump exit: {:?} stderr: {}",
        output.status.code(),
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8_lossy(&output.stdout);

    let layout_line = stdout
        .lines()
        .find(|l| l.starts_with("LAYOUT "))
        .expect("LAYOUT header");
    assert_eq!(
        layout_line.trim(),
        "LAYOUT 1000x3200",
        "--dump must still measure the whole document"
    );

    let mut max_bottom = 0.0f64;
    let mut boxes = 0usize;
    for line in stdout.lines().filter(|l| l.starts_with("  [")) {
        let rest = line
            .strip_prefix("  [")
            .and_then(|l| l.split_once(']'))
            .map(|(rect, _)| rect)
            .expect("box line keeps its [x y w h] rect");
        let fields: Vec<&str> = rest.split_whitespace().collect();
        assert_eq!(fields.len(), 4, "box rect keeps four numbers: {line:?}");
        let y: f64 = fields[1].parse().expect("box y");
        let h: f64 = fields[3].parse().expect("box height");
        max_bottom = max_bottom.max(y + h);
        boxes += 1;
    }
    assert!(boxes > 0, "dump lists layout boxes");
    assert!(
        max_bottom >= 3199.0,
        "boxes reach the document end, got max bottom {max_bottom}"
    );
}

// Review correction pins (R2-001/R3-1): the interactive path must keep its
// stage-specific CLI diagnostics, so a paint failure stays distinguishable
// from a window/SDL failure.

#[test]
fn paint_failures_keep_the_paint_diagnostic() {
    let err = browser_lite::shell::RunError::Paint("boom".to_string());
    assert_eq!(err.diagnostic(), "browser-lite: paint failed: boom");
}

#[test]
fn window_failures_keep_the_window_diagnostic() {
    let err = browser_lite::shell::RunError::Window("boom".to_string());
    assert_eq!(err.diagnostic(), "browser-lite: window failed: boom");
}
