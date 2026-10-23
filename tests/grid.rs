//! Minimal grid formatting context (S8).
//!
//! These tests use only the public interface: the `browser-lite --dump`
//! binary on a fixture, parsing the box geometry it prints. The expectations
//! come from the CSS grid specification, not from the implementation.

use std::process::Command;

#[derive(Debug, Clone, Copy)]
struct Rect {
    x: f64,
    y: f64,
    w: f64,
    h: f64,
}

fn dump(fixture: &str) -> String {
    let path = format!("{}/test/data/{fixture}", env!("CARGO_MANIFEST_DIR"));
    let output = Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .arg("--dump")
        .arg(&path)
        .output()
        .unwrap_or_else(|e| panic!("run browser-lite --dump on {fixture}: {e}"));
    assert!(
        output.status.success(),
        "--dump exit: {:?} stderr: {}",
        output.status.code(),
        String::from_utf8_lossy(&output.stderr)
    );
    String::from_utf8_lossy(&output.stdout).into_owned()
}

/// All boxes of the dump: `(label, rect)`.
fn boxes(stdout: &str) -> Vec<(String, Rect)> {
    let mut out = Vec::new();
    for line in stdout.lines().filter(|l| l.starts_with("  [")) {
        let (rect, label) = line
            .strip_prefix("  [")
            .and_then(|l| l.split_once(']'))
            .unwrap_or_else(|| panic!("box line keeps its [x y w h] rect: {line:?}"));
        let fields: Vec<&str> = rect.split_whitespace().collect();
        assert_eq!(fields.len(), 4, "box rect keeps four numbers: {line:?}");
        out.push((
            label.trim().to_string(),
            Rect {
                x: fields[0].parse().expect("box x"),
                y: fields[1].parse().expect("box y"),
                w: fields[2].parse().expect("box w"),
                h: fields[3].parse().expect("box h"),
            },
        ));
    }
    assert!(!out.is_empty(), "dump lists layout boxes");
    out
}

/// The text box holding exactly `word` (grid items carry one word each).
fn text_box(stdout: &str, word: &str) -> Rect {
    let want = format!("#text {word:?}");
    boxes(stdout)
        .into_iter()
        .find(|(label, _)| label == &want)
        .unwrap_or_else(|| panic!("dump holds the {word:?} text box"))
        .1
}

/// The item box holding the `word` text: the smallest `div` containing the
/// text centre. Text boxes only span their glyphs; the item box is the cell
/// the test asserts on.
fn item_box(stdout: &str, word: &str) -> Rect {
    let t = text_box(stdout, word);
    let (cx, cy) = (t.x + t.w / 2.0, t.y + t.h / 2.0);
    boxes(stdout)
        .into_iter()
        .filter(|(label, r)| {
            label == "div" && cx >= r.x && cx <= r.x + r.w && cy >= r.y && cy <= r.y + r.h
        })
        .map(|(_, r)| r)
        .min_by(|a, b| {
            (a.w * a.h)
                .partial_cmp(&(b.w * b.h))
                .unwrap_or(std::cmp::Ordering::Equal)
        })
        .unwrap_or_else(|| panic!("dump holds the {word:?} item box"))
}

/// The box of the grid container itself (fixtures use `<section>` for it, so
/// its label is unambiguous).
fn container(stdout: &str) -> Rect {
    boxes(stdout)
        .into_iter()
        .find(|(label, _)| label == "section")
        .expect("dump holds the section container box")
        .1
}

fn near(a: f64, b: f64) -> bool {
    (a - b).abs() < 0.5
}

// ---------------------------------------------------------------------------
// S8: minimal grid
// ---------------------------------------------------------------------------

#[test]
fn grid_basic_two_fixed_tracks_share_a_row() {
    let stdout = dump("grid-basic.html");
    let a = item_box(&stdout, "A");
    let b = item_box(&stdout, "B");
    let c = item_box(&stdout, "C");

    assert!(
        near(a.y, b.y),
        "the first two items share a row: a.y={} b.y={}",
        a.y,
        b.y
    );
    assert!(b.x > a.x, "the items run left to right");
    assert!(
        near(b.x - (a.x + a.w), 0.0),
        "adjacent fixed tracks touch: a ends at {} and b starts at {}",
        a.x + a.w,
        b.x
    );
    assert!(
        near(a.w, 100.0) && near(b.w, 100.0),
        "the px tracks are 100px wide: a.w={} b.w={}",
        a.w,
        b.w
    );

    // The third item wraps to the second row, back in the first column.
    assert!(
        c.y > b.y,
        "the third item wraps to a second row: b.y={} c.y={}",
        b.y,
        c.y
    );
    assert!(
        near(c.y - a.y, 40.0),
        "the rows are 40px tall: a.y={} c.y={}",
        a.y,
        c.y
    );
    assert!(near(c.x, a.x), "the wrapped item is back in column one");

    let g = container(&stdout);
    assert!(
        near(g.h, 80.0),
        "two 40px rows make an 80px container, got {}",
        g.h
    );
}

#[test]
fn grid_fr_tracks_share_the_free_space() {
    let stdout = dump("grid-fr.html");
    let a = item_box(&stdout, "A");
    let b = item_box(&stdout, "B");

    assert!(near(a.y, b.y), "the items share a row");
    assert!(
        near(a.w, 200.0),
        "1fr of 600px is 200px wide, got {}",
        a.w
    );
    assert!(
        near(b.w, 400.0),
        "2fr of 600px is 400px wide, got {}",
        b.w
    );
    assert!(
        near(b.x - (a.x + a.w), 0.0),
        "the fr tracks touch: a ends at {} and b starts at {}",
        a.x + a.w,
        b.x
    );
}

#[test]
fn grid_auto_track_sizes_to_its_content() {
    let stdout = dump("grid-auto.html");
    let a = item_box(&stdout, "A");
    let b = item_box(&stdout, "B");
    let c = item_box(&stdout, "C");

    assert!(
        near(a.w, 100.0),
        "the px track stays 100px wide, got {}",
        a.w
    );
    assert!(near(a.y, b.y), "the first two items share a row");
    assert!(
        near(b.x - (a.x + a.w), 0.0),
        "the auto track starts where the px track ends"
    );
    assert!(
        b.w > 0.0 && b.w < 150.0,
        "the auto track fits its short content instead of stretching, got {}",
        b.w
    );
    assert!(
        c.y > b.y && near(c.x, a.x),
        "the third item wraps to the first column: c=({},{}) a.x={}",
        c.x,
        c.y,
        a.x
    );
}

#[test]
fn grid_gap_separates_tracks_and_rows() {
    let stdout = dump("grid-gap.html");
    let a = item_box(&stdout, "A");
    let b = item_box(&stdout, "B");
    let c = item_box(&stdout, "C");

    assert!(
        near(b.x - (a.x + a.w), 20.0),
        "the column gap is 20px, got {}",
        b.x - (a.x + a.w)
    );
    assert!(
        c.y > b.y,
        "the third item wraps: b.y={} c.y={}",
        b.y,
        c.y
    );
    assert!(
        near(c.y - (a.y + a.h), 20.0),
        "the row gap is 20px, got {}",
        c.y - (a.y + a.h)
    );
}

#[test]
fn grid_column_span_covers_two_tracks() {
    let stdout = dump("grid-span.html");
    let a = item_box(&stdout, "A");
    let b = item_box(&stdout, "B");
    let c = item_box(&stdout, "C");

    assert!(
        near(a.w, 200.0),
        "span 2 over 100px tracks is 200px wide, got {}",
        a.w
    );
    assert!(near(a.y, b.y), "the spanned item shares its row with B");
    assert!(
        near(b.x - (a.x + a.w), 0.0),
        "B sits in the third track: a ends at {} and b starts at {}",
        a.x + a.w,
        b.x
    );
    assert!(
        near(b.w, 100.0),
        "the remaining track stays 100px wide, got {}",
        b.w
    );
    assert!(
        c.y > b.y && near(c.x, a.x),
        "the third item wraps to the first column: c=({},{}) a.x={}",
        c.x,
        c.y,
        a.x
    );
}

// ---------------------------------------------------------------------------
// Preservation: pages without grid lay out exactly as before.
// ---------------------------------------------------------------------------

#[test]
fn block_pages_without_grid_still_stack() {
    let stdout = dump("grid-block.html");
    let a = item_box(&stdout, "A");
    let b = item_box(&stdout, "B");

    assert!(
        b.y > a.y,
        "blocks stack vertically: a.y={} b.y={}",
        a.y,
        b.y
    );
    assert!(
        near(b.y - a.y, 30.0),
        "the first 30px block sets the gap: a.y={} b.y={}",
        a.y,
        b.y
    );
    assert!(near(a.x, b.x), "blocks share the left edge");
}
