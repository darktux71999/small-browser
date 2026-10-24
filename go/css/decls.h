/*
 * Shared cgo declarations for the browser-lite css bridge. This header
 * holds declarations only, because the //export preamble (callbacks.go) is
 * copied into two C outputs and may contain no definitions.
 */
#ifndef BROWSER_LITE_CSS_DECLS_H
#define BROWSER_LITE_CSS_DECLS_H

#include <stddef.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <libcss/libcss.h>

/* The exported Go callbacks (callbacks.go). Their signatures must match
 * libcss' css_select_handler member types exactly. */
extern css_error cssgo_node_name(void *pw, void *node, css_qname *qname);
extern css_error cssgo_node_classes(void *pw, void *node, lwc_string ***classes, uint32_t *n_classes);
extern css_error cssgo_node_id(void *pw, void *node, lwc_string **id);
extern css_error cssgo_named_ancestor_node(void *pw, void *node, const css_qname *qname, void **ancestor);
extern css_error cssgo_named_parent_node(void *pw, void *node, const css_qname *qname, void **parent);
extern css_error cssgo_named_sibling_node(void *pw, void *node, const css_qname *qname, void **sibling);
extern css_error cssgo_named_generic_sibling_node(void *pw, void *node, const css_qname *qname, void **sibling);
extern css_error cssgo_parent_node(void *pw, void *node, void **parent);
extern css_error cssgo_sibling_node(void *pw, void *node, void **sibling);
extern css_error cssgo_node_has_name(void *pw, void *node, const css_qname *qname, bool *match);
extern css_error cssgo_node_has_class(void *pw, void *node, lwc_string *name, bool *match);
extern css_error cssgo_node_has_id(void *pw, void *node, lwc_string *name, bool *match);
extern css_error cssgo_node_has_attribute(void *pw, void *node, const css_qname *qname, bool *match);
extern css_error cssgo_node_has_attribute_equal(void *pw, void *node, const css_qname *qname, lwc_string *value, bool *match);
extern css_error cssgo_node_has_attribute_dashmatch(void *pw, void *node, const css_qname *qname, lwc_string *value, bool *match);
extern css_error cssgo_node_has_attribute_includes(void *pw, void *node, const css_qname *qname, lwc_string *value, bool *match);
extern css_error cssgo_node_has_attribute_prefix(void *pw, void *node, const css_qname *qname, lwc_string *value, bool *match);
extern css_error cssgo_node_has_attribute_suffix(void *pw, void *node, const css_qname *qname, lwc_string *value, bool *match);
extern css_error cssgo_node_has_attribute_substring(void *pw, void *node, const css_qname *qname, lwc_string *value, bool *match);
extern css_error cssgo_node_is_root(void *pw, void *node, bool *match);
extern css_error cssgo_node_count_siblings(void *pw, void *node, bool same_name, bool after, int32_t *count);
extern css_error cssgo_node_is_empty(void *pw, void *node, bool *match);
extern css_error cssgo_node_is_link(void *pw, void *node, bool *match);
extern css_error cssgo_node_is_visited(void *pw, void *node, bool *match);
extern css_error cssgo_node_is_hover(void *pw, void *node, bool *match);
extern css_error cssgo_node_is_active(void *pw, void *node, bool *match);
extern css_error cssgo_node_is_focus(void *pw, void *node, bool *match);
extern css_error cssgo_node_is_enabled(void *pw, void *node, bool *match);
extern css_error cssgo_node_is_disabled(void *pw, void *node, bool *match);
extern css_error cssgo_node_is_checked(void *pw, void *node, bool *match);
extern css_error cssgo_node_is_target(void *pw, void *node, bool *match);
extern css_error cssgo_node_is_lang(void *pw, void *node, lwc_string *lang, bool *match);
extern css_error cssgo_node_presentational_hint(void *pw, void *node, uint32_t *nhints, css_hint **hints);
extern css_error cssgo_ua_default_for_property(void *pw, uint32_t property, css_hint *hint);
extern css_error cssgo_set_libcss_node_data(void *pw, void *node, void *libcss_node_data);
extern css_error cssgo_get_libcss_node_data(void *pw, void *node, void **libcss_node_data);

/* The stylesheet's resolve callback: every sheet is created with the URL its
 * relative urls and imports resolve against. */
extern css_error cssgo_resolve(void *pw, const char *base, lwc_string *rel, lwc_string **abs);

#endif /* BROWSER_LITE_CSS_DECLS_H */
