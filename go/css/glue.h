/*
 * Static C side of the browser-lite css bridge: the handler table, the
 * libwapcaplet macro replacements and the struct builders. Definitions only
 * appear here, never in callbacks.go's //export preamble.
 */
#ifndef BROWSER_LITE_CSS_GLUE_H
#define BROWSER_LITE_CSS_GLUE_H

#include "decls.h"

static const css_fixed cssgo_fixed_scale = 1024;

/* libcss' css_select_handler, wired to the callbacks above in the exact
 * order of libcss/select.h. libcss reads it once; the pointer hands the
 * caller the stable address. */
static const css_select_handler cssgo_handler = {
	.handler_version = CSS_SELECT_HANDLER_VERSION_1,
	.node_name = cssgo_node_name,
	.node_classes = cssgo_node_classes,
	.node_id = cssgo_node_id,
	.named_ancestor_node = cssgo_named_ancestor_node,
	.named_parent_node = cssgo_named_parent_node,
	.named_sibling_node = cssgo_named_sibling_node,
	.named_generic_sibling_node = cssgo_named_generic_sibling_node,
	.parent_node = cssgo_parent_node,
	.sibling_node = cssgo_sibling_node,
	.node_has_name = cssgo_node_has_name,
	.node_has_class = cssgo_node_has_class,
	.node_has_id = cssgo_node_has_id,
	.node_has_attribute = cssgo_node_has_attribute,
	.node_has_attribute_equal = cssgo_node_has_attribute_equal,
	.node_has_attribute_dashmatch = cssgo_node_has_attribute_dashmatch,
	.node_has_attribute_includes = cssgo_node_has_attribute_includes,
	.node_has_attribute_prefix = cssgo_node_has_attribute_prefix,
	.node_has_attribute_suffix = cssgo_node_has_attribute_suffix,
	.node_has_attribute_substring = cssgo_node_has_attribute_substring,
	.node_is_root = cssgo_node_is_root,
	.node_count_siblings = cssgo_node_count_siblings,
	.node_is_empty = cssgo_node_is_empty,
	.node_is_link = cssgo_node_is_link,
	.node_is_visited = cssgo_node_is_visited,
	.node_is_hover = cssgo_node_is_hover,
	.node_is_active = cssgo_node_is_active,
	.node_is_focus = cssgo_node_is_focus,
	.node_is_enabled = cssgo_node_is_enabled,
	.node_is_disabled = cssgo_node_is_disabled,
	.node_is_checked = cssgo_node_is_checked,
	.node_is_target = cssgo_node_is_target,
	.node_is_lang = cssgo_node_is_lang,
	.node_presentational_hint = cssgo_node_presentational_hint,
	.ua_default_for_property = cssgo_ua_default_for_property,
	.set_libcss_node_data = cssgo_set_libcss_node_data,
	.get_libcss_node_data = cssgo_get_libcss_node_data,
};

static inline const css_select_handler *cssgo_get_handler(void)
{
	return &cssgo_handler;
}

/* -----------------------------------------------------------------------
 * libwapcaplet helpers: lwc_string_data/_length/_unref are macros, so the
 * bridge reaches them through small inline wrappers instead.
 * ----------------------------------------------------------------------- */

static inline lwc_string *cssgo_intern(const char *data, size_t len)
{
	lwc_string *out = NULL;
	if (lwc_intern_string(data, len, &out) != lwc_error_ok || out == NULL)
		return NULL;
	return out;
}

static inline const char *cssgo_lwc_data(lwc_string *s)
{
	return lwc_string_data(s);
}

static inline size_t cssgo_lwc_len(lwc_string *s)
{
	return lwc_string_length(s);
}

static inline lwc_string *cssgo_lwc_ref(lwc_string *s)
{
	return lwc_string_ref(s);
}

static inline void cssgo_lwc_unref(lwc_string *s)
{
	lwc_string_unref(s);
}

/* -----------------------------------------------------------------------
 * Struct builders: css_media carries a member named "type" (not addressable
 * from Go) and libwapcaplet interning needs C-side copies, so the parameter
 * template, the unit context and the media struct are built here.
 * ----------------------------------------------------------------------- */

static inline css_fixed cssgo_fixed_from_double(double v)
{
	return (css_fixed)(v * 1024.0);
}

static inline void cssgo_unit_ctx_init(css_unit_ctx *ctx, double viewport_width,
	double viewport_height, double font_size_default, double dpi)
{
	memset(ctx, 0, sizeof *ctx);
	ctx->viewport_width = cssgo_fixed_from_double(viewport_width);
	ctx->viewport_height = cssgo_fixed_from_double(viewport_height);
	ctx->font_size_default = cssgo_fixed_from_double(font_size_default);
	ctx->font_size_minimum = 0;
	ctx->device_dpi = cssgo_fixed_from_double(dpi);
	ctx->root_style = NULL;
	ctx->pw = NULL;
	// "measure" is const-qualified; memset above zeroes it, as in the
	// Rust oracle's zeroed init.
}

static inline void cssgo_media_init(css_media *media, double viewport_width)
{
	memset(media, 0, sizeof *media);
	media->type = CSS_MEDIA_SCREEN;
	media->width = cssgo_fixed_from_double(viewport_width);
	media->height = cssgo_fixed_from_double(600.0);
	media->aspect_ratio = cssgo_fixed_from_double(viewport_width / 600.0);
	media->orientation = viewport_width >= 600.0 ?
		CSS_MEDIA_ORIENTATION_LANDSCAPE : CSS_MEDIA_ORIENTATION_PORTRAIT;
	media->color = cssgo_fixed_from_double(8.0); // 8 bits per channel
}

static inline void cssgo_params_init(css_stylesheet_params *params, const char *url,
	int inline_style)
{
	memset(params, 0, sizeof *params);
	params->params_version = CSS_STYLESHEET_PARAMS_VERSION_1;
	params->level = CSS_LEVEL_DEFAULT; // CSS_LEVEL_21
	params->charset = NULL;
	params->url = url;
	params->title = NULL;
	params->allow_quirks = 0;
	params->inline_style = inline_style != 0;
	params->resolve = cssgo_resolve;
	params->resolve_pw = NULL;
	params->import = NULL;
	params->import_pw = NULL;
	params->color = NULL;
	params->color_pw = NULL;
	params->font = NULL;
	params->font_pw = NULL;
}

// Copies a Go byte slice into C memory (the caller frees it). The empty
// string gets a NUL-terminated buffer, like Rust's CString::new.
static inline char *cssgo_strdup(const char *data, size_t len)
{
	char *out = (char *)malloc(len + 1);
	if (out == NULL)
		return NULL;
	memcpy(out, data, len);
	out[len] = '\0';
	return out;
}

/* The DOM nodes are addressed by id encoded in the pointer (id plus a base
 * that is never the NULL pointer). The base also stays above the Go
 * runtime's junk-pointer threshold (minLegalPointer, 4096): smaller
 * integers parked in unsafe.Pointer slots abort the process when a stack
 * grows under -race (runtime/stack.go adjustpointers). The values stay
 * opaque cookies libcss only echoes back, so the magnitude is irrelevant;
 * synthesized C-side to keep the integer-to-pointer conversion out of Go's
 * pointer checker. */
#define CSSGO_PTR_BASE 8192

static inline void *cssgo_node_ptr(intptr_t id)
{
	return (void *)(uintptr_t)(id + CSSGO_PTR_BASE);
}

/* The inverse of cssgo_node_ptr, for decoding cookies C echoes back. */
static inline intptr_t cssgo_ptr_id(void *p)
{
	return (intptr_t)(uintptr_t)p - CSSGO_PTR_BASE;
}

/* css_hint.data is a C union; Go cannot reach its members, so the
 * ua_default_for_property callback fills it through these wrappers. They
 * are real functions defined once in glue.c: this header is included by
 * several cgo translation units (a definition here duplicates the symbol)
 * and Go calls them through cgo (static inline emits no callable symbol). */
void cssgo_hint_set_color(css_hint *hint, css_color color);

void cssgo_hint_set_strings(css_hint *hint);

#endif /* BROWSER_LITE_CSS_GLUE_H */
