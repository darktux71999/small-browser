package css

import "strings"

// The background family's manual cascade (meta1 S10). libcss 0.9.2
// computes the family only as far as CSS 2.1 reaches: background-repeat
// and background-position arrive as computed values (mapping.go reads
// them for the painter), but every value form that postdates it —
// #rrggbbaa colours and image functions like linear-gradient(...) alike
// — is discarded, and the cascade knows neither var() nor @layer
// (Cascade 5). This pass closes those gaps for the background family
// only:
//
//   - a gradient-valued background declaration (the codeberg.org about
//     cards' vendor linear-gradient chain) is captured by the rewriter
//     and removed from the libcss text; this pass parses its two-stop
//     vertical ramp into Style.BackgroundGradient;
//   - a var()-valued background declaration resolves per element against
//     the custom-property map and applies the resolved colour or ramp;
//   - every bid carries its cascade layer (CSS Cascade 5 §6.4.2), so the
//     live page's layered theme declarations order correctly.
//
// Everything else stays with libcss: a plain colour, url or none keeps
// its computed value, and a plain bid that beats the captured one leaves
// the libcss state standing — the "a later url declaration clears the
// gradient slot" and "a later plain shorthand clears it" pins.
//
// The pass runs only for elements whose sheets or style="" attributes
// hold a captured (removed) background declaration: documents without
// gradient or var() backgrounds — the whole fixture corpus — never pay
// for it, and their computed styles stay byte-identical.

// isBackgroundFamily reports whether property is one of the three
// background names the manual cascade records: the shorthand and the
// image/colour longhands. (background-repeat and background-position are
// computed natively by libcss and never captured.)
func isBackgroundFamily(property string) bool {
	switch property {
	case "background", "background-color", "background-image":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Value classification
// ---------------------------------------------------------------------------

// bgSlot is one of the two slots a background declaration fills.
type bgSlot uint8

const (
	bgSlotImage bgSlot = iota
	bgSlotColor
)

// bgKind is what a winning bid applies: a parsed ramp, a parsed colour,
// an image-slot reset, or a url the engine cannot decode here.
type bgKind uint8

const (
	bgKindNone bgKind = iota // resets the image slot (background-image:none, or a colour shorthand)
	bgKindGradient
	bgKindColor
	bgKindURL // a url: bids, but never applies (the loader lives in layout)
)

// bgBid is one background declaration's contribution to one slot of one
// element: the cascade tuple plus the classified value.
type bgBid struct {
	bid      cascadeBid
	slot     bgSlot
	kind     bgKind
	gradient *LinearGradient // bgKindGradient
	color    Color           // bgKindColor
}

// classifyBackgroundValue splits one resolved background value into its
// slot bids. A shorthand contributes to both slots: a colour resets the
// image slot (CSS Backgrounds 3 §3.3 — the shorthand's omitted components
// take their initial values), while a gradient fills the image slot only.
// The asymmetry is deliberate and mirrors a browser: the prefixed
// gradient shorthand is invalid to libcss (and to a real browser), so it
// is dropped whole and the separately declared longhand colour keeps
// standing — the engine must not invent the shorthand's initial colour
// reset. A var() shorthand that RESOLVES to a colour is valid CSS, so it
// takes the full shorthand semantics (pinned by
// TestVarBackgroundShorthandClearsImageSlots).
func classifyBackgroundValue(property, value string) []bgBid {
	trimmed := strings.TrimSpace(value)
	lower := asciiLower(trimmed)
	switch {
	case lower == "none":
		return []bgBid{{slot: bgSlotImage, kind: bgKindNone}}
	case strings.HasPrefix(lower, "url("):
		return []bgBid{{slot: bgSlotImage, kind: bgKindURL}}
	}
	if g := parseLinearGradient(trimmed); g != nil {
		return []bgBid{{slot: bgSlotImage, kind: bgKindGradient, gradient: g}}
	}
	if c, ok := parseBgColor(trimmed); ok {
		bids := []bgBid{{slot: bgSlotColor, kind: bgKindColor, color: c}}
		if property == "background" {
			bids = append(bids, bgBid{slot: bgSlotImage, kind: bgKindNone})
		}
		return bids
	}
	return nil
}

// ---------------------------------------------------------------------------
// The two-stop vertical linear-gradient parser (CSS Images 3 §3.4 subset)
// ---------------------------------------------------------------------------

// parseLinearGradient parses the two-stop vertical linear-gradient subset
// the engine paints. The direction may be omitted (the initial "to
// bottom"), the modern "to bottom"/"to top" keywords, or the legacy
// unprefixed "top"/"bottom" keywords the vendor-prefixed forms used.
// Stops are a colour plus an optional position; the position is dropped —
// the ramp interpolates evenly between the box's top and bottom edges,
// the documented approximation of CSS Images 3 §3.4's stop positions.
// Anything else (diagonals, angles, left/right directions, any stop count
// but two, an unparsable colour) reports nil: the declaration stays
// inert, the behavior the engine had for backgrounds without an image.
func parseLinearGradient(value string) *LinearGradient {
	lower := asciiLower(value)
	idx := strings.Index(lower, "linear-gradient(")
	if idx < 0 {
		return nil
	}
	open := idx + len("linear-gradient(") - 1 // the '(' of the function
	closing := matchingParen(value, open)
	if closing < 0 {
		return nil
	}
	parts := SplitCommaList(value[open+1 : closing])
	stops := parts
	forward := true
	if len(parts) > 0 {
		switch dir := strings.TrimSpace(parts[0]); asciiLower(dir) {
		case "to bottom", "top":
			forward = true
			stops = parts[1:]
		case "to top", "bottom":
			forward = false
			stops = parts[1:]
		case "to left", "to right", "left", "right", "":
			return nil
		default:
			// "to top right" and every other diagonal, and every angle,
			// stay out of the vertical subset.
			if d := asciiLower(dir); strings.HasPrefix(d, "to ") || strings.HasSuffix(d, "deg") {
				return nil
			}
			// Otherwise the first part is a colour: no direction given,
			// the initial "to bottom" applies.
		}
	}
	if len(stops) != 2 {
		return nil
	}
	from, ok1 := parseBgColor(stopColor(stops[0]))
	to, ok2 := parseBgColor(stopColor(stops[1]))
	if !ok1 || !ok2 {
		return nil
	}
	if forward {
		return &LinearGradient{From: from, To: to}
	}
	return &LinearGradient{From: to, To: from}
}

// stopColor is the colour token of one gradient stop: everything before
// the first whitespace that sits outside parentheses (the optional stop
// position that follows is dropped). SplitCommaList keeps the whitespace
// after a comma with the part, so the leading trivia goes first — a stop
// never starts with significant whitespace (CSS tokenizer).
func stopColor(stop string) string {
	stop = strings.TrimSpace(stop)
	depth := 0
	for i := 0; i < len(stop); i++ {
		switch stop[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ' ', '\t', '\n', '\f', '\r':
			if depth == 0 {
				return strings.TrimSpace(stop[:i])
			}
		}
	}
	return strings.TrimSpace(stop)
}

// matchingParen answers the index of the ')' closing the '(' at open,
// or -1 when the run is unbalanced.
func matchingParen(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// The background colour parser
// ---------------------------------------------------------------------------

// parseBgColor parses the colour forms the background family uses: the
// #rgb, #rrggbb and #rrggbbaa hex forms (CSS Color 4's hex8 alpha — the
// codeberg cards' #144B49B2), the 16 CSS 2.1 keywords plus transparent,
// and the comma syntax of rgb()/rgba(). Everything else — CSS Color 4's
// space syntax, hsl(), the extended keyword set — reports false: the
// declaration stays inert, the degradation the engine had before this
// pass existed.
func parseBgColor(value string) (Color, bool) {
	v := strings.TrimSpace(value)
	if v == "" {
		return Color{}, false
	}
	if v[0] == '#' {
		return parseHexBgColor(v[1:])
	}
	lower := asciiLower(v)
	if lower == "transparent" {
		return Color{}, true
	}
	if c, ok := bgColorKeywords[lower]; ok {
		return c, true
	}
	if strings.HasPrefix(lower, "rgb(") || strings.HasPrefix(lower, "rgba(") {
		return parseRgbBgColor(lower)
	}
	return Color{}, false
}

// parseHexBgColor parses the 3/6/8-digit hex forms; the 3-digit form
// expands (#abc -> #aabbcc) and the 8-digit form carries the alpha.
func parseHexBgColor(hex string) (Color, bool) {
	digit := func(c byte) (uint32, bool) {
		switch {
		case '0' <= c && c <= '9':
			return uint32(c - '0'), true
		case 'a' <= c && c <= 'f':
			return uint32(c-'a') + 10, true
		case 'A' <= c && c <= 'F':
			return uint32(c-'A') + 10, true
		}
		return 0, false
	}
	pair := func(s string) (uint32, bool) {
		hi, ok1 := digit(s[0])
		lo, ok2 := digit(s[1])
		if !ok1 || !ok2 {
			return 0, false
		}
		return hi*16 + lo, true
	}
	switch len(hex) {
	case 3:
		r, okr := digit(hex[0])
		g, okg := digit(hex[1])
		b, okb := digit(hex[2])
		if !okr || !okg || !okb {
			return Color{}, false
		}
		return Color{R: float64(r*17) / 255, G: float64(g*17) / 255, B: float64(b*17) / 255, A: 1}, true
	case 6:
		r, okr := pair(hex[0:2])
		g, okg := pair(hex[2:4])
		b, okb := pair(hex[4:6])
		if !okr || !okg || !okb {
			return Color{}, false
		}
		return Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255, A: 1}, true
	case 8:
		r, okr := pair(hex[0:2])
		g, okg := pair(hex[2:4])
		b, okb := pair(hex[4:6])
		a, oka := pair(hex[6:8])
		if !okr || !okg || !okb || !oka {
			return Color{}, false
		}
		return Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255, A: float64(a) / 255}, true
	}
	return Color{}, false
}

// rgb8 builds an opaque colour from three 8-bit channels.
func rgb8(r, g, b uint32) Color {
	return Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255, A: 1}
}

// bgColorKeywords are the 16 CSS 2.1 colour keywords (the set libcss'
// own colour parser accepts) plus transparent.
var bgColorKeywords = map[string]Color{
	"black":   rgb8(0x00, 0x00, 0x00),
	"silver":  rgb8(0xc0, 0xc0, 0xc0),
	"gray":    rgb8(0x80, 0x80, 0x80),
	"white":   rgb8(0xff, 0xff, 0xff),
	"maroon":  rgb8(0x80, 0x00, 0x00),
	"red":     rgb8(0xff, 0x00, 0x00),
	"purple":  rgb8(0x80, 0x00, 0x80),
	"fuchsia": rgb8(0xff, 0x00, 0xff),
	"green":   rgb8(0x00, 0x80, 0x00),
	"lime":    rgb8(0x00, 0xff, 0x00),
	"olive":   rgb8(0x80, 0x80, 0x00),
	"yellow":  rgb8(0xff, 0xff, 0x00),
	"navy":    rgb8(0x00, 0x00, 0x80),
	"blue":    rgb8(0x00, 0x00, 0xff),
	"teal":    rgb8(0x00, 0x80, 0x80),
	"aqua":    rgb8(0x00, 0xff, 0xff),
}

// parseRgbBgColor parses the comma syntax of rgb()/rgba(): three (or four
// with alpha) comma-separated numbers or percentages. CSS Color 4's
// space syntax ("rgb(0 100% 0 / 50%)") is deliberately rejected: it stays
// inert, the same degradation the engine had before this pass existed.
func parseRgbBgColor(value string) (Color, bool) {
	open := strings.IndexByte(value, '(')
	if open < 0 || !strings.HasSuffix(value, ")") {
		return Color{}, false
	}
	parts := SplitCommaList(strings.TrimSpace(value[open+1 : len(value)-1]))
	if len(parts) != 3 && len(parts) != 4 {
		return Color{}, false
	}
	channels := [3]float64{}
	for i := 0; i < 3; i++ {
		c, ok := parseBgChannel(strings.TrimSpace(parts[i]))
		if !ok {
			return Color{}, false
		}
		channels[i] = c
	}
	alpha := 1.0
	if len(parts) == 4 {
		a, ok := parseBgAlpha(strings.TrimSpace(parts[3]))
		if !ok {
			return Color{}, false
		}
		alpha = a
	}
	return Color{R: channels[0], G: channels[1], B: channels[2], A: alpha}, true
}

// parseBgChannel parses one rgb() channel: a number in [0,255] or a
// percentage.
func parseBgChannel(text string) (float64, bool) {
	if strings.HasSuffix(text, "%") {
		pct, ok := parseNumber(strings.TrimSpace(strings.TrimSuffix(text, "%")))
		if !ok || pct < 0 || pct > 100 {
			return 0, false
		}
		return pct / 100 * 255, true
	}
	n, ok := parseNumber(text)
	if !ok || n < 0 || n > 255 {
		return 0, false
	}
	return n, true
}

// parseBgAlpha parses the rgba() alpha: a number in [0,1] or a percentage.
func parseBgAlpha(text string) (float64, bool) {
	if strings.HasSuffix(text, "%") {
		pct, ok := parseNumber(strings.TrimSpace(strings.TrimSuffix(text, "%")))
		if !ok || pct < 0 || pct > 100 {
			return 0, false
		}
		return pct / 100, true
	}
	n, ok := parseNumber(text)
	if !ok || n < 0 || n > 1 {
		return 0, false
	}
	return n, true
}

// ---------------------------------------------------------------------------
// The manual cascade
// ---------------------------------------------------------------------------

// elementBgBids holds the strongest background bid of one element per
// side (captured — removed from the libcss text — and plain) and per
// slot (image and colour).
type elementBgBids struct {
	capturedImage, capturedColor *bgBid
	plainImage, plainColor       *bgBid
}

// applyBackgroundStyles runs the background family's manual cascade over
// the author sheets and the style="" attributes and applies the winning
// captured declaration to `out`: the parsed ramp into
// Style.BackgroundGradient, a var()-resolved colour into Style.Background,
// and the image-slot resets the shorthand semantics ask for. It must run
// after the libcss cascade filled the computed state: a plain bid that
// wins — or an element without any captured declaration — leaves that
// state exactly as libcss computed it.
//
// viewportWidth gates the captured @media conditions (the same matcher
// the grid pass uses; the height stays libcss' 600px default), the
// conditions of nested @media blocks as a conjunction.
func applyBackgroundStyles(doc NodeIndex, sheets []string, viewportWidth float64, out []Style) {
	cp := NewCustomProperties(doc, sheets)

	// The gate: only documents whose sheets or style="" attributes hold a
	// captured (removed) background declaration — a gradient or a var()
	// value — are touched. Everything else is libcss' own, byte for byte.
	if !hasCapturedBackground(doc, cp) {
		return
	}

	best := make([]*elementBgBids, len(out))
	bids := func(id int) *elementBgBids {
		if id < 0 || id >= len(best) {
			return nil
		}
		if best[id] == nil {
			best[id] = &elementBgBids{}
		}
		return best[id]
	}
	stronger := func(prev, cand *bgBid) *bgBid {
		if prev == nil || cand.bid.winsOver(prev.bid) {
			return cand
		}
		return prev
	}

	consider := func(id int, decl CustomDecl, bid cascadeBid) {
		b := bids(id)
		if b == nil {
			return
		}
		resolved := decl.Value
		if containsVarFunction(decl.Value) {
			value, ok := cp.Resolve(id, decl.Value)
			if !ok {
				return // invalid at computed-value time: dropped
			}
			resolved = value
		}
		if !mediaListMatches(decl.MediaList, viewportWidth) {
			return
		}
		for _, classified := range classifyBackgroundValue(decl.Property, resolved) {
			classified.bid = bid
			if decl.Kept {
				switch classified.slot {
				case bgSlotImage:
					b.plainImage = stronger(b.plainImage, &classified)
				case bgSlotColor:
					b.plainColor = stronger(b.plainColor, &classified)
				}
				continue
			}
			switch classified.slot {
			case bgSlotImage:
				b.capturedImage = stronger(b.capturedImage, &classified)
			case bgSlotColor:
				b.capturedColor = stronger(b.capturedColor, &classified)
			}
		}
	}

	for _, decl := range cp.Declarations() {
		if !isBackgroundFamily(decl.Property) {
			continue
		}
		for _, selector := range strings.Split(decl.Selector, ",") {
			selector = strings.TrimSpace(selector)
			if selector == "" {
				continue
			}
			specificity := selectorSpecificity(selector)
			for _, id := range doc.QuerySelectorAll(selector) {
				consider(id, decl, cascadeBid{
					important:   decl.Important,
					specificity: specificity,
					order:       decl.Order,
					layerRank:   cp.layerRank(decl.Layer),
				})
			}
		}
	}

	// The style="" attribute: an inline declaration list, matched to its
	// own element and winning over every rule (like grid/radius/borders).
	for id := 0; id < doc.Len(); id++ {
		raw, ok := doc.Attr(id, "style")
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		_, inline := CaptureInline(raw)
		for _, decl := range inline {
			if !isBackgroundFamily(decl.Property) {
				continue
			}
			consider(id, decl, cascadeBid{
				important: decl.Important,
				inline:    true,
				order:     ^uint32(0), // u32::MAX
				layerRank: cp.layerRank(decl.Layer),
			})
		}
	}

	for id, b := range best {
		if b == nil {
			continue
		}
		// Image slot: the strongest captured bid wins over the strongest
		// plain one under the same cascade tuple; a plain winner — or no
		// captured bid at all — leaves libcss' computed state standing.
		if bid := winningImageBid(b); bid != nil {
			switch bid.kind {
			case bgKindGradient:
				out[id].BackgroundGradient = bid.gradient
				out[id].HasBackgroundImage = false
				out[id].BackgroundImage = ""
			case bgKindNone:
				out[id].BackgroundGradient = nil
				out[id].HasBackgroundImage = false
				out[id].BackgroundImage = ""
			default:
				// bgKindURL: a var()-resolved url the engine cannot decode
				// here (the loader lives in layout); the slot keeps
				// whatever libcss computed.
			}
		}
		// Colour slot: same two-sided rule; the winning resolved colour
		// overwrites the libcss-computed one.
		if bid := winningColorBid(b); bid != nil && bid.kind == bgKindColor {
			c := bid.color
			out[id].Background = &c
		}
	}
}

// winningImageBid answers the image slot's winning captured bid: the
// strongest captured one, but only when it beats the strongest plain bid
// (or none exists).
func winningImageBid(b *elementBgBids) *bgBid {
	if b.capturedImage == nil {
		return nil
	}
	if b.plainImage == nil || b.capturedImage.bid.winsOver(b.plainImage.bid) {
		return b.capturedImage
	}
	return nil
}

// winningColorBid is winningImageBid for the colour slot.
func winningColorBid(b *elementBgBids) *bgBid {
	if b.capturedColor == nil {
		return nil
	}
	if b.plainColor == nil || b.capturedColor.bid.winsOver(b.plainColor.bid) {
		return b.capturedColor
	}
	return nil
}

// hasCapturedBackground reports whether the sheets or any style=""
// attribute hold a captured (removed) background declaration — the only
// documents this pass touches.
func hasCapturedBackground(doc NodeIndex, cp *CustomProperties) bool {
	for _, decl := range cp.Declarations() {
		if !decl.Kept && isBackgroundFamily(decl.Property) {
			return true
		}
	}
	for id := 0; id < doc.Len(); id++ {
		raw, ok := doc.Attr(id, "style")
		if !ok {
			continue
		}
		_, inline := CaptureInline(raw)
		for _, decl := range inline {
			if !decl.Kept && isBackgroundFamily(decl.Property) {
				return true
			}
		}
	}
	return false
}

// mediaListMatches evaluates the captured @media conditions as a
// conjunction: a declaration behind nested @media blocks applies only
// when every condition matches (the same matcher the grid pass's
// viewport walker uses).
func mediaListMatches(conditions []string, viewportWidth float64) bool {
	for _, cond := range conditions {
		if !mediaQueryMatches(cond, viewportWidth, defaultViewportHeight) {
			return false
		}
	}
	return true
}
