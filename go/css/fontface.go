package css

import (
	"strings"
	"unicode"
)

// @font-face handling, ported from src/css_libcss.rs (T10): the blocks
// never reach libcss as ordinary rules (the rewriter strips them), and
// their family/src/weight/style data is extracted and returned to the
// caller. Fonts themselves are fetched and registered by a later wave (no
// net in this unit); source selection (url/format/local) is fully ported.

// FontFace is one @font-face block, reduced to what the engine reads.
// Weight/Style mirror the Rust Option<String> with "" when the block does
// not declare them (fontconfig matching inside the family does the real
// selection; the raw values are kept only for diagnostics).
type FontFace struct {
	// Family: the family the block registers, quotes stripped.
	Family string
	// Src: the raw `src: ...` list, as authored.
	Src string
	// Weight: font-weight as authored, "" when not declared.
	Weight string
	// Style: font-style as authored, "" when not declared.
	Style string
}

// FontFaceDeclarations returns every @font-face block of a sheet that
// declares a family and a src, recursing into rule-holding at-rules
// (@media, ...) the way the gap rewrite does (mirroring
// font_face_declarations).
func FontFaceDeclarations(css string) []FontFace {
	var out []FontFace
	forEachFontFace(css, func(body string) {
		var face FontFace
		for _, part := range SplitDeclarations(body) {
			property, value, ok := SplitDeclaration(part)
			if !ok {
				continue
			}
			core, _ := SplitImportant(value)
			switch asciiLower(strings.TrimSpace(property)) {
			case "font-family":
				face.Family = strings.TrimSpace(strings.Trim(strings.TrimSpace(core), "\"'"))
			case "src":
				face.Src = strings.TrimSpace(core)
			case "font-weight":
				face.Weight = strings.TrimSpace(core)
			case "font-style":
				face.Style = strings.TrimSpace(core)
			}
		}
		if face.Family != "" && face.Src != "" {
			out = append(out, face)
		}
	})
	return out
}

// FaceDetail renders the font-weight/font-style the block carries, for one
// per-family diagnostic message (mirroring face_detail).
func FaceDetail(face FontFace) string {
	var out strings.Builder
	if face.Weight != "" {
		out.WriteString(", font-weight " + face.Weight)
	}
	if face.Style != "" {
		out.WriteString(", font-style " + face.Style)
	}
	return out.String()
}

// ForEachFontFace calls f with the declaration body of every @font-face
// block in css, recursing into rule-holding at-rules (@media, ...) the way
// the gap rewrite does (mirroring for_each_font_face; the walkers stay
// separate so neither contract leaks into the other).
func ForEachFontFace(css string, f func(string)) { forEachFontFace(css, f) }

func forEachFontFace(css string, f func(string)) {
	bytes := []byte(css)
	i := 0
rules:
	for i < len(bytes) {
		i = skipTrivia(bytes, i)
		if i >= len(bytes) {
			return
		}
		if bytes[i] == '}' {
			i++
			continue
		}
		j := i
		var blockStart int
		for {
			if j >= len(bytes) {
				return
			}
			switch b := bytes[j]; {
			case b == '/' && j+1 < len(bytes) && bytes[j+1] == '*':
				j = skipComment(bytes, j)
				continue
			case b == '"' || b == '\'':
				j = skipString(bytes, j)
				continue
			case b == '{':
				blockStart = j
			case b == ';':
				// An at-rule without a block (@import): skip it.
				i = j + 1
				continue rules
			case b == '}':
				i = j
				continue rules
			default:
				j++
				continue
			}
			break
		}
		blockEnd := findBlockEnd(bytes, blockStart)
		prelude := css[i:blockStart]
		body := css[blockStart+1 : blockEnd]
		if atRuleHoldsRules(prelude) {
			forEachFontFace(body, f)
		} else if isFontFaceRule(prelude) {
			f(body)
		}
		i = blockEnd + 1
	}
}

// isFontFaceRule reports whether the prelude names @font-face
// (mirroring is_font_face_rule).
func isFontFaceRule(prelude string) bool {
	trimmed := strings.TrimLeftFunc(prelude, unicode.IsSpace)
	rest, found := strings.CutPrefix(trimmed, "@")
	if !found {
		return false
	}
	name := takeAtRuleName(rest)
	return asciiEqualFold(name, "font-face")
}

// atRuleHoldsRules reports whether this block holds a rule list instead of
// a declaration list: a style rule always holds declarations; an at-rule
// holds rules when it groups other rules (@media, @supports, @keyframes,
// ...), mirroring at_rule_holds_rules.
func atRuleHoldsRules(prelude string) bool {
	trimmed := strings.TrimLeftFunc(prelude, unicode.IsSpace)
	if !strings.HasPrefix(trimmed, "@") {
		return false
	}
	name := asciiLower(takeAtRuleName(trimmed[1:]))
	switch name {
	case "media", "supports", "layer", "container", "document", "scope":
		return true
	}
	return strings.HasSuffix(name, "-keyframes")
}

// takeAtRuleName reads the at-keyword that follows the '@': the leading run
// of ASCII alphanumerics and hyphens.
func takeAtRuleName(rest string) string {
	end := 0
	for end < len(rest) {
		c := rest[end]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' {
			end++
			continue
		}
		break
	}
	return rest[:end]
}

// ForEachStyleRule calls f with the prelude and declaration body of every
// style rule (an ordinary rule, i.e. one whose prelude does not start with
// '@') in css, recursing into rule-holding at-rules (@media, ...) the way
// the gap rewrite does (mirroring for_each_style_rule; the walkers stay
// separate so neither contract leaks into the other).
func ForEachStyleRule(css string, f func(prelude, body string)) {
	forEachStyleRule(css, f)
}

func forEachStyleRule(css string, f func(prelude, body string)) {
	bytes := []byte(css)
	i := 0
rules:
	for i < len(bytes) {
		i = skipTrivia(bytes, i)
		if i >= len(bytes) {
			return
		}
		if bytes[i] == '}' {
			i++
			continue
		}
		j := i
		var blockStart int
		for {
			if j >= len(bytes) {
				return
			}
			switch b := bytes[j]; {
			case b == '/' && j+1 < len(bytes) && bytes[j+1] == '*':
				j = skipComment(bytes, j)
				continue
			case b == '"' || b == '\'':
				j = skipString(bytes, j)
				continue
			case b == '{':
				blockStart = j
			case b == ';':
				// An at-rule without a block (@import, @charset).
				i = j + 1
				continue rules
			case b == '}':
				i = j
				continue rules
			default:
				j++
				continue
			}
			break
		}
		blockEnd := findBlockEnd(bytes, blockStart)
		prelude := css[i:blockStart]
		body := css[blockStart+1 : blockEnd]
		if atRuleHoldsRules(prelude) {
			forEachStyleRule(body, f)
		} else if !strings.HasPrefix(strings.TrimLeftFunc(prelude, unicode.IsSpace), "@") {
			f(prelude, body)
		}
		i = blockEnd + 1
	}
}

// SplitDeclarations splits a declaration block on its top-level
// semicolons, keeping quoted strings and comments intact
// (mirroring split_declarations).
func SplitDeclarations(body string) []string {
	bytes := []byte(body)
	var parts []string
	segment := 0
	for j := 0; j < len(bytes); {
		switch b := bytes[j]; {
		case b == '/' && j+1 < len(bytes) && bytes[j+1] == '*':
			j = skipComment(bytes, j)
			continue
		case b == '"' || b == '\'':
			j = skipString(bytes, j)
			continue
		case b == ';':
			parts = append(parts, body[segment:j])
			j++
			segment = j
			continue
		}
		j++
	}
	parts = append(parts, body[segment:])
	return parts
}

var genericFamilyKeywords = [5]string{"serif", "sans-serif", "cursive", "fantasy", "monospace"}
var cssWideKeywords = [4]string{"inherit", "initial", "unset", "revert"}

// NormalizeFontFamily normalises a font-family value on the way into
// libcss (mirroring normalize_font_family). Two fixes, both invisible to
// the cascade:
//
//   - Every unquoted multi-word name gains quotes. libcss 0.9.2 drops the
//     whole declaration when such a sequence contains another identifier
//     (font-family: Liberation Serif never reaches the style), and CSS
//     asks for those quotes anyway: a multi-ident sequence is ONE family
//     name, not a family followed by a generic keyword.
//   - A list that names families but declares no generic gets the implicit
//     sans-serif tail appended. libcss reports the generic status byte
//     unreliably for such declarations (out-of-enum values like 6/7,
//     varying per process), while a declaration with an explicit generic
//     always reports it correctly. The appended keyword is exactly the UA
//     default libcss itself falls back to (the UA callback answers
//     sans-serif), so the meaning does not change — it becomes
//     observable. inherit/initial/unset/revert are not family lists and
//     stay untouched.
func NormalizeFontFamily(value string) string {
	var items []string
	for _, item := range SplitCommaList(value) {
		trimmed := strings.TrimSpace(item)
		lower := asciiLower(trimmed)
		quoted := trimmed != "" &&
			!strings.HasPrefix(trimmed, "\"") &&
			!strings.HasPrefix(trimmed, "'") &&
			!containsString(genericFamilyKeywords[:], lower) &&
			!containsString(cssWideKeywords[:], lower) &&
			strings.IndexFunc(trimmed, unicode.IsSpace) >= 0
		if quoted {
			items = append(items, "\""+
				strings.ReplaceAll(strings.ReplaceAll(trimmed, "\\", "\\\\"), "\"", "\\\"")+
				"\"")
		} else {
			items = append(items, trimmed)
		}
	}

	hasKeyword := false
	hasFamily := false
	for _, item := range items {
		lower := asciiLower(item)
		if containsString(genericFamilyKeywords[:], lower) || containsString(cssWideKeywords[:], lower) {
			hasKeyword = true
		}
		if item != "" {
			hasFamily = true
		}
	}
	if hasFamily && !hasKeyword {
		items = append(items, "sans-serif")
	}
	return strings.Join(items, ", ")
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// SupportedFontSrc returns the first src entry this engine can actually
// load: a url(...) whose format(...) hint or extension says
// truetype/opentype. woff/woff2 have no cheap decoder here and local()
// would need family aliasing the engine does not do, so both are skipped
// in favour of a later entry. The return is the URL with any quotes
// removed; "" when nothing is loadable (mirroring supported_font_src).
func SupportedFontSrc(src string) string {
	for _, item := range SplitCommaList(src) {
		url := TokenArgs(item, "url")
		if url == "" {
			continue
		}
		var supported bool
		if format := TokenArgs(item, "format"); format != "" {
			switch asciiLower(format) {
			case "truetype", "opentype":
				supported = true
			}
		} else {
			lower := asciiLower(url)
			supported = strings.HasSuffix(lower, ".ttf") || strings.HasSuffix(lower, ".otf")
		}
		if supported && url != "" {
			return url
		}
	}
	return ""
}

// TokenArgs returns the inside of the first token(...) occurrence of item
// (case-insensitive), with any quotes around it removed. "" when there is
// no such completed call (mirroring token_args).
func TokenArgs(item string, token string) string {
	bytes := []byte(item)
	tb := []byte(token)
	for i := 0; i+len(tb) <= len(bytes); i++ {
		if !asciiEqualFold(string(bytes[i:i+len(tb)]), token) {
			continue
		}
		rest := item[i+len(tb):]
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		if !strings.HasPrefix(rest, "(") {
			continue
		}
		restBytes := []byte(rest)
		depth := 0
		for j := 0; j < len(restBytes); {
			b := restBytes[j]
			if b == '"' || b == '\'' {
				j = skipString(restBytes, j)
				continue
			}
			if b == '(' {
				depth++
			}
			if b == ')' {
				depth--
				if depth == 0 {
					return strings.TrimSpace(
						strings.Trim(strings.TrimSpace(rest[1:j]), "\"'"))
				}
			}
			j++
		}
		return ""
	}
	return ""
}

// SplitCommaList splits a comma-separated CSS value at top level: quotes
// and parentheses (the url(...)/format(...) items of src) keep their
// commas (mirroring split_comma_list).
func SplitCommaList(value string) []string {
	bytes := []byte(value)
	var items []string
	segment := 0
	depth := 0
	for j := 0; j < len(bytes); {
		b := bytes[j]
		switch {
		case b == '/' && j+1 < len(bytes) && bytes[j+1] == '*':
			j = skipComment(bytes, j)
			continue
		case b == '"' || b == '\'':
			j = skipString(bytes, j)
			continue
		case b == '(':
			depth++
			j++
			continue
		case b == ')':
			if depth > 0 {
				depth--
			}
			j++
			continue
		case b == ',' && depth == 0:
			items = append(items, value[segment:j])
			j++
			segment = j
			continue
		}
		j++
	}
	items = append(items, value[segment:])
	return items
}
