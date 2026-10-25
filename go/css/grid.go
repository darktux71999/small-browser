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
}

func (g *gridDecls) isEmpty() bool {
	return !g.hasColumn && !g.hasCol && !g.hasRow
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
		}
	}
	return decls
}

// applyGridStyles runs the grid cascade over the author sheets and fills
// the track lists and spans into `out`.
func applyGridStyles(doc NodeIndex, sheets []string, out []Style) {
	type gridBid struct {
		cascadeBid
		columns string
		hasCol  bool
		colSpan uint32
		hasCS   bool
		rowSpan uint32
		hasRS   bool
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
		}
		if wins {
			kept := merged
			best[id] = &kept
		}
	}

	for _, sheet := range sheets {
		ForEachStyleRule(sheet, func(prelude, body string) {
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
// and fills the corner radii into `out`.
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

	for id, bid := range best {
		if bid == nil {
			continue
		}
		var radii [4]Radius
		for i := range radii {
			radii[i] = RadiusPx(0)
		}
		if bid.hasSh {
			if parsed := parseBorderRadius(bid.shorthand); parsed != nil {
				copy(radii[:], parsed)
			}
		}
		for index := 0; index < 4; index++ {
			if bid.cornerSet[index] {
				if radius, ok := parseBorderRadiusLength(bid.corners[index]); ok {
					radii[index] = radius
				}
			}
		}
		out[id].BorderRadius = radii
	}
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
// specified) or an absolute px length.
func parseBorderRadiusLength(value string) (Radius, bool) {
	v := strings.TrimSpace(value)
	if number, found := strings.CutSuffix(v, "%"); found {
		pct, ok := parseNumber(strings.TrimSpace(number))
		if !ok || !isFinite(pct) || pct < 0 {
			return Radius{}, false
		}
		return RadiusPercent(pct), true
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
func parseBorderRadius(value string) []Radius {
	horizontal := strings.SplitN(value, "/", 2)[0]
	var radii []Radius
	for _, part := range strings.Fields(horizontal) {
		radius, ok := parseBorderRadiusLength(part)
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
