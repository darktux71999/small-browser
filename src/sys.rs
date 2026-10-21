//! Thin FFI to system lexbor 3.0.1 and quickjs-ng 0.17 (libqjs).
//! We link the distro libraries instead of vendoring: the engine is shared,
//! not duplicated.

#![allow(non_camel_case_types, non_snake_case)]

use std::os::raw::{c_char, c_int, c_void};

// ---------- lexbor ----------

pub type lxb_status_t = u32;

#[repr(C)]
pub struct lxb_dom_node_t {
    pub events: *mut c_void,
    pub local_name: usize,
    pub prefix: usize,
    pub ns: usize,
    pub owner_document: *mut c_void,
    pub next: *mut lxb_dom_node_t,
    pub prev: *mut lxb_dom_node_t,
    pub parent: *mut lxb_dom_node_t,
    pub first_child: *mut lxb_dom_node_t,
    pub last_child: *mut lxb_dom_node_t,
    pub user: *mut c_void,
    pub node_type: u32,
}

#[repr(C)]
pub struct lxb_dom_attr_t {
    pub node: lxb_dom_node_t,
    pub upper_name: usize,
    pub qualified_name: usize,
    pub value: *mut lexbor_str_t,
    pub owner: *mut c_void,
    pub next: *mut lxb_dom_attr_t,
    pub prev: *mut lxb_dom_attr_t,
}

#[repr(C)]
pub struct lxb_dom_element_t {
    pub node: lxb_dom_node_t,
    pub upper_name: usize,
    pub qualified_name: usize,
    pub is_value: *mut c_void,
    pub first_attr: *mut lxb_dom_attr_t,
    pub last_attr: *mut lxb_dom_attr_t,
}

#[repr(C)]
pub struct lexbor_str_t {
    pub data: *mut u8,
    pub length: usize,
}

pub const LXB_DOM_NODE_TYPE_ELEMENT: u32 = 1;
pub const LXB_DOM_NODE_TYPE_TEXT: u32 = 3;

#[link(name = "lexbor")]
extern "C" {
    pub fn lxb_html_document_create() -> *mut c_void;
    pub fn lxb_html_document_parse(
        document: *mut c_void,
        html: *const u8,
        size: usize,
    ) -> lxb_status_t;
    pub fn lxb_html_document_destroy(document: *mut c_void);
    pub fn lxb_dom_element_local_name(
        element: *mut lxb_dom_element_t,
        len: *mut usize,
    ) -> *const u8;
    pub fn lxb_dom_attr_local_name(attr: *mut lxb_dom_attr_t, len: *mut usize) -> *const u8;
    pub fn lxb_dom_node_text_content(node: *mut lxb_dom_node_t, len: *mut usize) -> *const u8;
}

// ---------- quickjs-ng ----------
//
// Default 64-bit build (no JS_NAN_BOXING, no JS_CHECK_JSVALUE): JSValue is a
// 16-byte by-value struct `{ union u; int64_t tag; }`, not a tagged pointer.

#[repr(C)]
#[derive(Clone, Copy)]
pub union JSValueUnion {
    pub int32: i32,
    pub float64: f64,
    pub ptr: *mut c_void,
    pub short_big_int: i32,
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct JSValue {
    pub u: JSValueUnion,
    pub tag: i64,
}

impl JSValue {
    pub fn undefined() -> JSValue {
        JSValue {
            u: JSValueUnion { int32: 0 },
            tag: 3, // JS_TAG_UNDEFINED
        }
    }
}

pub type JSRuntime = c_void;
pub type JSContext = c_void;
pub type JSCFunction = unsafe extern "C" fn(
    ctx: *mut JSContext,
    this_val: JSValue,
    argc: c_int,
    argv: *mut JSValue,
) -> JSValue;

pub const JS_CFUNC_GENERIC: c_int = 0;
pub const JS_EVAL_TYPE_GLOBAL: c_int = 0;
pub const JS_TAG_EXCEPTION: i64 = 6;

#[link(name = "qjs")]
extern "C" {
    pub fn JS_NewRuntime() -> *mut JSRuntime;
    pub fn JS_FreeRuntime(rt: *mut JSRuntime);
    pub fn JS_NewContext(rt: *mut JSRuntime) -> *mut JSContext;
    pub fn JS_FreeContext(ctx: *mut JSContext);
    pub fn JS_Eval(
        ctx: *mut JSContext,
        input: *const c_char,
        input_len: usize,
        filename: *const c_char,
        eval_flags: c_int,
    ) -> JSValue;
    pub fn JS_GetGlobalObject(ctx: *mut JSContext) -> JSValue;
    pub fn JS_NewObject(ctx: *mut JSContext) -> JSValue;
    pub fn JS_NewCFunction2(
        ctx: *mut JSContext,
        func: JSCFunction,
        name: *const c_char,
        length: c_int,
        cproto: c_int,
        magic: c_int,
    ) -> JSValue;
    pub fn JS_SetPropertyStr(
        ctx: *mut JSContext,
        this_obj: JSValue,
        prop: *const c_char,
        val: JSValue,
    ) -> c_int;
    pub fn JS_GetPropertyStr(
        ctx: *mut JSContext,
        this_obj: JSValue,
        prop: *const c_char,
    ) -> JSValue;
    pub fn JS_NewStringLen(ctx: *mut JSContext, s: *const c_char, len: usize) -> JSValue;
    pub fn JS_ToCStringLen2(
        ctx: *mut JSContext,
        plen: *mut usize,
        val: JSValue,
        cesu8: bool,
    ) -> *const c_char;
    pub fn JS_FreeCString(ctx: *mut JSContext, ptr: *const c_char);
    pub fn JS_FreeValue(ctx: *mut JSContext, v: JSValue);
    pub fn JS_GetException(ctx: *mut JSContext) -> JSValue;
    pub fn JS_ExecutePendingJob(rt: *mut JSRuntime, pctx: *mut *mut JSContext) -> c_int;
}

pub fn js_is_exception(v: JSValue) -> bool {
    v.tag == JS_TAG_EXCEPTION
}
