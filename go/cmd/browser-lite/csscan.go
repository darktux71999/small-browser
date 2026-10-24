package main

import (
	"strings"

	blnet "browser-lite/net"
)

// expandImports splices a sheet's prologue @import chain into the text,
// depth-first in document order, mirroring the oracle's finish_imports:
// imported rules keep their place before the importing sheet's rules, cycles
// terminate silently, and every fetched import registers its @font-face
// blocks on the way (the only place their text is in hand).
func expandImports(cssText, sheetURL string, l *loader, active map[string]bool) string {
	var out strings.Builder
	prologue := true
	for _, stmt := range splitStatements(cssText) {
		name, isAt := atName(stmt.prelude)
		switch {
		case isAt && name == "import" && !stmt.hasBlock && prologue:
			if expanded, ok := expandImport(stmt.prelude, sheetURL, l, active); ok {
				out.WriteString(expanded)
			}
		case isAt && (name == "charset" || name == "layer"):
			// @charset and @layer never end the import prologue.
			out.WriteString(stmt.text)
		default:
			out.WriteString(stmt.text)
			prologue = false
		}
	}
	return out.String()
}

// expandImport resolves, fetches and expands one @import prelude (without
// the trailing semicolon). It reports false when the rule contributes no
// text (filtered media, cycle, unfetchable sheet).
func expandImport(prelude, sheetURL string, l *loader, active map[string]bool) (string, bool) {
	raw, media, ok := importRef(prelude)
	if !ok {
		return "", false
	}
	if strings.TrimSpace(media) != "" && !mediaAppliesToScreen(media) {
		return "", false
	}
	fetchURL, childURL := raw, raw
	if resolved, ok := blnet.JoinURL(sheetURL, raw); ok {
		fetchURL, childURL = resolved, resolved
	}
	// An unresolvable reference reaches the loader unchanged, exactly like
	// the oracle's resolve callback handing the reference back.
	if active[fetchURL] {
		return "", false
	}
	cssText, ok := l.loadSheet(fetchURL)
	if !ok {
		return "", false
	}
	registerFaces(cssText, childURL, l)
	active[fetchURL] = true
	expanded := expandImports(cssText, childURL, l, active)
	delete(active, fetchURL)
	return expanded, true
}

// importRef splits an @import prelude into its URL and media list.
func importRef(prelude string) (url, media string, ok bool) {
	rest := strings.TrimSpace(prelude[len("@import"):])
	if len(rest) >= 4 && strings.EqualFold(rest[:4], "url(") {
		depth := 0
		for i := 4; i < len(rest); i++ {
			c := rest[i]
			if c == '"' || c == '\'' {
				j := skipString(rest, i)
				if j == i {
					return "", "", false
				}
				inner := rest[i+1 : j-1]
				// Find the closing paren after the string.
				end := strings.IndexByte(rest[j:], ')')
				if end < 0 {
					return "", "", false
				}
				return strings.TrimSpace(inner), strings.TrimSpace(rest[j+end+1:]), true
			}
			if c == '(' {
				depth++
			}
			if c == ')' {
				if depth == 0 {
					return strings.TrimSpace(rest[4:i]), strings.TrimSpace(rest[i+1:]), true
				}
				depth--
			}
		}
		return "", "", false
	}
	if rest == "" || (rest[0] != '"' && rest[0] != '\'') {
		return "", "", false
	}
	j := skipString(rest, 0)
	if j == 0 {
		return "", "", false
	}
	return rest[1 : j-1], strings.TrimSpace(rest[j:]), true
}

// stripFontFaces removes the @font-face blocks libcss must never see — at
// the top level and inside rule-holding at-rules — keeping every other byte.
func stripFontFaces(cssText string) string {
	var out strings.Builder
	stripInto(cssText, &out)
	return out.String()
}

func stripInto(cssText string, out *strings.Builder) {
	for _, stmt := range splitStatements(cssText) {
		name, isAt := atName(stmt.prelude)
		switch {
		case isAt && name == "font-face" && stmt.hasBlock:
			// Dropped: registerFaces already collected it.
		case isAt && stmt.hasBlock && holdsRules(name):
			out.WriteString(stmt.open)
			stripInto(stmt.body, out)
			out.WriteString("}")
		default:
			out.WriteString(stmt.text)
		}
	}
}

// holdsRules reports whether an at-rule holds a rule list instead of a
// declaration list, mirroring the oracle's at_rule_holds_rules.
func holdsRules(name string) bool {
	switch name {
	case "media", "supports", "layer", "container", "document", "scope":
		return true
	}
	return strings.HasSuffix(name, "-keyframes")
}

// stmt is one top-level statement: its prelude, and for blocks the text up
// to the body (open), the body, and the whole span (text).
type stmt struct {
	prelude  string
	hasBlock bool
	open     string
	body     string
	text     string
}

// splitStatements cuts CSS into top-level statements, keeping every byte in
// exactly one span: comments, strings and nested blocks are opaque.
func splitStatements(cssText string) []stmt {
	var out []stmt
	bytes := []byte(cssText)
	i := 0
	for i < len(bytes) {
		seg := i
		j := i
		blockStart := -1
	terminator:
		for {
			if j >= len(bytes) {
				break terminator
			}
			switch b := bytes[j]; {
			case b == '/' && j+1 < len(bytes) && bytes[j+1] == '*':
				j = skipComment(cssText, j)
				continue
			case b == '"' || b == '\'':
				j = skipString(cssText, j)
				continue
			case b == '{':
				blockStart = j
				break terminator
			case b == ';':
				out = append(out, stmt{
					prelude: cssText[seg:j],
					text:    cssText[seg : j+1],
				})
				i = j + 1
				break terminator
			case b == '}':
				out = append(out, stmt{
					prelude: cssText[seg:j],
					text:    cssText[seg : j+1],
				})
				i = j + 1
				break terminator
			}
			j++
		}
		if blockStart >= 0 {
			end := findBlockEnd(cssText, blockStart)
			out = append(out, stmt{
				prelude:  cssText[seg:blockStart],
				hasBlock: true,
				open:     cssText[seg : blockStart+1],
				body:     cssText[blockStart+1 : end],
				text:     cssText[seg : end+1],
			})
			i = end + 1
		} else if j >= len(bytes) {
			if seg < len(bytes) {
				out = append(out, stmt{prelude: cssText[seg:], text: cssText[seg:]})
			}
			break
		}
	}
	return out
}

// atName reads the at-keyword of a prelude: ("import", true) for @import,
// ("", false) for style rules.
func atName(prelude string) (string, bool) {
	trimmed := strings.TrimLeft(prelude, " \t\n\r\f\v")
	if !strings.HasPrefix(trimmed, "@") {
		return "", false
	}
	rest := trimmed[1:]
	end := 0
	for end < len(rest) {
		c := rest[end]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' {
			end++
			continue
		}
		break
	}
	return strings.ToLower(rest[:end]), true
}

func skipComment(s string, i int) int {
	end := strings.Index(s[i+2:], "*/")
	if end < 0 {
		return len(s)
	}
	return i + 2 + end + 2
}

func skipString(s string, i int) int {
	quote := s[i]
	j := i + 1
	for j < len(s) {
		if s[j] == '\\' {
			j += 2
			continue
		}
		if s[j] == quote {
			return j + 1
		}
		j++
	}
	return i
}

// findBlockEnd is the index of the closing brace matching the opening one
// at open, honouring strings, comments and nesting.
func findBlockEnd(s string, open int) int {
	depth := 0
	j := open
	for j < len(s) {
		switch c := s[j]; {
		case c == '/' && j+1 < len(s) && s[j+1] == '*':
			j = skipComment(s, j)
			continue
		case c == '"' || c == '\'':
			next := skipString(s, j)
			if next == j {
				j++
				continue
			}
			j = next
			continue
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return j
			}
		}
		j++
	}
	return len(s) - 1
}
