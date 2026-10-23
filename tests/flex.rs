//! Flex layout: the minimal flex formatting context (S7).
//!
//! These tests use only the public engine API: parse a fixture, compute the
//! styles with libcss, lay out, and inspect the produced boxes. The
//! expectations come from the CSS flexbox specification, not from the
//! implementation.

use browser_lite::css::{AlignItems, Display, FlexBasis, FlexDirection, FlexWrap, JustifyContent};
use browser_lite::css_libcss;
use browser_lite::dom::Document;
use browser_lite::layout::{self, Layout, LayoutBox};

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

use browser_lite::css::Style;

fn box_of<'a>(layout: &'a Layout, doc: &Document, selector: &str) -> &'a LayoutBox {
    let id = doc
        .query_selector(selector)
        .unwrap_or_else(|| panic!("fixture has no {selector}"));
    layout
        .boxes
        .iter()
        .find(|b| b.node == id)
        .unwrap_or_else(|| panic!("no box for {selector}"))
}

fn near(a: f64, b: f64) -> bool {
    (a - b).abs() < 0.5
}

// ---------------------------------------------------------------------------
// The cascade: a flex container is a block box that carries flex properties.
// ---------------------------------------------------------------------------

#[test]
fn a_flex_container_keeps_display_block_and_carries_its_flex_properties() {
    let (doc, styles) = load("flex-row.html");
    let row = &styles[doc.query_selector("#row").unwrap()];

    assert_eq!(row.display, Display::Block);
    let flex = row.flex.expect("the flex container carries its flex properties");
    assert_eq!(flex.direction, FlexDirection::Row);
    assert_eq!(flex.wrap, FlexWrap::NoWrap);
    assert_eq!(flex.justify, JustifyContent::FlexStart);
    assert_eq!(flex.align, AlignItems::Stretch);

    // The children are flex items: `flex: 0 1 auto` by default.
    let a = &styles[doc.query_selector("#a").unwrap()];
    assert_eq!(a.flex_item.grow, 0.0);
    assert_eq!(a.flex_item.shrink, 1.0);
    assert_eq!(a.flex_item.basis, FlexBasis::Auto);
}

#[test]
fn the_flex_shorthand_fills_grow_shrink_and_basis() {
    let (doc, styles) = load("flex-grow.html");
    let a = &styles[doc.query_selector("#a").unwrap()];
    let b = &styles[doc.query_selector("#b").unwrap()];

    assert_eq!(a.flex_item.grow, 1.0);
    assert_eq!(b.flex_item.grow, 2.0);
    assert_eq!(a.flex_item.shrink, 1.0);
    assert_eq!(a.flex_item.basis, FlexBasis::Px(0.0));
}

// ---------------------------------------------------------------------------
// The layout: the items share a row instead of stacking.
// ---------------------------------------------------------------------------

#[test]
fn flex_row_items_share_a_row() {
    let (doc, styles) = load("flex-row.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let a = box_of(&layout, &doc, "#a");
    let b = box_of(&layout, &doc, "#b");
    let c = box_of(&layout, &doc, "#c");

    assert!(
        near(a.y, b.y) && near(b.y, c.y),
        "the items share a row: a.y={} b.y={} c.y={}",
        a.y,
        b.y,
        c.y
    );
    assert!(
        a.x < b.x && b.x < c.x,
        "the items are laid out left to right: a.x={} b.x={} c.x={}",
        a.x,
        b.x,
        c.x
    );
    assert!(
        near(b.x - (a.x + a.w), 0.0),
        "the items are adjacent: a ends at {} and b starts at {}",
        a.x + a.w,
        b.x
    );

    // The container is as tall as its single line of items.
    let row = box_of(&layout, &doc, "#row");
    assert!(near(row.h, 40.0), "the row is 40px tall, got {}", row.h);
}

#[test]
fn flex_column_items_share_a_column() {
    let (doc, styles) = load("flex-column.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let a = box_of(&layout, &doc, "#a");
    let b = box_of(&layout, &doc, "#b");
    let c = box_of(&layout, &doc, "#c");

    assert!(
        near(a.x, b.x) && near(b.x, c.x),
        "the items share a column: a.x={} b.x={} c.x={}",
        a.x,
        b.x,
        c.x
    );
    assert!(
        a.y < b.y && b.y < c.y,
        "the items are stacked: a.y={} b.y={} c.y={}",
        a.y,
        b.y,
        c.y
    );

    let col = box_of(&layout, &doc, "#col");
    assert!(near(col.h, 120.0), "the column is 120px tall, got {}", col.h);
}

#[test]
fn flex_grow_shares_the_free_space() {
    let (doc, styles) = load("flex-grow.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let a = box_of(&layout, &doc, "#a");
    let b = box_of(&layout, &doc, "#b");

    assert!(
        near(a.w, 200.0),
        "the item with flex: 1 takes a third of 600px, got {}",
        a.w
    );
    assert!(
        near(b.w, 400.0),
        "the item with flex: 2 takes two thirds of 600px, got {}",
        b.w
    );
}

#[test]
fn gap_separates_the_items() {
    let (doc, styles) = load("flex-gap.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let a = box_of(&layout, &doc, "#a");
    let b = box_of(&layout, &doc, "#b");
    let c = box_of(&layout, &doc, "#c");

    assert!(
        near(b.x - (a.x + a.w), 20.0),
        "the gap between the first two items is 20px, got {}",
        b.x - (a.x + a.w)
    );
    assert!(
        near(c.x - (b.x + b.w), 20.0),
        "the gap between the last two items is 20px, got {}",
        c.x - (b.x + b.w)
    );

    // The gap reaches the cascade too.
    let flex = styles[doc.query_selector("#gap").unwrap()].flex.unwrap();
    assert!(near(flex.column_gap, 20.0), "column gap: {}", flex.column_gap);
    assert!(near(flex.row_gap, 20.0), "row gap: {}", flex.row_gap);
}

#[test]
fn justify_content_distributes_the_items() {
    let (doc, styles) = load("flex-justify.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let center = box_of(&layout, &doc, "#center");
    let ca = box_of(&layout, &doc, "#ca");
    assert!(
        near(ca.x - center.x, 150.0),
        "the centred items start 150px into the 600px row, got {}",
        ca.x - center.x
    );

    let end = box_of(&layout, &doc, "#end");
    let ea = box_of(&layout, &doc, "#ea");
    assert!(
        near(ea.x - end.x, 300.0),
        "the items pushed to the end start 300px into the row, got {}",
        ea.x - end.x
    );

    let between = box_of(&layout, &doc, "#between");
    let ba = box_of(&layout, &doc, "#ba");
    let bb = box_of(&layout, &doc, "#bb");
    let bc = box_of(&layout, &doc, "#bc");
    assert!(
        near(ba.x - between.x, 0.0),
        "the first item hugs the start, got {}",
        ba.x - between.x
    );
    // Three 100px items in a 600px row leave 300px of free space, which
    // space-between spreads over the TWO junctions between them: 150px each.
    assert!(
        near(bb.x - (ba.x + ba.w), 150.0),
        "space-between leaves 150px between the items, got {}",
        bb.x - (ba.x + ba.w)
    );
    assert!(
        near(bc.x - (bb.x + bb.w), 150.0),
        "space-between leaves 150px between the items, got {}",
        bc.x - (bb.x + bb.w)
    );
}

#[test]
fn align_items_places_the_items_across_the_cross_axis() {
    let (doc, styles) = load("flex-align.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let stretch = box_of(&layout, &doc, "#stretch");
    let sa = box_of(&layout, &doc, "#sa");
    assert!(
        near(sa.h, 100.0),
        "a stretched item fills the 100px row, got {}",
        sa.h
    );
    assert!(near(sa.y, stretch.y), "the item starts at the row top");

    let center = box_of(&layout, &doc, "#center");
    let ca = box_of(&layout, &doc, "#ca");
    assert!(
        ca.h < 100.0,
        "a centred item keeps its content height, got {}",
        ca.h
    );
    let above = ca.y - center.y;
    let below = center.y + center.h - (ca.y + ca.h);
    assert!(
        near(above, below),
        "the centred item sits in the middle: {} above, {} below",
        above,
        below
    );

    let start = box_of(&layout, &doc, "#start");
    let ta = box_of(&layout, &doc, "#ta");
    assert!(near(ta.y, start.y), "the item hugs the row top");
}

#[test]
fn flex_wrap_breaks_the_items_into_lines() {
    let (doc, styles) = load("flex-wrap.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let a = box_of(&layout, &doc, "#a");
    let b = box_of(&layout, &doc, "#b");
    let c = box_of(&layout, &doc, "#c");

    assert!(near(a.y, b.y), "the first two items share a line");
    assert!(
        c.y > b.y,
        "the third item wraps to a second line: b.y={} c.y={}",
        b.y,
        c.y
    );

    let wrap = box_of(&layout, &doc, "#wrap");
    assert!(
        near(wrap.h, 80.0),
        "two lines of 40px make an 80px container, got {}",
        wrap.h
    );
}

// Nested formatting contexts are laid out at their allotted width, not at the
// parent's available width (review finding R3-4, confirmed live: Codeberg's
// auth links were sized to the container and pushed past the window edge).

#[test]
fn nested_flex_item_is_content_sized() {
    let (doc, styles) = load("nested-contexts.html");
    let layout = layout::layout(&doc, &styles, VIEWPORT);

    let auth = box_of(&layout, &doc, "#auth");
    assert!(
        auth.w < 300.0,
        "a nested flex item must be content-sized, got width {}",
        auth.w
    );
    assert!(
        auth.x + auth.w <= 1000.5,
        "the auth group must stay inside the canvas: {}+{}",
        auth.x,
        auth.w
    );
}
