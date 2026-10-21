//! QuickJS runtime (system libqjs) with the minimal web globals for cut 1:
//! `console.log` and a mutable `document.title`.

use std::ffi::CString;
use std::os::raw::{c_char, c_int};

use crate::sys::{self, JSContext, JSValue};

pub struct JsRuntime {
    rt: *mut sys::JSRuntime,
    ctx: *mut JSContext,
    document: JSValue,
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
            let log = sys::JS_NewCFunction2(
                ctx,
                js_console_log,
                c"log".as_ptr(),
                1,
                sys::JS_CFUNC_GENERIC,
                0,
            );
            sys::JS_SetPropertyStr(ctx, console, c"log".as_ptr(), log);
            sys::JS_SetPropertyStr(ctx, global, c"console".as_ptr(), console);

            let document = sys::JS_NewObject(ctx);
            let title_value = js_string(ctx, title);
            sys::JS_SetPropertyStr(ctx, document, c"title".as_ptr(), title_value);
            sys::JS_SetPropertyStr(ctx, global, c"document".as_ptr(), document);

            // `global` and the objects reachable from it are owned by the
            // context; freeing them here would drop `document` too.
            Ok(JsRuntime {
                rt,
                ctx,
                document,
                title: title.to_string(),
            })
        }
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
    let mut parts = Vec::new();
    for i in 0..argc.max(0) {
        let value = *argv.add(i as usize);
        if let Some(s) = to_string(ctx, value) {
            parts.push(s);
        }
    }
    println!("[js] {}", parts.join(" "));
    JSValue::undefined()
}
