package css

import "strings"

// The stylesheet rewriter, ported verbatim in behaviour from
// src/css_libcss.rs. It runs over the raw stylesheet text before libcss
// sees it:
//
//   - the gap properties (libcss 0.9.2 knows column-gap but neither the gap
//     shorthand nor row-gap) are rewritten into the min-width (row gap) and
//     max-width (column gap) carriers, and the author's own min/max widths
//     are dropped to leave the carriers free;
//   - font-family declarations are normalised: every unquoted multi-word
//     name gains quotes, and a family list without a generic keyword gains
//     the implicit sans-serif tail (libcss 0.9.2 quirks: the generic status
//     byte is garbage for name-only declarations, and unquoted multi-word
//     sequences with a generic-like identifier are silently dropped);
//   - @font-face blocks are never handed to libcss as ordinary rules: the
//     rule-list walker drops them (their declarations are extracted
//     separately in fontface.go).
//
// Everything else is copied verbatim, byte for byte.

// RewriteGapCarriers rewrites the gap declarations of a rule list so the
// engine can read them, and drops the author's own min-width/max-width.
func RewriteGapCarriers(css string) string {
	var out strings.Builder
	out.Grow(len(css))
	rewriteRuleList(css, &out, nil)
	return out.String()
}

// RewriteGapDeclarations rewrites a declaration list (the style="" style),
// the same rewrite on the declaration-list level.
func RewriteGapDeclarations(css string) string {
	var out strings.Builder
	out.Grow(len(css))
	rewriteDeclarations(css, &out, nil, "")
	return out.String()
}

// rewriteRuleList copies a rule list, rewriting the declaration blocks it
// contains (mirroring rewrite_rule_list). cap is the custom-property side
// table collector; nil keeps the gap/font-family-only walk, non-nil also
// extracts the --* and var() declarations (see CaptureSheet) and records
// each captured declaration's selector and enclosing at-rule condition.
func rewriteRuleList(src string, out *strings.Builder, cap *capture) {
	bytes := []byte(src)
	i := 0
	for i < len(bytes) {
		trivia := i
		i = skipTrivia(bytes, i)
		out.WriteString(src[trivia:i])
		if i >= len(bytes) {
			return
		}
		if bytes[i] == '}' {
			// A stray closing brace: copy it and leave this block.
			out.WriteByte('}')
			i++
			continue
		}

		// The prelude of the rule (or of the at-rule) ends at the first
		// top-level { or ;.
		j := i
		blockStart := -1
		next := -1 // >= 0: the scan copied a whole segment; advance past it
	scan:
		for next < 0 {
			if j >= len(bytes) {
				out.WriteString(src[i:])
				return
			}
			b := bytes[j]
			switch {
			case b == '/' && j+1 < len(bytes) && bytes[j+1] == '*':
				j = skipComment(bytes, j)
				continue scan
			case b == '"' || b == '\'':
				j = skipString(bytes, j)
				continue scan
			case b == '{':
				blockStart = j
				break scan
			case b == ';':
				// An at-rule without a block (@import, @charset):
				// copy it verbatim.
				out.WriteString(src[i : j+1])
				next = j + 1
				break scan
			case b == '}':
				out.WriteString(src[i:j])
				next = j
				break scan
			}
			j++
		}
		if next >= 0 {
			i = next
			continue
		}

		blockEnd := findBlockEnd(bytes, blockStart)
		prelude := src[i:blockStart]
		body := src[blockStart+1 : blockEnd]
		if isFontFaceRule(prelude) {
			// @font-face is extracted by FontFaceDeclarations and must
			// never reach libcss as an ordinary declaration block.
			i = blockEnd + 1
			continue
		}
		out.WriteString(prelude)
		out.WriteByte('{')
		if atRuleHoldsRules(prelude) {
			if cap != nil {
				cap.media = append(cap.media, atRuleCondition(prelude))
			}
			rewriteRuleList(body, out, cap)
			if cap != nil {
				cap.media = cap.media[:len(cap.media)-1]
			}
		} else {
			rewriteDeclarations(body, out, cap, strings.TrimSpace(prelude))
		}
		out.WriteByte('}')
		i = blockEnd + 1
	}
}

// rewriteDeclarations copies a declaration list, rewriting the gap
// declarations (mirroring rewrite_declarations). selector/media are the
// capture context of the enclosing rule; "" for an inline list.
func rewriteDeclarations(src string, out *strings.Builder, cap *capture, selector string) {
	bytes := []byte(src)
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
			rewriteDeclaration(src[segment:j], out, cap, selector)
			out.WriteByte(';')
			j++
			segment = j
			continue
		}
		j++
	}
	rewriteDeclaration(src[segment:], out, cap, selector)
}

// rewriteDeclaration rewrites one declaration: the custom-property side
// table wins first — a --* or var() declaration is captured and leaves
// no text behind for libcss (which would discard it anyway) — then the
// gap properties become the carriers, the author's minimum/maximum
// widths are dropped, and font-family is normalised (mirroring
// rewrite_declaration).
func rewriteDeclaration(decl string, out *strings.Builder, cap *capture, selector string) {
	property, value, ok := SplitDeclaration(decl)
	if !ok {
		out.WriteString(decl)
		return
	}
	trimmed := strings.TrimSpace(property)
	if cap != nil {
		core, important := SplitImportant(value)
		if strings.HasPrefix(trimmed, "--") || containsVarFunction(core) {
			// Custom properties keep their authored case (they are
			// case-sensitive); ordinary names travel lowercased.
			key := asciiLower(trimmed)
			if strings.HasPrefix(trimmed, "--") {
				key = trimmed
			}
			cap.decls = append(cap.decls, CustomDecl{
				Property:  key,
				Value:     core,
				Selector:  selector,
				Media:     strings.Join(cap.media, " "),
				Important: important,
				Order:     uint32(len(cap.decls)),
			})
			return
		}
	}
	name := asciiLower(trimmed)
	switch name {
	case "gap", "row-gap", "column-gap":
		core, important := SplitImportant(value)
		parts := strings.Fields(core)
		// gap: <row> <column>; the longhands only own their own axis.
		var row, column string
		var hasRow, hasColumn bool
		switch {
		case name == "row-gap" && len(parts) == 1:
			row, hasRow = parts[0], true
		case name == "column-gap" && len(parts) == 1:
			column, hasColumn = parts[0], true
		case len(parts) == 1:
			row, column = parts[0], parts[0]
			hasRow, hasColumn = true, true
		case len(parts) == 2:
			row, column = parts[0], parts[1]
			hasRow, hasColumn = true, true
		}
		if hasRow {
			writeCarrier(out, "min-width", row, important)
			if hasColumn {
				out.WriteByte(';')
			}
		}
		if hasColumn {
			writeCarrier(out, "max-width", column, important)
		}
	case
		// The engine has no minimum/maximum sizing: these declarations are
		// inert, and the carriers need the room.
		"min-width", "max-width":
	case
		// libcss 0.9.2 drops the whole declaration when an unquoted family
		// sequence contains another identifier that a generic keyword
		// could match (font-family: Liberation Serif never reaches the
		// style), and CSS asks for quotes around any family name with
		// whitespace anyway. A without a generic keyword also makes
		// libcss report the generic status byte unreliably. A value that
		// needs no change is copied verbatim, byte for byte.
		"font-family":
		core, important := SplitImportant(value)
		normalized := NormalizeFontFamily(core)
		if normalized == strings.TrimSpace(core) {
			out.WriteString(decl)
		} else {
			out.WriteString(strings.TrimSpace(property))
			out.WriteByte(':')
			out.WriteString(normalized)
			if important {
				out.WriteString("!important")
			}
		}
	default:
		out.WriteString(decl)
	}
}

// writeCarrier writes one carrier declaration.
func writeCarrier(out *strings.Builder, property, value string, important bool) {
	out.WriteString(property)
	out.WriteByte(':')
	out.WriteString(value)
	if important {
		out.WriteString("!important")
	}
}

// SplitDeclaration splits a declaration into its property and its value at
// the first top-level ':' (mirroring split_declaration).
func SplitDeclaration(decl string) (property, value string, ok bool) {
	bytes := []byte(decl)
	for j := 0; j < len(bytes); {
		switch b := bytes[j]; {
		case b == '/' && j+1 < len(bytes) && bytes[j+1] == '*':
			j = skipComment(bytes, j)
			continue
		case b == '"' || b == '\'':
			j = skipString(bytes, j)
			continue
		case b == ':':
			return decl[:j], decl[j+1:], true
		}
		j++
	}
	return "", "", false
}

// SplitImportant splits the !important flag off a declaration value
// (mirroring split_important).
func SplitImportant(value string) (core string, important bool) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 10 && asciiEqualFold(trimmed[len(trimmed)-10:], "!important") {
		return strings.TrimSpace(trimmed[:len(trimmed)-10]), true
	}
	return trimmed, false
}

// skipString returns the index just after the string that starts at i
// (mirroring skip_string: backslash escapes double their next byte, a
// newline ends the string where it stands).
func skipString(bytes []byte, i int) int {
	quote := bytes[i]
	for j := i + 1; j < len(bytes); j++ {
		switch bytes[j] {
		case '\\':
			j++
		case '\n':
			return j
		case quote:
			return j + 1
		}
	}
	return len(bytes)
}

// skipComment returns the index just after the comment that starts at i
// (mirroring skip_comment).
func skipComment(bytes []byte, i int) int {
	for j := i + 2; j+1 < len(bytes); j++ {
		if bytes[j] == '*' && bytes[j+1] == '/' {
			return j + 2
		}
	}
	return len(bytes)
}

// skipTrivia skips whitespace and comments (mirroring skip_trivia, where
// whitespace is the ASCII set Rust's u8::is_ascii_whitespace accepts: the
// space, tab, newline, form feed and carriage return).
func skipTrivia(bytes []byte, i int) int {
	for {
		for i < len(bytes) && isASCIIWhitespace(bytes[i]) {
			i++
		}
		if i+1 < len(bytes) && bytes[i] == '/' && bytes[i+1] == '*' {
			i = skipComment(bytes, i)
			continue
		}
		return i
	}
}

// findBlockEnd returns the index of the '}' that closes the block opened
// at i (mirroring find_block_end); len(bytes) when unterminated.
func findBlockEnd(bytes []byte, i int) int {
	depth := 0
	for j := i; j < len(bytes); {
		switch b := bytes[j]; {
		case b == '/' && j+1 < len(bytes) && bytes[j+1] == '*':
			j = skipComment(bytes, j)
			continue
		case b == '"' || b == '\'':
			j = skipString(bytes, j)
			continue
		case b == '{':
			depth++
		case b == '}':
			depth--
			if depth == 0 {
				return j
			}
		}
		j++
	}
	return len(bytes)
}

func isASCIIWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\f' || b == '\r'
}

// asciiLower lowercases only ASCII bytes, like Rust's to_ascii_lowercase.
func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

// asciiEqualFold compares ASCII bytes case-insensitively.
func asciiEqualFold(a, b string) bool {
	return asciiLower(a) == asciiLower(b)
}
