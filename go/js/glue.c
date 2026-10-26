/* C definitions for go/js/glue.h: QuickJS helpers the Go side cannot
 * express plus the lifecycle wrappers that keep C pointer types out of Go
 * signatures. Mirrors the oracle's src/js.rs call sequence (eval, drain
 * jobs, take-exception-by-coercion, console "[js] ..." lines). */

#include "glue.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* The Go side moves JSValues only as JSG_VALUE_SIZE blobs; fail loudly if
 * this QuickJS build lays JSValue out differently. */
_Static_assert(sizeof(JSValue) == JSG_VALUE_SIZE, "JSValue is not 16 bytes");

/* ------------------------------------------------------------------ */
/* Lifecycle                                                           */
/* ------------------------------------------------------------------ */

void *jsgo_new_runtime(void)
{
	return JS_NewRuntime();
}

void jsgo_free_runtime(void *rt)
{
	JS_FreeRuntime((JSRuntime *)rt);
}

void *jsgo_new_context(void *rt)
{
	return JS_NewContext((JSRuntime *)rt);
}

void jsgo_free_context(void *ctx)
{
	JS_FreeContext((JSContext *)ctx);
}

void jsgo_set_opaque(void *ctx, void *cookie)
{
	JS_SetContextOpaque((JSContext *)ctx, cookie);
}

/* ------------------------------------------------------------------ */
/* Console: "[js] ..." lines exactly like the oracle's print_console.  */
/* ------------------------------------------------------------------ */

static JSValue jsgo_console(JSContext *ctx, JSValueConst this_val, int argc,
	JSValueConst *argv, int magic)
{
	(void)this_val;
	const char *prefix = "";
	if (magic == 1)
		prefix = "error: ";
	else if (magic == 2)
		prefix = "warn: ";
	printf("[js] %s", prefix);
	for (int i = 0; i < argc; i++) {
		size_t len = 0;
		const char *s = JS_ToCStringLen(ctx, &len, argv[i]);
		if (i > 0)
			putchar(' ');
		if (s) {
			fwrite(s, 1, len, stdout);
			JS_FreeCString(ctx, s);
		}
	}
	putchar('\n');
	fflush(stdout);
	return JS_UNDEFINED;
}

void jsgo_install_console(void *ctxp)
{
	JSContext *ctx = (JSContext *)ctxp;
	static const struct {
		const char *name;
		int magic;
	} methods[] = {
		{"log", 0},
		{"error", 1},
		{"warn", 2},
	};
	JSValue global = JS_GetGlobalObject(ctx);
	JSValue console = JS_NewObject(ctx);
	for (size_t i = 0; i < sizeof(methods) / sizeof(methods[0]); i++) {
		JSValue fn = JS_NewCFunction2(ctx,
			(JSCFunction *)jsgo_console,
			methods[i].name, 1, JS_CFUNC_generic_magic,
			methods[i].magic);
		if (JS_IsException(fn)) {
			JS_FreeValue(ctx, fn);
			continue;
		}
		if (JS_SetPropertyStr(ctx, console, methods[i].name, fn) < 0)
			JS_FreeValue(ctx, fn);
	}
	if (JS_SetPropertyStr(ctx, global, "console", console) < 0)
		JS_FreeValue(ctx, console);
	JS_FreeValue(ctx, global);
}

/* ------------------------------------------------------------------ */
/* Evaluation (oracle order: eval, free value, drain pending jobs).     */
/* ------------------------------------------------------------------ */

void jsgo_eval_into(void *ctxp, const char *src, size_t len,
	const char *filename, void *out)
{
	JSContext *ctx = (JSContext *)ctxp;
	JSValue v = JS_Eval(ctx, src, len, filename, JS_EVAL_TYPE_GLOBAL);
	*(JSValue *)out = v;
	JSContext *job_ctx;
	while (JS_ExecutePendingJob(JS_GetRuntime(ctx), &job_ctx) > 0) {
		/* Jobs run for side effects; their values are discarded. */
	}
}

void jsgo_drain(void *rtp)
{
	JSRuntime *rt = (JSRuntime *)rtp;
	JSContext *job_ctx;
	while (JS_ExecutePendingJob(rt, &job_ctx) > 0) {
	}
}

/* ------------------------------------------------------------------ */
/* Values                                                              */
/* ------------------------------------------------------------------ */

int jsgo_is_exc(const void *v)
{
	return JS_IsException(*(const JSValue *)v);
}

void jsgo_get_exc_into(void *ctxp, void *out)
{
	*(JSValue *)out = JS_GetException((JSContext *)ctxp);
}

int jsgo_kind(void *ctxp, const void *vp)
{
	JSContext *ctx = (JSContext *)ctxp;
	const JSValue *v = (const JSValue *)vp;
	switch (JS_VALUE_GET_TAG(*v)) {
	case JS_TAG_UNDEFINED:
		return JSG_UNDEF;
	case JS_TAG_NULL:
		return JSG_NULL;
	case JS_TAG_BOOL:
		return JSG_BOOL;
	case JS_TAG_INT:
	case JS_TAG_FLOAT64:
		return JSG_NUMBER;
	case JS_TAG_STRING:
	case JS_TAG_STRING_ROPE:
		return JSG_STRING;
	default:
		break;
	}
	if (JS_IsFunction(ctx, *v))
		return JSG_FUNCTION;
	return JSG_OBJECT;
}

int32_t jsgo_int32(const void *v)
{
	return JS_VALUE_GET_INT(*(const JSValue *)v);
}

double jsgo_float64(const void *v)
{
	JSValue x = *(const JSValue *)v;
	if (JS_VALUE_GET_TAG(x) == JS_TAG_INT)
		return (double)JS_VALUE_GET_INT(x);
	return JS_VALUE_GET_FLOAT64(x);
}

int jsgo_bool_val(const void *v)
{
	return JS_VALUE_GET_BOOL(*(const JSValue *)v);
}

char *jsgo_cstr(void *ctxp, const void *vp, size_t *len)
{
	size_t n = 0;
	/* Coercion, exactly like the oracle's take_exception: Error objects
	 * stringify to "Name: message", thrown strings pass through. */
	const char *s = JS_ToCStringLen((JSContext *)ctxp, &n,
		*(const JSValue *)vp);
	if (!s)
		return NULL;
	if (len)
		*len = n;
	return (char *)s;
}

void jsgo_cstr_free(void *ctxp, char *p)
{
	if (p)
		JS_FreeCString((JSContext *)ctxp, p);
}

void jsgo_make_undefined(void *out)
{
	*(JSValue *)out = JS_UNDEFINED;
}

void jsgo_make_null(void *out)
{
	*(JSValue *)out = JS_NULL;
}

void jsgo_make_bool(int b, void *out)
{
	*(JSValue *)out = b ? JS_TRUE : JS_FALSE;
}

void jsgo_make_num(double d, void *out)
{
	/* JS_NewFloat64 ignores its context; NULL keeps C types out of Go. */
	*(JSValue *)out = JS_NewFloat64(NULL, d);
}

void jsgo_make_str(void *ctxp, const char *s, size_t len, void *out)
{
	*(JSValue *)out = JS_NewStringLen((JSContext *)ctxp, s, len);
}

void jsgo_dup_into(void *ctxp, const void *v, void *out)
{
	*(JSValue *)out = JS_DupValue((JSContext *)ctxp, *(const JSValue *)v);
}

void jsgo_free_val(void *ctxp, void *v)
{
	JS_FreeValue((JSContext *)ctxp, *(JSValue *)v);
	*(JSValue *)v = JS_UNDEFINED;
}

/* ------------------------------------------------------------------ */
/* Objects, globals and calls                                          */
/* ------------------------------------------------------------------ */

void jsgo_get_str_into(void *ctxp, const void *obj, const char *prop,
	void *out)
{
	JSContext *ctx = (JSContext *)ctxp;
	*(JSValue *)out = JS_GetPropertyStr(ctx, *(const JSValue *)obj, prop);
}

int jsgo_set_global(void *ctxp, const char *name, const void *v)
{
	JSContext *ctx = (JSContext *)ctxp;
	JSValue global = JS_GetGlobalObject(ctx);
	int rc = JS_SetPropertyStr(ctx, global, name,
		JS_DupValue(ctx, *(const JSValue *)v));
	JS_FreeValue(ctx, global);
	return rc;
}

/* Trampoline for Go functions: the runtime cookie rides in the context
 * opaque (never a Go pointer in C, only the integer cookie), the magic
 * selects the Go callback, and jsgoGoCall fills ret or throws. */
static JSValue jsgo_trampoline(JSContext *ctx, JSValueConst this_val,
	int argc, JSValueConst *argv, int magic)
{
	(void)this_val;
	void *cookie = JS_GetContextOpaque(ctx);
	JSValue ret = JS_UNDEFINED;
	if (jsgoGoCall(cookie, magic, ctx, (int)argc, argv, &ret))
		return JS_EXCEPTION;
	return ret;
}

int jsgo_set_global_func(void *ctxp, const char *name, int magic)
{
	JSContext *ctx = (JSContext *)ctxp;
	JSValue global = JS_GetGlobalObject(ctx);
	JSValue fn = JS_NewCFunction2(ctx, (JSCFunction *)jsgo_trampoline,
		name, 2, JS_CFUNC_generic_magic, magic);
	int rc = -1;
	if (!JS_IsException(fn))
		rc = JS_SetPropertyStr(ctx, global, name, fn);
	else
		JS_FreeValue(ctx, fn);
	JS_FreeValue(ctx, global);
	return rc;
}

void jsgo_call_into(void *ctxp, const void *func, int argc,
	const void *argv, void *out)
{
	JSContext *ctx = (JSContext *)ctxp;
	*(JSValue *)out = JS_Call(ctx, *(const JSValue *)func, JS_UNDEFINED,
		argc, (JSValueConst *)argv);
}

/* ------------------------------------------------------------------ */
/* Go-callback support                                                 */
/* ------------------------------------------------------------------ */

void jsgo_throw_str(void *ctxp, const char *msg)
{
	JSContext *ctx = (JSContext *)ctxp;
	JSValue str = JS_NewString(ctx, msg);
	if (JS_IsException(str))
		(void)JS_ThrowOutOfMemory(ctx);
	else
		(void)JS_Throw(ctx, str);
}

void jsgo_ret_take(void *ctxp, void *ret, const void *v)
{
	*(JSValue *)ret = JS_DupValue((JSContext *)ctxp, *(const JSValue *)v);
}

void jsgo_arg_at_into(const void *argv, int i, void *out)
{
	/* Borrowed: the Go side converts immediately and never retains this. */
	*(JSValue *)out = ((JSValueConst *)argv)[i];
}

/* ------------------------------------------------------------------ */
/* Evaluation limit (S5 mitigation): the handler asks Go whether the   */
/* current Eval's wall-clock budget is spent; nonzero aborts the script */
/* with "InternalError: interrupted".                                  */
/* ------------------------------------------------------------------ */

static int jsgo_interrupt(JSRuntime *rt, void *opaque)
{
	(void)rt;
	return jsgoExpired(opaque);
}

void jsgo_set_interrupt(void *rtp, void *cookie)
{
	JS_SetInterruptHandler((JSRuntime *)rtp, jsgo_interrupt, cookie);
}

void jsgo_clear_interrupt(void *rtp)
{
	JS_SetInterruptHandler((JSRuntime *)rtp, NULL, NULL);
}
