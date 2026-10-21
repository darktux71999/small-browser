//! Blitz renderer + QuickJS scripting.
//!
//! Blitz owns HTML/CSS/layout/paint. QuickJS runs the page scripts and mutates
//! the same `BaseDocument` through a small DOM API. This module is the bridge;
//! the rendering stack is Blitz's, the engine is ours.

use std::any::Any;
use std::ops::{Deref, DerefMut};
use std::os::raw::{c_char, c_int, c_void};

use blitz_dom::{BaseDocument, Document, DocumentConfig};
use blitz_html::HtmlDocument;
use blitz_shell::{create_default_event_loop, BlitzApplication, WindowConfig};
use blitz_traits::events::UiEvent;
use markup5ever::{ns, LocalName, QualName};

use crate::sys::{self, JSContext, JSValue, JSValueUnion};

/// A Blitz document with a persistent QuickJS context and its scripts executed.
///
/// `base` lives in a `Box` so its address stays stable while the document is
/// moved into the window: QuickJS callbacks hold a raw pointer to it.
pub struct JsDocument {
    base: Box<BaseDocument>,
    rt: QjsDom,
}

impl Deref for JsDocument {
    type Target = BaseDocument;
    fn deref(&self) -> &BaseDocument {
        &self.base
    }
}

impl DerefMut for JsDocument {
    fn deref_mut(&mut self) -> &mut BaseDocument {
        &mut self.base
    }
}

impl Document for JsDocument {
    fn handle_ui_event(&mut self, event: UiEvent) {
        match event {
            UiEvent::MouseMove(e) => {
                self.base.set_hover_to(e.x, e.y);
            }
            UiEvent::MouseDown(_) => {
                self.base.active_node();
            }
            UiEvent::MouseUp(e) => {
                // Hit-test at the release position, then bubble "click".
                self.base.set_hover_to(e.x, e.y);
                self.base.unactive_node();
                let mut chain = Vec::new();
                let mut current = self.base.get_hover_node_id();
                while let Some(id) = current {
                    chain.push(id);
                    current = self.base.get_node(id).and_then(|n| n.parent);
                }
                for id in chain {
                    self.rt.dispatch(id, "click");
                }
                self.base.shell_provider.request_redraw();
            }
            _ => {}
        }
    }

    fn as_any_mut(&mut self) -> &mut dyn Any {
        self
    }
}

/// Parse HTML, run its inline scripts with QuickJS, and return a renderable
/// document. Used by both the single-window and tabbed entry points.
pub fn build_document(html: &str) -> Box<dyn Document> {
    // `ua_stylesheets: Some(..)` makes blitz-html append its DEFAULT_CSS
    // (display:block defaults etc.). Without it every box collapses.
    let config = DocumentConfig {
        ua_stylesheets: Some(Vec::new()),
        ..Default::default()
    };
    let parsed = HtmlDocument::from_html(html, config);
    let mut base = Box::new(BaseDocument::from(parsed));

    let scripts = extract_inline_scripts(html);
    let doc_ptr: *mut BaseDocument = &mut *base;
    let rt = QjsDom::new(doc_ptr);
    for (i, script) in scripts.iter().enumerate() {
        if let Err(err) = rt.eval(script, &format!("inline-{i}.js")) {
            eprintln!("browser-lite: js error: {err}");
        }
    }

    Box::new(JsDocument { base, rt })
}

/// Show one document.
pub fn run(html: &str) -> Result<(), String> {
    run_documents(vec![build_document(html)])
}

/// Show one window per document (each window is a tab in the current MVP).
pub fn run_documents(docs: Vec<Box<dyn Document>>) -> Result<(), String> {
    let event_loop = create_default_event_loop();
    let mut application = BlitzApplication::new(event_loop.create_proxy());
    for doc in docs {
        let renderer = anyrender_vello::VelloWindowRenderer::new();
        application.add_window(WindowConfig::new(doc, renderer));
    }
    event_loop
        .run_app(&mut application)
        .map_err(|e| format!("event loop: {e}"))
}

// ---------- QuickJS DOM bridge ----------

struct QjsDom {
    rt: *mut sys::JSRuntime,
    ctx: *mut JSContext,
}

const BOOTSTRAP: &str = r#"
(function () {
  class Element {
    constructor(id) { this.__id = id; }
    get id() { return __dom_attr(this.__id, "id") || ""; }
    get tagName() { return __dom_tag(this.__id).toUpperCase(); }
    setAttribute(name, value) { __dom_set_attr(this.__id, String(name), String(value)); }
    getAttribute(name) { return __dom_attr(this.__id, String(name)); }
    appendChild(child) { __dom_append(this.__id, child.__id); return child; }
    set textContent(value) { __dom_set_text(this.__id, String(value)); }
    get textContent() { return __dom_text(this.__id); }
  }
  globalThis.Element = Element;

  const __listeners = new Map();
  globalThis.__add_listener = function (id, type, fn) {
    let byType = __listeners.get(id);
    if (!byType) { byType = new Map(); __listeners.set(id, byType); }
    let list = byType.get(type);
    if (!list) { list = []; byType.set(type, list); }
    list.push(fn);
  };
  globalThis.__remove_listener = function (id, type, fn) {
    const byType = __listeners.get(id);
    if (!byType) return;
    const list = byType.get(type);
    if (!list) return;
    const idx = list.indexOf(fn);
    if (idx >= 0) list.splice(idx, 1);
  };
  globalThis.__dispatch = function (id, type) {
    const byType = __listeners.get(id);
    if (!byType) return 0;
    const list = byType.get(type);
    if (!list || !list.length) return 0;
    const self = new Element(id);
    const event = { type: type, target: self, defaultPrevented: false,
                    preventDefault() { this.defaultPrevented = true; },
                    stopPropagation() {} };
    for (const fn of list.slice()) fn.call(self, event);
    return 1;
  };

  Element.prototype.addEventListener = function (type, fn) {
    __add_listener(this.__id, String(type), fn);
  };
  Element.prototype.removeEventListener = function (type, fn) {
    __remove_listener(this.__id, String(type), fn);
  };

  const document = {
    get body() { const id = __dom_body(); return id < 0 ? null : new Element(id); },
    getElementById(id) { const n = __dom_get_id(String(id)); return n < 0 ? null : new Element(n); },
    createElement(tag) { return new Element(__dom_create(String(tag))); },
    querySelector(sel) { const n = __dom_query(String(sel)); return n < 0 ? null : new Element(n); },
    title: "",
  };
  globalThis.document = document;
})();
"#;

impl QjsDom {
    fn new(doc_ptr: *mut BaseDocument) -> QjsDom {
        unsafe {
            let rt = sys::JS_NewRuntime();
            assert!(!rt.is_null(), "quickjs: JS_NewRuntime failed");
            let ctx = sys::JS_NewContext(rt);
            assert!(!ctx.is_null(), "quickjs: JS_NewContext failed");
            sys::JS_SetContextOpaque(ctx, doc_ptr as *mut c_void);

            let global = sys::JS_GetGlobalObject(ctx);
            install(ctx, global, c"__dom_get_id", dom_get_id, 1);
            install(ctx, global, c"__dom_body", dom_body, 0);
            install(ctx, global, c"__dom_create", dom_create, 1);
            install(ctx, global, c"__dom_append", dom_append, 2);
            install(ctx, global, c"__dom_set_attr", dom_set_attr, 3);
            install(ctx, global, c"__dom_set_text", dom_set_text, 2);
            install(ctx, global, c"__dom_text", dom_text, 1);
            install(ctx, global, c"__dom_attr", dom_attr, 2);
            install(ctx, global, c"__dom_tag", dom_tag, 1);
            install(ctx, global, c"__dom_query", dom_query, 1);

            let console = sys::JS_NewObject(ctx);
            install(ctx, console, c"log", console_log, 1);
            sys::JS_SetPropertyStr(ctx, global, c"console".as_ptr(), console);

            sys::JS_FreeValue(ctx, global);

            let runtime = QjsDom { rt, ctx };
            runtime.eval(BOOTSTRAP, "bootstrap.js").ok();
            runtime
        }
    }

    fn eval(&self, source: &str, filename: &str) -> Result<(), String> {
        unsafe {
            let code = std::ffi::CString::new(source)
                .map_err(|_| "JS source contains NUL".to_string())?;
            let name = std::ffi::CString::new(filename).unwrap_or_default();
            let value = sys::JS_Eval(
                self.ctx,
                code.as_ptr(),
                code.as_bytes().len(),
                name.as_ptr(),
                sys::JS_EVAL_TYPE_GLOBAL,
            );
            let result = if sys::js_is_exception(value) {
                Err(self.take_exception())
            } else {
                Ok(())
            };
            sys::JS_FreeValue(self.ctx, value);
            let mut ctx: *mut JSContext = std::ptr::null_mut();
            while sys::JS_ExecutePendingJob(self.rt, &mut ctx) > 0 {}
            result
        }
    }

    /// Call the JS listener registry for one node and event name.
    fn dispatch(&self, id: usize, event: &str) {
        let source = format!("__dispatch({id}, \"{event}\")");
        if let Err(err) = self.eval(&source, "dispatch.js") {
            eprintln!("browser-lite: js dispatch error: {err}");
        }
    }

    fn take_exception(&self) -> String {        unsafe {
            let exc = sys::JS_GetException(self.ctx);
            let mut len = 0usize;
            let ptr = sys::JS_ToCStringLen2(self.ctx, &mut len, exc, false);
            let message = if ptr.is_null() {
                "unknown JS error".into()
            } else {
                let bytes = std::slice::from_raw_parts(ptr as *const u8, len);
                let s = String::from_utf8_lossy(bytes).into_owned();
                sys::JS_FreeCString(self.ctx, ptr);
                s
            };
            sys::JS_FreeValue(self.ctx, exc);
            message
        }
    }
}

impl Drop for QjsDom {
    fn drop(&mut self) {
        unsafe {
            sys::JS_FreeContext(self.ctx);
            sys::JS_FreeRuntime(self.rt);
        }
    }
}

unsafe fn install(ctx: *mut JSContext, obj: JSValue, name: &std::ffi::CStr, func: sys::JSCFunction, len: c_int) {
    let value = sys::JS_NewCFunction2(ctx, func, name.as_ptr(), len, sys::JS_CFUNC_GENERIC, 0);
    sys::JS_SetPropertyStr(ctx, obj, name.as_ptr(), value);
}

fn js_int(value: i64) -> JSValue {
    let v = if value < 0 { -1 } else { value as i32 };
    JSValue {
        u: JSValueUnion { int32: v },
        tag: 0, // JS_TAG_INT
    }
}

fn doc<'a>(ctx: *mut JSContext) -> &'a mut BaseDocument {
    unsafe { &mut *(sys::JS_GetContextOpaque(ctx) as *mut BaseDocument) }
}

unsafe fn arg_string(ctx: *mut JSContext, argv: *mut JSValue, index: c_int) -> String {
    let value = *argv.add(index as usize);
    let mut len = 0usize;
    let ptr = sys::JS_ToCStringLen2(ctx, &mut len, value, false);
    if ptr.is_null() {
        return String::new();
    }
    let bytes = std::slice::from_raw_parts(ptr as *const u8, len);
    let out = String::from_utf8_lossy(bytes).into_owned();
    sys::JS_FreeCString(ctx, ptr);
    out
}

unsafe fn arg_int(ctx: *mut JSContext, argv: *mut JSValue, index: c_int) -> i32 {
    use std::os::raw::c_longlong;
    let value = *argv.add(index as usize);
    let mut out: c_longlong = 0;
    sys::JS_ToInt64(ctx, &mut out, value);
    out as i32
}

fn qual_name(tag: &str) -> QualName {
    QualName::new(None, ns!(html), LocalName::from(tag))
}

unsafe extern "C" fn dom_get_id(ctx: *mut JSContext, _t: JSValue, _c: c_int, argv: *mut JSValue) -> JSValue {
    let id = arg_string(ctx, argv, 0);
    js_int(doc(ctx).get_element_by_id(&id).map(|n| n as i64).unwrap_or(-1))
}

unsafe extern "C" fn dom_body(ctx: *mut JSContext, _t: JSValue, _c: c_int, _a: *mut JSValue) -> JSValue {
    let found = doc(ctx).query_selector("body").ok().flatten();
    js_int(found.map(|n| n as i64).unwrap_or(-1))
}

unsafe extern "C" fn dom_query(ctx: *mut JSContext, _t: JSValue, _c: c_int, argv: *mut JSValue) -> JSValue {
    let selector = arg_string(ctx, argv, 0);
    let found = doc(ctx).query_selector(&selector).ok().flatten();
    js_int(found.map(|n| n as i64).unwrap_or(-1))
}

unsafe extern "C" fn dom_create(ctx: *mut JSContext, _t: JSValue, _c: c_int, argv: *mut JSValue) -> JSValue {
    let tag = arg_string(ctx, argv, 0);
    let d = doc(ctx);
    let mut mutator = d.mutate();
    let id = mutator.create_element(qual_name(&tag), Vec::new());
    js_int(id as i64)
}

unsafe extern "C" fn dom_append(ctx: *mut JSContext, _t: JSValue, _c: c_int, argv: *mut JSValue) -> JSValue {
    let parent = arg_int(ctx, argv, 0) as usize;
    let child = arg_int(ctx, argv, 1) as usize;
    let d = doc(ctx);
    let mut mutator = d.mutate();
    mutator.append_children(parent, &[child]);
    JSValue::undefined()
}

unsafe extern "C" fn dom_set_attr(ctx: *mut JSContext, _t: JSValue, _c: c_int, argv: *mut JSValue) -> JSValue {
    let id = arg_int(ctx, argv, 0) as usize;
    let name = arg_string(ctx, argv, 1);
    let value = arg_string(ctx, argv, 2);
    let d = doc(ctx);
    let mut mutator = d.mutate();
    mutator.set_attribute(id, qual_name(&name), &value);
    JSValue::undefined()
}

unsafe extern "C" fn dom_set_text(ctx: *mut JSContext, _t: JSValue, _c: c_int, argv: *mut JSValue) -> JSValue {
    let id = arg_int(ctx, argv, 0) as usize;
    let text = arg_string(ctx, argv, 1);
    let d = doc(ctx);
    let mut mutator = d.mutate();
    // DOM `textContent` replaces all children with a single text node.
    mutator.remove_and_drop_all_children(id);
    let text_id = mutator.create_text_node(&text);
    mutator.append_children(id, &[text_id]);
    JSValue::undefined()
}

unsafe extern "C" fn dom_text(ctx: *mut JSContext, _t: JSValue, _c: c_int, argv: *mut JSValue) -> JSValue {
    let id = arg_int(ctx, argv, 0) as usize;
    let d = doc(ctx);
    let text = d.get_node(id).map(|n| n.text_content()).unwrap_or_default();
    string_value(ctx, &text)
}

unsafe extern "C" fn dom_attr(ctx: *mut JSContext, _t: JSValue, _c: c_int, argv: *mut JSValue) -> JSValue {
    let id = arg_int(ctx, argv, 0) as usize;
    let name = arg_string(ctx, argv, 1);
    let d = doc(ctx);
    let value = d
        .get_node(id)
        .and_then(|n| n.attr(LocalName::from(name.as_str())).map(|v| v.to_string()));
    match value {
        Some(v) => string_value(ctx, &v),
        None => JSValue::undefined(),
    }
}

unsafe extern "C" fn dom_tag(ctx: *mut JSContext, _t: JSValue, _c: c_int, argv: *mut JSValue) -> JSValue {
    let id = arg_int(ctx, argv, 0) as usize;
    let d = doc(ctx);
    let tag = d
        .get_node(id)
        .and_then(|n| n.data.downcast_element().map(|e| e.name.local.to_string()))
        .unwrap_or_default();
    string_value(ctx, &tag)
}

unsafe extern "C" fn console_log(ctx: *mut JSContext, _t: JSValue, argc: c_int, argv: *mut JSValue) -> JSValue {
    let mut parts = Vec::new();
    for i in 0..argc.max(0) {
        parts.push(arg_string(ctx, argv, i));
    }
    println!("[js] {}", parts.join(" "));
    JSValue::undefined()
}

fn string_value(ctx: *mut JSContext, s: &str) -> JSValue {
    unsafe { sys::JS_NewStringLen(ctx, s.as_ptr() as *const c_char, s.len()) }
}

/// MVP script extraction: inline `<script>` bodies, no `src` yet.
fn extract_inline_scripts(html: &str) -> Vec<String> {
    let mut out = Vec::new();
    let lower = html.to_ascii_lowercase();
    let mut cursor = 0;
    while let Some(start_rel) = lower[cursor..].find("<script") {
        let start = cursor + start_rel;
        let Some(open_rel) = html[start..].find('>') else { break };
        let body_start = start + open_rel + 1;
        let Some(end_rel) = lower[body_start..].find("</script") else { break };
        let body_end = body_start + end_rel;
        let body = &html[body_start..body_end];
        if !body.trim().is_empty() {
            out.push(body.to_string());
        }
        cursor = lower[body_end..].find('>').map(|p| body_end + p + 1).unwrap_or(html.len());
    }
    out
}
