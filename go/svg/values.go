package svg

import (
	"math"
	"strconv"
	"strings"
)

// color is a straight-alpha sRGB colour with alpha in [0,1].
type color struct {
	R, G, B uint8
	A       float64
}

// namedColors is the CSS named-colour subset the SVG icons and logos use.
// Everything else is reported as unparseable and paints nothing.
var namedColors = map[string][3]uint8{
	"black": {0, 0, 0}, "white": {255, 255, 255}, "red": {255, 0, 0},
	"green": {0, 128, 0}, "lime": {0, 255, 0}, "blue": {0, 0, 255},
	"yellow": {255, 255, 0}, "cyan": {0, 255, 255}, "aqua": {0, 255, 255},
	"magenta": {255, 0, 255}, "fuchsia": {255, 0, 255}, "gray": {128, 128, 128},
	"grey": {128, 128, 128}, "silver": {192, 192, 192}, "maroon": {128, 0, 0},
	"olive": {128, 128, 0}, "purple": {128, 0, 128}, "teal": {0, 128, 128},
	"navy": {0, 0, 128}, "orange": {255, 165, 0}, "brown": {165, 42, 42},
	"pink": {255, 192, 203}, "gold": {255, 215, 0}, "indigo": {75, 0, 130},
	"violet": {238, 130, 238}, "darkgray": {169, 169, 169}, "darkgrey": {169, 169, 169},
	"lightgray": {211, 211, 211}, "lightgrey": {211, 211, 211}, "whitesmoke": {245, 245, 245},
	"darkblue": {0, 0, 139}, "darkred": {139, 0, 0}, "darkgreen": {0, 100, 0},
}

// parseColor reads a colour value. It returns false for none, currentColor
// (resolved by the caller), and anything it does not understand.
func parseColor(s string) (color, bool) {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	switch {
	case lower == "transparent":
		return color{A: 0}, true
	case strings.HasPrefix(s, "#"):
		return parseHex(s[1:])
	case strings.HasPrefix(lower, "rgb(") || strings.HasPrefix(lower, "rgba("):
		return parseRGBFunc(lower)
	}
	if rgb, ok := namedColors[lower]; ok {
		return color{R: rgb[0], G: rgb[1], B: rgb[2], A: 1}, true
	}
	return color{}, false
}

func parseHex(h string) (color, bool) {
	var digits []uint8
	for i := 0; i < len(h); i++ {
		v, ok := hexDigit(h[i])
		if !ok {
			return color{}, false
		}
		digits = append(digits, v)
	}
	switch len(digits) {
	case 3, 4:
		c := color{R: digits[0] * 17, G: digits[1] * 17, B: digits[2] * 17, A: 1}
		if len(digits) == 4 {
			c.A = float64(digits[3]*17) / 255
		}
		return c, true
	case 6, 8:
		c := color{
			R: digits[0]<<4 | digits[1],
			G: digits[2]<<4 | digits[3],
			B: digits[4]<<4 | digits[5],
			A: 1,
		}
		if len(digits) == 8 {
			c.A = float64(digits[6]<<4|digits[7]) / 255
		}
		return c, true
	}
	return color{}, false
}

func hexDigit(c byte) (uint8, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// parseRGBFunc reads rgb()/rgba() with comma or space separators, an
// optional "/ alpha", numbers or percentages. Channels clamp to [0,255].
func parseRGBFunc(s string) (color, bool) {
	open := strings.IndexByte(s, '(')
	end := strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return color{}, false
	}
	inner := strings.NewReplacer(",", " ", "/", " ").Replace(s[open+1 : end])
	fields := strings.Fields(inner)
	if len(fields) != 3 && len(fields) != 4 {
		return color{}, false
	}
	var ch [3]uint8
	for i := 0; i < 3; i++ {
		v, ok := parseChannel(fields[i])
		if !ok {
			return color{}, false
		}
		ch[i] = uint8(math.Round(math.Min(math.Max(v, 0), 255)))
	}
	c := color{R: ch[0], G: ch[1], B: ch[2], A: 1}
	if len(fields) == 4 {
		a, ok := parseAlpha(fields[3])
		if !ok {
			return color{}, false
		}
		c.A = a
	}
	return c, true
}

func parseChannel(f string) (float64, bool) {
	if strings.HasSuffix(f, "%") {
		v, ok := parseNumber(strings.TrimSuffix(f, "%"))
		return v * 255 / 100, ok
	}
	return parseNumber(f)
}

func parseAlpha(f string) (float64, bool) {
	if strings.HasSuffix(f, "%") {
		v, ok := parseNumber(strings.TrimSuffix(f, "%"))
		return clamp01(v / 100), ok
	}
	v, ok := parseNumber(f)
	return clamp01(v), ok
}

func parseNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func clamp01(v float64) float64 {
	return math.Min(math.Max(v, 0), 1)
}

// parseOpacity reads an opacity value (number or percentage), default 1.
func parseOpacity(s string) float64 {
	if s == "" {
		return 1
	}
	a, ok := parseAlpha(s)
	if !ok {
		return 1
	}
	return a
}

// parseLength reads a length in user units. Percentages and other units
// are rejected; "px" is accepted.
func parseLength(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "px")
	return parseNumber(strings.TrimSpace(s))
}

// parseViewBox reads four numbers with positive width and height.
func parseViewBox(s string) ([4]float64, bool) {
	nums := scanNumbers(s)
	if len(nums) != 4 || nums[2] <= 0 || nums[3] <= 0 {
		return [4]float64{}, false
	}
	return [4]float64{nums[0], nums[1], nums[2], nums[3]}, true
}

// scanNumbers reads a run of numbers separated by whitespace or commas,
// stopping at the first token that is not a number.
func scanNumbers(s string) []float64 {
	sc := &scanner{s: s}
	var out []float64
	for {
		v, ok := sc.num()
		if !ok {
			return out
		}
		out = append(out, v)
	}
}

// ---------------------------------------------------------------------------
// Transforms
// ---------------------------------------------------------------------------

// matrix is [a b c d e f]: x' = a*x + c*y + e, y' = b*x + d*y + f.
type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

// mul returns m x n: the transform that applies n first, then m.
func mul(m, n matrix) matrix {
	return matrix{
		m[0]*n[0] + m[2]*n[1],
		m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3],
		m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4],
		m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

func (m matrix) apply(p pt) pt {
	return pt{m[0]*p.X + m[2]*p.Y + m[4], m[1]*p.X + m[3]*p.Y + m[5]}
}

// parseTransform reads a transform list. An invalid list is ignored, as the
// SVG error rule says, and yields the identity.
func parseTransform(s string) matrix {
	sc := &scanner{s: s}
	m := identity
	for {
		sc.skipSpace()
		if sc.eof() {
			return m
		}
		start := sc.i
		for sc.i < len(sc.s) && isLetter(sc.s[sc.i]) {
			sc.i++
		}
		name := sc.s[start:sc.i]
		sc.skipSpace()
		if name == "" || sc.eof() || sc.s[sc.i] != '(' {
			return identity
		}
		close := strings.IndexByte(sc.s[sc.i:], ')')
		if close < 0 {
			return identity
		}
		args := scanNumbers(sc.s[sc.i+1 : sc.i+close])
		sc.i += close + 1
		step, ok := transformStep(name, args)
		if !ok {
			return identity
		}
		m = mul(m, step)
	}
}

func transformStep(name string, a []float64) (matrix, bool) {
	switch name {
	case "matrix":
		if len(a) == 6 {
			return matrix{a[0], a[1], a[2], a[3], a[4], a[5]}, true
		}
	case "translate":
		switch len(a) {
		case 1:
			return matrix{1, 0, 0, 1, a[0], 0}, true
		case 2:
			return matrix{1, 0, 0, 1, a[0], a[1]}, true
		}
	case "scale":
		switch len(a) {
		case 1:
			return matrix{a[0], 0, 0, a[0], 0, 0}, true
		case 2:
			return matrix{a[0], 0, 0, a[1], 0, 0}, true
		}
	case "rotate":
		switch len(a) {
		case 1:
			return rotation(a[0]), true
		case 3:
			return mul(matrix{1, 0, 0, 1, a[1], a[2]}, mul(rotation(a[0]), matrix{1, 0, 0, 1, -a[1], -a[2]})), true
		}
	case "skewX":
		if len(a) == 1 {
			return matrix{1, 0, math.Tan(a[0] * math.Pi / 180), 1, 0, 0}, true
		}
	case "skewY":
		if len(a) == 1 {
			return matrix{1, math.Tan(a[0] * math.Pi / 180), 0, 1, 0, 0}, true
		}
	}
	return identity, false
}

func rotation(deg float64) matrix {
	r := deg * math.Pi / 180
	s, c := math.Sin(r), math.Cos(r)
	return matrix{c, s, -s, c, 0, 0}
}

// ---------------------------------------------------------------------------
// Paint
// ---------------------------------------------------------------------------

// resolvePaint turns a fill or stroke value into a colour. url(#id) paints
// the gradient's average stop colour (dropped gradient geometry, see the
// package comment) or the fallback when the reference does not resolve.
func (d *Document) resolvePaint(value string, props map[string]string) (color, bool) {
	v := strings.TrimSpace(value)
	switch {
	case v == "" || v == "none":
		return color{}, false
	case strings.HasPrefix(v, "url("):
		end := strings.IndexByte(v, ')')
		if end < 0 {
			return color{}, false
		}
		ref := strings.Trim(strings.TrimSpace(v[4:end]), `'"`)
		if strings.HasPrefix(ref, "#") {
			if c, ok := d.gradientAverage(ref[1:]); ok {
				return c, true
			}
		}
		return d.resolvePaint(v[end+1:], props)
	case strings.EqualFold(v, "currentColor"):
		current, ok := parseColor(props["color"])
		if !ok {
			return color{R: 0, G: 0, B: 0, A: 1}, true
		}
		return current, true
	}
	return parseColor(v)
}

// gradientAverage is the flat approximation of a linear or radial gradient:
// the alpha-weighted mean colour of its stops, with the mean stop alpha.
// Stops are looked up through an href chain, like the SVG spec.
func (d *Document) gradientAverage(id string) (color, bool) {
	el := d.ids[id]
	if el == nil || (el.tag != "linearGradient" && el.tag != "radialGradient") {
		return color{}, false
	}
	var sumR, sumG, sumB, sumA float64
	n := 0
	for _, stop := range d.gradientStops(el) {
		sp := d.props(stop, nil)
		c, ok := parseColor(orDefault(sp["stop-color"], "black"))
		if !ok {
			continue
		}
		alpha := c.A * parseOpacity(sp["stop-opacity"])
		sumR += float64(c.R) * alpha
		sumG += float64(c.G) * alpha
		sumB += float64(c.B) * alpha
		sumA += alpha
		n++
	}
	if n == 0 || sumA == 0 {
		return color{}, false
	}
	return color{
		R: uint8(math.Round(sumR / sumA)),
		G: uint8(math.Round(sumG / sumA)),
		B: uint8(math.Round(sumB / sumA)),
		A: sumA / float64(n),
	}, true
}

// gradientStops returns the stops of a gradient, following href (or
// xlink:href) to a gradient that has them, up to a fixed hop count.
func (d *Document) gradientStops(el *element) []*element {
	cur := el
	for hops := 0; cur != nil && hops < 8; hops++ {
		var stops []*element
		for _, c := range cur.children {
			if c.tag == "stop" {
				stops = append(stops, c)
			}
		}
		if len(stops) > 0 {
			return stops
		}
		ref := strings.TrimPrefix(strings.TrimSpace(cur.attrs["href"]), "#")
		cur = d.ids[ref]
	}
	return nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// ---------------------------------------------------------------------------
// Number scanning
// ---------------------------------------------------------------------------

// scanner reads SVG numbers, which may run together ("-.5.5", "0 01 10")
// without separators, plus the single-digit arc flags.
type scanner struct {
	s string
	i int
}

func (sc *scanner) eof() bool { return sc.i >= len(sc.s) }

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func isLetter(c byte) bool { return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') }

func (sc *scanner) skipSpace() {
	for sc.i < len(sc.s) && (isSpace(sc.s[sc.i]) || sc.s[sc.i] == ',') {
		sc.i++
	}
}

// num reads the next number, skipping separators. On failure the position
// is left unchanged.
func (sc *scanner) num() (float64, bool) {
	sc.skipSpace()
	s := sc.s
	i := sc.i
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	for i < len(s) && isDigit(s[i]) {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && isDigit(s[i]) {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0, false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && isDigit(s[j]) {
			for j < len(s) && isDigit(s[j]) {
				j++
			}
			i = j
		}
	}
	v, ok := parseNumber(s[start:i])
	if !ok {
		return 0, false
	}
	sc.i = i
	return v, true
}

// flag reads one arc flag: a single '0' or '1' digit.
func (sc *scanner) flag() (bool, bool) {
	sc.skipSpace()
	if sc.i >= len(sc.s) {
		return false, false
	}
	switch sc.s[sc.i] {
	case '0':
		sc.i++
		return false, true
	case '1':
		sc.i++
		return true, true
	}
	return false, false
}
