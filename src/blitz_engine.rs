//! Blitz renderer + QuickJS scripting.
//!
//! Blitz owns HTML/CSS/layout/paint. QuickJS runs the page scripts and mutates
//! the same `BaseDocument` through a small DOM API. This module is the bridge;
//! the rendering stack is Blitz's, the engine is ours.

use std::any::Any;
use std::sync::Arc;
use std::ops::{Deref, DerefMut};
use std::os::raw::{c_char, c_int, c_void};

use blitz_dom::net::Resource;
use blitz_dom::{BaseDocument, Document, DocumentConfig};
use blitz_html::HtmlDocument;
use blitz_shell::{create_default_event_loop, BlitzApplication, BlitzShellNetCallback, WindowConfig};
use blitz_traits::events::UiEvent;
use blitz_traits::navigation::NavigationOptions;
use blitz_traits::net::NetProvider;
use markup5ever::{ns, LocalName, QualName};

use crate::sys::{self, JSContext, JSValue, JSValueUnion};

/// A Blitz document with a persistent QuickJS context and its scripts executed.
///
/// `base` lives in a `Box` so its address stays stable while the document is
/// moved into the window: QuickJS callbacks hold a raw pointer to it.
pub struct JsDocument {
    base: Box<BaseDocument>,
    rt: QjsDom,
    base_url: Option<String>,
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
            UiEvent::MouseDown(e) => {
                // Hover first: active_node() is a no-op without a hover node.
                self.base.set_hover_to(e.x, e.y);
                self.base.active_node();
                if let Some(id) = self.base.get_hover_node_id() {
                    self.base.set_focus_to(id);
                }
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
                for id in &chain {
                    self.rt.dispatch(*id, "click");
                }
                // Default action for links: our handler bypasses Blitz's
                // internal click handling, so resolve the href ourselves.
                self.follow_link(&chain);
                self.base.shell_provider.request_redraw();
            }
            UiEvent::KeyDown(e) => {
                self.dispatch_key("keydown", &e.key.to_string(), &e.code.to_string());
            }
            UiEvent::KeyUp(e) => {
                self.dispatch_key("keyup", &e.key.to_string(), &e.code.to_string());
            }
            _ => {}
        }
    }

    fn poll(&mut self, _task_context: Option<std::task::Context<'_>>) -> bool {
        // Run due timers; stay alive (return true) while any timer is pending
        // so a pending timer fires without further input: poll only runs
        // after a winit event, and true chains redraw -> Poll.
        match self.rt.run_due_timers() {
            Ok((ran, pending)) => {
                if ran > 0 {
                    self.base.shell_provider.request_redraw();
                    true
                } else {
                    pending > 0
                }
            }
            Err(err) => {
                eprintln!("browser-lite: js timer error: {err}");
                false
            }
        }
    }

    fn as_any_mut(&mut self) -> &mut dyn Any {
        self
    }
}

impl JsDocument {
    /// Resolve the first anchor in the chain and hand it to the navigation
    /// provider (the embedder loads it and swaps the document).
    fn follow_link(&self, chain: &[usize]) {
        for id in chain {
            let Some(node) = self.base.get_node(*id) else { continue };
            let tag = node
                .data
                .downcast_element()
                .map(|e| e.name.local.to_string())
                .unwrap_or_default();
            if tag != "a" {
                continue;
            }
            let Some(href) = node.attr(LocalName::from("href")) else { continue };
            if let Some(target) = self.resolve_url(href) {
                self.base.navigation_provider.navigate_to(NavigationOptions::new(
                    target,
                    "text/html".to_string(),
                    self.base.id(),
                ));
            }
            break;
        }
    }

    fn resolve_url(&self, href: &str) -> Option<url::Url> {
        if let Ok(absolute) = url::Url::parse(href) {
            return Some(absolute);
        }
        let base = self.base_url.as_deref()?;
        url::Url::parse(base).ok()?.join(href).ok()
    }

    /// Bubble a keyboard event from the focused node to the root.
    fn dispatch_key(&mut self, event: &str, key: &str, code: &str) {
        let target = match self.base.get_focussed_node_id() {
            Some(id) => id,
            None => return,
        };
        let mut chain = Vec::new();
        let mut current = Some(target);
        while let Some(id) = current {
            chain.push(id);
            current = self.base.get_node(id).and_then(|n| n.parent);
        }
        for id in chain {
            self.rt.dispatch_key(id, event, key, code);
        }
        self.base.shell_provider.request_redraw();
    }
}

/// Parse HTML, run its inline scripts with QuickJS, and return a renderable
/// document. Used by both the single-window and tabbed entry points.
pub fn build_document(html: &str) -> Box<dyn Document> {
    build_document_with(html, None, None, None)
}

/// Net provider that injects fetched subresources (CSS, images, fonts) into
/// the Blitz shell event loop.
pub fn net_provider(
    proxy: blitz_shell::EventLoopProxy<blitz_shell::BlitzShellEvent>,
) -> Arc<dyn NetProvider<Resource>> {
    let callback = BlitzShellNetCallback::shared(proxy);
    Arc::new(blitz_net::Provider::new(callback))
}

/// Build a document with a base URL (for relative links) and an optional
/// navigation provider (link clicks enqueue requests instead of doing nothing).
pub fn build_document_with(
    html: &str,
    base_url: Option<String>,
    navigation_provider: Option<std::sync::Arc<dyn blitz_traits::navigation::NavigationProvider>>,
    net_provider: Option<Arc<dyn NetProvider<Resource>>>,
) -> Box<dyn Document> {
    // `ua_stylesheets: Some(..)` makes blitz-html append its DEFAULT_CSS
    // (display:block defaults etc.). Without it every box collapses.
    let config = DocumentConfig {
        ua_stylesheets: Some(Vec::new()),
        base_url: base_url.clone(),
        navigation_provider,
        net_provider,
        ..Default::default()
    };
    // Blitz does not propagate the body background to the canvas, so the
    // window shows the desktop below the content. A fixed, behind-everything
    // div painted with the body colour fills the viewport.
    let color = canvas_background(html);
    let html = inject_canvas(html, &color);

    let parsed = HtmlDocument::from_html(&html, config);
    let mut base = Box::new(BaseDocument::from(parsed));

    let scripts = extract_inline_scripts(&html);
    let doc_ptr: *mut BaseDocument = &mut *base;
    let rt = QjsDom::new(doc_ptr);
    for (i, script) in scripts.iter().enumerate() {
        if let Err(err) = rt.eval(script, &format!("inline-{i}.js")) {
            eprintln!("browser-lite: js error: {err}");
        }
    }

    Box::new(JsDocument {
        base,
        rt,
        base_url,
    })
}

/// Show one document.
pub fn run(html: &str) -> Result<(), String> {
    run_documents(vec![build_document(html)])
}

/// Show one HTML document with subresource loading enabled.
pub fn run_html(html: &str, base_url: Option<String>) -> Result<(), String> {
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .map_err(|e| format!("tokio runtime: {e}"))?;
    let _guard = runtime.enter();

    let event_loop = create_default_event_loop();
    let provider = net_provider(event_loop.create_proxy());
    let doc = build_document_with(html, base_url, None, Some(provider));

    let renderer = anyrender_vello_cpu::VelloCpuWindowRenderer::new();
    let window = WindowConfig::new(doc, renderer);
    let mut application = BlitzApplication::new(event_loop.create_proxy());
    application.add_window(window);
    event_loop
        .run_app(&mut application)
        .map_err(|e| format!("event loop: {e}"))
}

/// Show one window per document (each window is a tab in the current MVP).
pub fn run_documents(docs: Vec<Box<dyn Document>>) -> Result<(), String> {
    let event_loop = create_default_event_loop();
    let mut application = BlitzApplication::new(event_loop.create_proxy());
    for doc in docs {
        // CPU renderer: the vello/wgpu path faults on NVK/Kepler.
        let renderer = anyrender_vello_cpu::VelloCpuWindowRenderer::new();
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
  globalThis.__dispatch = function (id, type, key, code) {
    const byType = __listeners.get(id);
    if (!byType) return 0;
    const list = byType.get(type);
    if (!list || !list.length) return 0;
    const self = new Element(id);
    const event = { type: type, target: self, key: key, code: code,
                    defaultPrevented: false,
                    preventDefault() { this.defaultPrevented = true; },
                    stopPropagation() {} };
    for (const fn of list.slice()) fn.call(self, event);
    return 1;
  };

  const __timers = new Map();
  let __nextTimerId = 1;
  globalThis.setTimeout = function (fn, delay, ...args) {
    const ms = Number(delay) || 0;
    const due = Date.now() + Math.max(0, ms);
    const id = __nextTimerId++;
    __timers.set(id, { fn: fn, due: due, args: args });
    return id;
  };
  globalThis.clearTimeout = function (id) { __timers.delete(Number(id)); };
  globalThis.__run_due_timers = function (now) {
    const due = [];
    for (const entry of __timers) {
      if (entry[1].due <= now) due.push(entry);
    }
    due.sort((a, b) => (a[1].due - b[1].due) || (a[0] - b[0]));
    let ran = 0;
    for (const entry of due) {
      const id = entry[0];
      const t = entry[1];
      if (!__timers.has(id)) continue;
      __timers.delete(id);
      try {
        if (typeof t.fn === "function") t.fn(...t.args);
        else eval(t.fn);
      } catch (e) { console.log("timer error", e); }
      ran++;
    }
    return ran;
  };
  globalThis.__pending_timers = function () { return __timers.size; };

  Element.prototype.addEventListener = function (type, fn) {
    __add_listener(this.__id, String(type), fn);
  };
  Element.prototype.removeEventListener = function (type, fn) {
    __remove_listener(this.__id, String(type), fn);
  };

  const document = {
    get body() { const id = __dom_body(); return id < 0 ? null : new Element(id); },
    get activeElement() { const id = __dom_active(); return id < 0 ? null : new Element(id); },
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
            install(ctx, global, c"__dom_active", dom_active, 0);

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

    /// Call the JS listener registry for one node with key/code (keyboard).
    /// Key strings travel through JS globals to avoid source escaping.
    fn dispatch_key(&self, id: usize, event: &str, key: &str, code: &str) {
        unsafe {
            let global = sys::JS_GetGlobalObject(self.ctx);
            let k = sys::JS_NewStringLen(self.ctx, key.as_ptr() as *const c_char, key.len());
            sys::JS_SetPropertyStr(self.ctx, global, c"__key_arg".as_ptr(), k);
            let c = sys::JS_NewStringLen(self.ctx, code.as_ptr() as *const c_char, code.len());
            sys::JS_SetPropertyStr(self.ctx, global, c"__code_arg".as_ptr(), c);
            sys::JS_FreeValue(self.ctx, global);
        }
        let source = format!("__dispatch({id}, \"{event}\", globalThis.__key_arg, globalThis.__code_arg)");
        if let Err(err) = self.eval(&source, "dispatch.js") {
            eprintln!("browser-lite: js dispatch error: {err}");
        }
    }

    /// Evaluate JS and read an i64 result (used by the timer tick).
    fn eval_i64(&self, source: &str) -> Result<i64, String> {
        unsafe {
            let code = std::ffi::CString::new(source)
                .map_err(|_| "JS source contains NUL".to_string())?;
            let name = std::ffi::CString::new("tick.js").unwrap_or_default();
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
                let mut out: i64 = 0;
                sys::JS_ToInt64(self.ctx, &mut out, value);
                Ok(out)
            };
            sys::JS_FreeValue(self.ctx, value);
            let mut ctx: *mut JSContext = std::ptr::null_mut();
            while sys::JS_ExecutePendingJob(self.rt, &mut ctx) > 0 {}
            result
        }
    }

    /// Run due timers; returns (ran, pending).
    fn run_due_timers(&self) -> Result<(i64, i64), String> {
        let ran = self.eval_i64("globalThis.__run_due_timers ? globalThis.__run_due_timers(Date.now()) : 0")?;
        let pending = self.eval_i64("globalThis.__pending_timers ? globalThis.__pending_timers() : 0")?;
        Ok((ran, pending))
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

unsafe extern "C" fn dom_active(ctx: *mut JSContext, _t: JSValue, _c: c_int, _a: *mut JSValue) -> JSValue {
    let focused = doc(ctx).get_focussed_node_id();
    js_int(focused.map(|n| n as i64).unwrap_or(-1))
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

/// Best-effort body background: inline style, then `<style>` rules whose
/// selector is exactly `body` (later rules win). Defaults to white.
fn canvas_background(html: &str) -> String {
    let mut color = "#ffffff".to_string();

    if let Some(tag_start) = find_ci(html, "<body") {
        if let Some(tag_end_rel) = html[tag_start..].find('>') {
            let tag = &html[tag_start..tag_start + tag_end_rel];
            if let Some(style) = attr_value(tag, "style") {
                if let Some(value) = background_decl(&style) {
                    color = value;
                }
            }
        }
    }

    let mut cursor = 0;
    while let Some(open_rel) = find_ci(&html[cursor..], "<style") {
        let open = cursor + open_rel;
        let Some(body_start_rel) = html[open..].find('>') else { break };
        let body_start = open + body_start_rel + 1;
        let Some(close_rel) = find_ci(&html[body_start..], "</style") else { break };
        let css = &html[body_start..body_start + close_rel];
        let mut rest = css;
        while let Some(brace) = rest.find('{') {
            let selector = rest[..brace].trim();
            let Some(close) = rest[brace..].find('}') else { break };
            let decls = &rest[brace + 1..brace + close];
            let is_body = selector
                .split(',')
                .any(|part| part.trim().eq_ignore_ascii_case("body"));
            if is_body {
                if let Some(value) = background_decl(decls) {
                    color = value;
                }
            }
            rest = &rest[brace + close + 1..];
        }
        cursor = body_start + close_rel + "</style".len();
    }
    color
}

fn background_decl(decls: &str) -> Option<String> {
    let mut found = None;
    for decl in decls.split(';') {
        let Some((prop, value)) = decl.split_once(':') else { continue };
        let prop = prop.trim().to_ascii_lowercase();
        if prop == "background" || prop == "background-color" {
            let value = value.trim();
            if !value.is_empty() && !value.contains("url(") {
                found = Some(value.to_string());
            }
        }
    }
    found
}

fn attr_value<'a>(tag: &'a str, name: &str) -> Option<&'a str> {
    let lower = tag.to_ascii_lowercase();
    let idx = lower.find(&format!("{name}="))?;
    let after = &tag[idx + name.len() + 1..];
    let quote = after.chars().next()?;
    if quote == '"' || quote == '\'' {
        let rest = &after[1..];
        let end = rest.find(quote)?;
        Some(&rest[..end])
    } else {
        let end = after.find(|c: char| c.is_whitespace() || c == '>').unwrap_or(after.len());
        Some(&after[..end])
    }
}

fn inject_canvas(html: &str, color: &str) -> String {
    let Some(start) = find_ci(html, "<body") else {
        return html.to_string();
    };
    let Some(end_rel) = html[start..].find('>') else {
        return html.to_string();
    };
    let insert_at = start + end_rel + 1;
    let layer = format!(
        "<div style=\"position:fixed;left:0;top:0;width:100vw;height:100vh;z-index:-1;background:{color}\"></div>"
    );
    let mut out = String::with_capacity(html.len() + layer.len());
    out.push_str(&html[..insert_at]);
    out.push_str(&layer);
    out.push_str(&html[insert_at..]);
    out
}

fn find_ci(haystack: &str, needle: &str) -> Option<usize> {
    haystack
        .to_ascii_lowercase()
        .find(&needle.to_ascii_lowercase())
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
