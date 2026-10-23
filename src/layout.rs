//! Block-flow and flex layout for the MVP subset.
//! Produces absolute boxes consumed by both the PNG painter and `--dump`.
//!
//! Text is measured, wrapped, aligned and justified by Pango: the run is laid
//! out once with the real font metrics inside the content box, and the same
//! layout is rebuilt at paint time from [`TextRun`]. Nothing here estimates
//! advances from the character count.
//!
//! A box whose computed `display` is `flex` opens a flex formatting context
//! ([`layout_flex_container`]): its children become flex items laid out along
//! the main axis instead of stacking in the block flow.
//!
//! A box whose computed `display` is `grid` opens a grid formatting context
//! ([`layout_grid_container`]): its children become grid items placed into
//! the tracks of `grid-template-columns`, one row at a time.

use std::os::raw::{c_int, c_void};
use std::rc::Rc;

use crate::css::{
    AlignItems, Display, FlexBasis, FlexDirection, GridTrack, JustifyContent, Style,
};
use crate::dom::{Document, NodeId};
use crate::img::{Image, ImageSet};

#[derive(Debug, Clone)]
pub struct LayoutBox {
    pub node: NodeId,
    pub tag: String,
    pub x: f64,
    pub y: f64,
    pub w: f64,
    pub h: f64,
    pub text: Option<String>,
    pub color: crate::css::Color,
    pub background: Option<crate::css::Color>,
    /// Decoded `background-image` pixels (S10), painted as the tiled box
    /// background behind the content; `None` when the element has no usable
    /// background image.
    pub background_image: Option<Rc<Image>>,
    pub font_size: f64,
    pub bold: bool,
    pub centered: bool,
    /// Decoded pixels for a replaced `<img>`; `None` for text/block boxes and
    /// for images that failed to load.
    pub image: Option<Rc<Image>>,
}

/// A text run as Pango sees it: the block's text plus the typography and the
/// wrap width. Paint rebuilds the layout from this instead of re-wrapping.
#[derive(Debug, Clone)]
pub struct TextRun {
    pub x: f64,
    pub y: f64,
    /// Wrap width, i.e. the content box the run lives in.
    pub width: f64,
    pub text: String,
    pub style: Style,
}

pub struct Layout {
    pub boxes: Vec<LayoutBox>,
    pub runs: Vec<TextRun>,
    pub width: f64,
    pub height: f64,
}

/// The size the layout dictates for a box, overriding what the cascade would
/// give it.
#[derive(Clone, Copy, Default)]
struct Forced {
    /// The border-box width.
    width: Option<f64>,
    /// The border-box height. It is a minimum: the content of the box may
    /// still overflow it (the engine has no overflow handling).
    height: Option<f64>,
    /// Lay the box out as a block even when its display is inline. A flex
    /// item is blockified.
    blockify: bool,
}

pub fn layout(doc: &Document, styles: &[Style], viewport_width: f64) -> Layout {
    layout_with_images(doc, styles, viewport_width, &ImageSet::empty())
}

/// Lays `doc` out with its `<img>` subresources and its `background-image`
/// URLs (S10) fetched and decoded.
///
/// `base` resolves relative URLs like the stylesheet loader does: the final
/// document URL for http(s) pages, the containing directory for local files.
/// Failures degrade to empty boxes and warnings; they never abort the render.
pub fn layout_with_subresources(
    doc: &Document,
    styles: &[Style],
    viewport_width: f64,
    base: &str,
) -> Layout {
    let images = ImageSet::load(doc, base).load_background_images(styles, base);
    layout_with_images(doc, styles, viewport_width, &images)
}

/// Lays `doc` out against an already loaded [`ImageSet`], so the caller keeps
/// the decoded images (and their warnings) for the rest of the run.
pub fn layout_with_images(
    doc: &Document,
    styles: &[Style],
    viewport_width: f64,
    images: &ImageSet,
) -> Layout {
    let shaper = TextShaper::new().expect("pango: cannot create a text measuring context");
    let mut boxes = Vec::new();
    let mut runs = Vec::new();
    let mut cursor = 0.0;

    for &child in &doc.nodes[doc.root].children {
        if styles[child].display == Display::None {
            continue;
        }
        cursor += layout_node(
            doc,
            styles,
            child,
            0.0,
            cursor,
            viewport_width,
            Forced::default(),
            &shaper,
            images,
            &mut boxes,
            &mut runs,
        );
    }

    Layout {
        boxes,
        runs,
        width: viewport_width,
        height: cursor.max(1.0),
    }
}

fn layout_node(
    doc: &Document,
    styles: &[Style],
    id: NodeId,
    origin_x: f64,
    origin_y: f64,
    available_width: f64,
    forced: Forced,
    shaper: &TextShaper,
    images: &ImageSet,
    out: &mut Vec<LayoutBox>,
    runs: &mut Vec<TextRun>,
) -> f64 {
    let style = &styles[id];
    if style.display == Display::None {
        return 0.0;
    }
    if is_image(doc, id) {
        return layout_image(
            doc,
            styles,
            id,
            origin_x,
            origin_y,
            available_width,
            forced,
            images,
            out,
        );
    }
    if style.display == Display::Inline && !forced.blockify {
        return 0.0; // inline runs are flushed by their block container
    }
    if style.flex.is_some() {
        return layout_flex_container(
            doc, styles, id, origin_x, origin_y, available_width, forced, shaper, images, out, runs,
        );
    }
    if style.grid.is_some() {
        return layout_grid_container(
            doc, styles, id, origin_x, origin_y, available_width, forced, shaper, images, out, runs,
        );
    }

    let content = content_box(style, origin_x, origin_y, available_width, forced);
    let border_x = content.border_x;
    let border_w = content.border_w;
    let content_x = content.x;
    let content_w = content.w;

    let border_y = origin_y + style.margin[0];
    let mut cursor_y = border_y + style.padding[0];

    // Placeholder so children find a stable index; height filled after flow.
    let box_index = out.len();
    out.push(LayoutBox {
        node: id,
        tag: doc.nodes[id].tag.clone(),
        x: border_x,
        y: border_y,
        w: border_w,
        h: 0.0,
        text: None,
        color: style.color,
        background: style.background,
        background_image: style
            .background_image
            .as_deref()
            .and_then(|url| images.background(url)),
        font_size: style.font_size,
        bold: style.bold,
        centered: style.inline_align,
        image: None,
    });

    let mut run = String::new();
    for &child in &doc.nodes[id].children {
        let child_style = &styles[child];
        match child_style.display {
            Display::None => {}
            _ if is_image(doc, child) => {
                // A replaced element takes its own line in this block flow, so
                // the text collected so far is flushed above it.
                if !run.trim().is_empty() {
                    cursor_y += flush_text(
                        shaper, style, content_x, cursor_y, content_w, &mut run, out, runs,
                    );
                }
                run.clear();
                cursor_y += layout_node(
                    doc,
                    styles,
                    child,
                    content_x,
                    cursor_y,
                    content_w,
                    Forced::default(),
                    shaper,
                    images,
                    out,
                    runs,
                );
            }
            Display::Block => {
                if !run.trim().is_empty() {
                    cursor_y += flush_text(
                        shaper, style, content_x, cursor_y, content_w, &mut run, out, runs,
                    );
                }
                run.clear();
                cursor_y += layout_node(
                    doc,
                    styles,
                    child,
                    content_x,
                    cursor_y,
                    content_w,
                    Forced::default(),
                    shaper,
                    images,
                    out,
                    runs,
                );
            }
            Display::Inline => collect_inline_text(doc, child, &mut run),
        }
    }
    if !run.trim().is_empty() {
        cursor_y +=
            flush_text(shaper, style, content_x, cursor_y, content_w, &mut run, out, runs);
    }

    let mut border_h = cursor_y - border_y + style.padding[2];
    if let Some(h) = style.height {
        border_h = border_h.max(h);
    }
    if let Some(h) = forced.height {
        border_h = border_h.max(h);
    }
    out[box_index].h = border_h.max(0.0);

    border_h + style.margin[0] + style.margin[2]
}

/// The content box of a block-level element: the rectangle its children (and a
/// replaced image) live in.
struct ContentBox {
    /// Left edge of the border box.
    border_x: f64,
    /// Width of the border box.
    border_w: f64,
    /// Top edge of the content box.
    y: f64,
    /// Left edge of the content box.
    x: f64,
    /// Width of the content box.
    w: f64,
}

fn content_box(
    style: &Style,
    origin_x: f64,
    origin_y: f64,
    available_width: f64,
    forced: Forced,
) -> ContentBox {
    let border_x = origin_x + style.margin[3];
    let border_w = match forced.width {
        // The flex algorithm decides the main size of an item; the cascade
        // does not get a say.
        Some(w) => w,
        None => style
            .width
            .map(|w| w + style.padding[1] + style.padding[3])
            .unwrap_or((available_width - style.margin[1] - style.margin[3]).max(1.0)),
    };
    ContentBox {
        border_x,
        border_w,
        y: origin_y + style.margin[0] + style.padding[0],
        x: border_x + style.padding[3],
        w: (border_w - style.padding[1] - style.padding[3]).max(1.0),
    }
}

// ---------------------------------------------------------------------------
// Flex layout (S7)
// ---------------------------------------------------------------------------

/// One flex item: a child of a flex container.
struct FlexItemBox {
    /// The DOM node of the item. `usize::MAX` marks an anonymous item built
    /// from the inline content of the container.
    node: NodeId,
    /// The text of an anonymous item.
    text: Option<String>,
    /// The item's own style.
    style: Style,
    /// The flex base size: the content-box size along the main axis.
    base: f64,
    /// The final content-box size along the main axis.
    main: f64,
    /// The content-box size along the cross axis.
    cross: f64,
    /// The border-box origin of the item.
    x: f64,
    y: f64,
}

/// Lays out a flex container: its children become flex items laid out along
/// the main axis of the container instead of stacking in the block flow.
///
/// The algorithm follows the flexbox specification: the base sizes come from
/// `flex-basis` (or from the content), the free space is shared with
/// `flex-grow`/`flex-shrink`, `justify-content` places the items along the
/// main axis and `align-items` places them across the cross axis.
#[allow(clippy::too_many_arguments)]
fn layout_flex_container(
    doc: &Document,
    styles: &[Style],
    id: NodeId,
    origin_x: f64,
    origin_y: f64,
    available_width: f64,
    forced: Forced,
    shaper: &TextShaper,
    images: &ImageSet,
    out: &mut Vec<LayoutBox>,
    runs: &mut Vec<TextRun>,
) -> f64 {
    let style = &styles[id];
    let flex = style.flex.unwrap_or_default();
    let row = flex.direction == FlexDirection::Row;

    let content = content_box(style, origin_x, origin_y, available_width, forced);
    let border_y = origin_y + style.margin[0];

    let box_index = out.len();
    out.push(LayoutBox {
        node: id,
        tag: doc.nodes[id].tag.clone(),
        x: content.border_x,
        y: border_y,
        w: content.border_w,
        h: 0.0,
        text: None,
        color: style.color,
        background: style.background,
        background_image: style
            .background_image
            .as_deref()
            .and_then(|url| images.background(url)),
        font_size: style.font_size,
        bold: style.bold,
        centered: style.inline_align,
        image: None,
    });

    // The inner main size of the container: the width of a row container, the
    // height of a column one (definite only when the author set it).
    let main_avail = if row {
        content.w
    } else {
        definite_inner_height(style).unwrap_or(f64::INFINITY)
    };
    // The inner cross size: the height of a row container, the width of a
    // column one.
    let cross_avail = if row {
        definite_inner_height(style).unwrap_or(f64::INFINITY)
    } else {
        content.w
    };

    let mut items = collect_flex_items(doc, styles, id);
    if items.is_empty() {
        let height = style.height.unwrap_or(0.0).max(0.0);
        out[box_index].h = height;
        return height + style.margin[0] + style.margin[2];
    }

    // The gap between the items of a line and the gap between the lines.
    let main_gap = if row { flex.column_gap } else { flex.row_gap };
    let cross_gap = if row { flex.row_gap } else { flex.column_gap };

    // 1. The cross size of the items of a column container: their width, which
    //    the base size (their height) depends on.
    if !row {
        for item in &mut items {
            item.cross = column_item_width(doc, styles, item, cross_avail, shaper, images);
        }
    }

    // 2. The flex base size of every item.
    for item in &mut items {
        item.base = flex_base_size(
            doc, styles, item, row, main_avail, shaper, images,
        );
        item.main = item.base;
    }

    // 3. The items are collected into lines: a line breaks when the next item
    //    does not fit in the main size of the container.
    let lines = flex_lines(&items, row, main_avail, main_gap);

    // 4. The flexible lengths: the free space of every line is shared with the
    //    grow factors, or taken back with the shrink factors.
    for &(start, count) in &lines {
        let used = line_main_size(&items[start..start + count], row, main_gap);
        let free = if main_avail.is_finite() {
            main_avail - used
        } else {
            0.0
        };
        if free > 0.0 {
            let grow: f64 = items[start..start + count]
                .iter()
                .map(|item| item.style.flex_item.grow)
                .sum();
            if grow > 0.0 {
                for item in &mut items[start..start + count] {
                    item.main += free * item.style.flex_item.grow / grow;
                }
            }
        } else if free < 0.0 {
            // The shrink factors are weighted with the base size.
            let shrink: f64 = items[start..start + count]
                .iter()
                .map(|item| item.style.flex_item.shrink * item.base)
                .sum();
            if shrink > 0.0 {
                for item in &mut items[start..start + count] {
                    let weight = item.style.flex_item.shrink * item.base;
                    item.main += free * weight / shrink;
                }
            }
        }
        for item in &mut items[start..start + count] {
            item.main = item.main.max(0.0);
        }
    }

    // 5. The cross size of the items of a row container: their height, at the
    //    width the flex algorithm gave them.
    if row {
        for item in &mut items {
            item.cross = measure_item_height(doc, styles, item, row, item.main, shaper, images);
        }
    }

    // 6. The cross size of every line.
    let mut line_cross: Vec<f64> = Vec::with_capacity(lines.len());
    for &(start, count) in &lines {
        let mut cross = 0.0f64;
        for item in &items[start..start + count] {
            let (p_start, p_end) = cross_padding(&item.style, row);
            let (m_start, m_end) = cross_margins(&item.style, row);
            cross = cross.max(item.cross + p_start + p_end + m_start + m_end);
        }
        // A container with a definite cross size gives that size to its lines.
        if cross_avail.is_finite() {
            cross = cross_avail;
        }
        line_cross.push(cross);
    }

    // 7. The items are placed along the main axis and across the cross axis.
    let mut cross_pos = if row { content.y } else { content.x };
    for (index, &(start, count)) in lines.iter().enumerate() {
        let line_size = line_cross[index];
        let used = line_main_size(&items[start..start + count], row, main_gap);
        let free = if main_avail.is_finite() {
            main_avail - used
        } else {
            0.0
        };
        let (offset, between) = justify_offset(flex.justify, free, count);
        let mut main_pos = (if row { content.x } else { content.y }) + offset;
        for item in &mut items[start..start + count] {
            let (m_start, _) = main_margins(&item.style, row);
            if row {
                item.x = main_pos + m_start;
            } else {
                item.y = main_pos + m_start;
            }
            // Every junction carries the container gap plus the extra space the
            // justify mode distributes (only the last item skips both).
            main_pos += item_outer_main(item, row) + main_gap + between;
        }

        for item in &mut items[start..start + count] {
            let (m_start, m_end) = cross_margins(&item.style, row);
            let (p_start, p_end) = cross_padding(&item.style, row);
            if flex.align == AlignItems::Stretch && cross_size_is_auto(item, row) {
                // A stretched item fills the line.
                item.cross = (line_size - m_start - m_end - p_start - p_end).max(0.0);
            }
            let outer = item.cross + p_start + p_end + m_start + m_end;
            let aligned = match flex.align {
                AlignItems::FlexStart => m_start,
                AlignItems::Center => m_start + (line_size - outer) / 2.0,
                AlignItems::FlexEnd => line_size - outer + m_start,
                AlignItems::Stretch => m_start,
            };
            if row {
                item.y = cross_pos + aligned;
            } else {
                item.x = cross_pos + aligned;
            }
        }

        cross_pos += line_size + cross_gap;
    }

    // 8. The items are laid out for real.
    let mut content_height = if row {
        line_cross.iter().sum::<f64>() + cross_gap * (lines.len().saturating_sub(1)) as f64
    } else {
        0.0
    };
    for item in &items {
        let (main_pad, cross_pad) = item_padding(&item.style, row);
        let forced = Forced {
            width: Some(if row {
                item.main + main_pad
            } else {
                item.cross + cross_pad
            }),
            height: Some(if row {
                item.cross + cross_pad
            } else {
                item.main + main_pad
            }),
            blockify: true,
        };
        let bottom = if let Some(text) = &item.text {
            // An anonymous item: the inline content of the container in a
            // block of its own.
            out.push(LayoutBox {
                node: usize::MAX,
                tag: "#anonymous".to_string(),
                x: item.x,
                y: item.y,
                w: item.main,
                h: item.cross,
                text: None,
                color: item.style.color,
                background: None,
                background_image: None,
                font_size: item.style.font_size,
                bold: item.style.bold,
                centered: false,
                image: None,
            });
            let mut run = text.clone();
            flush_text(
                shaper,
                &item.style,
                item.x,
                item.y,
                item.main.max(1.0),
                &mut run,
                out,
                runs,
            );
            item.y + item.cross
        } else {
            let total = layout_node(
                doc,
                styles,
                item.node,
                item.x,
                item.y,
                content.w,
                forced,
                shaper,
                images,
                out,
                runs,
            );
            let border_h = total - item.style.margin[0] - item.style.margin[2];
            item.y + border_h
        };
        content_height = content_height.max(bottom - content.y);
    }

    let mut border_h = content_height + style.padding[0] + style.padding[2];
    if let Some(h) = style.height {
        border_h = border_h.max(h);
    }
    out[box_index].h = border_h.max(0.0);

    border_h + style.margin[0] + style.margin[2]
}

/// The children of a flex container, as flex items.
///
/// Every in-flow child of a flex container is a flex item, and the inline
/// content of the container becomes an anonymous item.
fn collect_flex_items(doc: &Document, styles: &[Style], id: NodeId) -> Vec<FlexItemBox> {
    let mut items: Vec<FlexItemBox> = Vec::new();
    let mut text = String::new();

    let mut push_text = |items: &mut Vec<FlexItemBox>, text: &mut String| {
        if text.trim().is_empty() {
            text.clear();
            return;
        }
        items.push(FlexItemBox {
            node: usize::MAX,
            text: Some(text.clone()),
            style: styles[id].clone(),
            base: 0.0,
            main: 0.0,
            cross: 0.0,
            x: 0.0,
            y: 0.0,
        });
        text.clear();
    };

    for &child in &doc.nodes[id].children {
        let child_style = &styles[child];
        if child_style.display == Display::None {
            continue;
        }
        if doc.nodes[child].is_text {
            text.push_str(&doc.nodes[child].text);
            continue;
        }
        if child_style.display == Display::Inline {
            // The inline content of the container joins the anonymous item.
            collect_inline_text(doc, child, &mut text);
            continue;
        }
        push_text(&mut items, &mut text);
        items.push(FlexItemBox {
            node: child,
            text: None,
            style: child_style.clone(),
            base: 0.0,
            main: 0.0,
            cross: 0.0,
            x: 0.0,
            y: 0.0,
        });
    }
    push_text(&mut items, &mut text);

    items
}

/// The flex base size of an item: its `flex-basis` when it is definite, and
/// the size of its content otherwise.
fn flex_base_size(
    doc: &Document,
    styles: &[Style],
    item: &FlexItemBox,
    row: bool,
    main_avail: f64,
    shaper: &TextShaper,
    images: &ImageSet,
) -> f64 {
    match item.style.flex_item.basis {
        FlexBasis::Px(px) => px,
        FlexBasis::Percent(pct) => {
            if main_avail.is_finite() {
                main_avail * pct / 100.0
            } else {
                0.0
            }
        }
        // `auto` and `content`: the size of the content of the item. A
        // definite `width`/`height` acts as the basis.
        FlexBasis::Auto | FlexBasis::Content => {
            if row {
                item.style.width.unwrap_or_else(|| {
                    item_max_content_width(item, doc, styles, shaper, images)
                })
            } else {
                item.style.height.unwrap_or_else(|| {
                    measure_item_height(doc, styles, item, row, item.cross, shaper, images)
                })
            }
        }
    }
}

/// The items of a flex container, grouped into lines.
fn flex_lines(items: &[FlexItemBox], row: bool, main_avail: f64, main_gap: f64) -> Vec<(usize, usize)> {
    let mut lines: Vec<(usize, usize)> = Vec::new();
    if items.is_empty() {
        return lines;
    }
    let mut start = 0usize;
    let mut used = 0.0f64;
    for index in 0..items.len() {
        let outer = item_outer_main(&items[index], row);
        let needed = if index == start {
            outer
        } else {
            used + main_gap + outer
        };
        if index > start && needed > main_avail {
            lines.push((start, index - start));
            start = index;
            used = outer;
        } else {
            used = needed;
        }
    }
    lines.push((start, items.len() - start));
    lines
}

/// The main size of a line: the outer sizes of its items plus the gaps.
fn line_main_size(items: &[FlexItemBox], row: bool, main_gap: f64) -> f64 {
    let mut used = main_gap * items.len().saturating_sub(1) as f64;
    for item in items {
        used += item_outer_main(item, row);
    }
    used
}

/// The outer size of an item along the main axis: its content plus its
/// padding and its margins.
fn item_outer_main(item: &FlexItemBox, row: bool) -> f64 {
    let (pad, _) = item_padding(&item.style, row);
    let (m_start, m_end) = main_margins(&item.style, row);
    item.main + pad + m_start + m_end
}

/// The padding of an item along the main and the cross axis.
fn item_padding(style: &Style, row: bool) -> (f64, f64) {
    if row {
        (style.padding[3] + style.padding[1], style.padding[0] + style.padding[2])
    } else {
        (style.padding[0] + style.padding[2], style.padding[3] + style.padding[1])
    }
}

/// The margins of an item along the main axis (start, end).
fn main_margins(style: &Style, row: bool) -> (f64, f64) {
    if row {
        (style.margin[3], style.margin[1])
    } else {
        (style.margin[0], style.margin[2])
    }
}

/// The margins of an item along the cross axis (start, end).
fn cross_margins(style: &Style, row: bool) -> (f64, f64) {
    if row {
        (style.margin[0], style.margin[2])
    } else {
        (style.margin[3], style.margin[1])
    }
}

/// The padding of an item along the main axis (start, end).
fn main_padding(style: &Style, row: bool) -> (f64, f64) {
    if row {
        (style.padding[3], style.padding[1])
    } else {
        (style.padding[0], style.padding[2])
    }
}

/// The padding of an item along the cross axis (start, end).
fn cross_padding(style: &Style, row: bool) -> (f64, f64) {
    if row {
        (style.padding[0], style.padding[2])
    } else {
        (style.padding[3], style.padding[1])
    }
}

/// Is the cross size of the item automatic, so that `align-items: stretch`
/// applies to it?
fn cross_size_is_auto(item: &FlexItemBox, row: bool) -> bool {
    if row {
        item.style.height.is_none()
    } else {
        item.style.width.is_none()
    }
}

/// The offset of the items of a line and the extra space between them.
fn justify_offset(justify: JustifyContent, free: f64, count: usize) -> (f64, f64) {
    match justify {
        JustifyContent::FlexStart => (0.0, 0.0),
        JustifyContent::Center => (free / 2.0, 0.0),
        JustifyContent::FlexEnd => (free, 0.0),
        JustifyContent::SpaceBetween => {
            let free = free.max(0.0);
            (
                0.0,
                if count > 1 {
                    free / (count - 1) as f64
                } else {
                    0.0
                },
            )
        }
        JustifyContent::SpaceAround => {
            let free = free.max(0.0);
            let each = if count > 0 { free / count as f64 } else { 0.0 };
            (each / 2.0, each)
        }
        JustifyContent::SpaceEvenly => {
            let free = free.max(0.0);
            let each = if count > 0 {
                free / (count + 1) as f64
            } else {
                0.0
            };
            (each, each)
        }
    }
}

// ---------------------------------------------------------------------------
// Grid layout (S8)
// ---------------------------------------------------------------------------

/// One grid item: a child of a grid container.
struct GridItemBox {
    /// The DOM node of the item. `usize::MAX` marks an anonymous item built
    /// from the inline content of the container.
    node: NodeId,
    /// The text of an anonymous item.
    text: Option<String>,
    /// The item's own style.
    style: Style,
    /// How many tracks (columns) and rows the item spans.
    col_span: u32,
    row_span: u32,
    /// The placed column and row of the item's start.
    col: usize,
    row: usize,
    /// The border-box origin of the item.
    x: f64,
    y: f64,
    /// The content-box size of the item: its spanned tracks and rows.
    w: f64,
    h: f64,
}

/// Lays out a grid container: its children become grid items placed into the
/// tracks of `grid-template-columns` in row-major order (sparse packing, one
/// row at a time), with auto row heights from the content.
///
/// The algorithm resolves the track widths (definite `px` tracks, `auto`
/// tracks from their content, `fr` tracks from the free space left), places
/// every item (honouring `grid-column`/`grid-row` spans), measures the row
/// heights at the spanned widths, and stretches every item to fill its cell.
#[allow(clippy::too_many_arguments)]
fn layout_grid_container(
    doc: &Document,
    styles: &[Style],
    id: NodeId,
    origin_x: f64,
    origin_y: f64,
    available_width: f64,
    forced: Forced,
    shaper: &TextShaper,
    images: &ImageSet,
    out: &mut Vec<LayoutBox>,
    runs: &mut Vec<TextRun>,
) -> f64 {
    let style = &styles[id];
    let mut tracks = style.grid.clone().map(|grid| grid.tracks).unwrap_or_default();
    if tracks.is_empty() {
        // The implicit single column, for `display: grid` without a track
        // list (the CSS layer normally fills this in).
        tracks.push(GridTrack::Auto);
    }
    let ncols = tracks.len();

    let content = content_box(style, origin_x, origin_y, available_width, forced);
    let border_y = origin_y + style.margin[0];

    let box_index = out.len();
    out.push(LayoutBox {
        node: id,
        tag: doc.nodes[id].tag.clone(),
        x: content.border_x,
        y: border_y,
        w: content.border_w,
        h: 0.0,
        text: None,
        color: style.color,
        background: style.background,
        background_image: style
            .background_image
            .as_deref()
            .and_then(|url| images.background(url)),
        font_size: style.font_size,
        bold: style.bold,
        centered: style.inline_align,
        image: None,
    });

    let mut items = collect_grid_items(doc, styles, id);
    if items.is_empty() {
        let height = style.height.unwrap_or(0.0).max(0.0);
        out[box_index].h = height;
        return height + style.margin[0] + style.margin[2];
    }

    let col_gap = style.column_gap;
    let row_gap = style.row_gap;

    // 1. The items are placed row by row: every item takes the first run of
    //    free cells at or after the cursor (sparse packing: never backfill).
    let mut occupied: Vec<Vec<bool>> = Vec::new();
    let mut cursor = (0usize, 0usize);
    for item in &mut items {
        let span = (item.col_span as usize).clamp(1, ncols);
        item.col_span = span as u32;
        let rspan = (item.row_span as usize).clamp(1, 1000);
        item.row_span = rspan as u32;
        let (row, col) = place_grid_item(&mut occupied, ncols, cursor, span, rspan);
        item.row = row;
        item.col = col;
        cursor = if col + span >= ncols {
            (row + 1, 0)
        } else {
            (row, col + span)
        };
    }
    let nrows = occupied.len().max(1);

    // 2. The track widths: definite tracks keep their size, `auto` tracks
    //    size to the content of the items that occupy only them, and `fr`
    //    tracks share the free space that is left.
    let mut widths = vec![0.0f64; ncols];
    let mut fr_total = 0.0f64;
    for (index, track) in tracks.iter().enumerate() {
        match track {
            GridTrack::Px(px) => widths[index] = *px,
            GridTrack::Fr(fr) => fr_total += fr,
            GridTrack::Auto => {
                widths[index] = auto_track_width(&items, index, doc, styles, shaper, images);
            }
        }
    }
    let used: f64 =
        widths.iter().sum::<f64>() + col_gap * ncols.saturating_sub(1) as f64;
    if used < content.w && fr_total > 0.0 {
        let free = content.w - used;
        for (index, track) in tracks.iter().enumerate() {
            if let GridTrack::Fr(fr) = track {
                widths[index] += free * fr / fr_total;
            }
        }
    }
    for width in &mut widths {
        *width = (*width).max(0.0);
    }

    // 3. The row heights: every row is as tall as the tallest item that
    //    starts in it, measured at its spanned width.
    let mut heights = vec![0.0f64; nrows];
    for item in &items {
        if item.row_span != 1 {
            continue;
        }
        let width = span_size(&widths, item.col, item.col_span as usize, col_gap);
        let item_view = flex_view_of(item);
        let height = measure_item_height(doc, styles, &item_view, true, width, shaper, images);
        heights[item.row] = heights[item.row].max(height);
    }

    // 4. The items are placed at their cell origins and stretched to it.
    let mut col_x = vec![0.0f64; ncols];
    let mut x = content.x;
    for (index, width) in widths.iter().enumerate() {
        col_x[index] = x;
        x += width + col_gap;
    }
    let mut row_y = vec![0.0f64; nrows];
    let mut y = content.y;
    for (index, height) in heights.iter().enumerate() {
        row_y[index] = y;
        y += height + row_gap;
    }
    for item in &mut items {
        item.x = col_x[item.col];
        item.y = row_y[item.row];
        item.w = span_size(&widths, item.col, item.col_span as usize, col_gap);
        item.h = span_size(&heights, item.row, item.row_span as usize, row_gap);
    }

    // 5. The items are laid out for real, stretched to fill their cell.
    let mut content_height = 0.0f64;
    for item in &items {
        let forced = Forced {
            width: Some((item.w + item.style.padding[3] + item.style.padding[1]).max(0.0)),
            height: Some((item.h + item.style.padding[0] + item.style.padding[2]).max(0.0)),
            blockify: true,
        };
        let bottom = if let Some(text) = &item.text {
            // An anonymous item: the inline content of the container in a
            // block of its own.
            out.push(LayoutBox {
                node: usize::MAX,
                tag: "#anonymous".to_string(),
                x: item.x,
                y: item.y,
                w: item.w,
                h: item.h,
                text: None,
                color: item.style.color,
                background: None,
                background_image: None,
                font_size: item.style.font_size,
                bold: item.style.bold,
                centered: false,
                image: None,
            });
            let mut run = text.clone();
            flush_text(
                shaper,
                &item.style,
                item.x,
                item.y,
                item.w.max(1.0),
                &mut run,
                out,
                runs,
            );
            item.y + item.h
        } else {
            let total = layout_node(
                doc,
                styles,
                item.node,
                item.x,
                item.y,
                content.w,
                forced,
                shaper,
                images,
                out,
                runs,
            );
            let border_h = total - item.style.margin[0] - item.style.margin[2];
            item.y + border_h
        };
        content_height = content_height.max(bottom - content.y);
    }

    let mut border_h = content_height + style.padding[0] + style.padding[2];
    if let Some(h) = style.height {
        border_h = border_h.max(h);
    }
    out[box_index].h = border_h.max(0.0);

    border_h + style.margin[0] + style.margin[2]
}

/// The children of a grid container, as grid items.
///
/// Every in-flow child of a grid container is a grid item, and the inline
/// content of the container becomes an anonymous item.
fn collect_grid_items(doc: &Document, styles: &[Style], id: NodeId) -> Vec<GridItemBox> {
    let mut items: Vec<GridItemBox> = Vec::new();
    let mut text = String::new();

    let mut push_text = |items: &mut Vec<GridItemBox>, text: &mut String| {
        if text.trim().is_empty() {
            text.clear();
            return;
        }
        items.push(GridItemBox {
            node: usize::MAX,
            text: Some(text.clone()),
            style: styles[id].clone(),
            col_span: 1,
            row_span: 1,
            col: 0,
            row: 0,
            x: 0.0,
            y: 0.0,
            w: 0.0,
            h: 0.0,
        });
        text.clear();
    };

    for &child in &doc.nodes[id].children {
        let child_style = &styles[child];
        if child_style.display == Display::None {
            continue;
        }
        if doc.nodes[child].is_text {
            text.push_str(&doc.nodes[child].text);
            continue;
        }
        if child_style.display == Display::Inline {
            // The inline content of the container joins the anonymous item.
            collect_inline_text(doc, child, &mut text);
            continue;
        }
        push_text(&mut items, &mut text);
        items.push(GridItemBox {
            node: child,
            text: None,
            style: child_style.clone(),
            col_span: child_style.grid_item.col_span,
            row_span: child_style.grid_item.row_span,
            col: 0,
            row: 0,
            x: 0.0,
            y: 0.0,
            w: 0.0,
            h: 0.0,
        });
    }
    push_text(&mut items, &mut text);

    items
}

/// Places one item: the first run of `span` free cells in row `cursor.0` at
/// or after `cursor.1`, or in a later row from its start. Marks the spanned
/// cells occupied and returns the start of the item.
fn place_grid_item(
    occupied: &mut Vec<Vec<bool>>,
    ncols: usize,
    cursor: (usize, usize),
    span: usize,
    rspan: usize,
) -> (usize, usize) {
    let (mut row, col) = if cursor.1 >= ncols {
        (cursor.0 + 1, 0)
    } else {
        cursor
    };
    loop {
        while occupied.len() < row + rspan {
            occupied.push(vec![false; ncols]);
        }
        let first = if row == cursor.0 { col } else { 0 };
        for start in first..=ncols.saturating_sub(span) {
            let free = (0..rspan)
                .all(|dr| (0..span).all(|dc| !occupied[row + dr][start + dc]));
            if free {
                for dr in 0..rspan {
                    for dc in 0..span {
                        occupied[row + dr][start + dc] = true;
                    }
                }
                return (row, start);
            }
        }
        row += 1;
    }
}

/// The width of one `auto` track: the widest content of the items that
/// occupy only that track. Tracks with no such item (or only spanned items)
/// start at zero and let the `fr` tracks or the container edge decide.
fn auto_track_width(
    items: &[GridItemBox],
    col: usize,
    doc: &Document,
    styles: &[Style],
    shaper: &TextShaper,
    images: &ImageSet,
) -> f64 {
    let mut width = 0.0f64;
    for item in items {
        if item.col != col || item.col_span != 1 {
            continue;
        }
        width = width.max(item_max_content_width(
            &flex_view_of(item),
            doc,
            styles,
            shaper,
            images,
        ));
    }
    width
}

/// The flex view of a grid item, so grid reuses the flex measuring helpers
/// over the same content instead of re-implementing them.
fn flex_view_of(item: &GridItemBox) -> FlexItemBox {
    FlexItemBox {
        node: item.node,
        text: item.text.clone(),
        style: item.style.clone(),
        base: 0.0,
        main: 0.0,
        cross: 0.0,
        x: 0.0,
        y: 0.0,
    }
}

/// The size of `span` consecutive tracks starting at `start`, including the
/// gaps between them.
fn span_size(sizes: &[f64], start: usize, span: usize, gap: f64) -> f64 {
    let end = (start + span).min(sizes.len());
    let size: f64 = sizes[start.min(sizes.len())..end].iter().sum();
    size + gap * end.saturating_sub(start + 1) as f64
}

/// The width of an item of a column container: the width of the line when it
/// stretches, and the width of its content otherwise.
fn column_item_width(
    doc: &Document,
    styles: &[Style],
    item: &FlexItemBox,
    cross_avail: f64,
    shaper: &TextShaper,
    images: &ImageSet,
) -> f64 {
    let (m_start, m_end) = cross_margins(&item.style, false);
    let (p_start, p_end) = cross_padding(&item.style, false);
    if cross_size_is_auto(item, false) {
        let content = cross_avail - m_start - m_end - p_start - p_end;
        if content.is_finite() {
            return content.max(0.0);
        }
    }
    item.style.width.unwrap_or_else(|| {
        item_max_content_width(item, doc, styles, shaper, images).min(cross_avail)
    })
}

/// The max-content width of a flex item: the DOM subtree of a real item, or the
/// unwrapped text of an anonymous one (whose `node` is the `usize::MAX`
/// sentinel and must never reach the DOM lookups).
fn item_max_content_width(
    item: &FlexItemBox,
    doc: &Document,
    styles: &[Style],
    shaper: &TextShaper,
    images: &ImageSet,
) -> f64 {
    if item.node == usize::MAX {
        let text = item.text.as_deref().unwrap_or("");
        // A huge width keeps every word on one line: the layout width is an
        // i32 of Pango units, so stay far below its range.
        let measured = shaper.measure(text, &item.style, 1_000_000.0);
        return measured.lines.iter().map(|line| line.w).fold(0.0, f64::max);
    }
    max_content_width(doc, styles, item.node, shaper, images)
}

/// The height of the content of an item, measured in a scratch buffer so the
/// real layout can place the item afterwards.
fn measure_item_height(
    doc: &Document,
    styles: &[Style],
    item: &FlexItemBox,
    row: bool,
    content_width: f64,
    shaper: &TextShaper,
    images: &ImageSet,
) -> f64 {
    if let Some(text) = &item.text {
        return measure_text_height(shaper, text, &item.style, content_width);
    }
    let (p_start, p_end) = cross_padding(&item.style, row);
    let forced = Forced {
        width: Some((content_width + p_start + p_end).max(0.0)),
        height: None,
        blockify: true,
    };
    let mut boxes: Vec<LayoutBox> = Vec::new();
    let mut runs: Vec<TextRun> = Vec::new();
    let total = layout_node(
        doc,
        styles,
        item.node,
        0.0,
        0.0,
        content_width,
        forced,
        shaper,
        images,
        &mut boxes,
        &mut runs,
    );
    // The returned height is the border height plus the vertical margins.
    let border_h = total - item.style.margin[0] - item.style.margin[2];
    (border_h - item.style.padding[0] - item.style.padding[2]).max(0.0)
}

/// The height of a text run laid out at `width`.
fn measure_text_height(shaper: &TextShaper, text: &str, style: &Style, width: f64) -> f64 {
    let normalized = text.split_whitespace().collect::<Vec<_>>().join(" ");
    if normalized.is_empty() {
        return 0.0;
    }
    shaper.measure(&normalized, style, width.max(1.0)).height
}

/// The width the content of an item would take without wrapping.
fn max_content_width(
    doc: &Document,
    styles: &[Style],
    id: NodeId,
    shaper: &TextShaper,
    images: &ImageSet,
) -> f64 {
    let style = &styles[id];
    if is_image(doc, id) {
        return images.image(id).map(|i| i.width as f64).unwrap_or(0.0);
    }
    let mut width = 0.0f64;
    let mut text = String::new();
    for &child in &doc.nodes[id].children {
        let child_style = &styles[child];
        if child_style.display == Display::None {
            continue;
        }
        if is_image(doc, child) {
            width = width.max(images.image(child).map(|i| i.width as f64).unwrap_or(0.0));
        } else if child_style.display == Display::Block {
            width = width.max(max_content_width(doc, styles, child, shaper, images));
        } else {
            collect_inline_text(doc, child, &mut text);
        }
    }
    if !text.trim().is_empty() {
        width = width.max(natural_text_width(shaper, &text, style));
    }
    width
}

/// The width of a text run measured without wrapping.
fn natural_text_width(shaper: &TextShaper, text: &str, style: &Style) -> f64 {
    let normalized = text.split_whitespace().collect::<Vec<_>>().join(" ");
    if normalized.is_empty() {
        return 0.0;
    }
    let measured = shaper.measure(&normalized, style, 100_000.0);
    measured.lines.iter().map(|line| line.w).sum()
}

/// The inner height of a box with a definite height.
fn definite_inner_height(style: &Style) -> Option<f64> {
    style
        .height
        .map(|h| (h - style.padding[0] - style.padding[2]).max(0.0))
}

fn is_image(doc: &Document, id: NodeId) -> bool {
    !doc.nodes[id].is_text && doc.nodes[id].tag.eq_ignore_ascii_case("img")
}

/// Lays out a replaced `<img>`: one box holding the decoded pixels, sized from
/// its intrinsic dimensions (or the author's width/height) and never wider than
/// the content box. Returns the space it takes in the flow.
fn layout_image(
    doc: &Document,
    styles: &[Style],
    id: NodeId,
    origin_x: f64,
    origin_y: f64,
    available_width: f64,
    forced: Forced,
    images: &ImageSet,
    out: &mut Vec<LayoutBox>,
) -> f64 {
    let style = &styles[id];
    let content = content_box(style, origin_x, origin_y, available_width, forced);
    let image = images.image(id);
    let (w, h) = image_size(
        &doc.nodes[id],
        style,
        image.as_deref(),
        content.w,
        forced.width.map(|w| w - style.padding[1] - style.padding[3]),
    );
    let y = origin_y + style.margin[0] + style.padding[0];

    out.push(LayoutBox {
        node: id,
        tag: doc.nodes[id].tag.clone(),
        x: content.x,
        y,
        w,
        h,
        text: None,
        color: style.color,
        background: style.background,
        background_image: style
            .background_image
            .as_deref()
            .and_then(|url| images.background(url)),
        font_size: style.font_size,
        bold: style.bold,
        centered: style.inline_align,
        image,
    });

    h + style.margin[0] + style.margin[2]
}

/// The painted size of an image box.
///
/// CSS `width`/`height` win over the HTML `width`/`height` attributes, and both
/// win over the intrinsic pixel size. A single dimension keeps the intrinsic
/// aspect ratio; without pixels (failed or missing image) it stays 0. The
/// result never exceeds the content box: a wider image is scaled down on both
/// axes instead of overflowing its container.
fn image_size(
    node: &crate::dom::Node,
    style: &Style,
    image: Option<&Image>,
    max_width: f64,
    forced_width: Option<f64>,
) -> (f64, f64) {
    let (intrinsic_w, intrinsic_h) = image
        .map(|image| (image.width as f64, image.height as f64))
        .unwrap_or((0.0, 0.0));

    let width_hint = style
        .width
        .or_else(|| node.attr("width").and_then(length_px));
    let height_hint = style
        .height
        .or_else(|| node.attr("height").and_then(length_px));

    let (mut w, mut h) = match (width_hint, height_hint) {
        (Some(w), Some(h)) => (w, h),
        (Some(w), None) => (w, scale_by(intrinsic_w, intrinsic_h, w)),
        (None, Some(h)) => (scale_by(intrinsic_h, intrinsic_w, h), h),
        (None, None) => (intrinsic_w, intrinsic_h),
    };

    // The flex algorithm decided the width of the item: the image fills it.
    if let Some(forced) = forced_width {
        w = forced;
        h = match height_hint {
            Some(h) => h,
            None => scale_by(intrinsic_w, intrinsic_h, forced),
        };
        return (w.max(0.0), h.max(0.0));
    }

    if w > max_width {
        let scale = max_width / w;
        w = max_width;
        h *= scale;
    }
    (w.max(0.0), h.max(0.0))
}

/// `known * wanted / basis`, i.e. the missing side of an aspect ratio. A zero
/// basis (no pixels decoded) has no ratio to preserve.
fn scale_by(basis: f64, other: f64, wanted: f64) -> f64 {
    if basis > 0.0 {
        other * wanted / basis
    } else {
        0.0
    }
}

/// An HTML length attribute (`width="30"`, `height="30px"`) in CSS pixels.
/// Percentages and unparsable values are ignored: they do not size the box.
fn length_px(value: &str) -> Option<f64> {
    let value = value.trim();
    let value = value
        .strip_suffix("px")
        .or_else(|| value.strip_suffix("PX"))
        .unwrap_or(value)
        .trim();
    let px: f64 = value.parse().ok()?;
    (px.is_finite() && px >= 0.0).then_some(px)
}

/// Concatenates the inline descendants of a block into a single text run.
fn collect_inline_text(doc: &Document, id: NodeId, out: &mut String) {
    let node = &doc.nodes[id];
    if node.is_text {
        out.push_str(&node.text);
        return;
    }
    if matches!(node.tag.as_str(), "script" | "style" | "head" | "title") {
        return;
    }
    for &child in &node.children {
        collect_inline_text(doc, child, out);
    }
}

/// Lays a run out with Pango inside `width` and pushes one `#text` box per
/// visual line, plus the run itself for the painter. Returns the run height.
fn flush_text(
    shaper: &TextShaper,
    style: &Style,
    x: f64,
    y: f64,
    width: f64,
    run: &mut String,
    out: &mut Vec<LayoutBox>,
    runs: &mut Vec<TextRun>,
) -> f64 {
    let text = run.split_whitespace().collect::<Vec<_>>().join(" ");
    run.clear();
    if text.is_empty() {
        return 0.0;
    }

    let measured = shaper.measure(&text, style, width);
    for line in &measured.lines {
        if line.text.is_empty() {
            continue;
        }
        out.push(LayoutBox {
            node: usize::MAX,
            tag: "#text".to_string(),
            x: x + line.x,
            y: y + line.y,
            w: line.w,
            h: line.h,
            text: Some(line.text.clone()),
            color: style.color,
            background: None,
            background_image: None,
            font_size: style.font_size,
            bold: style.bold,
            centered: false,
            image: None,
        });
    }

    runs.push(TextRun {
        x,
        y,
        width,
        text,
        style: style.clone(),
    });
    measured.height
}

/// One visual line as Pango placed it, relative to the run origin.
struct MeasuredLine {
    x: f64,
    y: f64,
    w: f64,
    h: f64,
    text: String,
}

struct MeasuredText {
    height: f64,
    lines: Vec<MeasuredLine>,
}

/// A Pango context used only for measuring. It hangs off a scratch image
/// surface; the real painting context is built by `paint` from its own cairo
/// target, using the same 72 dpi so both agree on metrics.
struct TextShaper {
    ctx: *mut c_void,
    // Keep the scratch target alive: the PangoContext references it.
    _surface: cairo::ImageSurface,
    _cr: cairo::Context,
}

impl TextShaper {
    fn new() -> Option<TextShaper> {
        let surface = cairo::ImageSurface::create(cairo::Format::ARgb32, 8, 8).ok()?;
        let cr = cairo::Context::new(&surface).ok()?;
        let ctx = unsafe { pango::pango_cairo_create_context(cr.to_raw_none() as *mut c_void) };
        if ctx.is_null() {
            return None;
        }
        // 72 dpi: 1pt == 1px, so CSS px sizes map 1:1 to Pango font sizes.
        unsafe { pango::pango_cairo_context_set_resolution(ctx, 72.0) };
        Some(TextShaper {
            ctx,
            _surface: surface,
            _cr: cr,
        })
    }

    fn measure(&self, text: &str, style: &Style, width: f64) -> MeasuredText {
        let layout = pango::create_layout(self.ctx, text, style, width);
        if layout.is_null() {
            return MeasuredText {
                height: 0.0,
                lines: Vec::new(),
            };
        }

        let mut pixel_width = 0;
        let mut pixel_height = 0;
        let mut lines = Vec::new();
        unsafe {
            pango::pango_layout_get_pixel_size(layout, &mut pixel_width, &mut pixel_height);
            let count = pango::pango_layout_get_line_count(layout);
            for index in 0..count {
                let line = pango::pango_layout_get_line(layout, index);
                if line.is_null() {
                    continue;
                }
                let mut ink = pango::PangoRectangle::default();
                let mut logical = pango::PangoRectangle::default();
                pango::pango_layout_line_get_pixel_extents(line, &mut ink, &mut logical);
                let start = (*line).start_index.max(0) as usize;
                let len = (*line).length.max(0) as usize;
                // Line extents are relative to the line itself; the position of
                // the line's first character is relative to the layout origin
                // and already includes the alignment offset.
                let mut pos = pango::PangoRectangle::default();
                pango::pango_layout_index_to_pos(layout, start as c_int, &mut pos);
                let slice = text.get(start..start.saturating_add(len)).unwrap_or("");
                lines.push(MeasuredLine {
                    x: pos.x as f64 / pango::PANGO_SCALE,
                    y: pos.y as f64 / pango::PANGO_SCALE,
                    w: logical.width as f64,
                    h: logical.height as f64,
                    text: slice.trim().to_string(),
                });
            }
            pango::g_object_unref(layout);
        }

        MeasuredText {
            height: pixel_height as f64,
            lines,
        }
    }
}

impl Drop for TextShaper {
    fn drop(&mut self) {
        unsafe { pango::g_object_unref(self.ctx) };
    }
}

/// Hand-written Pango/PangoCairo FFI, in the same style as `src/sys.rs`.
/// Signatures follow the system Pango 1.58 headers; no intermediate crates.
pub(crate) mod pango {
    use std::ffi::CString;
    use std::os::raw::{c_char, c_double, c_int, c_void};

    use crate::css::{Style, TextAlign};

    /// Pango's fixed-point scale for font sizes and lengths.
    pub const PANGO_SCALE: f64 = 1024.0;

    const PANGO_ALIGN_LEFT: c_int = 0;
    const PANGO_ALIGN_CENTER: c_int = 1;
    const PANGO_ALIGN_RIGHT: c_int = 2;

    const PANGO_STYLE_NORMAL: c_int = 0;
    const PANGO_STYLE_ITALIC: c_int = 2;

    #[repr(C)]
    #[derive(Default)]
    pub struct PangoRectangle {
        pub x: c_int,
        pub y: c_int,
        pub width: c_int,
        pub height: c_int,
    }

    /// `PangoLayoutLine` is a public struct in the pango headers: reading
    /// `start_index`/`length` gives the bytes of each visual line.
    #[repr(C)]
    pub struct PangoLayoutLine {
        pub layout: *mut c_void,
        pub start_index: c_int,
        pub length: c_int,
        pub runs: *mut c_void,
    }

    #[link(name = "pangocairo-1.0")]
    extern "C" {
        pub fn pango_cairo_create_context(cr: *mut c_void) -> *mut c_void;
        pub fn pango_cairo_context_set_resolution(context: *mut c_void, dpi: c_double);
        pub fn pango_cairo_update_layout(cr: *mut c_void, layout: *mut c_void);
        pub fn pango_cairo_show_layout(cr: *mut c_void, layout: *mut c_void);
    }

    #[link(name = "pango-1.0")]
    extern "C" {
        pub fn pango_layout_new(context: *mut c_void) -> *mut c_void;
        pub fn pango_layout_set_text(layout: *mut c_void, text: *const c_char, length: c_int);
        pub fn pango_layout_set_font_description(layout: *mut c_void, desc: *const c_void);
        pub fn pango_layout_set_width(layout: *mut c_void, width: c_int);
        pub fn pango_layout_set_alignment(layout: *mut c_void, alignment: c_int);
        pub fn pango_layout_set_justify(layout: *mut c_void, justify: c_int);
        pub fn pango_layout_set_attributes(layout: *mut c_void, attrs: *mut c_void);
        pub fn pango_layout_get_pixel_size(
            layout: *mut c_void,
            width: *mut c_int,
            height: *mut c_int,
        );
        pub fn pango_layout_get_line_count(layout: *mut c_void) -> c_int;
        pub fn pango_layout_get_line(layout: *mut c_void, line: c_int) -> *mut PangoLayoutLine;
        pub fn pango_layout_index_to_pos(
            layout: *mut c_void,
            index: c_int,
            pos: *mut PangoRectangle,
        );
        pub fn pango_layout_line_get_pixel_extents(
            line: *mut PangoLayoutLine,
            ink_rect: *mut PangoRectangle,
            logical_rect: *mut PangoRectangle,
        );
        pub fn pango_font_description_from_string(str: *const c_char) -> *mut c_void;
        pub fn pango_font_description_free(desc: *mut c_void);
        pub fn pango_font_description_set_family(desc: *mut c_void, family: *const c_char);
        pub fn pango_font_description_set_size(desc: *mut c_void, size: c_int);
        pub fn pango_font_description_set_weight(desc: *mut c_void, weight: c_int);
        pub fn pango_font_description_set_style(desc: *mut c_void, style: c_int);
        pub fn pango_attr_list_new() -> *mut c_void;
        pub fn pango_attr_list_insert(list: *mut c_void, attr: *mut c_void);
        pub fn pango_attr_list_unref(list: *mut c_void);
        pub fn pango_attr_letter_spacing_new(spacing: c_int) -> *mut c_void;
        pub fn pango_attr_line_height_new(factor: c_double) -> *mut c_void;
    }

    #[link(name = "gobject-2.0")]
    extern "C" {
        pub fn g_object_unref(obj: *mut c_void);
    }

    /// Builds the Pango layout shared by measurement and painting, so both see
    /// the same font, width, alignment, line height and tracking.
    pub fn create_layout(
        ctx: *mut c_void,
        text: &str,
        style: &Style,
        width: f64,
    ) -> *mut c_void {
        unsafe {
            let layout = pango_layout_new(ctx);
            if layout.is_null() {
                return layout;
            }

            let desc = pango_font_description_from_string(b"Sans\0".as_ptr() as *const c_char);
            let family = CString::new(family_name(&style.font_family)).unwrap_or_default();
            pango_font_description_set_family(desc, family.as_ptr());
            pango_font_description_set_size(
                desc,
                (style.font_size.max(0.0) * PANGO_SCALE) as c_int,
            );
            pango_font_description_set_weight(desc, style.font_weight.clamp(100, 900) as c_int);
            pango_font_description_set_style(
                desc,
                if style.italic {
                    PANGO_STYLE_ITALIC
                } else {
                    PANGO_STYLE_NORMAL
                },
            );
            pango_layout_set_font_description(layout, desc);
            pango_font_description_free(desc);

            // NUL would truncate the CString; Pango takes an explicit length.
            let text = text.replace('\0', " ");
            let text_c = CString::new(text).unwrap_or_default();
            pango_layout_set_text(layout, text_c.as_ptr(), text_c.as_bytes().len() as c_int);
            pango_layout_set_width(layout, (width.max(1.0) * PANGO_SCALE) as c_int);
            pango_layout_set_alignment(
                layout,
                match style.text_align {
                    TextAlign::Left | TextAlign::Justify => PANGO_ALIGN_LEFT,
                    TextAlign::Center => PANGO_ALIGN_CENTER,
                    TextAlign::Right => PANGO_ALIGN_RIGHT,
                },
            );
            if style.text_align == TextAlign::Justify {
                pango_layout_set_justify(layout, 1);
            }

            // Line height and tracking are attributes in Pango 1.58: the layout
            // has no setter for them.
            let attrs = pango_attr_list_new();
            pango_attr_list_insert(
                attrs,
                pango_attr_line_height_new(style.line_height.max(0.0)),
            );
            if style.letter_spacing != 0.0 {
                pango_attr_list_insert(
                    attrs,
                    pango_attr_letter_spacing_new((style.letter_spacing * PANGO_SCALE) as c_int),
                );
            }
            pango_layout_set_attributes(layout, attrs);
            pango_attr_list_unref(attrs);

            layout
        }
    }

    /// libcss keeps the family as written; strip the CSS quotes and fall back
    /// to Pango's default family when it is empty.
    fn family_name(family: &str) -> String {
        let trimmed = family.trim().trim_matches(['"', '\'']).trim();
        if trimmed.is_empty() {
            "Sans".to_string()
        } else {
            trimmed.to_string()
        }
    }
}

