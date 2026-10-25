package css

import (
	"math"
	"strconv"
	"strings"
	"unicode"
)

// isUnicodeSpace mirrors Rust's char::is_whitespace (the Unicode
// White_Space property), used by the track-list splitter.
func isUnicodeSpace(r rune) bool {
	return unicode.IsSpace(r)
}

// The grid / border-radius passes, mirroring apply_grid_styles,
// grid_declarations, apply_border_radius_styles, radius_declarations and
// selector_specificity of css_libcss.rs plus the css.rs parsers they call
// (parse_grid_tracks, parse_grid_span, parse_border_radius,
// parse_border_radius_length). libcss 0.9.2 computes none of these
// properties, so the same manual cascade resolution reads them straight
// from the author stylesheets after the computed cascade fixed display.
// S4 extends the radius reader with rem/em (resolved against the computed
// font sizes: the root element's for rem, the element's own for em) and
// resolves var() values first — the S4 contract's approximation, which
// the frozen oracle lacks until its own S4 lands.

type cascadeBid struct {
	important   bool
	inline      bool
	specificity [3]uint32
	order       uint32
}

// winsWhen mirrors the (important, inline, specificity, order) > compare.
func (b cascadeBid) winsOver(prev cascadeBid) bool {
	if b.important != prev.important {
		return b.important
	}
	if b.inline != prev.inline {
		return b.inline
	}
	if b.specificity != prev.specificity {
		return b.specificity[0] > prev.specificity[0] ||
			(b.specificity[0] == prev.specificity[0] && b.specificity[1] > prev.specificity[1]) ||
			(b.specificity[0] == prev.specificity[0] && b.specificity[1] == prev.specificity[1] && b.specificity[2] > prev.specificity[2])
	}
	return b.order > prev.order
}

// gridDecls is the grid declaration block of one rule (GridDecls).
type gridDecls struct {
	important bool
	columns   string // "" when absent
	hasColumn bool
	colSpan   uint32
	hasCol    bool
	rowSpan   uint32
	hasRow    bool
	justify   GridSelfAlign
	hasJust   bool
	align     GridSelfAlign
	hasAlign  bool
}

func (g *gridDecls) isEmpty() bool {
	return !g.hasColumn && !g.hasCol && !g.hasRow && !g.hasJust && !g.hasAlign
}

// gridDeclarations parses the grid properties of one declaration block; the
// raw column text plus the spans, with `important` when any carries it.
func gridDeclarations(body string) gridDecls {
	decls := gridDecls{}
	for _, part := range SplitDeclarations(body) {
		property, value, ok := SplitDeclaration(part)
		if !ok {
			continue
		}
		switch asciiLower(strings.TrimSpace(property)) {
		case "grid-template-columns":
			core, important := SplitImportant(value)
			decls.columns = strings.TrimSpace(core)
			decls.hasColumn = true
			decls.important = decls.important || important
		case "grid-column":
			core, important := SplitImportant(value)
			if span, ok := parseGridSpan(core); ok {
				decls.colSpan = span
				decls.hasCol = true
				decls.important = decls.important || important
			}
		case "grid-row":
			core, important := SplitImportant(value)
			if span, ok := parseGridSpan(core); ok {
				decls.rowSpan = span
				decls.hasRow = true
				decls.important = decls.important || important
			}
		case "place-self":
			core, important := SplitImportant(value)
			align, justify := parsePlaceSelf(core)
			decls.align = align
			decls.hasAlign = true
			decls.justify = justify
			decls.hasJust = true
			decls.important = decls.important || important
		case "justify-self":
			core, important := SplitImportant(value)
			decls.justify = parseSelfAlign(core)
			decls.hasJust = true
			decls.important = decls.important || important
		case "align-self":
			core, important := SplitImportant(value)
			decls.align = parseSelfAlign(core)
			decls.hasAlign = true
			decls.important = decls.important || important
		}
	}
	return decls
}

// applyGridStyles runs the grid cascade over the author sheets and fills
// the track lists and spans into `out`. @media blocks apply only when
// their query matches the layout viewport (CSS Media Queries 3/4 core:
// media types, and/or lists, min-/max-width/height in px; unknown features
// evaluate to not-matching, mirroring libcss' own @media gating of the
// computed cascade). The other rule-holding at-rules (@container,
// @supports, ...) still apply unconditionally — the engine cannot evaluate
// container queries, so their current always-apply behavior is preserved.
// viewportWidth/Height are the CSS pixels css.Compute laid out with.
func applyGridStyles(doc NodeIndex, sheets []string, viewportWidth, viewportHeight float64, out []Style) {
	type gridBid struct {
		cascadeBid
		columns string
		hasCol  bool
		colSpan uint32
		hasCS   bool
		rowSpan uint32
		hasRS   bool
		justify GridSelfAlign
		hasJ    bool
		align   GridSelfAlign
		hasA    bool
	}

	best := make([]*gridBid, len(out))
	order := uint32(0)
	consider := func(id int, bid gridBid, best []*gridBid) {
		if id >= len(best) {
			return
		}
		wins := true
		merged := bid
		if prev := best[id]; prev != nil {
			wins = bid.winsOver(prev.cascadeBid)
			// A rule only carries the grid properties it declares: keep the
			// winner's values, filling the gaps from the previous best.
			if !bid.hasCol {
				merged.columns = prev.columns
				merged.hasCol = prev.hasCol
			}
			if !bid.hasCS {
				merged.colSpan = prev.colSpan
				merged.hasCS = prev.hasCS
			}
			if !bid.hasRS {
				merged.rowSpan = prev.rowSpan
				merged.hasRS = prev.hasRS
			}
			if !bid.hasJ {
				merged.justify = prev.justify
				merged.hasJ = prev.hasJ
			}
			if !bid.hasA {
				merged.align = prev.align
				merged.hasA = prev.hasA
			}
		}
		if wins {
			kept := merged
			best[id] = &kept
		}
	}

	for _, sheet := range sheets {
		ForEachStyleRuleViewport(sheet, viewportWidth, viewportHeight, func(prelude, body string) {
			order++
			decls := gridDeclarations(body)
			if decls.isEmpty() {
				return
			}
			for _, selector := range strings.Split(prelude, ",") {
				selector = strings.TrimSpace(selector)
				if selector == "" {
					continue
				}
				specificity := selectorSpecificity(selector)
				for _, id := range doc.QuerySelectorAll(selector) {
					consider(id, gridBid{
						cascadeBid: cascadeBid{
							important:   decls.important,
							inline:      false,
							specificity: specificity,
							order:       order,
						},
						columns: decls.columns,
						hasCol:  decls.hasColumn,
						colSpan: decls.colSpan,
						hasCS:   decls.hasCol,
						rowSpan: decls.rowSpan,
						hasRS:   decls.hasRow,
						justify: decls.justify,
						hasJ:    decls.hasJust,
						align:   decls.align,
						hasA:    decls.hasAlign,
					}, best)
				}
			}
		})
	}

	// The style="" attribute: an inline declaration list, parsed like the
	// inline sheets above but matched to its own element.
	for id := 0; id < doc.Len(); id++ {
		decls, ok := doc.Attr(id, "style")
		if !ok || strings.TrimSpace(decls) == "" {
			continue
		}
		inline := RewriteGapDeclarations(decls)
		parsed := gridDeclarations(inline)
		if parsed.isEmpty() {
			continue
		}
		consider(id, gridBid{
			cascadeBid: cascadeBid{
				important: parsed.important,
				inline:    true,
				order:     ^uint32(0), // u32::MAX
			},
			columns: parsed.columns,
			hasCol:  parsed.hasColumn,
			colSpan: parsed.colSpan,
			hasCS:   parsed.hasCol,
			rowSpan: parsed.rowSpan,
			hasRS:   parsed.hasRow,
			justify: parsed.justify,
			hasJ:    parsed.hasJust,
			align:   parsed.align,
			hasA:    parsed.hasAlign,
		}, best)
	}

	for id, bid := range best {
		if bid == nil {
			continue
		}
		if bid.hasCS {
			out[id].GridItem.ColSpan = bid.colSpan
		}
		if bid.hasRS {
			out[id].GridItem.RowSpan = bid.rowSpan
		}
		if bid.hasJ {
			out[id].GridItem.Justify = bid.justify
		}
		if bid.hasA {
			out[id].GridItem.Align = bid.align
		}
		if out[id].Grid != nil {
			var tracks []GridTrack
			if bid.hasCol {
				tracks = parseGridTracks(bid.columns)
			}
			if tracks == nil {
				tracks = []GridTrack{{Kind: TrackAuto}}
			}
			out[id].Grid.Tracks = tracks
		}
	}
	// A grid container without any track declaration lays out in the
	// single implicit column.
	for i := range out {
		if out[i].Grid != nil && len(out[i].Grid.Tracks) == 0 {
			out[i].Grid.Tracks = []GridTrack{{Kind: TrackAuto}}
		}
	}
}

// radiusDecls is the radius declaration block of one rule (RadiusDecls).
type radiusDecls struct {
	important bool
	shorthand string
	hasSh     bool
	corners   [4]string
	cornerSet [4]bool
}

func (r *radiusDecls) isEmpty() bool {
	if r.hasSh {
		return false
	}
	for _, set := range r.cornerSet {
		if set {
			return false
		}
	}
	return true
}

// radiusDeclarations parses the radius properties of one declaration block:
// the raw shorthand plus one raw text per corner (top-left, top-right,
// bottom-right, bottom-left), with `important` when any carries it.
func radiusDeclarations(body string) radiusDecls {
	decls := radiusDecls{}
	for _, part := range SplitDeclarations(body) {
		property, value, ok := SplitDeclaration(part)
		if !ok {
			continue
		}
		core, important := SplitImportant(value)
		index := -1
		switch asciiLower(strings.TrimSpace(property)) {
		case "border-radius":
			decls.shorthand = strings.TrimSpace(core)
			decls.hasSh = true
			decls.important = decls.important || important
			continue
		case "border-top-left-radius":
			index = 0
		case "border-top-right-radius":
			index = 1
		case "border-bottom-right-radius":
			index = 2
		case "border-bottom-left-radius":
			index = 3
		default:
			continue
		}
		decls.corners[index] = strings.TrimSpace(core)
		decls.cornerSet[index] = true
		decls.important = decls.important || important
	}
	return decls
}

// applyBorderRadiusStyles runs the radius cascade over the author sheets
// and fills the corner radii into `out`. rem/em lengths (S4) resolve
// against the computed font sizes — the root element's for rem, each
// element's own for em — and a value arriving through var() is resolved
// per element first, then parsed by the same reader as a plain value.
func applyBorderRadiusStyles(doc NodeIndex, sheets []string, out []Style) {
	type radiusBid struct {
		cascadeBid
		shorthand string
		hasSh     bool
		corners   [4]string
		cornerSet [4]bool
	}

	best := make([]*radiusBid, len(out))
	order := uint32(0)
	consider := func(id int, bid radiusBid, best []*radiusBid) {
		if id >= len(best) {
			return
		}
		wins := true
		merged := bid
		if prev := best[id]; prev != nil {
			wins = bid.winsOver(prev.cascadeBid)
			if !bid.hasSh {
				merged.shorthand = prev.shorthand
				merged.hasSh = prev.hasSh
			}
			for i := 0; i < 4; i++ {
				if !bid.cornerSet[i] {
					merged.corners[i] = prev.corners[i]
					merged.cornerSet[i] = prev.cornerSet[i]
				}
			}
		}
		if wins {
			kept := merged
			best[id] = &kept
		}
	}

	for _, sheet := range sheets {
		ForEachStyleRule(sheet, func(prelude, body string) {
			order++
			decls := radiusDeclarations(body)
			if decls.isEmpty() {
				return
			}
			for _, selector := range strings.Split(prelude, ",") {
				selector = strings.TrimSpace(selector)
				if selector == "" {
					continue
				}
				specificity := selectorSpecificity(selector)
				for _, id := range doc.QuerySelectorAll(selector) {
					consider(id, radiusBid{
						cascadeBid: cascadeBid{
							important:   decls.important,
							inline:      false,
							specificity: specificity,
							order:       order,
						},
						shorthand: decls.shorthand,
						hasSh:     decls.hasSh,
						corners:   decls.corners,
						cornerSet: decls.cornerSet,
					}, best)
				}
			}
		})
	}

	for id := 0; id < doc.Len(); id++ {
		decls, ok := doc.Attr(id, "style")
		if !ok || strings.TrimSpace(decls) == "" {
			continue
		}
		inline := RewriteGapDeclarations(decls)
		parsed := radiusDeclarations(inline)
		if parsed.isEmpty() {
			continue
		}
		consider(id, radiusBid{
			cascadeBid: cascadeBid{
				important: parsed.important,
				inline:    true,
				order:     ^uint32(0), // u32::MAX
			},
			shorthand: parsed.shorthand,
			hasSh:     parsed.hasSh,
			corners:   parsed.corners,
			cornerSet: parsed.cornerSet,
		}, best)
	}

	// The rem resolution source: the root element's computed font-size,
	// reused from Style (16 unless html declares another size).
	remPx := rootFontSizePx(doc, out)

	// A radius value may arrive through var(): the rewriter captures
	// border-radius: var(...) declarations (libcss 0.9.2 discards them, so
	// the frozen oracle degrades such a radius), and the S4 contract
	// resolves them here — cp.Resolve's answer goes through the same
	// length reader as a plain value. The side table is captured on first
	// use: documents without a var() radius never pay for the walk. An
	// unresolvable var() degrades exactly like the unparsable text it was
	// before S4: the declaration contributes nothing.
	var cp *CustomProperties
	resolve := func(id int, value string) (string, bool) {
		if !containsVarFunction(value) {
			return value, true
		}
		if cp == nil {
			cp = NewCustomProperties(doc, sheets)
		}
		return cp.Resolve(id, value)
	}

	for id, bid := range best {
		if bid == nil {
			continue
		}
		// The em resolution source: the element's own computed
		// font-size, reused from Style (16 when somehow unknown).
		emPx := out[id].FontSize
		if !isFinite(emPx) {
			emPx = defaultFontSize
		}
		var radii [4]Radius
		for i := range radii {
			radii[i] = RadiusPx(0)
		}
		if bid.hasSh {
			if value, ok := resolve(id, bid.shorthand); ok {
				if parsed := parseBorderRadius(value, emPx, remPx); parsed != nil {
					copy(radii[:], parsed)
				}
			}
		}
		for index := 0; index < 4; index++ {
			if bid.cornerSet[index] {
				if value, ok := resolve(id, bid.corners[index]); ok {
					if radius, ok := parseBorderRadiusLength(value, emPx, remPx); ok {
						radii[index] = radius
					}
				}
			}
		}
		out[id].BorderRadius = radii
	}
}

// rootFontSizePx answers the root element's computed font-size, the rem
// resolution source: Style.FontSize of the html element (libcss' computed
// value, reused as-is), 16 when the document has no root element or its
// size is unknown.
func rootFontSizePx(doc NodeIndex, out []Style) float64 {
	for id := 0; id < doc.Len(); id++ {
		if id != doc.Root() && isRootElement(doc, id) {
			if px := out[id].FontSize; isFinite(px) {
				return px
			}
			return defaultFontSize
		}
	}
	return defaultFontSize
}

// selectorSpecificity approximates a selector the way the cascade orders it:
// ids, classes/attributes/pseudo-classes and type selectors. It only orders
// competing grid/radius declarations.
func selectorSpecificity(selector string) [3]uint32 {
	var ids, classes, tags uint32
	// Pseudo-elements (::before) count as types, not classes.
	plain := strings.ReplaceAll(selector, "::", "  ")
	for _, chunk := range splitAny(plain, " >+~,") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" || chunk == "*" {
			continue
		}
		ids += uint32(strings.Count(chunk, "#"))
		classes += uint32(strings.Count(chunk, "."))
		classes += uint32(strings.Count(chunk, "["))
		classes += uint32(strings.Count(chunk, ":"))
		// A leading tag name: the chunk starts with a type selector unless
		// it opens with an id, class, attribute, pseudo-class or universal.
		if first := []rune(chunk)[0]; isASCIILetter(byte(first)) {
			tags++
		}
	}
	return [3]uint32{ids, classes, tags}
}

func isASCIILetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// splitAny splits on every byte in the table (Rust's split(|c| matches!...)).
func splitAny(text, table string) []string {
	var out []string
	current := strings.Builder{}
	for i := 0; i < len(text); i++ {
		if strings.IndexByte(table, text[i]) >= 0 {
			out = append(out, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(text[i])
	}
	return append(out, current.String())
}

// ---------------------------------------------------------------------------
// css.rs track/span/radius parsers
// ---------------------------------------------------------------------------

// parseGridTracks parses a grid-template-columns value into its tracks:
// px lengths, fr fractions, auto, and repeat(n, track) over a single
// track. Unknown tokens degrade to TrackAuto so one typo never breaks the
// whole declaration; nil when the track list ends up empty.
func parseGridTracks(value string) []GridTrack {
	var tracks []GridTrack
	for _, token := range splitTrackList(value) {
		if expanded := expandRepeat(token); expanded != nil {
			tracks = append(tracks, expanded...)
		} else {
			tracks = append(tracks, parseGridTrack(token))
		}
	}
	return tracks
}

// splitTrackList splits a track list on whitespace outside parentheses, so
// repeat(2, 100px) stays one token.
func splitTrackList(value string) []string {
	var tokens []string
	var current strings.Builder
	depth := 0
	for _, ch := range value {
		switch {
		case ch == '(':
			depth++
			current.WriteRune(ch)
		case ch == ')':
			if depth > 0 {
				depth--
			}
			current.WriteRune(ch)
		case depth == 0 && isUnicodeSpace(ch):
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func parseGridTrack(token string) GridTrack {
	token = strings.TrimSpace(token)
	if asciiEqualFold(token, "auto") {
		return GridTrack{Kind: TrackAuto}
	}
	// minmax(min, max): each bound is a px length or auto (CSS Grid L3
	// §5.2.1). A bound with any other unit, or a fr extremum, degrades to
	// a plain auto track instead of failing the declaration.
	if inner, found := cutFoldPrefix(token, "minmax("); found {
		if body, ok := strings.CutSuffix(inner, ")"); ok {
			if track, ok := parseMinmaxTrack(body); ok {
				return track
			}
		}
		return GridTrack{Kind: TrackAuto}
	}
	// The suffix strips are case-sensitive pairs exactly like Rust's
	// strip_suffix("px").or(strip_suffix("PX")).
	if number, found := strings.CutSuffix(token, "px"); found {
		if px, ok := parseNumber(number); ok && isFinite(px) && px >= 0 {
			return GridTrack{Kind: TrackPx, Value: px}
		}
		return GridTrack{Kind: TrackAuto}
	}
	if number, found := strings.CutSuffix(token, "PX"); found {
		if px, ok := parseNumber(number); ok && isFinite(px) && px >= 0 {
			return GridTrack{Kind: TrackPx, Value: px}
		}
		return GridTrack{Kind: TrackAuto}
	}
	if number, found := strings.CutSuffix(token, "fr"); found {
		if fr, ok := parseNumber(number); ok && isFinite(fr) && fr > 0 {
			return GridTrack{Kind: TrackFr, Value: fr}
		}
		return GridTrack{Kind: TrackAuto}
	}
	if number, found := strings.CutSuffix(token, "FR"); found {
		if fr, ok := parseNumber(number); ok && isFinite(fr) && fr > 0 {
			return GridTrack{Kind: TrackFr, Value: fr}
		}
		return GridTrack{Kind: TrackAuto}
	}
	return GridTrack{Kind: TrackAuto}
}

// parseMinmaxTrack parses the inside of minmax(min, max): two comma
// arguments, each a px length or auto. ok is false for anything else (a fr
// extremum, a percentage, unparsable text), and the caller degrades the
// whole track to auto.
func parseMinmaxTrack(body string) (GridTrack, bool) {
	args := SplitCommaList(body)
	if len(args) != 2 {
		return GridTrack{}, false
	}
	track := GridTrack{Kind: TrackMinMax}
	for i, arg := range args {
		arg = strings.TrimSpace(arg)
		if asciiEqualFold(arg, "auto") {
			if i == 0 {
				track.MinAuto = true
			} else {
				track.MaxAuto = true
			}
			continue
		}
		var px float64
		var ok bool
		if number, found := strings.CutSuffix(arg, "px"); found {
			px, ok = parseNumber(number)
		} else if number, found := strings.CutSuffix(arg, "PX"); found {
			px, ok = parseNumber(number)
		}
		if !ok || !isFinite(px) || px < 0 {
			return GridTrack{}, false
		}
		if i == 0 {
			track.Min = px
		} else {
			track.Max = px
		}
	}
	return track, true
}

// cutFoldPrefix cuts a case-insensitive ASCII prefix, mirroring the
// repeat(/REPEAT( handling of expandRepeat.
func cutFoldPrefix(token, prefix string) (string, bool) {
	if len(token) < len(prefix) {
		return "", false
	}
	for i := 0; i < len(prefix); i++ {
		c, p := token[i], prefix[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != p {
			return "", false
		}
	}
	return token[len(prefix):], true
}

// expandRepeat expands repeat(n, track) over a single track; nil when the
// token is not a repetition (nested or multi-track lists degrade to a
// single auto track instead of failing the declaration).
func expandRepeat(token string) []GridTrack {
	inner, found := strings.CutPrefix(token, "repeat(")
	if !found {
		inner, found = strings.CutPrefix(token, "REPEAT(")
		if !found {
			return nil
		}
	}
	inner, found = strings.CutSuffix(inner, ")")
	if !found {
		return nil
	}
	countText, track, found := strings.Cut(inner, ",")
	if !found {
		return nil
	}
	count, ok := ParseUint(strings.TrimSpace(countText))
	if !ok {
		return nil
	}
	if count == 0 || count > 1000 {
		return []GridTrack{}
	}
	if len(splitTrackList(track)) != 1 {
		return []GridTrack{{Kind: TrackAuto}}
	}
	one := parseGridTrack(strings.TrimSpace(track))
	out := make([]GridTrack, count)
	for i := range out {
		out[i] = one
	}
	return out
}

// parsePlaceSelf parses a place-self value into its (align, justify) axes
// (CSS Box Alignment L3 §7: the first value aligns the block axis, the
// optional second the inline axis; one value sets both).
func parsePlaceSelf(value string) (GridSelfAlign, GridSelfAlign) {
	words := strings.Fields(value)
	if len(words) == 0 {
		return GridSelfAuto, GridSelfAuto
	}
	align := parseSelfAlign(words[0])
	if len(words) == 1 {
		return align, align
	}
	return align, parseSelfAlign(words[1])
}

// parseSelfAlign parses one self-alignment keyword (CSS Box Alignment L3
// §§5-6). left/right degrade to start/end: the engine lays out
// left-to-right only. Anything unknown is auto (stretch), the same
// degradation the track parser applies to unknown tokens.
func parseSelfAlign(value string) GridSelfAlign {
	switch asciiLower(strings.TrimSpace(value)) {
	case "stretch":
		return GridSelfStretch
	case "start", "flex-start", "self-start", "left":
		return GridSelfStart
	case "end", "flex-end", "self-end", "right":
		return GridSelfEnd
	case "center":
		return GridSelfCenter
	default:
		return GridSelfAuto
	}
}

// parseGridSpan parses a grid-column/grid-row value into the number of
// tracks it spans: the N in span N (a bare span spans one). Values without
// a span (2, 1 / 3, auto) place instead of spanning and report not-ok,
// leaving the default span of one alone.
func parseGridSpan(value string) (uint32, bool) {
	words := strings.Fields(value)
	for i, word := range words {
		if asciiEqualFold(word, "span") {
			span := uint32(1)
			if i+1 < len(words) {
				if n, ok := ParseUint(words[i+1]); ok {
					span = n
				}
			}
			if span > 1000 {
				span = 1000
			}
			if span < 1 {
				span = 1
			}
			return span, true
		}
	}
	return 0, false
}

// parseBorderRadiusLength parses one radius length: a percentage (kept as
// specified), a rem or em length (S4: resolved against the font sizes the
// caller supplies — remPx the root element's computed font-size, 16 unless
// html declares another size, emPx the element's own computed font-size,
// 16 when unknown; the approximation the S4 contract pins, always reusing
// Style.FontSize instead of recomputing font sizes), or an absolute
// px/pt length. Unresolvable input — an unknown unit (in, cm, ...), a
// negative or non-finite value, unparsable text — reports false, and the
// reader keeps the degradation it had before rem/em: a failing shorthand
// leaves all four corners at the default 0px, a failing corner keeps the
// value the winning shorthand left. Unit matching is case-sensitive, like
// parsePxText's px/pt.
func parseBorderRadiusLength(value string, emPx, remPx float64) (Radius, bool) {
	v := strings.TrimSpace(value)
	if number, found := strings.CutSuffix(v, "%"); found {
		pct, ok := parseNumber(strings.TrimSpace(number))
		if !ok || !isFinite(pct) || pct < 0 {
			return Radius{}, false
		}
		return RadiusPercent(pct), true
	}
	if number, found := strings.CutSuffix(v, "rem"); found {
		if n, ok := parseNumber(strings.TrimSpace(number)); ok && isFinite(n) && n >= 0 && isFinite(remPx) {
			return RadiusPx(n * remPx), true
		}
		return Radius{}, false
	}
	if number, found := strings.CutSuffix(v, "em"); found {
		if n, ok := parseNumber(strings.TrimSpace(number)); ok && isFinite(n) && n >= 0 && isFinite(emPx) {
			return RadiusPx(n * emPx), true
		}
		return Radius{}, false
	}
	px, ok := parsePxText(v)
	if !ok || !isFinite(px) || px < 0 {
		return Radius{}, false
	}
	return RadiusPx(px), true
}

// parseBorderRadius parses a border-radius value: 1-4 lengths (top-left,
// top-right, bottom-right, bottom-left, the CSS order) with an optional
// `/ <vertical>` half that is dropped (elliptical radii degrade to the
// horizontal radius). nil when any length is invalid.
func parseBorderRadius(value string, emPx, remPx float64) []Radius {
	horizontal := strings.SplitN(value, "/", 2)[0]
	var radii []Radius
	for _, part := range strings.Fields(horizontal) {
		radius, ok := parseBorderRadiusLength(part, emPx, remPx)
		if !ok {
			return nil
		}
		radii = append(radii, radius)
	}
	switch len(radii) {
	case 0:
		return nil
	case 1:
		return []Radius{radii[0], radii[0], radii[0], radii[0]}
	case 2:
		return []Radius{radii[0], radii[1], radii[0], radii[1]}
	case 3:
		return []Radius{radii[0], radii[1], radii[2], radii[1]}
	default:
		return []Radius{radii[0], radii[1], radii[2], radii[3]}
	}
}

// parsePxText mirrors css.rs::parse_px: a px or pt suffix is dropped and
// the rest parses as a number.
func parsePxText(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if number, found := strings.CutSuffix(value, "px"); found {
		return parseNumber(number)
	}
	if number, found := strings.CutSuffix(value, "pt"); found {
		return parseNumber(number)
	}
	return parseNumber(value)
}

// parseNumber parses an f64 the way Rust's str::parse does: no surrounding
// whitespace, no trailing junk.
func parseNumber(text string) (float64, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	// strconv.ParseFloat accepts NaN/Inf spellings and hex floats that
	// Rust's parse does not; both are rejected here.
	if strings.ContainsAny(text, "xXnN") {
		return 0, false
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// ParseUint parses a u32 with Rust's semantics (no signs, no overflow).
func ParseUint(text string) (uint32, bool) {
	if text == "" {
		return 0, false
	}
	var v uint64
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + uint64(c-'0')
		if v > 0xffffffff {
			return 0, false
		}
	}
	return uint32(v), true
}

func isFinite(v float64) bool {
	return !math.IsInf(v, 0) && !math.IsNaN(v)
}
