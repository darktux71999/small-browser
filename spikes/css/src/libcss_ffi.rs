//! FFI escrito a mano sobre libcss 0.9.2 (NetSurf) y libwapcaplet (T3).
//!
//! Sólo se declara el subconjunto que el spike usa: carga de hojas, contexto de
//! selección, `css_select_style` + `css_computed_style_compose`, los getters
//! computados que alimentan `SpikeStyle` y los 36 callbacks de
//! `css_select_handler` (los que no aplican contestan "no coincide").
//!
//! Sin crates nuevos: mismas firmas contra las cabeceras de
//! `~/.cache/css-spike/prefix/include`, igual que `src/ffi.rs` con Pango/cairo.
//!
//! Semántica tomada del código de libcss (no supuesta):
//! - `css_color` es **AARRGGBB** (`src/parse/properties/utils.c`), no RRGGBBAA.
//! - `css_select_style` no hereda: hay que componer con el estilo del padre, y
//!   el elemento raíz necesita un padre real (`css_select_default_style`);
//!   componer con padre NULL revienta al copiar propiedades heredadas.
//! - `params.inline_style = true` es para listas de declaraciones (`style=""`),
//!   no para el contenido de `<style>`, que lleva reglas.
//! - libcss toma prestada una referencia de las cadenas que devuelven
//!   `node_name`/`node_classes`/`node_id` y las libera él; el array de clases lo
//!   reserva y libera el cliente.

use std::ffi::{CStr, CString};
use std::mem;
use std::os::raw::{c_char, c_int, c_void};
use std::ptr;
use std::slice;

use browser_lite::dom::{Document, NodeId};

use crate::style::{Display, SpikeStyle, TextAlign};

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
const CSS_ORIGIN_AUTHOR: u32 = 2;
const CSS_SELECT_HANDLER_VERSION_1: u32 = 1;
const CSS_PSEUDO_ELEMENT_COUNT: usize = 5;

const CSS_MEDIA_SCREEN: u32 = 1 << 6;

// css_display_e
const CSS_DISPLAY_INLINE: u8 = 0x01;
const CSS_DISPLAY_BLOCK: u8 = 0x02;
const CSS_DISPLAY_NONE: u8 = 0x10;

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

// css_letter_spacing_e (alias de column-gap)
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

// ---------------------------------------------------------------------------
// Tipos opacos
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

/// `lwc_string` (libwapcaplet): el texto vive justo detrás de la cabecera
/// (`(str) + 1` en `lwc_string_data`), por eso hace falta replicar el layout.
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
// css_select_handler (orden exacto de libcss/select.h)
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
// Declaraciones externas
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
// Utilidades de cadenas y errores
// ---------------------------------------------------------------------------

/// Interna una cadena en el diccionario de libwapcaplet y transfiere la
/// referencia al llamante (libcss la libera cuando termina con el nodo).
unsafe fn intern(s: &str) -> *mut LwcString {
    let mut out: *mut LwcString = ptr::null_mut();
    let err = lwc_intern_string(s.as_ptr() as *const c_char, s.len(), &mut out);
    assert!(err == 0 && !out.is_null(), "libwapcaplet: no se pudo internar {s:?}");
    out
}

/// Bytes de una `lwc_string` (equivalente a `lwc_string_data` + `_length`).
unsafe fn lwc_bytes<'a>(s: *mut LwcString) -> &'a [u8] {
    let data = (s as *const u8).add(mem::size_of::<LwcString>());
    slice::from_raw_parts(data, (*s).len)
}

unsafe fn lwc_ref(s: *mut LwcString) -> *mut LwcString {
    (*s).refcnt += 1;
    s
}

/// Réplica del macro `lwc_string_unref` de libwapcaplet.
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
        panic!("libcss: {what} fallo ({err}: {text})");
    }
}

fn fixed_to_f64(v: CssFixed) -> f64 {
    v as f64 / FIXED_SCALE
}

fn to_fixed(v: f64) -> CssFixed {
    (v * FIXED_SCALE) as CssFixed
}

// ---------------------------------------------------------------------------
// Contexto de selección (el `pw` que libcss devuelve a los callbacks)
// ---------------------------------------------------------------------------

struct SelectCtx {
    /// El documento sobrevive a todo `compute`; se borra al terminar.
    doc: *const Document,
    /// Arrays de clases reservados por nosotros; libcss 0.9.2 no libera el
    /// array (sólo las cadenas), así que los recuperamos al terminar.
    class_arrays: Vec<(*mut *mut LwcString, usize)>,
    /// `libcss_node_data` por nodo, para liberarlo con el handler al final.
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

    fn node(&self, id: NodeId) -> &browser_lite::dom::Node {
        &self.doc().nodes[id]
    }

    fn attr(&self, id: NodeId, name: &str) -> Option<&str> {
        self.node(id).attr(name)
    }

    /// ¿La etiqueta del nodo coincide con el nombre del selector?
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
// Callbacks de selección
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
    // El nodo sintético #document se trata como "sin padre": así `:root`
    // coincide con <html>, igual que en el DOM de NetSurf.
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
        // Los nombres de atributo del DOM llegan en minúsculas.
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
            continue; // :nth-child cuenta elementos
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
            // AARRGGBB: negro opaco (mismo valor que usa NetSurf).
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
    // El spike no descarga recursos: la URL relativa se toma como absoluta.
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
// Hojas de estilo
// ---------------------------------------------------------------------------

unsafe fn create_empty_sheet(url: &[u8]) -> *mut CssStylesheet {
    let url = CString::new(url).unwrap_or_default();
    let mut params: CssStylesheetParams = mem::zeroed();
    params.params_version = CSS_STYLESHEET_PARAMS_VERSION_1;
    params.level = CSS_LEVEL_DEFAULT;
    params.url = url.as_ptr();
    params.resolve = Some(cb_resolve);
    let mut sheet: *mut CssStylesheet = ptr::null_mut();
    check(css_stylesheet_create(&params, &mut sheet), "css_stylesheet_create (import)");
    check(css_stylesheet_data_done(sheet), "css_stylesheet_data_done (import)");
    sheet
}

/// Registra hojas vacías para los `@import` pendientes: sin red no hay nada que
/// descargar, pero libcss exige que cada import tenga su hoja registrada.
unsafe fn finish_imports(parent: *mut CssStylesheet) {
    loop {
        let mut url: *mut LwcString = ptr::null_mut();
        if css_stylesheet_next_pending_import(parent, &mut url) != CSS_OK {
            return;
        }
        let bytes = lwc_bytes(url).to_vec();
        let child = create_empty_sheet(&bytes);
        check(css_stylesheet_register_import(parent, child), "css_stylesheet_register_import");
        lwc_unref(url);
    }
}

unsafe fn parse_sheet(css: &str, index: usize) -> *mut CssStylesheet {
    let url = CString::new(format!("css-spike://sheet/{index}")).expect("url de hoja");
    let mut params: CssStylesheetParams = mem::zeroed();
    params.params_version = CSS_STYLESHEET_PARAMS_VERSION_1;
    params.level = CSS_LEVEL_DEFAULT;
    params.url = url.as_ptr();
    params.resolve = Some(cb_resolve);
    // El contenido de <style> es una lista de reglas, no de declaraciones:
    // `inline_style = true` está reservado a los atributos `style=""`.
    params.inline_style = false;

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
// Mapeo estilo computado -> SpikeStyle
// ---------------------------------------------------------------------------

fn rgba(c: CssColor) -> (f64, f64, f64, f64) {
    // css_color es AARRGGBB (ver libcss/src/parse/properties/utils.c).
    let a = ((c >> 24) & 0xff) as f64 / 255.0;
    let r = ((c >> 16) & 0xff) as f64 / 255.0;
    let g = ((c >> 8) & 0xff) as f64 / 255.0;
    let b = (c & 0xff) as f64 / 255.0;
    (r, g, b, a)
}

type LengthGetter = unsafe extern "C" fn(
    *const CssComputedStyle,
    *mut CssFixed,
    *mut CssUnit,
) -> u8;

/// Longitud absoluta en px; `None` para auto, `inherit` y porcentajes (el spike
/// no tiene bloque contenedor para resolverlos).
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
) -> SpikeStyle {
    let mut out = SpikeStyle::default();

    out.display = match css_computed_display(style, is_root) {
        CSS_DISPLAY_NONE => Display::None,
        CSS_DISPLAY_INLINE => Display::Inline,
        CSS_DISPLAY_BLOCK => Display::Block,
        _ => Display::Block,
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
            if c.3 > 0.0 {
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
        _ => TextAlign::Left, // inherit / default ya resueltos por compose
    };

    out.letter_spacing =
        length_px(style, unit_ctx, css_computed_letter_spacing, CSS_LETTER_SPACING_SET)
            .unwrap_or(0.0);

    out
}

// ---------------------------------------------------------------------------
// Entrada pública
// ---------------------------------------------------------------------------

/// Viewport fijo del spike: `Provider::compute` no recibe el ancho y el CLI usa
/// 800 px por defecto; las media queries se evalúan contra este valor.
const VIEWPORT_WIDTH: f64 = 800.0;
const VIEWPORT_HEIGHT: f64 = 600.0;
const DEFAULT_FONT_SIZE: f64 = 16.0;

fn make_unit_ctx() -> CssUnitCtx {
    CssUnitCtx {
        viewport_width: to_fixed(VIEWPORT_WIDTH),
        viewport_height: to_fixed(VIEWPORT_HEIGHT),
        font_size_default: to_fixed(DEFAULT_FONT_SIZE),
        font_size_minimum: 0,
        device_dpi: to_fixed(96.0),
        root_style: ptr::null(),
        pw: ptr::null_mut(),
        measure: None,
    }
}

fn make_media() -> CssMedia {
    let mut media: CssMedia = unsafe { mem::zeroed() };
    media.media_type = CSS_MEDIA_SCREEN;
    media.width = to_fixed(VIEWPORT_WIDTH);
    media.height = to_fixed(VIEWPORT_HEIGHT);
    media.aspect_ratio = to_fixed(VIEWPORT_WIDTH / VIEWPORT_HEIGHT);
    media.color = to_fixed(8.0); // 8 bits por canal
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

/// Calcula un `SpikeStyle` por nodo del documento (T3, S2/S3).
pub fn compute(doc: &Document, sheets: &[String]) -> Vec<SpikeStyle> {
    let mut out = vec![SpikeStyle::default(); doc.nodes.len()];

    unsafe {
        let mut select_ctx: *mut CssSelectCtx = ptr::null_mut();
        check(css_select_ctx_create(&mut select_ctx), "css_select_ctx_create");

        let mut parsed: Vec<*mut CssStylesheet> = Vec::new();
        for (i, css) in sheets.iter().enumerate() {
            let sheet = parse_sheet(css, i);
            check(
                css_select_ctx_append_sheet(select_ctx, sheet, CSS_ORIGIN_AUTHOR, ptr::null()),
                "css_select_ctx_append_sheet",
            );
            parsed.push(sheet);
        }

        let unit_ctx = make_unit_ctx();
        let media = make_media();
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

            let mut results: *mut CssSelectResults = ptr::null_mut();
            check(
                css_select_style(
                    select_ctx,
                    node_ptr(id),
                    &unit_ctx,
                    &media,
                    ptr::null(),
                    handler_ptr,
                    pw_ptr,
                    &mut results,
                ),
                "css_select_style",
            );

            let partial = (*results).styles[0];
            let mut composed: *mut CssComputedStyle = ptr::null_mut();

            // El elemento raíz (sin padre según `parent_node`) ya sale con los
            // valores absolutos resueltos por libcss: componer con NULL revienta
            // al copiar propiedades heredadas. Para el resto, compose.
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
            // El puntero queda en `styles`: lo liberamos nosotros al terminar.
            (*results).styles[0] = ptr::null_mut();
            css_select_results_destroy(results);

            styles[id] = Some(style);
            out[id] = map_style(style, &unit_ctx, is_root_element(doc, id));
        }

        // Teardown: node data de libcss, estilos, arrays de clases, hojas y ctx.
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
