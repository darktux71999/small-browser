//! Block-flow layout for the MVP subset.
//! Produces absolute boxes consumed by both the PNG painter and `--dump`.

use crate::css::{Display, Style};
use crate::dom::{Document, NodeId};

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
    pub font_size: f64,
    pub bold: bool,
    pub centered: bool,
}

pub struct Layout {
    pub boxes: Vec<LayoutBox>,
    pub width: f64,
    pub height: f64,
}

pub fn layout(doc: &Document, styles: &[Style], viewport_width: f64) -> Layout {
    let mut boxes = Vec::new();
    let mut cursor = 0.0;

    for &child in &doc.nodes[doc.root].children {
        if styles[child].display == Display::None {
            continue;
        }
        cursor += layout_node(doc, styles, child, 0.0, cursor, viewport_width, &mut boxes);
    }

    Layout {
        boxes,
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
    out: &mut Vec<LayoutBox>,
) -> f64 {
    let style = &styles[id];
    if style.display == Display::None {
        return 0.0;
    }
    if style.display == Display::Inline {
        return 0.0; // inline runs are flushed by their block container
    }

    let border_x = origin_x + style.margin[3];
    let border_w = style
        .width
        .map(|w| w + style.padding[1] + style.padding[3])
        .unwrap_or((available_width - style.margin[1] - style.margin[3]).max(1.0));
    let content_x = border_x + style.padding[3];
    let content_w = (border_w - style.padding[1] - style.padding[3]).max(1.0);

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
        font_size: style.font_size,
        bold: style.bold,
        centered: style.inline_align,
    });

    let mut run = String::new();
    for &child in &doc.nodes[id].children {
        let child_style = &styles[child];
        match child_style.display {
            Display::None => {}
            Display::Block => {
                if !run.trim().is_empty() {
                    cursor_y += flush_text(doc, style, content_x, cursor_y, content_w, &mut run, out);
                }
                run.clear();
                cursor_y +=
                    layout_node(doc, styles, child, content_x, cursor_y, content_w, out);
            }
            Display::Inline => collect_inline_text(doc, child, &mut run),
        }
    }
    if !run.trim().is_empty() {
        cursor_y += flush_text(doc, style, content_x, cursor_y, content_w, &mut run, out);
    }

    let mut border_h = cursor_y - border_y + style.padding[2];
    if let Some(h) = style.height {
        border_h = border_h.max(h);
    }
    out[box_index].h = border_h.max(0.0);

    border_h + style.margin[0] + style.margin[2]
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

fn flush_text(
    _doc: &Document,
    style: &Style,
    x: f64,
    y: f64,
    width: f64,
    run: &mut String,
    out: &mut Vec<LayoutBox>,
) -> f64 {
    let text = run.split_whitespace().collect::<Vec<_>>().join(" ");
    run.clear();
    if text.is_empty() {
        return 0.0;
    }

    let char_w = (style.font_size * 0.52).max(1.0);
    let max_chars = ((width / char_w).floor() as usize).max(1);
    let line_height = style.font_size * 1.25;

    let mut lines: Vec<String> = Vec::new();
    let mut current = String::new();
    for word in text.split(' ') {
        let candidate_len = if current.is_empty() {
            word.chars().count()
        } else {
            current.chars().count() + 1 + word.chars().count()
        };
        if candidate_len > max_chars && !current.is_empty() {
            lines.push(std::mem::take(&mut current));
        }
        if !current.is_empty() {
            current.push(' ');
        }
        current.push_str(word);
    }
    if !current.is_empty() {
        lines.push(current);
    }

    let mut cy = y;
    for line in &lines {
        let line_w = line.chars().count() as f64 * char_w;
        let lx = if style.inline_align {
            x + ((width - line_w) / 2.0).max(0.0)
        } else {
            x
        };
        out.push(LayoutBox {
            node: usize::MAX,
            tag: "#text".to_string(),
            x: lx,
            y: cy,
            w: line_w.min(width),
            h: line_height,
            text: Some(line.clone()),
            color: style.color,
            background: None,
            font_size: style.font_size,
            bold: style.bold,
            centered: false,
        });
        cy += line_height;
    }
    cy - y
}
