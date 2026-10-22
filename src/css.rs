//! CSS subset parser + cascade.
//!
//! Scope is the MVP subset: compound/descendant selectors, specificity
//! (id, class, type), origin order, `!important`, and the properties layout
//! and paint actually use. The module boundary keeps this swappable for
//! lexbor/css later without touching layout.

use std::collections::HashMap;

use crate::dom::{Document, NodeId};

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, PartialOrd, Ord)]
pub struct Specificity {
    pub ids: u32,
    pub classes: u32,
    pub tags: u32,
}

#[derive(Debug, Clone)]
pub struct Declaration {
    pub property: String,
    pub value: String,
    pub important: bool,
}

#[derive(Debug, Clone)]
pub struct Rule {
    pub selector: Selector,
    pub decls: Vec<Declaration>,
}

#[derive(Debug, Clone)]
pub struct Selector {
    pub compounds: Vec<Compound>,
    pub specificity: Specificity,
}

#[derive(Debug, Clone, Default)]
pub struct Compound {
    pub tag: Option<String>,
    pub id: Option<String>,
    pub classes: Vec<String>,
}

#[derive(Debug, Default)]
pub struct Stylesheet {
    pub rules: Vec<Rule>,
}

impl Stylesheet {
    pub fn parse(source: &str) -> Stylesheet {
        let mut sheet = Stylesheet::default();
        let src = strip_comments(source);
        let mut rest = src.as_str();
        while let Some(open) = rest.find('{') {
            let selector_text = rest[..open].trim();
            let Some(close_rel) = rest[open..].find('}') else {
                break;
            };
            let body = &rest[open + 1..open + close_rel];
            rest = &rest[open + close_rel + 1..];

            // Drop at-rules; the MVP does not apply them.
            if selector_text.starts_with('@') {
                continue;
            }
            let decls = parse_declarations(body);
            if decls.is_empty() {
                continue;
            }
            for part in selector_text.split(',') {
                if let Some(selector) = parse_selector(part.trim()) {
                    sheet.rules.push(Rule {
                        selector,
                        decls: decls.clone(),
                    });
                }
            }
        }
        sheet
    }
}

fn strip_comments(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let bytes = s.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        if i + 1 < bytes.len() && bytes[i] == b'/' && bytes[i + 1] == b'*' {
            i += 2;
            while i + 1 < bytes.len() && !(bytes[i] == b'*' && bytes[i + 1] == b'/') {
                i += 1;
            }
            i = (i + 2).min(bytes.len());
        } else {
            out.push(bytes[i] as char);
            i += 1;
        }
    }
    out
}

fn parse_declarations(body: &str) -> Vec<Declaration> {
    body.split(';')
        .filter_map(|decl| {
            let (prop, value) = decl.split_once(':')?;
            let prop = prop.trim().to_ascii_lowercase();
            if prop.is_empty() {
                return None;
            }
            let mut value = value.trim().to_string();
            let important = value.to_ascii_lowercase().ends_with("!important");
            if important {
                value = value[..value.len() - "!important".len()].trim().to_string();
            }
            Some(Declaration {
                property: prop,
                value,
                important,
            })
        })
        .collect()
}

fn parse_selector(text: &str) -> Option<Selector> {
    let mut compounds = Vec::new();
    let mut specificity = Specificity::default();

    for part in text.split_whitespace().filter(|p| *p != ">") {
        let mut compound = Compound::default();
        let tag_end = part.find(['#', '.']).unwrap_or(part.len());
        let tag = &part[..tag_end];
        if !tag.is_empty() && tag != "*" {
            compound.tag = Some(tag.to_ascii_lowercase());
            specificity.tags += 1;
        }
        let mut rest = &part[tag_end..];
        while !rest.is_empty() {
            let (kind, tail) = rest.split_at(1);
            let end = tail.find(['#', '.']).unwrap_or(tail.len());
            let (name, next) = tail.split_at(end);
            if name.is_empty() {
                return None;
            }
            match kind {
                "#" => {
                    if compound.id.is_some() {
                        return None;
                    }
                    compound.id = Some(name.to_string());
                    specificity.ids += 1;
                }
                "." => {
                    compound.classes.push(name.to_string());
                    specificity.classes += 1;
                }
                _ => return None,
            }
            rest = next;
        }
        compounds.push(compound);
    }

    if compounds.is_empty() {
        None
    } else {
        Some(Selector {
            compounds,
            specificity,
        })
    }
}

// ---------- computed style ----------

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Display {
    Block,
    Inline,
    None,
}

/// Horizontal alignment of text inside a block.
///
/// The legacy cascade (`parse` + `compute` in this module) only records the
/// centred case in `Style::inline_align`; libcss fills the full set.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum TextAlign {
    #[default]
    Left,
    Center,
    Right,
    Justify,
}

/// The main axis of a flex container: `row` (horizontal) or `column`
/// (vertical).
///
/// The reverse variants (`row-reverse`, `column-reverse`) are not part of the
/// minimal flexbox; the engine lays them out like their non-reversed twin.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum FlexDirection {
    #[default]
    Row,
    Column,
}

/// Whether a flex container breaks its items into several lines.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum FlexWrap {
    #[default]
    NoWrap,
    Wrap,
}

/// How a flex container distributes its items along the main axis.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum JustifyContent {
    #[default]
    FlexStart,
    Center,
    FlexEnd,
    SpaceBetween,
    SpaceAround,
    SpaceEvenly,
}

/// How a flex container aligns its items along the cross axis.
///
/// `baseline` is not part of the minimal flexbox: it falls back to
/// [`AlignItems::FlexStart`].
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum AlignItems {
    #[default]
    Stretch,
    FlexStart,
    Center,
    FlexEnd,
}

/// The flex base size of a flex item.
#[derive(Debug, Clone, Copy, Default, PartialEq)]
pub enum FlexBasis {
    /// `auto`: the item's content size (its `width`/`height` when definite).
    #[default]
    Auto,
    /// `content`: the item's content size.
    Content,
    /// A definite length in CSS pixels.
    Px(f64),
    /// A percentage of the flex container's main size.
    Percent(f64),
}

/// The flex container properties of an element.
///
/// A flex container is an element whose computed `display` is `flex` (or
/// `inline-flex`, which the engine treats as a block-level flex container).
/// The engine keeps `display: block` for it and marks the container with
/// `Style::flex`.
#[derive(Debug, Clone, Copy, Default, PartialEq)]
pub struct FlexStyle {
    pub direction: FlexDirection,
    pub wrap: FlexWrap,
    pub justify: JustifyContent,
    pub align: AlignItems,
    /// The gap between the lines of the container, in CSS pixels.
    pub row_gap: f64,
    /// The gap between the items of a line, in CSS pixels.
    pub column_gap: f64,
}

/// The flex item properties of an element: `flex-grow`, `flex-shrink` and
/// `flex-basis`.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct FlexItem {
    pub grow: f64,
    pub shrink: f64,
    pub basis: FlexBasis,
}

impl Default for FlexItem {
    fn default() -> Self {
        // The CSS initial value of `flex` is `0 1 auto`.
        FlexItem {
            grow: 0.0,
            shrink: 1.0,
            basis: FlexBasis::Auto,
        }
    }
}

#[derive(Debug, Clone, Copy)]
pub struct Color {
    pub r: f64,
    pub g: f64,
    pub b: f64,
    pub a: f64,
}

impl Color {
    pub const BLACK: Color = Color { r: 0.0, g: 0.0, b: 0.0, a: 1.0 };
    pub const WHITE: Color = Color { r: 1.0, g: 1.0, b: 1.0, a: 1.0 };
    pub const BLUE: Color = Color { r: 0.0, g: 0.0, b: 1.0, a: 1.0 };
}

#[derive(Debug, Clone)]
pub struct Style {
    pub display: Display,
    pub width: Option<f64>,
    pub height: Option<f64>,
    pub margin: [f64; 4],  // top, right, bottom, left
    pub padding: [f64; 4],
    pub color: Color,
    pub background: Option<Color>,
    pub font_size: f64,
    // Typography, populated by the libcss path. `bold` and `inline_align` stay
    // for layout/paint compatibility and mirror weight >= 600 and centred text.
    pub font_family: String,
    pub font_weight: u32,
    pub italic: bool,
    pub line_height: f64,
    pub text_align: TextAlign,
    pub letter_spacing: f64,
    pub bold: bool,
    pub inline_align: bool,
    /// The flex container properties; `Some` when the element is a flex
    /// container (`display: flex` or `display: inline-flex`).
    ///
    /// The engine has no `Display::Flex` variant (the CSS spike matches
    /// `Display` exhaustively), so a flex container keeps `Display::Block` and
    /// is marked here instead.
    pub flex: Option<FlexStyle>,
    /// The element's own flex item properties, used when the element is an
    /// item of a flex container.
    pub flex_item: FlexItem,
}

impl Default for Style {
    fn default() -> Self {
        Style {
            display: Display::Inline,
            width: None,
            height: None,
            margin: [0.0; 4],
            padding: [0.0; 4],
            color: Color::BLACK,
            background: None,
            font_size: 16.0,
            font_family: String::from("Sans"),
            font_weight: 400,
            italic: false,
            line_height: 1.2,
            text_align: TextAlign::Left,
            letter_spacing: 0.0,
            bold: false,
            inline_align: false,
            flex: None,
            flex_item: FlexItem::default(),
        }
    }
}

pub fn compute(doc: &Document, sheet: &Stylesheet) -> Vec<Style> {
    let mut styles = vec![Style::default(); doc.nodes.len()];

    for id in 0..doc.nodes.len() {
        if doc.nodes[id].is_text {
            styles[id] = Style {
                display: Display::Inline,
                ..Style::default()
            };
            continue;
        }
        if id == doc.root {
            styles[id].display = Display::Block;
            continue;
        }

        let mut style = Style::default();
        // Inherited values are the base; UA defaults and author rules override.
        if let Some(parent) = doc.nodes[id].parent {
            let inherited = &styles[parent];
            style.color = inherited.color;
            style.font_size = inherited.font_size;
            style.bold = inherited.bold;
        }
        apply_ua_defaults(&mut style, &doc.nodes[id].tag);

        let mut matched: Vec<(&Rule, bool)> = sheet
            .rules
            .iter()
            .filter(|rule| matches(doc, id, &rule.selector))
            .map(|rule| {
                let important = rule.decls.iter().any(|d| d.important);
                (rule, important)
            })
            .collect();
        matched.sort_by(|a, b| {
            a.1.cmp(&b.1)
                .then(a.0.selector.specificity.cmp(&b.0.selector.specificity))
        });
        for (rule, _) in matched {
            for decl in &rule.decls {
                apply(&mut style, decl);
            }
        }

        if let Some(inline) = doc.nodes[id].attr("style") {
            for decl in parse_declarations(inline) {
                apply(&mut style, &decl);
            }
        }

        styles[id] = style;
    }

    styles
}

fn apply_ua_defaults(s: &mut Style, tag: &str) {
    match tag {
        "head" | "script" | "style" | "title" | "meta" | "link" => {
            s.display = Display::None;
        }
        "body" => {
            s.display = Display::Block;
            s.margin = [8.0; 4];
        }
        "p" => {
            s.display = Display::Block;
            s.margin = [16.0, 0.0, 16.0, 0.0];
        }
        "h1" => {
            s.display = Display::Block;
            s.font_size = 32.0;
            s.bold = true;
            s.margin = [21.0, 0.0, 21.0, 0.0];
        }
        "h2" => {
            s.display = Display::Block;
            s.font_size = 24.0;
            s.bold = true;
            s.margin = [19.0, 0.0, 19.0, 0.0];
        }
        "li" => {
            s.display = Display::Block;
            s.margin = [4.0, 0.0, 4.0, 24.0];
        }
        "a" => {
            s.color = Color::BLUE;
        }
        "strong" | "b" => {
            s.bold = true;
        }
        "html" | "div" | "section" | "article" | "header" | "footer" | "main" | "nav" | "ul"
        | "ol" | "h3" | "h4" | "h5" | "h6" | "pre" | "blockquote" | "form" | "fieldset"
        | "figure" | "table" => {
            s.display = Display::Block;
        }
        _ => {}
    }
}

fn matches(doc: &Document, id: NodeId, selector: &Selector) -> bool {
    let Some(last) = selector.compounds.last() else {
        return false;
    };
    if !matches_compound(doc, id, last) {
        return false;
    }
    let mut ancestor = doc.nodes[id].parent;
    for compound in selector.compounds.iter().rev().skip(1) {
        loop {
            let Some(a) = ancestor else { return false };
            if matches_compound(doc, a, compound) {
                ancestor = doc.nodes[a].parent;
                break;
            }
            ancestor = doc.nodes[a].parent;
        }
    }
    true
}

fn matches_compound(doc: &Document, id: NodeId, c: &Compound) -> bool {
    let node = &doc.nodes[id];
    if node.is_text {
        return false;
    }
    if let Some(tag) = &c.tag {
        if !node.tag.eq_ignore_ascii_case(tag) {
            return false;
        }
    }
    if let Some(attr) = &c.id {
        if node.attr("id") != Some(attr.as_str()) {
            return false;
        }
    }
    for class in &c.classes {
        let has = node
            .attr("class")
            .map(|v| v.split_whitespace().any(|x| x == class))
            .unwrap_or(false);
        if !has {
            return false;
        }
    }
    true
}

fn apply(style: &mut Style, decl: &Declaration) {
    let value = decl.value.trim();
    match decl.property.as_str() {
        "display" => {
            style.display = match value {
                "none" => Display::None,
                "block" | "flex" | "grid" | "list-item" | "table" => Display::Block,
                _ => Display::Inline,
            };
        }
        "width" => style.width = parse_px(value),
        "height" => style.height = parse_px(value),
        "margin" => {
            let v = parse_box(value);
            style.margin = v;
        }
        "margin-top" => style.margin[0] = parse_px(value).unwrap_or(0.0),
        "margin-right" => style.margin[1] = parse_px(value).unwrap_or(0.0),
        "margin-bottom" => style.margin[2] = parse_px(value).unwrap_or(0.0),
        "margin-left" => style.margin[3] = parse_px(value).unwrap_or(0.0),
        "padding" => style.padding = parse_box(value),
        "padding-top" => style.padding[0] = parse_px(value).unwrap_or(0.0),
        "padding-right" => style.padding[1] = parse_px(value).unwrap_or(0.0),
        "padding-bottom" => style.padding[2] = parse_px(value).unwrap_or(0.0),
        "padding-left" => style.padding[3] = parse_px(value).unwrap_or(0.0),
        "color" => {
            if let Some(c) = parse_color(value) {
                style.color = c;
            }
        }
        "background" | "background-color" => {
            if let Some(c) = parse_color(value) {
                style.background = Some(c);
            }
        }
        "font-size" => {
            if let Some(px) = parse_px(value) {
                style.font_size = px;
            }
        }
        "font-weight" => style.bold = value == "bold" || value.parse::<u32>().unwrap_or(0) >= 600,
        "text-align" => style.inline_align = value == "center",
        _ => {}
    }
}

pub fn parse_px(value: &str) -> Option<f64> {
    let value = value.trim();
    let number = value
        .strip_suffix("px")
        .or_else(|| value.strip_suffix("pt"))
        .unwrap_or(value);
    number.parse::<f64>().ok()
}

fn parse_box(value: &str) -> [f64; 4] {
    let parts: Vec<f64> = value
        .split_whitespace()
        .map(|p| parse_px(p).unwrap_or(0.0))
        .collect();
    match parts.as_slice() {
        [a] => [*a; 4],
        [v, h] => [*v, *h, *v, *h],
        [t, h, b] => [*t, *h, *b, *h],
        [t, r, b, l] => [*t, *r, *b, *l],
        _ => [0.0; 4],
    }
}

fn parse_color(value: &str) -> Option<Color> {
    let v = value.trim().to_ascii_lowercase();
    let hex = |s: &str| u8::from_str_radix(s, 16).ok();
    match v.as_str() {
        "black" => return Some(Color::BLACK),
        "white" => return Some(Color::WHITE),
        "blue" => return Some(Color::BLUE),
        "red" => return Some(Color { r: 1.0, g: 0.0, b: 0.0, a: 1.0 }),
        "green" => return Some(Color { r: 0.0, g: 0.5, b: 0.0, a: 1.0 }),
        "gray" | "grey" => return Some(Color { r: 0.5, g: 0.5, b: 0.5, a: 1.0 }),
        _ => {}
    }
    if let Some(hexpart) = v.strip_prefix('#') {
        return match hexpart.len() {
            3 => Some(Color {
                r: hex(&hexpart[0..1])? as f64 / 15.0,
                g: hex(&hexpart[1..2])? as f64 / 15.0,
                b: hex(&hexpart[2..3])? as f64 / 15.0,
                a: 1.0,
            }),
            6 => Some(Color {
                r: hex(&hexpart[0..2])? as f64 / 255.0,
                g: hex(&hexpart[2..4])? as f64 / 255.0,
                b: hex(&hexpart[4..6])? as f64 / 255.0,
                a: 1.0,
            }),
            _ => None,
        };
    }
    if let Some(inner) = v.strip_prefix("rgb(").and_then(|s| s.strip_suffix(')')) {
        let parts: Vec<f64> = inner
            .split(',')
            .filter_map(|p| p.trim().parse::<f64>().ok())
            .collect();
        if let [r, g, b] = parts.as_slice() {
            return Some(Color {
                r: r / 255.0,
                g: g / 255.0,
                b: b / 255.0,
                a: 1.0,
            });
        }
    }
    None
}

/// Style lookup helper used by layout.
pub fn style_of(styles: &[Style], id: NodeId) -> &Style {
    &styles[id]
}

#[allow(dead_code)]
pub fn text_color_css(style: &Style) -> (f64, f64, f64) {
    (style.color.r, style.color.g, style.color.b)
}

#[allow(dead_code)]
fn _unused(_: HashMap<String, String>) {}
