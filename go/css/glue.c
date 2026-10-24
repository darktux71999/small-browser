/* Definitions for the glue declared in glue.h. Kept in their own
 * translation unit: the header is included by several cgo preambles (a
 * definition there duplicates the symbol at link time) and Go calls these
 * two wrappers through cgo (static inline emits no callable symbol). */

#include <string.h>

#include <libcss/libcss.h>

#include "glue.h"

void cssgo_hint_set_color(css_hint *hint, css_color color)
{
	memset(&hint->data, 0, sizeof hint->data);
	hint->data.color = color;
}

void cssgo_hint_set_strings(css_hint *hint)
{
	memset(&hint->data, 0, sizeof hint->data);
}
