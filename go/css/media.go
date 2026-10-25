package css

import (
	"strings"
	"unicode"
)

// Viewport-gated style-rule walking for the manual cascades.
//
// libcss gates the computed cascade on the real @media match, but the
// manual cascades (grid tracks, radii) re-read the author sheets with
// ForEachStyleRule, which descends into every rule-holding at-rule
// unconditionally. A later @media(max-width: ...) grid-template-columns
// then beat the base rule on viewports where the query is false — on live
// codeberg the 739px single-column rule overrode repeat(2,1fr) at 1000px
// and stacked the support rows.
//
// ForEachStyleRuleViewport walks like ForEachStyleRule but skips @media
// blocks whose query does not match the layout viewport (CSS Media Queries
// 3/4 core: media types, comma lists, and/or chains, min-/max-width/height
// in px; an unknown feature makes its query not-match, a negated unknown
// matches — Media Queries 4 §2.3). Every other rule-holding at-rule
// (@container, @supports, ...) keeps the old always-descend behavior: the
// engine cannot evaluate container queries, so that limitation is
// preserved, not changed, here.
func ForEachStyleRuleViewport(cssText string, viewportWidth, viewportHeight float64, f func(prelude, body string)) {
	forEachStyleRuleViewport(cssText, viewportWidth, viewportHeight, f)
}

func forEachStyleRuleViewport(cssText string, viewportWidth, viewportHeight float64, f func(prelude, body string)) {
	bytes := []byte(cssText)
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
		prelude := cssText[i:blockStart]
		body := cssText[blockStart+1 : blockEnd]
		if isMediaRule(prelude) {
			if mediaMatchesViewport(prelude, viewportWidth, viewportHeight) {
				forEachStyleRuleViewport(body, viewportWidth, viewportHeight, f)
			}
		} else if atRuleHoldsRules(prelude) {
			forEachStyleRuleViewport(body, viewportWidth, viewportHeight, f)
		} else if !strings.HasPrefix(strings.TrimLeftFunc(prelude, unicode.IsSpace), "@") {
			f(prelude, body)
		}
		i = blockEnd + 1
	}
}

// isMediaRule reports whether the prelude opens an @media block.
func isMediaRule(prelude string) bool {
	trimmed := strings.TrimLeftFunc(prelude, unicode.IsSpace)
	rest, found := strings.CutPrefix(trimmed, "@")
	if !found {
		return false
	}
	return asciiLower(takeAtRuleName(rest)) == "media"
}

// mediaMatchesViewport reports whether an @media prelude matches at the
// given viewport size. A comma list matches when any query matches.
func mediaMatchesViewport(prelude string, viewportWidth, viewportHeight float64) bool {
	trimmed := strings.TrimLeftFunc(prelude, unicode.IsSpace)
	rest := strings.TrimPrefix(trimmed, "@")
	name := takeAtRuleName(strings.ToLower(rest))
	query := strings.TrimSpace(rest[len(name):])
	for _, part := range splitMediaList(query) {
		if mediaQueryMatches(strings.TrimSpace(part), viewportWidth, viewportHeight) {
			return true
		}
	}
	return false
}

// splitMediaList splits a media query list on top-level commas, keeping
// parenthesised features (and quoted strings) intact.
func splitMediaList(query string) []string {
	var parts []string
	var current strings.Builder
	depth := 0
	for i := 0; i < len(query); {
		switch c := query[i]; {
		case c == '"' || c == '\'':
			end := skipString([]byte(query), i)
			current.WriteString(query[i:end])
			i = end
		case c == '(':
			depth++
			current.WriteByte(c)
			i++
		case c == ')':
			if depth > 0 {
				depth--
			}
			current.WriteByte(c)
			i++
		case c == ',' && depth == 0:
			parts = append(parts, current.String())
			current.Reset()
			i++
		default:
			current.WriteByte(c)
			i++
		}
	}
	parts = append(parts, current.String())
	return parts
}

// mediaQueryMatches evaluates one comma-free media query:
// [only|not]? <type>? [and|or <feature>]*. An unparseable connective, an
// unknown feature, or a missing value makes the query not-match (unknown
// media input is false unless negated — Media Queries 4 §2.3).
func mediaQueryMatches(query string, viewportWidth, viewportHeight float64) bool {
	lower := asciiLower(query)
	tokens := splitMediaTokens(lower)
	negated := false
	pos := 0
	// The optional leading modifiers and media type live in the text
	// before the first feature.
	typeOK := true
	pending := "and"
	if pos < len(tokens) && !tokens[pos].feature {
		words := strings.Fields(tokens[pos].text)
		pos++
		for len(words) > 0 && (words[0] == "only" || words[0] == "not") {
			if words[0] == "not" {
				negated = !negated
			}
			words = words[1:]
		}
		if len(words) > 0 && words[0] != "and" && words[0] != "or" {
			mediaType := words[0]
			words = words[1:]
			typeOK = mediaType == "" || mediaType == "all" || mediaType == "screen"
		}
		if len(words) == 0 {
			// Bare type ("screen") or bare modifiers: nothing pending.
		} else if len(words) == 1 && (words[0] == "and" || words[0] == "or") {
			// A trailing connective belongs to the first feature.
			pending = words[0]
		} else {
			// Any other leftover word is unparseable: unknown → false
			// unless negated.
			return negated
		}
	}
	value := true
	unknown := false
	first := true
	for ; pos < len(tokens); pos++ {
		tok := tokens[pos]
		if !tok.feature {
			for _, word := range strings.Fields(tok.text) {
				if word != "and" && word != "or" {
					return negated
				}
				pending = word
			}
			continue
		}
		known, match := mediaFeatureMatches(tok.text, viewportWidth, viewportHeight)
		if !known {
			unknown = true
		}
		if first {
			value = match
			first = false
		} else if pending == "or" {
			value = value || match
		} else {
			value = value && match
		}
		pending = "and"
	}
	result := typeOK && value
	if unknown {
		// Unknown input is false; a lone negation flips it to true.
		result = false
	}
	if negated {
		return !result
	}
	return result
}

// mediaToken is one piece of a media query: a parenthesised feature or the
// connective text around the features.
type mediaToken struct {
	feature bool
	text    string
}

// splitMediaTokens splits a lowercased query into features "(...)" and the
// text between them.
func splitMediaTokens(query string) []mediaToken {
	var tokens []mediaToken
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, mediaToken{text: current.String()})
			current.Reset()
		}
	}
	for i := 0; i < len(query); {
		if query[i] == '(' {
			end := i + 1
			depth := 1
			for end < len(query) && depth > 0 {
				switch query[end] {
				case '(':
					depth++
				case ')':
					depth--
				}
				end++
			}
			flush()
			tokens = append(tokens, mediaToken{feature: true, text: query[i:end]})
			i = end
			continue
		}
		current.WriteByte(query[i])
		i++
	}
	flush()
	return tokens
}

// mediaFeatureMatches evaluates one "(name: value)" feature. It reports
// whether the feature is known and whether it matches. Known: min-width,
// max-width, min-height, max-height with px lengths (fractional accepted,
// like 767.98px). Everything else is unknown.
func mediaFeatureMatches(feature string, viewportWidth, viewportHeight float64) (known, match bool) {
	inner := strings.TrimSpace(feature)
	if !strings.HasPrefix(inner, "(") || !strings.HasSuffix(inner, ")") {
		return false, false
	}
	inner = strings.TrimSpace(inner[1 : len(inner)-1])
	name, value, found := strings.Cut(inner, ":")
	if !found {
		return false, false
	}
	name = strings.TrimSpace(name)
	px, ok := mediaPx(strings.TrimSpace(value))
	if !ok {
		return false, false
	}
	switch name {
	case "min-width":
		return true, viewportWidth >= px
	case "max-width":
		return true, viewportWidth <= px
	case "min-height":
		return true, viewportHeight >= px
	case "max-height":
		return true, viewportHeight <= px
	}
	return false, false
}

// mediaPx parses a media-query length: a plain number with a px unit.
// Unitless zero is valid CSS; any other unit is out of scope and unknown.
func mediaPx(value string) (float64, bool) {
	if number, found := strings.CutSuffix(value, "px"); found {
		return parseNumber(number)
	}
	if number, found := strings.CutSuffix(value, "PX"); found {
		return parseNumber(number)
	}
	if value == "0" {
		return 0, true
	}
	return 0, false
}
