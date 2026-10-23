//! Style computation through NetSurf libcss 0.9.2 (MIT).
//!
//! This module is the engine's parse + select + cascade layer. It owns a
//! hand-written FFI over the three static NetSurf libraries (`libcss`,
//! `libparserutils`, `libwapcaplet`) and maps the computed style into the
//! engine's [`crate::css::Style`]. `crate::css` keeps its legacy subset parser
//! and cascade for comparison and for the spike; nothing here changes it.
//!
//! Semantics taken from libcss itself, not assumed:
//!
//! - `css_color` is **AARRGGBB** (`src/parse/properties/utils.c`).
//! - `css_select_style` does not inherit: compose its result with the parent's
//!   composed style. The root element must **not** be composed (`css_select_style`
//!   already resolves its absolute values, and composing with a NULL parent
//!   returns `CSS_BADPARM`).
//! - `params.inline_style = true` is for declaration lists (`style=""`), not for
//!   `<style>` content, which carries rules.
//! - libcss ships **no** user-agent stylesheet: [`UA_STYLESHEET`] supplies the
//!   display defaults, margins and heading sizes the engine needs.
//! - Media features (`@media`) are resolved against the viewport passed to
//!   [`compute`], not against a fixed size.
//! - External author sheets (`<link rel="stylesheet">` and `@import` chains)
//!   are fetched by [`compute_with_subresources`]; the plain [`compute`] path
//!   never touches the network or the filesystem beyond the document itself.
//!   `@import` is resolved through `params.resolve` (libcss passes the sheet's
//!   own URL as the base) and registered with `css_stylesheet_register_import`
//!   after parsing, which is what keeps the imported rules before the
//!   importing sheet in the cascade.
//! - libcss borrows a reference to the strings returned by
//!   `node_name`/`node_classes`/`node_id` and frees them itself; the class array
//!   is allocated and freed by the client.

use std::collections::{HashMap, HashSet};
use std::ffi::{CStr, CString};
use std::fs::File;
use std::io::Read;
use std::mem;
use std::os::raw::{c_char, c_int, c_void};
use std::path::{Component, Path, PathBuf};
use std::ptr;
use std::slice;
use std::sync::{Mutex, OnceLock};
use std::time::Duration;

use url::Url;

use crate::css::{
    AlignItems, Color, Display, FlexBasis, FlexDirection, FlexItem, FlexStyle, FlexWrap,
    GridItem, GridStyle, JustifyContent, Style, TextAlign,
};
use crate::dom::{Document, Node, NodeId};

pub type CssError = c_int;
pub type CssFixed = i32;
pub type CssColor = u32;
type CssUnit = u32;

pub const CSS_OK: CssError = 0;
const CSS_INVALID: CssError = 3;
const CSS_NEEDDATA: CssError = 5;
const CSS_IMPORTS_PENDING: CssError = 8;

const CSS_STYLESHEET_PARAMS_VERSION_1: u32 = 1;
const CSS_LEVEL_DEFAULT: u32 = 2; // CSS_LEVEL_21
const CSS_ORIGIN_UA: u32 = 0;
const CSS_ORIGIN_AUTHOR: u32 = 2;
const CSS_SELECT_HANDLER_VERSION_1: u32 = 1;
const CSS_PSEUDO_ELEMENT_COUNT: usize = 5;

const CSS_MEDIA_SCREEN: u32 = 1 << 6;
const CSS_MEDIA_ORIENTATION_PORTRAIT: u32 = 0;
const CSS_MEDIA_ORIENTATION_LANDSCAPE: u32 = 1;

// css_display_e
const CSS_DISPLAY_INLINE: u8 = 0x01;
const CSS_DISPLAY_BLOCK: u8 = 0x02;
const CSS_DISPLAY_LIST_ITEM: u8 = 0x03;
const CSS_DISPLAY_RUN_IN: u8 = 0x04;
const CSS_DISPLAY_INLINE_BLOCK: u8 = 0x05;
const CSS_DISPLAY_TABLE: u8 = 0x06;
const CSS_DISPLAY_INLINE_TABLE: u8 = 0x07;
const CSS_DISPLAY_TABLE_ROW_GROUP: u8 = 0x08;
const CSS_DISPLAY_TABLE_HEADER_GROUP: u8 = 0x09;
const CSS_DISPLAY_TABLE_FOOTER_GROUP: u8 = 0x0a;
const CSS_DISPLAY_TABLE_ROW: u8 = 0x0b;
const CSS_DISPLAY_TABLE_COLUMN_GROUP: u8 = 0x0c;
const CSS_DISPLAY_TABLE_COLUMN: u8 = 0x0d;
const CSS_DISPLAY_TABLE_CELL: u8 = 0x0e;
const CSS_DISPLAY_TABLE_CAPTION: u8 = 0x0f;
const CSS_DISPLAY_NONE: u8 = 0x10;
const CSS_DISPLAY_FLEX: u8 = 0x11;
const CSS_DISPLAY_INLINE_FLEX: u8 = 0x12;
const CSS_DISPLAY_GRID: u8 = 0x13;
const CSS_DISPLAY_INLINE_GRID: u8 = 0x14;

// css_width_e / css_height_e / css_margin_e / css_padding_e
const CSS_WIDTH_SET: u8 = 0x01;
const CSS_HEIGHT_SET: u8 = 0x01;
const CSS_MARGIN_SET: u8 = 0x01;
const CSS_PADDING_SET: u8 = 0x01;

// css_color_e / css_background_color_e
const CSS_COLOR_COLOR: u8 = 0x01;
const CSS_BACKGROUND_COLOR_COLOR: u8 = 0x01;
const CSS_BACKGROUND_COLOR_CURRENT_COLOR: u8 = 0x02;

// css_background_image_e: NONE and IMAGE deliberately share the value 0x1;
// the caller tells them apart by whether the URL out-parameter is NULL
// (libcss 0.9.2, /usr/include/libcss/properties.h). INHERIT never reaches a
// composed style.
const CSS_BACKGROUND_IMAGE_INHERIT: u8 = 0x0;

// css_font_family_e
const CSS_FONT_FAMILY_SERIF: u8 = 0x1;
const CSS_FONT_FAMILY_SANS_SERIF: u8 = 0x2;
const CSS_FONT_FAMILY_CURSIVE: u8 = 0x3;
const CSS_FONT_FAMILY_FANTASY: u8 = 0x4;
const CSS_FONT_FAMILY_MONOSPACE: u8 = 0x5;

// css_font_size_e
const CSS_FONT_SIZE_DIMENSION: u8 = 0x0a;

// css_font_style_e
const CSS_FONT_STYLE_ITALIC: u8 = 0x2;
const CSS_FONT_STYLE_OBLIQUE: u8 = 0x3;

// css_font_weight_e
const CSS_FONT_WEIGHT_INHERIT: u8 = 0x0;
const CSS_FONT_WEIGHT_NORMAL: u8 = 0x1;
const CSS_FONT_WEIGHT_BOLD: u8 = 0x2;
const CSS_FONT_WEIGHT_BOLDER: u8 = 0x3;
const CSS_FONT_WEIGHT_LIGHTER: u8 = 0x4;
const CSS_FONT_WEIGHT_100: u8 = 0x5;
const CSS_FONT_WEIGHT_900: u8 = 0xd;

// css_line_height_e
const CSS_LINE_HEIGHT_INHERIT: u8 = 0x0;
const CSS_LINE_HEIGHT_NUMBER: u8 = 0x1;
const CSS_LINE_HEIGHT_DIMENSION: u8 = 0x2;

// css_text_align_e
const CSS_TEXT_ALIGN_LEFT: u8 = 0x2;
const CSS_TEXT_ALIGN_RIGHT: u8 = 0x3;
const CSS_TEXT_ALIGN_CENTER: u8 = 0x4;
const CSS_TEXT_ALIGN_JUSTIFY: u8 = 0x5;
const CSS_TEXT_ALIGN_LIBCSS_LEFT: u8 = 0x7;
const CSS_TEXT_ALIGN_LIBCSS_CENTER: u8 = 0x8;
const CSS_TEXT_ALIGN_LIBCSS_RIGHT: u8 = 0x9;

// css_letter_spacing_e (aliases css_column_gap_e)
const CSS_LETTER_SPACING_SET: u8 = 0x1;

// css_column_gap_e
const CSS_COLUMN_GAP_SET: u8 = 0x1;

// css_min_width_e / css_max_width_e
const CSS_MIN_WIDTH_SET: u8 = 0x1;
const CSS_MAX_WIDTH_SET: u8 = 0x1;

// css_align_items_e
const CSS_ALIGN_ITEMS_STRETCH: u8 = 0x1;
const CSS_ALIGN_ITEMS_FLEX_START: u8 = 0x2;
const CSS_ALIGN_ITEMS_FLEX_END: u8 = 0x3;
const CSS_ALIGN_ITEMS_CENTER: u8 = 0x4;
const CSS_ALIGN_ITEMS_BASELINE: u8 = 0x5;

// css_flex_basis_e
const CSS_FLEX_BASIS_SET: u8 = 0x1;
const CSS_FLEX_BASIS_AUTO: u8 = 0x2;
const CSS_FLEX_BASIS_CONTENT: u8 = 0x3;

// css_flex_direction_e
const CSS_FLEX_DIRECTION_ROW: u8 = 0x1;
const CSS_FLEX_DIRECTION_ROW_REVERSE: u8 = 0x2;
const CSS_FLEX_DIRECTION_COLUMN: u8 = 0x3;
const CSS_FLEX_DIRECTION_COLUMN_REVERSE: u8 = 0x4;

// css_flex_grow_e / css_flex_shrink_e
const CSS_FLEX_GROW_SET: u8 = 0x1;
const CSS_FLEX_SHRINK_SET: u8 = 0x1;

// css_flex_wrap_e
const CSS_FLEX_WRAP_NOWRAP: u8 = 0x1;
const CSS_FLEX_WRAP_WRAP: u8 = 0x2;
const CSS_FLEX_WRAP_WRAP_REVERSE: u8 = 0x3;

// css_justify_content_e
const CSS_JUSTIFY_CONTENT_FLEX_START: u8 = 0x1;
const CSS_JUSTIFY_CONTENT_FLEX_END: u8 = 0x2;
const CSS_JUSTIFY_CONTENT_CENTER: u8 = 0x3;
const CSS_JUSTIFY_CONTENT_SPACE_BETWEEN: u8 = 0x4;
const CSS_JUSTIFY_CONTENT_SPACE_AROUND: u8 = 0x5;
const CSS_JUSTIFY_CONTENT_SPACE_EVENLY: u8 = 0x6;

// css_unit
const CSS_UNIT_PCT: CssUnit = 0x15;

// css_property_e
const CSS_PROP_COLOR: u32 = 0x018;
const CSS_PROP_FONT_FAMILY: u32 = 0x024;
const CSS_PROP_QUOTES: u32 = 0x04a;
const CSS_PROP_VOICE_FAMILY: u32 = 0x05c;
const CSS_QUOTES_NONE: u8 = 0x1;

// css_node_data_action
const CSS_NODE_DELETED: u32 = 0;

const FIXED_SCALE: f64 = 1024.0;

/// Default font size for `medium` and for unresolved font sizes.
const DEFAULT_FONT_SIZE: f64 = 16.0;

/// Nominal viewport height used by `height`/`aspect-ratio` media features.
/// The engine's layout width is the real viewport dimension; its height grows
/// with the content, so media evaluation uses this stable reference.
const DEFAULT_VIEWPORT_HEIGHT: f64 = 600.0;

/// Minimal user-agent stylesheet.
///
/// libcss ships none, so display defaults, document margins and heading sizes
/// are the client's responsibility. Rules are applied at the UA origin, which
/// loses to every author rule in the cascade; this sheet therefore only fills
/// gaps. It deliberately stays small and CSS 2.1-only: the engine's block layout
/// understands block/inline/none, absolute margins and font sizes.
pub const UA_STYLESHEET: &str = r#"
html, body, div, p, section, article, header, footer, main, nav, form,
fieldset, figure, blockquote, pre, ul, ol, li, table,
h1, h2, h3, h4, h5, h6 {
    display: block;
    margin: 0;
}
head, script, style, title, meta, link {
    display: none;
}
body {
    margin: 8px;
}
p {
    margin: 16px 0;
}
blockquote {
    margin: 16px 40px;
}
ul, ol {
    margin: 16px 0;
    padding-left: 40px;
}
li {
    margin: 4px 0 4px 24px;
}
h1 { font-size: 32px; font-weight: bold; margin: 21px 0; }
h2 { font-size: 24px; font-weight: bold; margin: 19px 0; }
h3 { font-size: 19px; font-weight: bold; margin: 19px 0; }
h4 { font-size: 16px; font-weight: bold; margin: 21px 0; }
h5 { font-size: 13px; font-weight: bold; margin: 22px 0; }
h6 { font-size: 11px; font-weight: bold; margin: 25px 0; }
a:link {
    color: #0000ff;
}
strong, b {
    font-weight: bold;
}
em, i {
    font-style: italic;
}
pre, code, kbd, samp {
    font-family: monospace;
}
"#;

// ---------------------------------------------------------------------------
// Opaque types
// ---------------------------------------------------------------------------

#[repr(C)]
pub struct CssStylesheet {
    _private: [u8; 0],
}
#[repr(C)]
pub struct CssSelectCtx {
    _private: [u8; 0],
}
#[repr(C)]
pub struct CssComputedStyle {
    _private: [u8; 0],
}

/// `lwc_string` (libwapcaplet): the text lives just behind the header
/// (`(str) + 1` in `lwc_string_data`), so the layout has to be replicated.
#[repr(C)]
pub struct LwcString {
    prevptr: *mut *mut LwcString,
    next: *mut LwcString,
    len: usize,
    hash: u32,
    refcnt: u32,
    insensitive: *mut LwcString,
}

#[repr(C)]
pub struct CssQname {
    ns: *mut LwcString,
    name: *mut LwcString,
}

#[repr(C)]
pub struct CssUnitCtx {
    viewport_width: CssFixed,
    viewport_height: CssFixed,
    font_size_default: CssFixed,
    font_size_minimum: CssFixed,
    device_dpi: CssFixed,
    root_style: *const CssComputedStyle,
    pw: *mut c_void,
    measure: Option<unsafe extern "C" fn(*mut c_void, *const CssComputedStyle, CssUnit) -> CssFixed>,
}

#[repr(C)]
pub struct CssMediaResolution {
    value: CssFixed,
    unit: CssUnit,
}

#[repr(C)]
pub struct CssMedia {
    media_type: u32,
    width: CssFixed,
    height: CssFixed,
    aspect_ratio: CssFixed,
    orientation: u32,
    resolution: CssMediaResolution,
    scan: u32,
    grid: CssFixed,
    update: u32,
    overflow_block: u32,
    overflow_inline: u32,
    color: CssFixed,
    color_index: CssFixed,
    monochrome: CssFixed,
    inverted_colors: CssFixed,
    prefers_color_scheme: *mut LwcString,
    pointer: u32,
    any_pointer: u32,
    hover: u32,
    any_hover: u32,
    light_level: u32,
    scripting: u32,
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct CssHintLength {
    value: CssFixed,
    unit: CssUnit,
}

#[repr(C)]
pub union CssHintData {
    pub color: CssColor,
    pub fixed: CssFixed,
    pub length: CssHintLength,
    pub string: *mut LwcString,
    pub strings: *mut *mut LwcString,
}

#[repr(C)]
pub struct CssHint {
    pub data: CssHintData,
    pub prop: u32,
    pub status: u8,
}

#[repr(C)]
pub struct CssSelectResults {
    pub styles: [*mut CssComputedStyle; CSS_PSEUDO_ELEMENT_COUNT],
}

type ResolveFn =
    unsafe extern "C" fn(*mut c_void, *const c_char, *mut LwcString, *mut *mut LwcString) -> CssError;
type ImportFn = unsafe extern "C" fn(*mut c_void, *mut CssStylesheet, *mut LwcString) -> CssError;
type ColorFn = unsafe extern "C" fn(*mut c_void, *mut LwcString, *mut CssColor) -> CssError;
type FontFn = unsafe extern "C" fn(*mut c_void, *mut LwcString, *mut c_void) -> CssError;

#[repr(C)]
pub struct CssStylesheetParams {
    params_version: u32,
    level: u32,
    charset: *const c_char,
    url: *const c_char,
    title: *const c_char,
    allow_quirks: bool,
    inline_style: bool,
    resolve: Option<ResolveFn>,
    resolve_pw: *mut c_void,
    import: Option<ImportFn>,
    import_pw: *mut c_void,
    color: Option<ColorFn>,
    color_pw: *mut c_void,
    font: Option<FontFn>,
    font_pw: *mut c_void,
}

// ---------------------------------------------------------------------------
// css_select_handler (exact order from libcss/select.h)
// ---------------------------------------------------------------------------

type NodeNameFn =
    unsafe extern "C" fn(*mut c_void, *mut c_void, *mut CssQname) -> CssError;
type NodeClassesFn =
    unsafe extern "C" fn(*mut c_void, *mut c_void, *mut *mut *mut LwcString, *mut u32) -> CssError;
type NodeIdFn =
    unsafe extern "C" fn(*mut c_void, *mut c_void, *mut *mut LwcString) -> CssError;
type NamedNodeFn = unsafe extern "C" fn(
    *mut c_void,
    *mut c_void,
    *const CssQname,
    *mut *mut c_void,
) -> CssError;
type ParentNodeFn = unsafe extern "C" fn(*mut c_void, *mut c_void, *mut *mut c_void) -> CssError;
type HasNameFn =
    unsafe extern "C" fn(*mut c_void, *mut c_void, *const CssQname, *mut bool) -> CssError;
type HasClassFn =
    unsafe extern "C" fn(*mut c_void, *mut c_void, *mut LwcString, *mut bool) -> CssError;
type HasAttrFn =
    unsafe extern "C" fn(*mut c_void, *mut c_void, *const CssQname, *mut bool) -> CssError;
type HasAttrValueFn = unsafe extern "C" fn(
    *mut c_void,
    *mut c_void,
    *const CssQname,
    *mut LwcString,
    *mut bool,
) -> CssError;
type IsFn = unsafe extern "C" fn(*mut c_void, *mut c_void, *mut bool) -> CssError;
type CountSiblingsFn =
    unsafe extern "C" fn(*mut c_void, *mut c_void, bool, bool, *mut i32) -> CssError;
type IsLangFn =
    unsafe extern "C" fn(*mut c_void, *mut c_void, *mut LwcString, *mut bool) -> CssError;
type PresentationalHintFn = unsafe extern "C" fn(
    *mut c_void,
    *mut c_void,
    *mut u32,
    *mut *mut CssHint,
) -> CssError;
type UaDefaultFn = unsafe extern "C" fn(*mut c_void, u32, *mut CssHint) -> CssError;
type SetNodeDataFn = unsafe extern "C" fn(*mut c_void, *mut c_void, *mut c_void) -> CssError;
type GetNodeDataFn =
    unsafe extern "C" fn(*mut c_void, *mut c_void, *mut *mut c_void) -> CssError;

#[repr(C)]
pub struct CssSelectHandler {
    pub handler_version: u32,
    pub node_name: Option<NodeNameFn>,
    pub node_classes: Option<NodeClassesFn>,
    pub node_id: Option<NodeIdFn>,
    pub named_ancestor_node: Option<NamedNodeFn>,
    pub named_parent_node: Option<NamedNodeFn>,
    pub named_sibling_node: Option<NamedNodeFn>,
    pub named_generic_sibling_node: Option<NamedNodeFn>,
    pub parent_node: Option<ParentNodeFn>,
    pub sibling_node: Option<ParentNodeFn>,
    pub node_has_name: Option<HasNameFn>,
    pub node_has_class: Option<HasClassFn>,
    pub node_has_id: Option<HasClassFn>,
    pub node_has_attribute: Option<HasAttrFn>,
    pub node_has_attribute_equal: Option<HasAttrValueFn>,
    pub node_has_attribute_dashmatch: Option<HasAttrValueFn>,
    pub node_has_attribute_includes: Option<HasAttrValueFn>,
    pub node_has_attribute_prefix: Option<HasAttrValueFn>,
    pub node_has_attribute_suffix: Option<HasAttrValueFn>,
    pub node_has_attribute_substring: Option<HasAttrValueFn>,
    pub node_is_root: Option<IsFn>,
    pub node_count_siblings: Option<CountSiblingsFn>,
    pub node_is_empty: Option<IsFn>,
    pub node_is_link: Option<IsFn>,
    pub node_is_visited: Option<IsFn>,
    pub node_is_hover: Option<IsFn>,
    pub node_is_active: Option<IsFn>,
    pub node_is_focus: Option<IsFn>,
    pub node_is_enabled: Option<IsFn>,
    pub node_is_disabled: Option<IsFn>,
    pub node_is_checked: Option<IsFn>,
    pub node_is_target: Option<IsFn>,
    pub node_is_lang: Option<IsLangFn>,
    pub node_presentational_hint: Option<PresentationalHintFn>,
    pub ua_default_for_property: Option<UaDefaultFn>,
    pub set_libcss_node_data: Option<SetNodeDataFn>,
    pub get_libcss_node_data: Option<GetNodeDataFn>,
}

// ---------------------------------------------------------------------------
// External declarations
// ---------------------------------------------------------------------------

#[link(name = "css")]
extern "C" {
    fn css_stylesheet_create(
        params: *const CssStylesheetParams,
        stylesheet: *mut *mut CssStylesheet,
    ) -> CssError;
    fn css_stylesheet_destroy(sheet: *mut CssStylesheet) -> CssError;
    fn css_stylesheet_append_data(
        sheet: *mut CssStylesheet,
        data: *const u8,
        len: usize,
    ) -> CssError;
    fn css_stylesheet_data_done(sheet: *mut CssStylesheet) -> CssError;
    fn css_stylesheet_next_pending_import(
        parent: *mut CssStylesheet,
        url: *mut *mut LwcString,
    ) -> CssError;
    fn css_stylesheet_register_import(
        parent: *mut CssStylesheet,
        import: *mut CssStylesheet,
    ) -> CssError;

    fn css_select_ctx_create(ctx: *mut *mut CssSelectCtx) -> CssError;
    fn css_select_ctx_destroy(ctx: *mut CssSelectCtx) -> CssError;
    fn css_select_ctx_append_sheet(
        ctx: *mut CssSelectCtx,
        sheet: *const CssStylesheet,
        origin: u32,
        media: *const c_char,
    ) -> CssError;
    fn css_select_style(
        ctx: *mut CssSelectCtx,
        node: *mut c_void,
        unit_ctx: *const CssUnitCtx,
        media: *const CssMedia,
        inline_style: *const CssStylesheet,
        handler: *mut CssSelectHandler,
        pw: *mut c_void,
        result: *mut *mut CssSelectResults,
    ) -> CssError;
    fn css_select_results_destroy(results: *mut CssSelectResults) -> CssError;

    fn css_computed_style_destroy(style: *mut CssComputedStyle) -> CssError;
    fn css_computed_style_compose(
        parent: *const CssComputedStyle,
        child: *const CssComputedStyle,
        unit_ctx: *const CssUnitCtx,
        result: *mut *mut CssComputedStyle,
    ) -> CssError;

    fn css_libcss_node_data_handler(
        handler: *mut CssSelectHandler,
        action: u32,
        pw: *mut c_void,
        node: *mut c_void,
        clone_node: *mut c_void,
        libcss_node_data: *mut c_void,
    ) -> CssError;

    fn css_error_to_string(error: CssError) -> *const c_char;

    fn css_unit_len2css_px(
        style: *const CssComputedStyle,
        ctx: *const CssUnitCtx,
        length: CssFixed,
        unit: CssUnit,
    ) -> CssFixed;

    fn css_computed_display(style: *const CssComputedStyle, root: bool) -> u8;
    fn css_computed_width(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_height(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_margin_top(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_margin_right(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_margin_bottom(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_margin_left(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_padding_top(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_padding_right(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_padding_bottom(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_padding_left(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_color(style: *const CssComputedStyle, color: *mut CssColor) -> u8;
    fn css_computed_background_color(style: *const CssComputedStyle, color: *mut CssColor) -> u8;
    fn css_computed_background_image(
        style: *const CssComputedStyle,
        url: *mut *mut LwcString,
    ) -> u8;
    fn css_computed_font_family(
        style: *const CssComputedStyle,
        names: *mut *mut *mut LwcString,
    ) -> u8;
    fn css_computed_font_size(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_font_weight(style: *const CssComputedStyle) -> u8;
    fn css_computed_font_style(style: *const CssComputedStyle) -> u8;
    fn css_computed_line_height(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_text_align(style: *const CssComputedStyle) -> u8;
    fn css_computed_letter_spacing(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_min_width(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_max_width(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_column_gap(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_align_items(style: *const CssComputedStyle) -> u8;
    fn css_computed_flex_basis(
        style: *const CssComputedStyle,
        length: *mut CssFixed,
        unit: *mut CssUnit,
    ) -> u8;
    fn css_computed_flex_direction(style: *const CssComputedStyle) -> u8;
    fn css_computed_flex_grow(style: *const CssComputedStyle, number: *mut CssFixed) -> u8;
    fn css_computed_flex_shrink(style: *const CssComputedStyle, number: *mut CssFixed) -> u8;
    fn css_computed_flex_wrap(style: *const CssComputedStyle) -> u8;
    fn css_computed_justify_content(style: *const CssComputedStyle) -> u8;
}

#[link(name = "wapcaplet")]
extern "C" {
    fn lwc_intern_string(s: *const c_char, len: usize, out: *mut *mut LwcString) -> c_int;
    fn lwc_string_destroy(s: *mut LwcString);
}

// ---------------------------------------------------------------------------
// String and error helpers
// ---------------------------------------------------------------------------

/// Intern a string in the libwapcaplet dictionary and hand the reference to the
/// caller (libcss releases it when it finishes with the node).
unsafe fn intern(s: &str) -> *mut LwcString {
    let mut out: *mut LwcString = ptr::null_mut();
    let err = lwc_intern_string(s.as_ptr() as *const c_char, s.len(), &mut out);
    assert!(
        err == 0 && !out.is_null(),
        "libwapcaplet: could not intern {s:?}"
    );
    out
}

/// Bytes of an `lwc_string` (equivalent to `lwc_string_data` + `_length`).
unsafe fn lwc_bytes<'a>(s: *mut LwcString) -> &'a [u8] {
    let data = (s as *const u8).add(mem::size_of::<LwcString>());
    slice::from_raw_parts(data, (*s).len)
}

unsafe fn lwc_ref(s: *mut LwcString) -> *mut LwcString {
    (*s).refcnt += 1;
    s
}

/// Mirror of libwapcaplet's `lwc_string_unref` macro.
unsafe fn lwc_unref(s: *mut LwcString) {
    if s.is_null() {
        return;
    }
    (*s).refcnt -= 1;
    if (*s).refcnt == 0 || ((*s).refcnt == 1 && (*s).insensitive == s) {
        lwc_string_destroy(s);
    }
}

fn check(err: CssError, what: &str) {
    if err != CSS_OK {
        let text = unsafe {
            let p = css_error_to_string(err);
            if p.is_null() {
                String::from("?")
            } else {
                CStr::from_ptr(p).to_string_lossy().into_owned()
            }
        };
        panic!("libcss: {what} failed ({err}: {text})");
    }
}

fn fixed_to_f64(v: CssFixed) -> f64 {
    v as f64 / FIXED_SCALE
}

fn to_fixed(v: f64) -> CssFixed {
    (v * FIXED_SCALE) as CssFixed
}

// ---------------------------------------------------------------------------
// Selection context (the `pw` libcss hands back to the callbacks)
// ---------------------------------------------------------------------------

struct SelectCtx {
    /// The document outlives `compute`; it is cleared when the call returns.
    doc: *const Document,
    /// Class arrays are allocated by us: libcss 0.9.2 frees the strings but not
    /// the array, so it is recovered during teardown.
    class_arrays: Vec<(*mut *mut LwcString, usize)>,
    /// `libcss_node_data` per node, released through the handler at the end.
    node_data: Vec<(NodeId, *mut c_void)>,
}

impl SelectCtx {
    fn new(doc: &Document) -> SelectCtx {
        SelectCtx {
            doc: doc as *const Document,
            class_arrays: Vec::new(),
            node_data: Vec::new(),
        }
    }

    fn doc(&self) -> &Document {
        unsafe { &*self.doc }
    }

    fn node(&self, id: NodeId) -> &Node {
        &self.doc().nodes[id]
    }

    fn attr(&self, id: NodeId, name: &str) -> Option<&str> {
        self.node(id).attr(name)
    }

    /// Does the node's tag match the selector's qualified name?
    unsafe fn tag_matches(&self, id: NodeId, qname: *const CssQname) -> bool {
        if qname.is_null() || (*qname).name.is_null() {
            return false;
        }
        let name = lwc_bytes((*qname).name);
        if name == b"*" {
            return true;
        }
        self.node(id).tag.as_bytes().eq_ignore_ascii_case(name)
    }

    fn prev_element(&self, id: NodeId) -> Option<NodeId> {
        let doc = self.doc();
        let parent = doc.nodes[id].parent?;
        let siblings = &doc.nodes[parent].children;
        let pos = siblings.iter().position(|&c| c == id)?;
        siblings[..pos]
            .iter()
            .rev()
            .copied()
            .find(|&c| !doc.nodes[c].is_text)
    }

    unsafe fn prev_matching(&self, id: NodeId, qname: *const CssQname) -> Option<NodeId> {
        let doc = self.doc();
        let parent = doc.nodes[id].parent?;
        let siblings = &doc.nodes[parent].children;
        let pos = siblings.iter().position(|&c| c == id)?;
        siblings[..pos]
            .iter()
            .rev()
            .copied()
            .find(|&c| !doc.nodes[c].is_text && self.tag_matches(c, qname))
    }

    fn ancestor_matching(&self, id: NodeId, qname: *const CssQname) -> Option<NodeId> {
        let doc = self.doc();
        let mut cur = doc.nodes[id].parent;
        while let Some(p) = cur {
            if p != doc.root && unsafe { self.tag_matches(p, qname) } {
                return Some(p);
            }
            cur = doc.nodes[p].parent;
        }
        None
    }

    fn class_list(&self, id: NodeId) -> Vec<String> {
        self.attr(id, "class")
            .map(|v| v.split_whitespace().map(|s| s.to_string()).collect())
            .unwrap_or_default()
    }
}

unsafe fn ctx_from(pw: *mut c_void) -> &'static mut SelectCtx {
    &mut *(pw as *mut SelectCtx)
}

fn node_ptr(id: NodeId) -> *mut c_void {
    (id + 1) as *mut c_void
}

fn node_id(node: *mut c_void) -> NodeId {
    node as usize - 1
}

// ---------------------------------------------------------------------------
// Selector callbacks
// ---------------------------------------------------------------------------

unsafe extern "C" fn cb_node_name(
    pw: *mut c_void,
    node: *mut c_void,
    qname: *mut CssQname,
) -> CssError {
    let ctx = ctx_from(pw);
    let id = node_id(node);
    (*qname).ns = ptr::null_mut();
    (*qname).name = intern(&ctx.node(id).tag);
    CSS_OK
}

unsafe extern "C" fn cb_node_classes(
    pw: *mut c_void,
    node: *mut c_void,
    classes: *mut *mut *mut LwcString,
    n_classes: *mut u32,
) -> CssError {
    let ctx = ctx_from(pw);
    let list = ctx.class_list(node_id(node));
    if list.is_empty() {
        *classes = ptr::null_mut();
        *n_classes = 0;
        return CSS_OK;
    }
    let boxed: Box<[*mut LwcString]> = list.iter().map(|c| intern(c)).collect();
    let len = boxed.len();
    let raw = Box::into_raw(boxed) as *mut *mut LwcString;
    ctx.class_arrays.push((raw, len));
    *classes = raw;
    *n_classes = len as u32;
    CSS_OK
}

unsafe extern "C" fn cb_node_id(
    pw: *mut c_void,
    node: *mut c_void,
    id_out: *mut *mut LwcString,
) -> CssError {
    let ctx = ctx_from(pw);
    *id_out = match ctx.attr(node_id(node), "id") {
        Some(id) if !id.is_empty() => intern(id),
        _ => ptr::null_mut(),
    };
    CSS_OK
}

unsafe extern "C" fn cb_named_ancestor_node(
    pw: *mut c_void,
    node: *mut c_void,
    qname: *const CssQname,
    ancestor: *mut *mut c_void,
) -> CssError {
    let ctx = ctx_from(pw);
    *ancestor = match ctx.ancestor_matching(node_id(node), qname) {
        Some(id) => node_ptr(id),
        None => ptr::null_mut(),
    };
    CSS_OK
}

unsafe extern "C" fn cb_named_parent_node(
    pw: *mut c_void,
    node: *mut c_void,
    qname: *const CssQname,
    parent: *mut *mut c_void,
) -> CssError {
    let ctx = ctx_from(pw);
    let id = node_id(node);
    let doc = ctx.doc();
    *parent = match doc.nodes[id].parent {
        Some(p) if p != doc.root && ctx.tag_matches(p, qname) => node_ptr(p),
        _ => ptr::null_mut(),
    };
    CSS_OK
}

unsafe extern "C" fn cb_named_sibling_node(
    pw: *mut c_void,
    node: *mut c_void,
    qname: *const CssQname,
    sibling: *mut *mut c_void,
) -> CssError {
    let ctx = ctx_from(pw);
    *sibling = match ctx.prev_matching(node_id(node), qname) {
        Some(id) => node_ptr(id),
        None => ptr::null_mut(),
    };
    CSS_OK
}

unsafe extern "C" fn cb_parent_node(
    pw: *mut c_void,
    node: *mut c_void,
    parent: *mut *mut c_void,
) -> CssError {
    let ctx = ctx_from(pw);
    let id = node_id(node);
    let doc = ctx.doc();
    // The synthetic #document node counts as "no parent": this makes `:root`
    // match <html>, as in NetSurf's DOM.
    *parent = match doc.nodes[id].parent {
        Some(p) if p != doc.root => node_ptr(p),
        _ => ptr::null_mut(),
    };
    CSS_OK
}

unsafe extern "C" fn cb_sibling_node(
    pw: *mut c_void,
    node: *mut c_void,
    sibling: *mut *mut c_void,
) -> CssError {
    let ctx = ctx_from(pw);
    *sibling = match ctx.prev_element(node_id(node)) {
        Some(id) => node_ptr(id),
        None => ptr::null_mut(),
    };
    CSS_OK
}

unsafe extern "C" fn cb_node_has_name(
    pw: *mut c_void,
    node: *mut c_void,
    qname: *const CssQname,
    matched: *mut bool,
) -> CssError {
    let ctx = ctx_from(pw);
    *matched = ctx.tag_matches(node_id(node), qname);
    CSS_OK
}

unsafe extern "C" fn cb_node_has_class(
    pw: *mut c_void,
    node: *mut c_void,
    name: *mut LwcString,
    matched: *mut bool,
) -> CssError {
    let ctx = ctx_from(pw);
    let want = lwc_bytes(name);
    *matched = ctx
        .class_list(node_id(node))
        .iter()
        .any(|c| c.as_bytes() == want);
    CSS_OK
}

unsafe extern "C" fn cb_node_has_id(
    pw: *mut c_void,
    node: *mut c_void,
    name: *mut LwcString,
    matched: *mut bool,
) -> CssError {
    let ctx = ctx_from(pw);
    let want = lwc_bytes(name);
    *matched = ctx
        .attr(node_id(node), "id")
        .map(|id| id.as_bytes() == want)
        .unwrap_or(false);
    CSS_OK
}

fn attribute(id: NodeId, ctx: &SelectCtx, qname: *const CssQname) -> Option<String> {
    unsafe {
        if qname.is_null() || (*qname).name.is_null() {
            return None;
        }
        let name = lwc_bytes((*qname).name);
        // DOM attribute names arrive lowercased.
        ctx.node(id)
            .attrs
            .iter()
            .find(|(k, _)| k.as_bytes().eq_ignore_ascii_case(name))
            .map(|(_, v)| v.clone())
    }
}

unsafe extern "C" fn cb_node_has_attribute(
    pw: *mut c_void,
    node: *mut c_void,
    qname: *const CssQname,
    matched: *mut bool,
) -> CssError {
    let ctx = ctx_from(pw);
    *matched = attribute(node_id(node), ctx, qname).is_some();
    CSS_OK
}

type AttrMatcher = fn(actual: &str, wanted: &str) -> bool;

unsafe fn has_attribute_value(
    pw: *mut c_void,
    node: *mut c_void,
    qname: *const CssQname,
    value: *mut LwcString,
    matched: *mut bool,
    matcher: AttrMatcher,
) -> CssError {
    let ctx = ctx_from(pw);
    let wanted = String::from_utf8_lossy(lwc_bytes(value)).into_owned();
    *matched = attribute(node_id(node), ctx, qname)
        .map(|actual| matcher(&actual, &wanted))
        .unwrap_or(false);
    CSS_OK
}

fn attr_equal(actual: &str, wanted: &str) -> bool {
    actual == wanted
}
fn attr_dashmatch(actual: &str, wanted: &str) -> bool {
    actual == wanted
        || (actual.len() > wanted.len()
            && actual.starts_with(wanted)
            && actual.as_bytes()[wanted.len()] == b'-')
}
fn attr_includes(actual: &str, wanted: &str) -> bool {
    actual.split_whitespace().any(|v| v == wanted)
}
fn attr_prefix(actual: &str, wanted: &str) -> bool {
    actual.starts_with(wanted)
}
fn attr_suffix(actual: &str, wanted: &str) -> bool {
    actual.ends_with(wanted)
}
fn attr_substring(actual: &str, wanted: &str) -> bool {
    actual.contains(wanted)
}

macro_rules! attr_value_cb {
    ($name:ident, $matcher:expr) => {
        unsafe extern "C" fn $name(
            pw: *mut c_void,
            node: *mut c_void,
            qname: *const CssQname,
            value: *mut LwcString,
            matched: *mut bool,
        ) -> CssError {
            has_attribute_value(pw, node, qname, value, matched, $matcher)
        }
    };
}

attr_value_cb!(cb_node_has_attribute_equal, attr_equal);
attr_value_cb!(cb_node_has_attribute_dashmatch, attr_dashmatch);
attr_value_cb!(cb_node_has_attribute_includes, attr_includes);
attr_value_cb!(cb_node_has_attribute_prefix, attr_prefix);
attr_value_cb!(cb_node_has_attribute_suffix, attr_suffix);
attr_value_cb!(cb_node_has_attribute_substring, attr_substring);

unsafe extern "C" fn cb_node_is_root(
    pw: *mut c_void,
    node: *mut c_void,
    matched: *mut bool,
) -> CssError {
    let ctx = ctx_from(pw);
    let doc = ctx.doc();
    *matched = match doc.nodes[node_id(node)].parent {
        None => true,
        Some(p) => p == doc.root,
    };
    CSS_OK
}

unsafe extern "C" fn cb_node_count_siblings(
    pw: *mut c_void,
    node: *mut c_void,
    same_name: bool,
    after: bool,
    count: *mut i32,
) -> CssError {
    let ctx = ctx_from(pw);
    let doc = ctx.doc();
    let id = node_id(node);
    *count = 0;
    let Some(parent) = doc.nodes[id].parent else {
        return CSS_OK;
    };
    let Some(pos) = doc.nodes[parent].children.iter().position(|&c| c == id) else {
        return CSS_OK;
    };
    let siblings = &doc.nodes[parent].children;
    let name = doc.nodes[id].tag.as_str();
    let mut n = 0i32;
    let range: &[NodeId] = if after {
        &siblings[pos + 1..]
    } else {
        &siblings[..pos]
    };
    for &c in range {
        let child = &doc.nodes[c];
        if child.is_text {
            continue; // :nth-child counts elements
        }
        if same_name && !child.tag.eq_ignore_ascii_case(name) {
            continue;
        }
        n += 1;
    }
    *count = n;
    CSS_OK
}

unsafe extern "C" fn cb_node_is_empty(
    pw: *mut c_void,
    node: *mut c_void,
    matched: *mut bool,
) -> CssError {
    let ctx = ctx_from(pw);
    *matched = ctx.node(node_id(node)).children.is_empty();
    CSS_OK
}

unsafe extern "C" fn cb_node_is_link(
    pw: *mut c_void,
    node: *mut c_void,
    matched: *mut bool,
) -> CssError {
    let ctx = ctx_from(pw);
    let id = node_id(node);
    *matched = ctx.node(id).tag.eq_ignore_ascii_case("a") && ctx.attr(id, "href").is_some();
    CSS_OK
}

unsafe extern "C" fn cb_false(
    _pw: *mut c_void,
    _node: *mut c_void,
    matched: *mut bool,
) -> CssError {
    *matched = false;
    CSS_OK
}

unsafe extern "C" fn cb_node_is_lang(
    pw: *mut c_void,
    node: *mut c_void,
    lang: *mut LwcString,
    matched: *mut bool,
) -> CssError {
    let ctx = ctx_from(pw);
    let doc = ctx.doc();
    let want = lwc_bytes(lang);
    *matched = false;
    let mut cur = Some(node_id(node));
    while let Some(id) = cur {
        if let Some(actual) = ctx.attr(id, "lang").or_else(|| ctx.attr(id, "xml:lang")) {
            let actual = actual.as_bytes();
            *matched = actual.eq_ignore_ascii_case(want)
                || (actual.len() < want.len()
                    && want[..actual.len()].eq_ignore_ascii_case(actual)
                    && want.get(actual.len()) == Some(&b'-'));
            break;
        }
        cur = doc.nodes[id].parent;
    }
    CSS_OK
}

unsafe extern "C" fn cb_node_presentational_hint(
    _pw: *mut c_void,
    _node: *mut c_void,
    nhints: *mut u32,
    hints: *mut *mut CssHint,
) -> CssError {
    *nhints = 0;
    *hints = ptr::null_mut();
    CSS_OK
}

unsafe extern "C" fn cb_ua_default_for_property(
    _pw: *mut c_void,
    property: u32,
    hint: *mut CssHint,
) -> CssError {
    match property {
        CSS_PROP_COLOR => {
            // AARRGGBB: opaque black (the value NetSurf uses).
            (*hint).data.color = 0xff00_0000;
            (*hint).status = CSS_COLOR_COLOR;
        }
        CSS_PROP_FONT_FAMILY => {
            (*hint).data.strings = ptr::null_mut();
            (*hint).status = CSS_FONT_FAMILY_SANS_SERIF;
        }
        CSS_PROP_QUOTES => {
            (*hint).data.strings = ptr::null_mut();
            (*hint).status = CSS_QUOTES_NONE;
        }
        CSS_PROP_VOICE_FAMILY => {
            (*hint).data.strings = ptr::null_mut();
            (*hint).status = 0;
        }
        _ => return CSS_INVALID,
    }
    CSS_OK
}

unsafe extern "C" fn cb_set_libcss_node_data(
    pw: *mut c_void,
    node: *mut c_void,
    data: *mut c_void,
) -> CssError {
    let ctx = ctx_from(pw);
    let id = node_id(node);
    if data.is_null() {
        ctx.node_data.retain(|(n, _)| *n != id);
    } else {
        ctx.node_data.push((id, data));
    }
    CSS_OK
}

unsafe extern "C" fn cb_get_libcss_node_data(
    pw: *mut c_void,
    node: *mut c_void,
    data: *mut *mut c_void,
) -> CssError {
    let ctx = ctx_from(pw);
    let id = node_id(node);
    *data = ctx
        .node_data
        .iter()
        .rev()
        .find(|(n, _)| *n == id)
        .map(|(_, d)| *d)
        .unwrap_or(ptr::null_mut());
    CSS_OK
}

unsafe extern "C" fn cb_resolve(
    _pw: *mut c_void,
    base: *const c_char,
    rel: *mut LwcString,
    abs: *mut *mut LwcString,
) -> CssError {
    // libcss passes the sheet's own URL as `base`, which is why every sheet is
    // created with the URL its relative `@import`s resolve against.
    if rel.is_null() {
        *abs = ptr::null_mut();
        return CSS_OK;
    }
    let rel_text = String::from_utf8_lossy(lwc_bytes(rel)).into_owned();
    let base_text = if base.is_null() {
        String::new()
    } else {
        CStr::from_ptr(base).to_string_lossy().into_owned()
    };
    match join_url(&base_text, &rel_text) {
        Some(absolute) => *abs = intern(&absolute),
        // Unresolvable: hand the reference back unchanged. The import then has
        // no fetchable URL and is registered as an empty sheet.
        None => *abs = lwc_ref(rel),
    }
    CSS_OK
}

fn select_handler() -> CssSelectHandler {
    CssSelectHandler {
        handler_version: CSS_SELECT_HANDLER_VERSION_1,
        node_name: Some(cb_node_name),
        node_classes: Some(cb_node_classes),
        node_id: Some(cb_node_id),
        named_ancestor_node: Some(cb_named_ancestor_node),
        named_parent_node: Some(cb_named_parent_node),
        named_sibling_node: Some(cb_named_sibling_node),
        named_generic_sibling_node: Some(cb_named_sibling_node),
        parent_node: Some(cb_parent_node),
        sibling_node: Some(cb_sibling_node),
        node_has_name: Some(cb_node_has_name),
        node_has_class: Some(cb_node_has_class),
        node_has_id: Some(cb_node_has_id),
        node_has_attribute: Some(cb_node_has_attribute),
        node_has_attribute_equal: Some(cb_node_has_attribute_equal),
        node_has_attribute_dashmatch: Some(cb_node_has_attribute_dashmatch),
        node_has_attribute_includes: Some(cb_node_has_attribute_includes),
        node_has_attribute_prefix: Some(cb_node_has_attribute_prefix),
        node_has_attribute_suffix: Some(cb_node_has_attribute_suffix),
        node_has_attribute_substring: Some(cb_node_has_attribute_substring),
        node_is_root: Some(cb_node_is_root),
        node_count_siblings: Some(cb_node_count_siblings),
        node_is_empty: Some(cb_node_is_empty),
        node_is_link: Some(cb_node_is_link),
        node_is_visited: Some(cb_false),
        node_is_hover: Some(cb_false),
        node_is_active: Some(cb_false),
        node_is_focus: Some(cb_false),
        node_is_enabled: Some(cb_false),
        node_is_disabled: Some(cb_false),
        node_is_checked: Some(cb_false),
        node_is_target: Some(cb_false),
        node_is_lang: Some(cb_node_is_lang),
        node_presentational_hint: Some(cb_node_presentational_hint),
        ua_default_for_property: Some(cb_ua_default_for_property),
        set_libcss_node_data: Some(cb_set_libcss_node_data),
        get_libcss_node_data: Some(cb_get_libcss_node_data),
    }
}

// ---------------------------------------------------------------------------
// Stylesheets
// ---------------------------------------------------------------------------

unsafe fn create_empty_sheet(url: &[u8]) -> *mut CssStylesheet {
    let url = CString::new(url).unwrap_or_default();
    let mut params: CssStylesheetParams = mem::zeroed();
    params.params_version = CSS_STYLESHEET_PARAMS_VERSION_1;
    params.level = CSS_LEVEL_DEFAULT;
    params.url = url.as_ptr();
    params.resolve = Some(cb_resolve);
    let mut sheet: *mut CssStylesheet = ptr::null_mut();
    check(
        css_stylesheet_create(&params, &mut sheet),
        "css_stylesheet_create (import)",
    );
    check(
        css_stylesheet_data_done(sheet),
        "css_stylesheet_data_done (import)",
    );
    sheet
}

/// Register the sheets libcss is waiting for on this parent's `@import` list.
///
/// Each pending URL is fetched (and its own imports with it), parsed, and
/// registered, so imported rules keep their place *before* the importing
/// sheet's rules in the cascade. Without a loader — the legacy [`compute`] path
/// — an empty sheet is registered instead, as libcss requires.
unsafe fn finish_imports(parent: *mut CssStylesheet, mut loader: Option<&mut Loader>) {
    loop {
        let mut url: *mut LwcString = ptr::null_mut();
        if css_stylesheet_next_pending_import(parent, &mut url) != CSS_OK || url.is_null() {
            return;
        }
        let url_text = String::from_utf8_lossy(lwc_bytes(url)).into_owned();
        let child = match loader.as_deref_mut() {
            Some(loader) => match loader.load(&url_text) {
                Some(css) => parse_fetched_sheet(&css, &url_text, loader),
                None => create_empty_sheet(url_text.as_bytes()),
            },
            None => create_empty_sheet(url_text.as_bytes()),
        };
        check(
            css_stylesheet_register_import(parent, child),
            "css_stylesheet_register_import",
        );
        lwc_unref(url);
    }
}

/// Parse a fetched sheet, marking its URL as in progress while it is parsed so
/// an `@import` cycle terminates instead of recursing forever.
unsafe fn parse_fetched_sheet(
    css: &str,
    url: &str,
    loader: &mut Loader,
) -> *mut CssStylesheet {
    if !loader.enter(url) {
        return create_empty_sheet(url.as_bytes());
    }
    let sheet = parse_sheet_with_imports(css, url, false, Some(&mut *loader));
    loader.exit(url);
    sheet
}

unsafe fn parse_sheet(css: &str, url: &str, inline: bool) -> *mut CssStylesheet {
    parse_sheet_with_imports(css, url, inline, None)
}

unsafe fn parse_sheet_with_imports(
    css: &str,
    url: &str,
    inline: bool,
    mut loader: Option<&mut Loader>,
) -> *mut CssStylesheet {
    // libcss knows `column-gap` but neither the `gap` shorthand nor
    // `row-gap`, so the gaps are rewritten into the carriers the engine
    // reads them from before the sheet is parsed.
    let rewritten = if inline {
        rewrite_gap_declarations(css)
    } else {
        rewrite_gap_carriers(css)
    };
    let url = CString::new(url).expect("stylesheet url");
    let mut params: CssStylesheetParams = mem::zeroed();
    params.params_version = CSS_STYLESHEET_PARAMS_VERSION_1;
    params.level = CSS_LEVEL_DEFAULT;
    params.url = url.as_ptr();
    params.resolve = Some(cb_resolve);
    // `<style>` content is a rule list; `inline_style = true` is reserved for
    // `style=""` declaration lists.
    params.inline_style = inline;

    let mut sheet: *mut CssStylesheet = ptr::null_mut();
    check(css_stylesheet_create(&params, &mut sheet), "css_stylesheet_create");

    let mut err = css_stylesheet_append_data(sheet, rewritten.as_ptr(), rewritten.len());
    if err == CSS_NEEDDATA {
        err = CSS_OK;
    }
    check(err, "css_stylesheet_append_data");

    let err = css_stylesheet_data_done(sheet);
    if err == CSS_IMPORTS_PENDING {
        finish_imports(sheet, loader.as_deref_mut());
    } else {
        check(err, "css_stylesheet_data_done");
    }
    sheet
}

// ---------------------------------------------------------------------------
// Gap carriers (S7)
// ---------------------------------------------------------------------------

/// Rewrites the gap declarations of a stylesheet so that the engine can read
/// them, and drops the author's own `min-width`/`max-width`.
///
/// libcss 0.9.2 knows `column-gap` but neither the `gap` shorthand nor
/// `row-gap`, so the gaps of a real page would be lost. The engine reads the
/// row gap from `min-width` and the column gap from `max-width`: both are
/// non-inherited lengths that no other part of the engine looks at, so the
/// author's own minimum/maximum widths (which the engine ignores anyway) are
/// dropped to leave the carriers free.
///
/// The rest of the stylesheet is copied verbatim.
fn rewrite_gap_carriers(css: &str) -> String {
    let mut out = String::with_capacity(css.len());
    rewrite_rule_list(css, &mut out);
    out
}

/// The same rewrite for a declaration list: the `style=""` attribute.
fn rewrite_gap_declarations(css: &str) -> String {
    let mut out = String::with_capacity(css.len());
    rewrite_declarations(css, &mut out);
    out
}

/// Copies a rule list, rewriting the declaration blocks it contains.
fn rewrite_rule_list(src: &str, out: &mut String) {
    let bytes = src.as_bytes();
    let mut i = 0usize;
    'rules: while i < bytes.len() {
        let trivia = i;
        i = skip_trivia(bytes, i);
        out.push_str(&src[trivia..i]);
        if i >= bytes.len() {
            break;
        }
        if bytes[i] == b'}' {
            // A stray closing brace: copy it and leave this block.
            out.push('}');
            i += 1;
            continue;
        }

        // The prelude of the rule (or of the at-rule) ends at the first
        // top-level `{` or `;`.
        let mut j = i;
        let block_start;
        loop {
            if j >= bytes.len() {
                out.push_str(&src[i..]);
                return;
            }
            match bytes[j] {
                b'/' if j + 1 < bytes.len() && bytes[j + 1] == b'*' => {
                    j = skip_comment(bytes, j);
                    continue;
                }
                b'"' | b'\'' => {
                    j = skip_string(bytes, j);
                    continue;
                }
                b'{' => {
                    block_start = j;
                    break;
                }
                b';' => {
                    // An at-rule without a block (`@import`, `@charset`):
                    // copy it verbatim.
                    out.push_str(&src[i..=j]);
                    i = j + 1;
                    continue 'rules;
                }
                b'}' => {
                    out.push_str(&src[i..j]);
                    i = j;
                    continue 'rules;
                }
                _ => {}
            }
            j += 1;
        }

        let block_end = find_block_end(bytes, block_start);
        let prelude = &src[i..block_start];
        let body = &src[block_start + 1..block_end];
        out.push_str(prelude);
        out.push('{');
        if at_rule_holds_rules(prelude) {
            rewrite_rule_list(body, out);
        } else {
            rewrite_declarations(body, out);
        }
        out.push('}');
        i = block_end + 1;
    }
}

/// Does this block hold a rule list instead of a declaration list?
///
/// A style rule always holds declarations; an at-rule holds rules when it
/// groups other rules (`@media`, `@supports`, `@keyframes`, ...).
fn at_rule_holds_rules(prelude: &str) -> bool {
    let trimmed = prelude.trim_start();
    if !trimmed.starts_with('@') {
        return false;
    }
    let name: String = trimmed[1..]
        .chars()
        .take_while(|c| c.is_ascii_alphanumeric() || *c == '-')
        .collect::<String>()
        .to_ascii_lowercase();
    matches!(
        name.as_str(),
        "media" | "supports" | "layer" | "container" | "document" | "scope"
    ) || name.ends_with("-keyframes")
}

/// Copies a declaration list, rewriting the gap declarations.
fn rewrite_declarations(src: &str, out: &mut String) {
    let bytes = src.as_bytes();
    let mut segment = 0usize;
    let mut j = 0usize;
    while j < bytes.len() {
        match bytes[j] {
            b'/' if j + 1 < bytes.len() && bytes[j + 1] == b'*' => {
                j = skip_comment(bytes, j);
                continue;
            }
            b'"' | b'\'' => {
                j = skip_string(bytes, j);
                continue;
            }
            b';' => {
                rewrite_declaration(&src[segment..j], out);
                out.push(';');
                j += 1;
                segment = j;
                continue;
            }
            _ => {}
        }
        j += 1;
    }
    rewrite_declaration(&src[segment..], out);
}

/// Rewrites one declaration: the gap properties become the carriers and the
/// author's minimum/maximum widths are dropped.
fn rewrite_declaration(decl: &str, out: &mut String) {
    let Some((property, value)) = split_declaration(decl) else {
        out.push_str(decl);
        return;
    };
    match property.trim().to_ascii_lowercase().as_str() {
        "gap" | "row-gap" | "column-gap" => {
            let name = property.trim().to_ascii_lowercase();
            let (value, important) = split_important(value);
            let parts: Vec<&str> = value.split_whitespace().collect();
            // `gap: <row> <column>`; the longhands only own their own axis.
            let (row, column) = match (name.as_str(), parts.as_slice()) {
                ("row-gap", [one]) => (Some(*one), None),
                ("column-gap", [one]) => (None, Some(*one)),
                (_, [one]) => (Some(*one), Some(*one)),
                (_, [row, column]) => (Some(*row), Some(*column)),
                _ => (None, None),
            };
            if let Some(row) = row {
                write_carrier(out, "min-width", row, important);
                if column.is_some() {
                    out.push(';');
                }
            }
            if let Some(column) = column {
                write_carrier(out, "max-width", column, important);
            }
        }
        // The engine has no minimum/maximum sizing: these declarations are
        // inert, and the carriers need the room.
        "min-width" | "max-width" => {}
        _ => out.push_str(decl),
    }
}

/// Writes one carrier declaration.
fn write_carrier(out: &mut String, property: &str, value: &str, important: bool) {
    out.push_str(property);
    out.push(':');
    out.push_str(value);
    if important {
        out.push_str("!important");
    }
}

/// Splits a declaration into its property and its value at the first top-level
/// `:`.
fn split_declaration(decl: &str) -> Option<(&str, &str)> {
    let bytes = decl.as_bytes();
    let mut j = 0usize;
    while j < bytes.len() {
        match bytes[j] {
            b'/' if j + 1 < bytes.len() && bytes[j + 1] == b'*' => {
                j = skip_comment(bytes, j);
                continue;
            }
            b'"' | b'\'' => {
                j = skip_string(bytes, j);
                continue;
            }
            b':' => return Some((&decl[..j], &decl[j + 1..])),
            _ => {}
        }
        j += 1;
    }
    None
}

/// Splits the `!important` flag off a declaration value.
fn split_important(value: &str) -> (&str, bool) {
    let trimmed = value.trim();
    if trimmed.len() >= 10 && trimmed[trimmed.len() - 10..].eq_ignore_ascii_case("!important") {
        (trimmed[..trimmed.len() - 10].trim(), true)
    } else {
        (trimmed, false)
    }
}

/// The index just after the string that starts at `i`.
fn skip_string(bytes: &[u8], i: usize) -> usize {
    let quote = bytes[i];
    let mut j = i + 1;
    while j < bytes.len() {
        if bytes[j] == b'\\' {
            j += 2;
            continue;
        }
        if bytes[j] == b'\n' {
            return j;
        }
        if bytes[j] == quote {
            return j + 1;
        }
        j += 1;
    }
    j
}

/// The index just after the comment that starts at `i`.
fn skip_comment(bytes: &[u8], i: usize) -> usize {
    let mut j = i + 2;
    while j + 1 < bytes.len() {
        if bytes[j] == b'*' && bytes[j + 1] == b'/' {
            return j + 2;
        }
        j += 1;
    }
    bytes.len()
}

/// Skips whitespace and comments.
fn skip_trivia(bytes: &[u8], i: usize) -> usize {
    let mut j = i;
    loop {
        while j < bytes.len() && bytes[j].is_ascii_whitespace() {
            j += 1;
        }
        if j + 1 < bytes.len() && bytes[j] == b'/' && bytes[j + 1] == b'*' {
            j = skip_comment(bytes, j);
            continue;
        }
        return j;
    }
}

/// The index of the `}` that closes the block opened at `i`.
fn find_block_end(bytes: &[u8], i: usize) -> usize {
    let mut depth = 0usize;
    let mut j = i;
    while j < bytes.len() {
        match bytes[j] {
            b'/' if j + 1 < bytes.len() && bytes[j + 1] == b'*' => {
                j = skip_comment(bytes, j);
                continue;
            }
            b'"' | b'\'' => {
                j = skip_string(bytes, j);
                continue;
            }
            b'{' => depth += 1,
            b'}' => {
                depth = depth.saturating_sub(1);
                if depth == 0 {
                    return j;
                }
            }
            _ => {}
        }
        j += 1;
    }
    bytes.len()
}

// ---------------------------------------------------------------------------
// Grid properties (S8)
// ---------------------------------------------------------------------------

/// libcss 0.9.2 knows `display: grid` but none of the grid placement
/// properties, so they are read here, straight from the author stylesheets.
///
/// The cascade is the author's document order with specificity and
/// `!important`, resolved per element: every style rule whose selector
/// matches the element contributes its grid declarations, and so does the
/// element's own `style=""` attribute (which wins over plain author rules but
/// loses to author `!important`, per CSS). Only three properties are read —
/// `grid-template-columns`, `grid-column` and `grid-row` — because those are
/// all the minimal grid lays out with.
fn apply_grid_styles(doc: &Document, sheets: &[&str], out: &mut [Style]) {
    #[derive(Clone)]
    struct Bid {
        important: bool,
        inline: bool,
        specificity: (u32, u32, u32),
        order: u32,
        columns: Option<String>,
        col_span: Option<u32>,
        row_span: Option<u32>,
    }

    let mut best: Vec<Option<Bid>> = vec![None; out.len()];
    let mut order = 0u32;

    let mut consider = |id: NodeId,
                        bid: Bid,
                        best: &mut Vec<Option<Bid>>| {
        if id >= best.len() {
            return;
        }
        let wins = match &best[id] {
            None => true,
            Some(prev) => (bid.important, bid.inline, bid.specificity, bid.order)
                > (prev.important, prev.inline, prev.specificity, prev.order),
        };
        if wins {
            // A rule only carries the grid properties it declares: keep the
            // winner's values, filling the gaps from the previous best.
            let merged = match &best[id] {
                Some(prev) => Bid {
                    columns: bid.columns.or_else(|| prev.columns.clone()),
                    col_span: bid.col_span.or(prev.col_span),
                    row_span: bid.row_span.or(prev.row_span),
                    ..bid
                },
                None => bid,
            };
            best[id] = Some(merged);
        }
    };

    for css in sheets {
        for_each_style_rule(css, &mut |prelude, body| {
            order = order.saturating_add(1);
            let decls = grid_declarations(body);
            if decls.is_empty() {
                return;
            }
            for selector in prelude.split(',') {
                let selector = selector.trim();
                if selector.is_empty() {
                    continue;
                }
                let specificity = selector_specificity(selector);
                for id in doc.query_selector_all(selector) {
                    consider(
                        id,
                        Bid {
                            important: decls.important,
                            inline: false,
                            specificity,
                            order,
                            columns: decls.columns.clone(),
                            col_span: decls.col_span,
                            row_span: decls.row_span,
                        },
                        &mut best,
                    );
                }
            }
        });
    }

    // The `style=""` attribute: an inline declaration list, parsed like the
    // inline sheets above but matched to its own element.
    for id in 0..doc.nodes.len() {
        let Some(decls) = doc.nodes[id].attr("style") else {
            continue;
        };
        if decls.trim().is_empty() {
            continue;
        }
        let mut inline = String::new();
        rewrite_declarations(decls, &mut inline);
        let decls = grid_declarations(&inline);
        if decls.is_empty() {
            continue;
        }
        consider(
            id,
            Bid {
                important: decls.important,
                inline: true,
                specificity: (0, 0, 0),
                order: u32::MAX,
                columns: decls.columns.clone(),
                col_span: decls.col_span,
                row_span: decls.row_span,
            },
            &mut best,
        );
    }

    for (id, bid) in best.into_iter().enumerate() {
        let Some(bid) = bid else { continue };
        if let Some(span) = bid.col_span {
            out[id].grid_item.col_span = span;
        }
        if let Some(span) = bid.row_span {
            out[id].grid_item.row_span = span;
        }
        if out[id].grid.is_some() {
            let tracks = bid
                .columns
                .as_deref()
                .and_then(crate::css::parse_grid_tracks)
                .unwrap_or_else(|| vec![crate::css::GridTrack::Auto]);
            out[id].grid.as_mut().expect("grid container").tracks = tracks;
        }
    }
    // A grid container without any track declaration lays out in the single
    // implicit column.
    for style in out.iter_mut() {
        if let Some(grid) = style.grid.as_mut() {
            if grid.tracks.is_empty() {
                grid.tracks.push(crate::css::GridTrack::Auto);
            }
        }
    }
}

/// The grid declarations of one declaration block: the raw column text plus
/// the spans. `important` is set when any of them carries `!important`.
#[derive(Default)]
struct GridDecls {
    important: bool,
    columns: Option<String>,
    col_span: Option<u32>,
    row_span: Option<u32>,
}

impl GridDecls {
    fn is_empty(&self) -> bool {
        self.columns.is_none() && self.col_span.is_none() && self.row_span.is_none()
    }
}

fn grid_declarations(body: &str) -> GridDecls {
    let mut decls = GridDecls::default();
    for part in split_declarations(body) {
        let Some((property, value)) = split_declaration(&part) else {
            continue;
        };
        match property.trim().to_ascii_lowercase().as_str() {
            "grid-template-columns" => {
                let (value, important) = split_important(value);
                decls.columns = Some(value.trim().to_string());
                decls.important |= important;
            }
            "grid-column" => {
                let (value, important) = split_important(value);
                if let Some(span) = crate::css::parse_grid_span(value) {
                    decls.col_span = Some(span);
                    decls.important |= important;
                }
            }
            "grid-row" => {
                let (value, important) = split_important(value);
                if let Some(span) = crate::css::parse_grid_span(value) {
                    decls.row_span = Some(span);
                    decls.important |= important;
                }
            }
            _ => {}
        }
    }
    decls
}

/// Calls `f` with the selector prelude and the declaration body of every
/// style rule in `css`, recursing into rule-holding at-rules (`@media`,
/// `@supports`, ...) the same way the gap rewrite does.
fn for_each_style_rule(css: &str, f: &mut impl FnMut(&str, &str)) {
    let bytes = css.as_bytes();
    let mut i = 0usize;
    'rules: while i < bytes.len() {
        i = skip_trivia(bytes, i);
        if i >= bytes.len() {
            return;
        }
        if bytes[i] == b'}' {
            i += 1;
            continue;
        }
        let mut j = i;
        let block_start;
        loop {
            if j >= bytes.len() {
                return;
            }
            match bytes[j] {
                b'/' if j + 1 < bytes.len() && bytes[j + 1] == b'*' => {
                    j = skip_comment(bytes, j);
                    continue;
                }
                b'"' | b'\'' => {
                    j = skip_string(bytes, j);
                    continue;
                }
                b'{' => {
                    block_start = j;
                    break;
                }
                b';' => {
                    // An at-rule without a block (`@import`, `@charset`).
                    i = j + 1;
                    continue 'rules;
                }
                b'}' => {
                    i = j;
                    continue 'rules;
                }
                _ => {}
            }
            j += 1;
        }
        let block_end = find_block_end(bytes, block_start);
        let prelude = &css[i..block_start];
        let body = &css[block_start + 1..block_end];
        if at_rule_holds_rules(prelude) {
            for_each_style_rule(body, f);
        } else if !prelude.trim_start().starts_with('@') {
            f(prelude, body);
        }
        i = block_end + 1;
    }
}

/// Splits a declaration block on its top-level semicolons, keeping quoted
/// strings and comments intact.
fn split_declarations(body: &str) -> Vec<String> {
    let bytes = body.as_bytes();
    let mut parts = Vec::new();
    let mut segment = 0usize;
    let mut j = 0usize;
    while j < bytes.len() {
        match bytes[j] {
            b'/' if j + 1 < bytes.len() && bytes[j + 1] == b'*' => {
                j = skip_comment(bytes, j);
                continue;
            }
            b'"' | b'\'' => {
                j = skip_string(bytes, j);
                continue;
            }
            b';' => {
                parts.push(body[segment..j].to_string());
                j += 1;
                segment = j;
                continue;
            }
            _ => {}
        }
        j += 1;
    }
    parts.push(body[segment..].to_string());
    parts
}

/// The specificity of a selector, approximated the way the cascade needs it:
/// ids (`#a`), classes/attributes/pseudo-classes (`.b`, `[c]`, `:d`), and
/// type selectors (`div`). It only orders competing grid declarations.
fn selector_specificity(selector: &str) -> (u32, u32, u32) {
    let mut ids = 0u32;
    let mut classes = 0u32;
    let mut tags = 0u32;
    // Pseudo-elements (`::before`) count as types, not classes.
    let plain = selector.replace("::", "  ");
    for chunk in plain.split(|c| matches!(c, ' ' | '>' | '+' | '~' | ',')) {
        let chunk = chunk.trim();
        if chunk.is_empty() || chunk == "*" {
            continue;
        }
        ids += chunk.bytes().filter(|&b| b == b'#').count() as u32;
        classes += chunk.bytes().filter(|&b| b == b'.').count() as u32;
        classes += chunk.bytes().filter(|&b| b == b'[').count() as u32;
        classes += chunk.bytes().filter(|&b| b == b':').count() as u32;
        // A leading tag name: the chunk starts with a type selector unless
        // it opens with an id, class, attribute, pseudo-class or universal.
        if let Some(first) = chunk.chars().next() {
            if first.is_ascii_alphabetic() {
                tags += 1;
            }
        }
    }
    (ids, classes, tags)
}

// ---------------------------------------------------------------------------
// Computed style -> css::Style
// ---------------------------------------------------------------------------

fn rgba(c: CssColor) -> Color {
    // css_color is AARRGGBB (see libcss/src/parse/properties/utils.c).
    let a = ((c >> 24) & 0xff) as f64 / 255.0;
    let r = ((c >> 16) & 0xff) as f64 / 255.0;
    let g = ((c >> 8) & 0xff) as f64 / 255.0;
    let b = (c & 0xff) as f64 / 255.0;
    Color { r, g, b, a }
}

type LengthGetter = unsafe extern "C" fn(
    *const CssComputedStyle,
    *mut CssFixed,
    *mut CssUnit,
) -> u8;

/// Absolute length in px; `None` for auto, inherit and percentages (the engine
/// has no containing block here to resolve those against, same as `css.rs`).
unsafe fn length_px(
    style: *const CssComputedStyle,
    unit_ctx: &CssUnitCtx,
    get: LengthGetter,
    set_status: u8,
) -> Option<f64> {
    let mut len: CssFixed = 0;
    let mut unit: CssUnit = 0;
    if get(style, &mut len, &mut unit) != set_status || unit == CSS_UNIT_PCT {
        return None;
    }
    Some(fixed_to_f64(css_unit_len2css_px(style, unit_ctx, len, unit)))
}

/// The flex container properties of a computed style.
///
/// The gap values do not come from libcss: it knows `column-gap` but not the
/// `gap` shorthand nor `row-gap`, so the stylesheet is rewritten before it is
/// parsed and the gaps travel in `min-width` (row gap) and `max-width`
/// (column gap). See [`rewrite_gap_carriers`].
unsafe fn flex_style(style: *const CssComputedStyle, unit_ctx: &CssUnitCtx) -> FlexStyle {
    FlexStyle {
        direction: match css_computed_flex_direction(style) {
            CSS_FLEX_DIRECTION_COLUMN | CSS_FLEX_DIRECTION_COLUMN_REVERSE => FlexDirection::Column,
            // `row-reverse` is not part of the minimal flexbox: it lays out
            // like `row`.
            CSS_FLEX_DIRECTION_ROW | CSS_FLEX_DIRECTION_ROW_REVERSE => FlexDirection::Row,
            _ => FlexDirection::Row,
        },
        wrap: match css_computed_flex_wrap(style) {
            CSS_FLEX_WRAP_WRAP | CSS_FLEX_WRAP_WRAP_REVERSE => FlexWrap::Wrap,
            CSS_FLEX_WRAP_NOWRAP => FlexWrap::NoWrap,
            _ => FlexWrap::NoWrap,
        },
        justify: match css_computed_justify_content(style) {
            CSS_JUSTIFY_CONTENT_CENTER => JustifyContent::Center,
            CSS_JUSTIFY_CONTENT_FLEX_END => JustifyContent::FlexEnd,
            CSS_JUSTIFY_CONTENT_SPACE_BETWEEN => JustifyContent::SpaceBetween,
            CSS_JUSTIFY_CONTENT_SPACE_AROUND => JustifyContent::SpaceAround,
            CSS_JUSTIFY_CONTENT_SPACE_EVENLY => JustifyContent::SpaceEvenly,
            CSS_JUSTIFY_CONTENT_FLEX_START => JustifyContent::FlexStart,
            _ => JustifyContent::FlexStart,
        },
        align: match css_computed_align_items(style) {
            CSS_ALIGN_ITEMS_FLEX_START => AlignItems::FlexStart,
            CSS_ALIGN_ITEMS_CENTER => AlignItems::Center,
            CSS_ALIGN_ITEMS_FLEX_END => AlignItems::FlexEnd,
            // `baseline` is not part of the minimal flexbox.
            CSS_ALIGN_ITEMS_BASELINE => AlignItems::FlexStart,
            CSS_ALIGN_ITEMS_STRETCH => AlignItems::Stretch,
            _ => AlignItems::Stretch,
        },
        row_gap: length_px(style, unit_ctx, css_computed_min_width, CSS_MIN_WIDTH_SET)
            .unwrap_or(0.0),
        // The carrier holds the column gap; libcss' own `column-gap` is the
        // fallback for the declarations the rewrite did not reach.
        column_gap: length_px(style, unit_ctx, css_computed_max_width, CSS_MAX_WIDTH_SET)
            .or_else(|| length_px(style, unit_ctx, css_computed_column_gap, CSS_COLUMN_GAP_SET))
            .unwrap_or(0.0),
    }
}

/// The flex item properties of a computed style: `flex-grow`, `flex-shrink`
/// and `flex-basis`.
unsafe fn flex_item(style: *const CssComputedStyle, unit_ctx: &CssUnitCtx) -> FlexItem {
    let mut number: CssFixed = 0;
    let grow = if css_computed_flex_grow(style, &mut number) == CSS_FLEX_GROW_SET {
        fixed_to_f64(number)
    } else {
        0.0
    };
    let shrink = if css_computed_flex_shrink(style, &mut number) == CSS_FLEX_SHRINK_SET {
        fixed_to_f64(number)
    } else {
        1.0
    };

    let mut len: CssFixed = 0;
    let mut unit: CssUnit = 0;
    let basis = match css_computed_flex_basis(style, &mut len, &mut unit) {
        CSS_FLEX_BASIS_SET => {
            if unit == CSS_UNIT_PCT {
                FlexBasis::Percent(fixed_to_f64(len))
            } else {
                FlexBasis::Px(fixed_to_f64(css_unit_len2css_px(style, unit_ctx, len, unit)))
            }
        }
        CSS_FLEX_BASIS_CONTENT => FlexBasis::Content,
        CSS_FLEX_BASIS_AUTO => FlexBasis::Auto,
        _ => FlexBasis::Auto,
    };

    FlexItem {
        grow,
        shrink,
        basis,
    }
}

unsafe fn map_style(
    style: *const CssComputedStyle,
    unit_ctx: &CssUnitCtx,
    is_root: bool,
) -> Style {
    let mut out = Style::default();

    let display = css_computed_display(style, is_root);
    out.display = match display {
        CSS_DISPLAY_NONE => Display::None,
        CSS_DISPLAY_BLOCK
        | CSS_DISPLAY_LIST_ITEM
        | CSS_DISPLAY_TABLE
        | CSS_DISPLAY_GRID
        | CSS_DISPLAY_INLINE_GRID => Display::Block,
        // A flex container is a block-level box: the engine has no
        // `Display::Flex` variant (the CSS spike matches `Display`
        // exhaustively), so the container keeps `Display::Block` and carries
        // its flex properties in `Style::flex`.
        CSS_DISPLAY_FLEX | CSS_DISPLAY_INLINE_FLEX => Display::Block,
        // The block flow engine has no inline-block/inline-table layout.
        // Inline-level displays stay inline runs, matching the legacy cascade's
        // mapping (only block/list-item/table/flex/grid become block boxes).
        CSS_DISPLAY_INLINE
        | CSS_DISPLAY_RUN_IN
        | CSS_DISPLAY_INLINE_BLOCK
        | CSS_DISPLAY_INLINE_TABLE
        | CSS_DISPLAY_TABLE_ROW_GROUP
        | CSS_DISPLAY_TABLE_HEADER_GROUP
        | CSS_DISPLAY_TABLE_FOOTER_GROUP
        | CSS_DISPLAY_TABLE_ROW
        | CSS_DISPLAY_TABLE_COLUMN_GROUP
        | CSS_DISPLAY_TABLE_COLUMN
        | CSS_DISPLAY_TABLE_CELL
        | CSS_DISPLAY_TABLE_CAPTION => Display::Inline,
        // CSS_DISPLAY_INHERIT is resolved by compose before mapping.
        _ => Display::Inline,
    };

    if matches!(display, CSS_DISPLAY_FLEX | CSS_DISPLAY_INLINE_FLEX) {
        out.flex = Some(flex_style(style, unit_ctx));
    }
    if matches!(display, CSS_DISPLAY_GRID | CSS_DISPLAY_INLINE_GRID) {
        // Like flex, a grid container keeps `Display::Block` and is marked
        // here; the track list arrives in `apply_grid_styles`, which reads
        // the properties libcss does not know.
        out.display = Display::Block;
        out.grid = Some(GridStyle::default());
    }
    out.grid_item = GridItem::default();
    // The gaps travel in the min-width (row) and max-width (column)
    // carriers for every element, not just flex containers: grid reuses the
    // same values instead of re-parsing `gap`. See `flex_style`.
    out.row_gap =
        length_px(style, unit_ctx, css_computed_min_width, CSS_MIN_WIDTH_SET).unwrap_or(0.0);
    out.column_gap = length_px(style, unit_ctx, css_computed_max_width, CSS_MAX_WIDTH_SET)
        .or_else(|| length_px(style, unit_ctx, css_computed_column_gap, CSS_COLUMN_GAP_SET))
        .unwrap_or(0.0);
    out.flex_item = flex_item(style, unit_ctx);

    out.width = length_px(style, unit_ctx, css_computed_width, CSS_WIDTH_SET);
    out.height = length_px(style, unit_ctx, css_computed_height, CSS_HEIGHT_SET);

    let margins = [
        css_computed_margin_top,
        css_computed_margin_right,
        css_computed_margin_bottom,
        css_computed_margin_left,
    ];
    for (i, get) in margins.into_iter().enumerate() {
        out.margin[i] = length_px(style, unit_ctx, get, CSS_MARGIN_SET).unwrap_or(0.0);
    }
    let paddings = [
        css_computed_padding_top,
        css_computed_padding_right,
        css_computed_padding_bottom,
        css_computed_padding_left,
    ];
    for (i, get) in paddings.into_iter().enumerate() {
        out.padding[i] = length_px(style, unit_ctx, get, CSS_PADDING_SET).unwrap_or(0.0);
    }

    let mut color: CssColor = 0;
    if css_computed_color(style, &mut color) == CSS_COLOR_COLOR {
        out.color = rgba(color);
    }

    let mut background: CssColor = 0;
    out.background = match css_computed_background_color(style, &mut background) {
        CSS_BACKGROUND_COLOR_COLOR => {
            let c = rgba(background);
            if c.a > 0.0 {
                Some(c)
            } else {
                None
            }
        }
        CSS_BACKGROUND_COLOR_CURRENT_COLOR => Some(out.color),
        _ => None,
    };

    // libcss resolved the URL against the sheet's own URL at parse time (the
    // `resolve` callback; see `cb_resolve`), so for a local or http page it
    // is already absolute — like the font-family string, it is borrowed from
    // the computed style. NONE and IMAGE share the return value, so the NULL
    // out-parameter is what says "no image".
    let mut background_url: *mut LwcString = ptr::null_mut();
    if css_computed_background_image(style, &mut background_url) != CSS_BACKGROUND_IMAGE_INHERIT
        && !background_url.is_null()
    {
        out.background_image = Some(String::from_utf8_lossy(lwc_bytes(background_url)).into_owned());
    }

    let mut families: *mut *mut LwcString = ptr::null_mut();
    let family_type = css_computed_font_family(style, &mut families);
    out.font_family = if !families.is_null() && !(*families).is_null() {
        String::from_utf8_lossy(lwc_bytes(*families)).into_owned()
    } else {
        match family_type {
            CSS_FONT_FAMILY_SERIF => "Serif",
            CSS_FONT_FAMILY_CURSIVE => "Cursive",
            CSS_FONT_FAMILY_FANTASY => "Fantasy",
            CSS_FONT_FAMILY_MONOSPACE => "Monospace",
            CSS_FONT_FAMILY_SANS_SERIF => "Sans",
            _ => "Sans",
        }
        .to_string()
    };

    if let Some(px) =
        length_px(style, unit_ctx, css_computed_font_size, CSS_FONT_SIZE_DIMENSION)
    {
        out.font_size = px;
    }

    out.font_weight = match css_computed_font_weight(style) {
        CSS_FONT_WEIGHT_NORMAL | CSS_FONT_WEIGHT_INHERIT => 400,
        CSS_FONT_WEIGHT_BOLD | CSS_FONT_WEIGHT_BOLDER => 700,
        CSS_FONT_WEIGHT_LIGHTER => 300,
        v @ CSS_FONT_WEIGHT_100..=CSS_FONT_WEIGHT_900 => (v as u32 - 4) * 100,
        _ => 400,
    };

    out.italic = matches!(
        css_computed_font_style(style),
        CSS_FONT_STYLE_ITALIC | CSS_FONT_STYLE_OBLIQUE
    );

    let mut lh_len: CssFixed = 0;
    let mut lh_unit: CssUnit = 0;
    out.line_height = match css_computed_line_height(style, &mut lh_len, &mut lh_unit) {
        CSS_LINE_HEIGHT_NUMBER => fixed_to_f64(lh_len),
        CSS_LINE_HEIGHT_DIMENSION => {
            let px = if lh_unit == CSS_UNIT_PCT {
                None
            } else {
                Some(fixed_to_f64(css_unit_len2css_px(style, unit_ctx, lh_len, lh_unit)))
            };
            match px {
                Some(px) if out.font_size > 0.0 => px / out.font_size,
                _ => 1.2,
            }
        }
        CSS_LINE_HEIGHT_INHERIT => 1.2,
        _ => 1.2, // normal
    };

    out.text_align = match css_computed_text_align(style) {
        CSS_TEXT_ALIGN_CENTER | CSS_TEXT_ALIGN_LIBCSS_CENTER => TextAlign::Center,
        CSS_TEXT_ALIGN_RIGHT | CSS_TEXT_ALIGN_LIBCSS_RIGHT => TextAlign::Right,
        CSS_TEXT_ALIGN_JUSTIFY => TextAlign::Justify,
        CSS_TEXT_ALIGN_LEFT | CSS_TEXT_ALIGN_LIBCSS_LEFT => TextAlign::Left,
        _ => TextAlign::Left, // inherit / default already resolved by compose
    };

    out.letter_spacing =
        length_px(style, unit_ctx, css_computed_letter_spacing, CSS_LETTER_SPACING_SET)
            .unwrap_or(0.0);

    // Keep layout/paint's coarse flags in sync with the typography fields.
    out.bold = out.font_weight >= 600;
    out.inline_align = out.text_align == TextAlign::Center;

    out
}

// ---------------------------------------------------------------------------
// External stylesheets (S2)
// ---------------------------------------------------------------------------

/// Default cap for one external stylesheet, before charset decoding.
pub const MAX_SHEET_BYTES: usize = 2 * 1024 * 1024;

/// Default total timeout for fetching one external stylesheet.
pub const SHEET_TIMEOUT: Duration = Duration::from_secs(8);

/// Caps applied to one external stylesheet.
#[derive(Debug, Clone)]
pub struct SheetLimits {
    /// Largest accepted body. A larger sheet is skipped, never truncated.
    pub max_bytes: usize,
    /// Largest accepted transfer time for one sheet.
    pub timeout: Duration,
}

impl Default for SheetLimits {
    fn default() -> SheetLimits {
        SheetLimits {
            max_bytes: MAX_SHEET_BYTES,
            timeout: SHEET_TIMEOUT,
        }
    }
}

/// One author stylesheet source, in document order.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum StylesheetRef {
    /// A `<style>` block, with its CSS text.
    Inline(String),
    /// A `<link rel="stylesheet">` element.
    External {
        /// The `href` exactly as written; resolve it against the document base.
        href: String,
        /// The `media` attribute, when there is one.
        media: Option<String>,
    },
}

/// A stylesheet ready to be parsed: its CSS text plus the URL that relative
/// references inside it resolve against.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LoadedStylesheet {
    /// The sheet's URL after resolution. For an inline `<style>` this is the
    /// document base, so `@import url(...)` resolves like a link.
    pub url: String,
    /// The CSS text.
    pub css: String,
}

/// The author stylesheets of one run, plus the fetch cache behind them.
///
/// [`SheetSet::load`] fetches the top-level `<link>` sheets; `@import` chains
/// are fetched while the styles are computed, because only libcss knows which
/// imports a sheet really contains. After `compute_with_sheets`, [`fetched`]
/// and [`warnings`] report the whole run.
///
/// [`fetched`]: SheetSet::fetched
/// [`warnings`]: SheetSet::warnings
pub struct SheetSet {
    sheets: Vec<LoadedStylesheet>,
    loader: Loader,
}

impl SheetSet {
    /// Fetch the document's author stylesheets with the default limits.
    pub fn load(doc: &Document, base: &str) -> SheetSet {
        SheetSet::load_with_limits(doc, base, SheetLimits::default())
    }

    /// Fetch the document's author stylesheets with explicit limits.
    ///
    /// `base` is the URL relative references resolve against: the document's
    /// final URL for http(s) pages, or its containing directory for local
    /// files. A sheet that cannot be fetched is skipped with a warning on
    /// stderr; the rest of the document still styles.
    pub fn load_with_limits(doc: &Document, base: &str, limits: SheetLimits) -> SheetSet {
        let mut set = SheetSet {
            sheets: Vec::new(),
            loader: Loader::new(limits),
        };
        for source in subresource_sheets(doc) {
            match source {
                StylesheetRef::Inline(css) => set.sheets.push(LoadedStylesheet {
                    url: base.to_string(),
                    css,
                }),
                StylesheetRef::External { href, media } => {
                    if media
                        .as_deref()
                        .is_some_and(|media| !media_applies_to_screen(media))
                    {
                        continue;
                    }
                    let Some(url) = join_url(base, &href) else {
                        set.loader.warn(format!(
                            "skipping stylesheet {href:?}: cannot resolve it against {base:?}"
                        ));
                        continue;
                    };
                    if let Some(css) = set.loader.load(&url) {
                        set.sheets.push(LoadedStylesheet { url, css });
                    }
                }
            }
        }
        set
    }

    /// The sheets in document order: inline blocks and fetched external sheets
    /// are interleaved exactly as the document declares them.
    pub fn sheets(&self) -> &[LoadedStylesheet] {
        &self.sheets
    }

    /// Non-fatal problems met while fetching (also printed on stderr).
    pub fn warnings(&self) -> &[String] {
        &self.loader.warnings
    }

    /// How many distinct URLs were fetched (cached URLs are not fetched twice).
    pub fn fetched(&self) -> usize {
        self.loader.fetched
    }
}

/// The document's author stylesheet sources, in document order.
pub fn subresource_sheets(doc: &Document) -> Vec<StylesheetRef> {
    let mut out = Vec::new();
    for (id, node) in doc.nodes.iter().enumerate() {
        if node.is_text {
            continue;
        }
        if node.tag.eq_ignore_ascii_case("style") {
            let css = doc.text_content(id);
            if !css.trim().is_empty() {
                out.push(StylesheetRef::Inline(css));
            }
        } else if node.tag.eq_ignore_ascii_case("link") {
            let rel = node.attr("rel").unwrap_or_default();
            let is_sheet = rel
                .split_whitespace()
                .any(|token| token.eq_ignore_ascii_case("stylesheet"));
            if !is_sheet {
                continue;
            }
            if let Some(href) = node.attr("href") {
                out.push(StylesheetRef::External {
                    href: href.to_string(),
                    media: node.attr("media").map(str::to_string),
                });
            }
        }
    }
    out
}

/// Compute styles with the document's external stylesheets applied.
///
/// This is the fetch-aware entry point: `<link rel="stylesheet">` sheets and
/// their `@import` chains are fetched (http(s) or local files) and cascaded at
/// author origin in document order together with the inline `<style>` blocks.
/// Failures degrade to a warning and never abort the render.
pub fn compute_with_subresources(doc: &Document, base: &str, viewport_width: f64) -> Vec<Style> {
    let mut set = SheetSet::load(doc, base);
    compute_with_sheets(doc, &mut set, viewport_width)
}

/// Compute styles from an already loaded [`SheetSet`].
///
/// `@import` chains are fetched during this call, so read [`SheetSet::warnings`]
/// and [`SheetSet::fetched`] afterwards to see the complete run.
pub fn compute_with_sheets(
    doc: &Document,
    set: &mut SheetSet,
    viewport_width: f64,
) -> Vec<Style> {
    let SheetSet { sheets, loader } = set;
    let inputs: Vec<SheetInput> = sheets
        .iter()
        .map(|sheet| SheetInput {
            url: &sheet.url,
            css: &sheet.css,
        })
        .collect();
    compute_inner(doc, &inputs, viewport_width, Some(loader))
}

// ---------------------------------------------------------------------------
// Subresource fetching
// ---------------------------------------------------------------------------

/// One stylesheet to parse: its CSS and the URL its relative references
/// resolve against.
struct SheetInput<'a> {
    url: &'a str,
    css: &'a str,
}

/// Fetch cache and diagnostics for one run.
///
/// The cache is keyed by resolved URL, so two `<link>`s to the same sheet, or
/// an import reached twice, cost one fetch. `active` breaks `@import` cycles.
struct Loader {
    limits: SheetLimits,
    cache: HashMap<String, Option<String>>,
    active: HashSet<String>,
    fetched: usize,
    warnings: Vec<String>,
}

impl Loader {
    fn new(limits: SheetLimits) -> Loader {
        Loader {
            limits,
            cache: HashMap::new(),
            active: HashSet::new(),
            fetched: 0,
            warnings: Vec::new(),
        }
    }

    fn warn(&mut self, message: String) {
        eprintln!("browser-lite: {message}");
        self.warnings.push(message);
    }

    /// Fetch `url` once per run. `None` means the sheet is unusable; the
    /// failure is cached too, so a broken URL is not retried.
    fn load(&mut self, url: &str) -> Option<String> {
        if let Some(cached) = self.cache.get(url) {
            return cached.clone();
        }
        self.fetched += 1;
        let result = match fetch_sheet(url, &self.limits) {
            Ok(css) => Some(css),
            Err(err) => {
                self.warn(format!("skipping stylesheet {url}: {err}"));
                None
            }
        };
        self.cache.insert(url.to_string(), result.clone());
        result
    }

    /// Start parsing `url`, returning `false` when it is already being parsed
    /// (an `@import` cycle).
    fn enter(&mut self, url: &str) -> bool {
        self.active.insert(url.to_string())
    }

    fn exit(&mut self, url: &str) {
        self.active.remove(url);
    }
}

/// Is a `media` attribute relevant to a screen render?
///
/// The engine only paints to a screen. The attribute is a media query list, and
/// a list made up exclusively of other media types (typically `print`)
/// contributes nothing. Queries without a type (`(min-width: 40em)`) target
/// `all`; the conditions themselves are left to the sheet's own `@media` rules,
/// because the engine does not pass per-sheet media conditions to libcss.
fn media_applies_to_screen(media: &str) -> bool {
    if media.trim().is_empty() {
        return true;
    }
    media.split(',').any(|query| {
        let mut words = query.split_whitespace();
        let Some(first) = words.next() else {
            return true;
        };
        let (negated, kind) = if first.eq_ignore_ascii_case("not") {
            (true, words.next().unwrap_or(""))
        } else if first.eq_ignore_ascii_case("only") {
            (false, words.next().unwrap_or(""))
        } else {
            (false, first)
        };
        let matches = kind.is_empty()
            || kind.starts_with('(')
            || kind.eq_ignore_ascii_case("all")
            || kind.eq_ignore_ascii_case("screen");
        negated != matches
    })
}

/// Resolve `rel` against `base`.
///
/// `base` is an absolute URL (http(s), file, or any other scheme) or a
/// filesystem path. A path is a directory when it ends with a separator or
/// names an existing directory; otherwise its parent directory is the base,
/// which is what a stylesheet URL needs. Relative paths are normalised so the
/// cache key is stable (`a/../b.css` and `b.css` are one URL).
fn join_url(base: &str, rel: &str) -> Option<String> {
    let rel = rel.trim();
    if rel.is_empty() {
        return None;
    }
    if Url::parse(rel).is_ok() {
        return Some(rel.to_string());
    }
    if let Ok(base_url) = Url::parse(base) {
        return base_url.join(rel).ok().map(|url| url.to_string());
    }
    if base.is_empty() {
        return None;
    }
    let base_path = Path::new(base);
    let dir = if base.ends_with('/') || base_path.is_dir() {
        base_path.to_path_buf()
    } else {
        base_path
            .parent()
            .unwrap_or(base_path)
            .to_path_buf()
    };
    let joined = if Path::new(rel).is_absolute() {
        PathBuf::from(rel)
    } else {
        dir.join(rel)
    };
    Some(normalize_path(&joined).to_string_lossy().into_owned())
}

/// Lexical `.`/`..` removal, without touching the filesystem.
fn normalize_path(path: &Path) -> PathBuf {
    let mut out = PathBuf::new();
    for component in path.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                if !out.pop() {
                    out.push("..");
                }
            }
            other => out.push(other.as_os_str()),
        }
    }
    out
}

/// Fetch one stylesheet body, honouring the size cap and timeout.
fn fetch_sheet(url: &str, limits: &SheetLimits) -> Result<String, String> {
    if url.starts_with("http://") || url.starts_with("https://") {
        let (bytes, content_type) =
            crate::net::fetch_bytes_limited(url, limits.max_bytes, limits.timeout)?;
        return crate::net::decode_body(&bytes, content_type.as_deref());
    }
    let path = local_path(url).ok_or_else(|| format!("unsupported stylesheet URL {url:?}"))?;
    read_capped(&path, limits.max_bytes)
}

/// The filesystem path behind a stylesheet URL, if there is one.
fn local_path(url: &str) -> Option<PathBuf> {
    match Url::parse(url) {
        Ok(parsed) if parsed.scheme() == "file" => parsed.to_file_path().ok(),
        // Another scheme (data:, about:, ...): not a subresource we can read.
        Ok(_) => None,
        // Not a URL: a plain filesystem path.
        Err(_) => Some(PathBuf::from(url)),
    }
}

/// Read a file, rejecting (not truncating) anything over `max_bytes`.
fn read_capped(path: &Path, max_bytes: usize) -> Result<String, String> {
    let meta = std::fs::metadata(path).map_err(|err| err.to_string())?;
    if meta.is_dir() {
        return Err(String::from("is a directory"));
    }
    if meta.len() > max_bytes as u64 {
        return Err(format!(
            "{} bytes exceeds the {max_bytes} byte limit",
            meta.len()
        ));
    }
    let file = File::open(path).map_err(|err| err.to_string())?;
    let mut bytes = Vec::new();
    file.take(max_bytes as u64 + 1)
        .read_to_end(&mut bytes)
        .map_err(|err| err.to_string())?;
    if bytes.len() > max_bytes {
        return Err(format!("body exceeds the {max_bytes} byte limit"));
    }
    Ok(String::from_utf8_lossy(&bytes).into_owned())
}

/// Collect the document's `<style>` sections, in document order.
pub fn stylesheets(doc: &Document) -> Vec<String> {
    doc.query_selector_all("style")
        .into_iter()
        .map(|id| doc.text_content(id))
        .filter(|s| !s.trim().is_empty())
        .collect()
}

fn make_unit_ctx(viewport_width: f64) -> CssUnitCtx {
    CssUnitCtx {
        viewport_width: to_fixed(viewport_width),
        viewport_height: to_fixed(DEFAULT_VIEWPORT_HEIGHT),
        font_size_default: to_fixed(DEFAULT_FONT_SIZE),
        font_size_minimum: 0,
        device_dpi: to_fixed(96.0),
        root_style: ptr::null(),
        pw: ptr::null_mut(),
        measure: None,
    }
}

fn make_media(viewport_width: f64) -> CssMedia {
    let mut media: CssMedia = unsafe { mem::zeroed() };
    media.media_type = CSS_MEDIA_SCREEN;
    media.width = to_fixed(viewport_width);
    media.height = to_fixed(DEFAULT_VIEWPORT_HEIGHT);
    media.aspect_ratio = to_fixed(viewport_width / DEFAULT_VIEWPORT_HEIGHT);
    media.orientation = if viewport_width >= DEFAULT_VIEWPORT_HEIGHT {
        CSS_MEDIA_ORIENTATION_LANDSCAPE
    } else {
        CSS_MEDIA_ORIENTATION_PORTRAIT
    };
    media.color = to_fixed(8.0); // 8 bits per channel
    media
}

fn ancestor_style(
    doc: &Document,
    styles: &[Option<*mut CssComputedStyle>],
    id: NodeId,
) -> Option<*mut CssComputedStyle> {
    let mut cur = doc.nodes[id].parent;
    while let Some(p) = cur {
        if let Some(style) = styles[p] {
            return Some(style);
        }
        cur = doc.nodes[p].parent;
    }
    None
}

fn is_root_element(doc: &Document, id: NodeId) -> bool {
    match doc.nodes[id].parent {
        None => true,
        Some(p) => p == doc.root,
    }
}

/// Compute one [`Style`] per DOM node, with `@media` resolved against
/// `viewport_width`.
///
/// `sheets` are author stylesheets (see [`stylesheets`]). The embedded UA sheet
/// is always applied at the UA origin, so author rules win. Text nodes and the
/// synthetic `#document` node keep the engine defaults.
///
/// This path never fetches: external sheets are [`compute_with_subresources`].
pub fn compute(doc: &Document, sheets: &[String], viewport_width: f64) -> Vec<Style> {
    let urls: Vec<String> = (0..sheets.len())
        .map(|i| format!("browser-lite://sheet/{i}"))
        .collect();
    let inputs: Vec<SheetInput> = sheets
        .iter()
        .zip(&urls)
        .map(|(css, url)| SheetInput {
            url,
            css: css.as_str(),
        })
        .collect();
    compute_inner(doc, &inputs, viewport_width, None)
}

fn compute_inner(
    doc: &Document,
    sheets: &[SheetInput<'_>],
    viewport_width: f64,
    mut loader: Option<&mut Loader>,
) -> Vec<Style> {
    let viewport_width = viewport_width.max(1.0);
    // libwapcaplet interns strings in a process-wide dictionary with
    // non-atomic refcounts, so concurrent selection would race on them. The
    // engine renders one document at a time; the lock also keeps tests that
    // compute styles in parallel from corrupting that dictionary.
    static COMPUTE_LOCK: OnceLock<Mutex<()>> = OnceLock::new();
    let _serialised = COMPUTE_LOCK
        .get_or_init(|| Mutex::new(()))
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());

    let mut out = vec![Style::default(); doc.nodes.len()];
    if doc.root < out.len() {
        out[doc.root].display = Display::Block;
    }

    unsafe {
        let mut select_ctx: *mut CssSelectCtx = ptr::null_mut();
        check(
            css_select_ctx_create(&mut select_ctx),
            "css_select_ctx_create",
        );

        let mut parsed: Vec<*mut CssStylesheet> = Vec::new();

        let ua = parse_sheet(UA_STYLESHEET, "browser-lite://ua", false);
        check(
            css_select_ctx_append_sheet(select_ctx, ua, CSS_ORIGIN_UA, ptr::null()),
            "css_select_ctx_append_sheet (ua)",
        );
        parsed.push(ua);

        for input in sheets {
            let sheet = match loader.as_deref_mut() {
                Some(loader) => parse_fetched_sheet(input.css, input.url, loader),
                None => parse_sheet(input.css, input.url, false),
            };
            check(
                css_select_ctx_append_sheet(select_ctx, sheet, CSS_ORIGIN_AUTHOR, ptr::null()),
                "css_select_ctx_append_sheet (author)",
            );
            parsed.push(sheet);
        }

        let unit_ctx = make_unit_ctx(viewport_width);
        let media = make_media(viewport_width);
        let mut handler = select_handler();
        let handler_ptr = &mut handler as *mut CssSelectHandler;
        let mut pw = SelectCtx::new(doc);
        let pw_ptr = &mut pw as *mut SelectCtx as *mut c_void;

        let mut styles: Vec<Option<*mut CssComputedStyle>> = vec![None; doc.nodes.len()];

        for id in 0..doc.nodes.len() {
            if doc.nodes[id].is_text || id == doc.root {
                continue;
            }
            let parent_style = ancestor_style(doc, &styles, id);

            // `style=""` is a declaration list: its own sheet, not the rule sheets.
            let inline = doc.nodes[id].attr("style").and_then(|decls| {
                if decls.trim().is_empty() {
                    None
                } else {
                    Some(parse_sheet(
                        decls,
                        &format!("browser-lite://inline/{id}"),
                        true,
                    ))
                }
            });
            if let Some(sheet) = inline {
                parsed.push(sheet);
            }

            let mut results: *mut CssSelectResults = ptr::null_mut();
            check(
                css_select_style(
                    select_ctx,
                    node_ptr(id),
                    &unit_ctx,
                    &media,
                    inline.unwrap_or(ptr::null_mut()),
                    handler_ptr,
                    pw_ptr,
                    &mut results,
                ),
                "css_select_style",
            );

            let partial = (*results).styles[0];
            let mut composed: *mut CssComputedStyle = ptr::null_mut();

            // The root element (no parent according to `parent_node`) leaves
            // `css_select_style` with absolute values already resolved:
            // composing it against NULL hits CSS_BADPARM when inherited
            // properties are copied. Every other element composes.
            let style = match parent_style {
                Some(parent) => {
                    check(
                        css_computed_style_compose(parent, partial, &unit_ctx, &mut composed),
                        "css_computed_style_compose",
                    );
                    css_computed_style_destroy(partial);
                    composed
                }
                None => partial,
            };
            // The pointer moves to `styles`; it is freed during teardown.
            (*results).styles[0] = ptr::null_mut();
            css_select_results_destroy(results);

            styles[id] = Some(style);
            out[id] = map_style(style, &unit_ctx, is_root_element(doc, id));
        }

        // Teardown: libcss node data, styles, class arrays, sheets and context.
        for (id, data) in pw.node_data.drain(..) {
            css_libcss_node_data_handler(
                handler_ptr,
                CSS_NODE_DELETED,
                pw_ptr,
                node_ptr(id),
                ptr::null_mut(),
                data,
            );
        }
        for style in styles.into_iter().flatten() {
            css_computed_style_destroy(style);
        }
        for (raw, len) in pw.class_arrays.drain(..) {
            drop(Box::from_raw(slice::from_raw_parts_mut(raw, len)));
        }
        css_select_ctx_destroy(select_ctx);
        for sheet in parsed {
            css_stylesheet_destroy(sheet);
        }
    }

    // Grid placement properties are invisible to libcss: they are read from
    // the same author sheets here, after the cascade above fixed display.
    let css_texts: Vec<&str> = sheets.iter().map(|input| input.css).collect();
    apply_grid_styles(doc, &css_texts, &mut out);

    out
}
