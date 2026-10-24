package text

import (
	"errors"
	"strings"
)

var errNoContext = errors.New("text: cannot create a pango measuring context")

// ResolveFamily picks the one family Pango loads from a computed stack,
// mirroring pango::resolve_family (src/layout.rs:1911-1925): the list is
// walked in author order, a CSS generic keyword maps straight to its
// Pango/fontconfig alias, a named family is used only when fontconfig
// really has it, and an exhausted list falls back to the sans default.
func ResolveFamily(families []string) string {
	for _, raw := range families {
		name := strings.Trim(strings.TrimSpace(raw), `"'`)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if generic, ok := genericFamily(name); ok {
			return generic
		}
		if familyAvailable(name) {
			return name
		}
	}
	return "Sans"
}

// genericFamily maps CSS generic keywords to the Pango families fontconfig
// substitutes for them, per the T10 mapping (src/layout.rs:1930-1946).
func genericFamily(name string) (string, bool) {
	switch {
	case nameEqual(name, "serif"):
		return "Serif", true
	case nameEqual(name, "sans-serif") ||
		nameEqual(name, "system-ui") ||
		nameEqual(name, "ui-sans-serif"):
		return "Sans", true
	case nameEqual(name, "monospace") ||
		nameEqual(name, "ui-monospace"):
		return "Monospace", true
	case nameEqual(name, "cursive"):
		return "Cursive", true
	case nameEqual(name, "fantasy"):
		return "Fantasy", true
	}
	return "", false
}

func nameEqual(a, b string) bool {
	return strings.EqualFold(a, b)
}
