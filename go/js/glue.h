/*
 * Static C side of the browser-lite js bridge: the QuickJS helpers Go
 * cannot express (union/tag access, value constructors, the C-function
 * trampoline, the interrupt handler, console) plus the lifecycle wrappers
 * that keep C pointer types out of Go signatures.
 *
 * Layout mirrors go/css/glue.h: declarations here, definitions in glue.c
 * (this header is included by the cgo preamble, so a definition here
 * would duplicate the symbol). JSValue traffic crosses the boundary only
 * through 16-byte blobs addressed by void* (see jsgo_store_size): Go
 * never names the struct and never hands C a Go pointer.
 */
#ifndef BROWSER_LITE_JS_GLUE_H
#define BROWSER_LITE_JS_GLUE_H

#include <stddef.h>
#include <stdint.h>
#include <quickjs.h>

/* Integer cookies parked in pointers stay above NULL and the Go runtime's
 * junk-pointer threshold (minLegalPointer, 4096), as in go/css/glue.h. */
#define JSG_PTR_BASE 8192

/* JSValue is a 16-byte by-value struct on the default (non-NaN-boxing)
 * 64-bit build, the same layout src/sys.rs assumes. */
#define JSG_VALUE_SIZE 16

static inline void *jsgo_cookie_ptr(intptr_t id)
{
	return (void *)(uintptr_t)(id + JSG_PTR_BASE);
}

static inline intptr_t jsgo_cookie_id(void *p)
{
	return (intptr_t)(uintptr_t)p - JSG_PTR_BASE;
}

/* Value kinds returned by jsgo_kind. */
enum {
	JSG_UNDEF = 0,
	JSG_NULL,
	JSG_BOOL,
	JSG_NUMBER,
	JSG_STRING,
	JSG_OBJECT,
	JSG_FUNCTION
};

/* Lifecycle (wrappers keep the QuickJS pointer types out of Go). */
void *jsgo_new_runtime(void);
void jsgo_free_runtime(void *rt);
void *jsgo_new_context(void *rt);
void jsgo_free_context(void *ctx);
void jsgo_set_opaque(void *ctx, void *cookie);
void jsgo_install_console(void *ctx);

/* Evaluation. The source must be NUL-terminated with len excluding the
 * NUL (quickjs-ng walks the terminator even when a length is passed). */
void jsgo_eval_into(void *ctx, const char *src, size_t len,
	const char *filename, void *out);
void jsgo_drain(void *rt);

/* Values. Every JSValue crosses as a JSG_VALUE_SIZE blob behind void*. */
int jsgo_is_exc(const void *v);
void jsgo_get_exc_into(void *ctx, void *out);
int jsgo_kind(void *ctx, const void *v);
int32_t jsgo_int32(const void *v);
double jsgo_float64(const void *v);
int jsgo_bool_val(const void *v);
char *jsgo_cstr(void *ctx, const void *v, size_t *len);
void jsgo_cstr_free(void *ctx, char *p);
void jsgo_make_undefined(void *out);
void jsgo_make_null(void *out);
void jsgo_make_bool(int b, void *out);
void jsgo_make_num(double d, void *out);
void jsgo_make_str(void *ctx, const char *s, size_t len, void *out);
void jsgo_dup_into(void *ctx, const void *v, void *out);
void jsgo_free_val(void *ctx, void *v);

/* Objects, globals and calls. */
void jsgo_get_str_into(void *ctx, const void *obj, const char *prop,
	void *out);
int jsgo_set_global(void *ctx, const char *name, const void *v);
int jsgo_set_global_func(void *ctx, const char *name, int magic);
void jsgo_call_into(void *ctx, const void *func, int argc,
	const void *argv, void *out);

/* Go-callback support: jsgoGoCall fills *ret and returns 0, or throws
 * through jsgo_throw_str and returns nonzero. */
void jsgo_throw_str(void *ctx, const char *msg);
void jsgo_ret_take(void *ctx, void *ret, const void *v);
void jsgo_arg_at_into(const void *argv, int i, void *out);
extern int jsgoGoCall(void *rtCookie, int magic, void *ctx, int argc,
	void *argv, void *ret);
extern int jsgoExpired(void *cookie);

/* Evaluation limit (S5 mitigation): the handler consults Go through the
 * runtime cookie and aborts the script when the deadline has passed. */
void jsgo_set_interrupt(void *rt, void *cookie);
void jsgo_clear_interrupt(void *rt);

#endif /* BROWSER_LITE_JS_GLUE_H */
