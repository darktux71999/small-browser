//! QuickJS runtime (system libqjs) with the minimal web globals page scripts
//! expect: `console`, a mutable `document.title`, and a `window` surface.
//!
//! `window` is the global object itself (`window === globalThis`), so
//! `window.foo` and bare `foo` are the same binding. On top of that the
//! bootstrap installs:
//!
//! - `location` with read-only `href`, `protocol`, `host`, `hostname` and
//!   `pathname` (filled by [`JsRuntime::set_url`]; `about:blank` by default).
//!   Assigning to those members does not navigate: there is no navigation yet.
//! - `addEventListener` / `removeEventListener` for `DOMContentLoaded` and
//!   `load`.
//! - `navigator.userAgent`.
//!
//! The engine has no parser or event loop, so page lifecycle events fire at the
//! end of every [`JsRuntime::eval`] call for the handlers registered so far
//! (each handler runs once per event type). Handler exceptions are reported
//! through `console.error` and do not stop the remaining handlers. Script
//! errors themselves keep bubbling out of `eval` so the engine can report them
//! without aborting the page.

use std::ffi::CString;
use std::os::raw::{c_char, c_int};

use url::Url;

use crate::sys::{self, JSContext, JSValue};

/// Kept in sync with `net::USER_AGENT` so script-visible identity matches the
/// requests the engine actually sends.
const USER_AGENT: &str = "browser-lite/0.1";

/// Installed once per runtime, before any page script runs.
const BOOTSTRAP: &str = r#"
(function () {
  "use strict";
  const g = globalThis;

  // Mutable state behind the read-only `location` view; Rust writes it.
  const state = { href: "", protocol: "", host: "", hostname: "", pathname: "" };
  Object.defineProperty(g, "__bl_location", {
    value: state, writable: false, enumerable: false, configurable: false,
  });

  const location = {};
  for (const key of ["href", "protocol", "host", "hostname", "pathname"]) {
    Object.defineProperty(location, key, {
      get: function () { return g.__bl_location[key]; },
      enumerable: true,
      configurable: false,
    });
  }
  Object.defineProperty(location, "toString", {
    value: function () { return g.__bl_location.href; },
    enumerable: false,
  });
  Object.defineProperty(g, "location", {
    value: location, writable: false, enumerable: true, configurable: false,
  });

  const listeners = new Map();
  function register(type, fn) {
    if (typeof fn !== "function" && !(fn && typeof fn.handleEvent === "function")) return;
    const key = String(type);
    let list = listeners.get(key);
    if (!list) {
      list = [];
      listeners.set(key, list);
    }
    if (list.indexOf(fn) === -1) list.push(fn);
  }
  function unregister(type, fn) {
    const list = listeners.get(String(type));
    if (!list) return;
    const at = list.indexOf(fn);
    if (at !== -1) list.splice(at, 1);
  }
  Object.defineProperty(g, "addEventListener", {
    value: function (type, fn) { register(type, fn); },
    writable: true, enumerable: true, configurable: true,
  });
  Object.defineProperty(g, "removeEventListener", {
    value: function (type, fn) { unregister(type, fn); },
    writable: true, enumerable: true, configurable: true,
  });

  function report(err) {
    try {
      console.error("uncaught error in event handler: " + (err && err.message ? err.message : String(err)));
    } catch (_) {}
  }

  // Fires and consumes the handlers registered for one lifecycle event.
  Object.defineProperty(g, "__bl_dispatch", {
    value: function (type) {
      const key = String(type);
      const list = listeners.get(key);
      if (!list || list.length === 0) return 0;
      const handlers = list.slice();
      list.length = 0;
      const event = { type: key, target: g, currentTarget: g, bubbles: false };
      for (const fn of handlers) {
        try {
          if (typeof fn === "function") fn.call(g, event);
          else fn.handleEvent(event);
        } catch (err) {
          report(err);
        }
      }
      return handlers.length;
    },
    writable: false, enumerable: false, configurable: false,
  });

  // `window` is the global alias: `window.foo` and bare `foo` are one binding.
  Object.defineProperty(g, "window", {
    value: g, writable: false, enumerable: true, configurable: false,
  });
})();
"#;

pub struct JsRuntime {
    rt: *mut sys::JSRuntime,
    ctx: *mut JSContext,
    document: JSValue,
    /// Backing object behind the JS `location` getters (`__bl_location`).
    location: JSValue,
    title: String,
}

impl JsRuntime {
    pub fn new(title: &str) -> Result<JsRuntime, String> {
        unsafe {
            let rt = sys::JS_NewRuntime();
            if rt.is_null() {
                return Err("quickjs: JS_NewRuntime failed".into());
            }
            let ctx = sys::JS_NewContext(rt);
            if ctx.is_null() {
                sys::JS_FreeRuntime(rt);
                return Err("quickjs: JS_NewContext failed".into());
            }

            let global = sys::JS_GetGlobalObject(ctx);

            let console = sys::JS_NewObject(ctx);
            for (name, func) in [
                (c"log", js_console_log as sys::JSCFunction),
                (c"error", js_console_error as sys::JSCFunction),
                (c"warn", js_console_warn as sys::JSCFunction),
            ] {
                let f = sys::JS_NewCFunction2(ctx, func, name.as_ptr(), 1, sys::JS_CFUNC_GENERIC, 0);
                sys::JS_SetPropertyStr(ctx, console, name.as_ptr(), f);
            }
            sys::JS_SetPropertyStr(ctx, global, c"console".as_ptr(), console);

            let document = sys::JS_NewObject(ctx);
            let title_value = js_string(ctx, title);
            sys::JS_SetPropertyStr(ctx, document, c"title".as_ptr(), title_value);
            sys::JS_SetPropertyStr(ctx, global, c"document".as_ptr(), document);

            let navigator = sys::JS_NewObject(ctx);
            let user_agent = js_string(ctx, USER_AGENT);
            sys::JS_SetPropertyStr(ctx, navigator, c"userAgent".as_ptr(), user_agent);
            sys::JS_SetPropertyStr(ctx, global, c"navigator".as_ptr(), navigator);

            // `global` and the objects reachable from it are owned by the
            // context; freeing them here would drop `document` too.
            let mut runtime = JsRuntime {
                rt,
                ctx,
                document,
                location: JSValue::undefined(),
                title: title.to_string(),
            };

            runtime.eval_raw(BOOTSTRAP, "browser-lite:bootstrap")?;
            let state = sys::JS_GetPropertyStr(ctx, global, c"__bl_location".as_ptr());
            if sys::js_is_exception(state) {
                return Err(runtime.take_exception());
            }
            runtime.location = state;
            runtime.set_url("about:blank")?;

            Ok(runtime)
        }
    }

    /// Sets the document URL exposed through `location`.
    ///
    /// Absolute URLs only: the engine parses it with the `url` crate, which is
    /// also how subresource URLs are resolved elsewhere. On error the previous
    /// location stays untouched.
    pub fn set_url(&mut self, url: &str) -> Result<(), String> {
        let parsed = Url::parse(url).map_err(|err| format!("invalid URL {url:?}: {err}"))?;
        let hostname = parsed.host_str().unwrap_or_default().to_string();
        let host = match parsed.port() {
            Some(port) => format!("{hostname}:{port}"),
            None => hostname.clone(),
        };
        let pathname = if parsed.path().is_empty() {
            "/".to_string()
        } else {
            parsed.path().to_string()
        };
        let mut protocol = parsed.scheme().to_string();
        protocol.push(':');

        unsafe {
            self.set_location_member(c"href", parsed.as_str());
            self.set_location_member(c"protocol", &protocol);
            self.set_location_member(c"host", &host);
            self.set_location_member(c"hostname", &hostname);
            self.set_location_member(c"pathname", &pathname);
        }
        Ok(())
    }

    /// Test-only access to the raw context.
    pub fn ctx_ptr(&mut self) -> *mut JSContext {
        self.ctx
    }

    /// Test-only access to the document handle.
    pub fn document_value(&mut self) -> JSValue {
        self.document
    }

    pub fn eval(&mut self, source: &str, filename: &str) -> Result<(), String> {
        let result = self.eval_raw(source, filename);
        // No parser or event loop exists, so lifecycle events for the handlers
        // registered so far fire here, after each script. Page events must never
        // mask the script's own error.
        let events = self.dispatch_page_events();
        match result {
            Ok(()) => events,
            Err(err) => Err(err),
        }
    }

    pub fn title(&mut self) -> String {
        unsafe {
            let value = sys::JS_GetPropertyStr(self.ctx, self.document, c"title".as_ptr());
            if let Some(s) = to_string(self.ctx, value) {
                self.title = s;
            }
            sys::JS_FreeValue(self.ctx, value);
            self.title.clone()
        }
    }

    fn eval_raw(&mut self, source: &str, filename: &str) -> Result<(), String> {
        unsafe {
            // quickjs-ng 0.17 walks a NUL-terminated source even when a length
            // is passed: a bare &str (not NUL-terminated) parses garbage.
            let code = CString::new(source).map_err(|_| "JS source contains NUL".to_string())?;
            let name = CString::new(filename).unwrap_or_default();
            let value = sys::JS_Eval(
                self.ctx,
                code.as_ptr(),
                code.as_bytes().len(),
                name.as_ptr(),
                sys::JS_EVAL_TYPE_GLOBAL,
            );
            let mut result = Ok(());
            if sys::js_is_exception(value) {
                result = Err(self.take_exception());
            }
            sys::JS_FreeValue(self.ctx, value);
            self.drain_jobs();
            result
        }
    }

    fn dispatch_page_events(&mut self) -> Result<(), String> {
        self.eval_raw("__bl_dispatch('DOMContentLoaded');", "browser-lite:events")?;
        self.eval_raw("__bl_dispatch('load');", "browser-lite:events")
    }

    unsafe fn set_location_member(&mut self, name: &std::ffi::CStr, value: &str) {
        let value = js_string(self.ctx, value);
        sys::JS_SetPropertyStr(self.ctx, self.location, name.as_ptr(), value);
    }

    fn take_exception(&mut self) -> String {
        unsafe {
            let exc = sys::JS_GetException(self.ctx);
            let message = to_string(self.ctx, exc).unwrap_or_else(|| "unknown JS error".into());
            sys::JS_FreeValue(self.ctx, exc);
            message
        }
    }

    fn drain_jobs(&mut self) {
        unsafe {
            let mut ctx: *mut JSContext = std::ptr::null_mut();
            while sys::JS_ExecutePendingJob(self.rt, &mut ctx) > 0 {}
        }
    }
}

impl Drop for JsRuntime {
    fn drop(&mut self) {
        unsafe {
            // `location` is still referenced by the global property; this only
            // drops the handle we hold.
            sys::JS_FreeValue(self.ctx, self.location);
            sys::JS_FreeContext(self.ctx);
            sys::JS_FreeRuntime(self.rt);
        }
    }
}

fn to_string(ctx: *mut JSContext, value: JSValue) -> Option<String> {
    unsafe {
        let mut len = 0usize;
        let ptr = sys::JS_ToCStringLen2(ctx, &mut len, value, false);
        if ptr.is_null() {
            return None;
        }
        let bytes = std::slice::from_raw_parts(ptr as *const u8, len);
        let out = String::from_utf8_lossy(bytes).into_owned();
        sys::JS_FreeCString(ctx, ptr);
        Some(out)
    }
}

fn js_string(ctx: *mut JSContext, s: &str) -> JSValue {
    unsafe { sys::JS_NewStringLen(ctx, s.as_ptr() as *const c_char, s.len()) }
}

unsafe extern "C" fn js_console_log(
    ctx: *mut JSContext,
    _this: JSValue,
    argc: c_int,
    argv: *mut JSValue,
) -> JSValue {
    print_console(ctx, argc, argv, "")
}

unsafe extern "C" fn js_console_error(
    ctx: *mut JSContext,
    _this: JSValue,
    argc: c_int,
    argv: *mut JSValue,
) -> JSValue {
    print_console(ctx, argc, argv, "error: ")
}

unsafe extern "C" fn js_console_warn(
    ctx: *mut JSContext,
    _this: JSValue,
    argc: c_int,
    argv: *mut JSValue,
) -> JSValue {
    print_console(ctx, argc, argv, "warn: ")
}

unsafe fn print_console(
    ctx: *mut JSContext,
    argc: c_int,
    argv: *mut JSValue,
    prefix: &str,
) -> JSValue {
    let mut parts = Vec::new();
    for i in 0..argc.max(0) {
        let value = *argv.add(i as usize);
        if let Some(s) = to_string(ctx, value) {
            parts.push(s);
        }
    }
    println!("[js] {prefix}{}", parts.join(" "));
    JSValue::undefined()
}
