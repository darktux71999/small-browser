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
//! - libcss borrows a reference to the strings returned by
//!   `node_name`/`node_classes`/`node_id` and frees them itself; the class array
//!   is allocated and freed by the client.

use std::ffi::{CStr, CString};
use std::mem;
use std::os::raw::{c_char, c_int, c_void};
use std::ptr;
use std::slice;
use std::sync::{Mutex, OnceLock};

use crate::css::{Color, Display, Style, TextAlign};
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
    _base: *const c_char,
    rel: *mut LwcString,
    abs: *mut *mut LwcString,
) -> CssError {
    // The engine does not fetch subresources here: a relative URL is taken as
    // absolute, which is enough for the empty import sheets.
    if rel.is_null() {
        *abs = ptr::null_mut();
    } else {
        *abs = lwc_ref(rel);
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

/// Register empty sheets for pending `@import`s: there is no network resource
/// to fetch here, but libcss requires every import to have a registered sheet.
unsafe fn finish_imports(parent: *mut CssStylesheet) {
    loop {
        let mut url: *mut LwcString = ptr::null_mut();
        if css_stylesheet_next_pending_import(parent, &mut url) != CSS_OK {
            return;
        }
        let bytes = lwc_bytes(url).to_vec();
        let child = create_empty_sheet(&bytes);
        check(
            css_stylesheet_register_import(parent, child),
            "css_stylesheet_register_import",
        );
        lwc_unref(url);
    }
}

unsafe fn parse_sheet(css: &str, url: &str, inline: bool) -> *mut CssStylesheet {
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

    let mut err = css_stylesheet_append_data(sheet, css.as_ptr(), css.len());
    if err == CSS_NEEDDATA {
        err = CSS_OK;
    }
    check(err, "css_stylesheet_append_data");

    let err = css_stylesheet_data_done(sheet);
    if err == CSS_IMPORTS_PENDING {
        finish_imports(sheet);
    } else {
        check(err, "css_stylesheet_data_done");
    }
    sheet
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

unsafe fn map_style(
    style: *const CssComputedStyle,
    unit_ctx: &CssUnitCtx,
    is_root: bool,
) -> Style {
    let mut out = Style::default();

    out.display = match css_computed_display(style, is_root) {
        CSS_DISPLAY_NONE => Display::None,
        CSS_DISPLAY_BLOCK
        | CSS_DISPLAY_LIST_ITEM
        | CSS_DISPLAY_TABLE
        | CSS_DISPLAY_FLEX
        | CSS_DISPLAY_GRID => Display::Block,
        // The block flow engine has no inline-block/inline-table/flex layout.
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
        | CSS_DISPLAY_TABLE_CAPTION
        | CSS_DISPLAY_INLINE_FLEX
        | CSS_DISPLAY_INLINE_GRID => Display::Inline,
        // CSS_DISPLAY_INHERIT is resolved by compose before mapping.
        _ => Display::Inline,
    };

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
// Public entry points
// ---------------------------------------------------------------------------

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
pub fn compute(doc: &Document, sheets: &[String], viewport_width: f64) -> Vec<Style> {
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

        for (i, css) in sheets.iter().enumerate() {
            let sheet = parse_sheet(css, &format!("browser-lite://sheet/{i}"), false);
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

    out
}
