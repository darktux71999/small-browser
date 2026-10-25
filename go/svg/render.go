package svg

import (
	"math"
	"sort"
)

// Render rasterizes the document into a straight-alpha RGBA buffer whose
// longer side is maxSide (clamped to [1, MaxRasterSide]); the shorter side
// follows the viewBox aspect ratio. The viewBox (or, without one, the
// intrinsic size) fits into the raster with xMidYMid meet.
func (d *Document) Render(maxSide int) (width, height int, rgba []byte) {
	maxSide = min(max(maxSide, 1), MaxRasterSide)
	vx, vy, vw, vh := 0.0, 0.0, 0.0, 0.0
	if d.hasViewBox {
		vx, vy, vw, vh = d.viewBox[0], d.viewBox[1], d.viewBox[2], d.viewBox[3]
	} else {
		vw, vh = d.IntrinsicSize()
	}
	width, height = rasterSize(vw, vh, maxSide)
	s := math.Min(float64(width)/vw, float64(height)/vh)
	base := matrix{s, 0, 0, s, (float64(width)-vw*s)/2 - vx*s, (float64(height)-vh*s)/2 - vy*s}

	cv := newCanvas(width, height)
	d.paintElement(d.root, identity, nil, 1, base, cv)
	return width, height, cv.straight()
}

// rasterSize keeps the viewBox aspect with the longer side at maxSide.
func rasterSize(vw, vh float64, maxSide int) (int, int) {
	if vw >= vh {
		return maxSide, max(1, int(math.Round(float64(maxSide)*vh/vw)))
	}
	return max(1, int(math.Round(float64(maxSide)*vw/vh))), maxSide
}

// paintElement paints el and its children. ctm maps el's parent user space
// to user space; base maps user space to raster pixels; groupOpacity is the
// product of the opacities of the ancestors (opacity is not inherited but
// multiplies down the tree, an approximation of group opacity).
func (d *Document) paintElement(el *element, ctm matrix, parent map[string]string, groupOpacity float64, base matrix, cv *canvas) {
	props := d.props(el, parent)
	if props["display"] == "none" {
		return
	}
	m := mul(ctm, parseTransform(el.attrs["transform"]))
	opacity := groupOpacity * parseOpacity(props["opacity"])
	switch el.tag {
	case "svg", "g":
		for _, child := range el.children {
			d.paintElement(child, m, props, opacity, base, cv)
		}
	case "path", "rect", "circle", "ellipse", "line", "polyline", "polygon":
		lines := shapeLines(el)
		if len(lines) == 0 {
			return
		}
		dev := mul(base, m)
		d.paintFill(lines, props, opacity, dev, cv)
		d.paintStroke(lines, props, opacity, dev, cv)
	}
	// Everything else (defs, style, gradients, text, use, clipPath, ...) is
	// not painted; see the package comment for the dropped set.
}

func (d *Document) paintFill(lines []polyline, props map[string]string, opacity float64, dev matrix, cv *canvas) {
	value, ok := props["fill"]
	if !ok {
		value = "black"
	}
	c, ok := d.resolvePaint(value, props)
	if !ok {
		return
	}
	alpha := c.A * opacity * parseOpacity(props["fill-opacity"])
	polys := make([][]pt, 0, len(lines))
	for _, l := range lines {
		polys = append(polys, transformPoints(l.pts, dev))
	}
	evenOdd := props["fill-rule"] == "evenodd"
	cv.composite(rasterize(polys, evenOdd, cv.w, cv.h), c, alpha)
}

func (d *Document) paintStroke(lines []polyline, props map[string]string, opacity float64, dev matrix, cv *canvas) {
	c, ok := d.resolvePaint(orDefault(props["stroke"], "none"), props)
	if !ok {
		return
	}
	width, ok := parseLength(orDefault(props["stroke-width"], "1"))
	if !ok || width <= 0 {
		return
	}
	round := props["stroke-linecap"] == "round"
	alpha := c.A * opacity * parseOpacity(props["stroke-opacity"])
	outline := strokeOutline(lines, width, round)
	polys := make([][]pt, 0, len(outline))
	for _, p := range outline {
		polys = append(polys, transformPoints(p, dev))
	}
	cv.composite(rasterize(polys, false, cv.w, cv.h), c, alpha)
}

func transformPoints(pts []pt, m matrix) []pt {
	out := make([]pt, len(pts))
	for i, p := range pts {
		out[i] = m.apply(p)
	}
	return out
}

// ---------------------------------------------------------------------------
// Stroking
// ---------------------------------------------------------------------------

// joinCos is the cosine of the smallest turn that gets a round join disc.
// Smaller turns (a flattened curve's chord-to-chord angle) are covered by the
// quads alone.
var joinCos = math.Cos(0.15)

// strokeOutline turns polylines into a set of positive-winding polygons whose
// nonzero union is the stroke: one quad per segment, a disc at each sharp
// join, and discs at the ends for round caps. Every polygon is oriented the
// same way, so overlaps never cancel.
func strokeOutline(lines []polyline, width float64, roundCaps bool) [][]pt {
	hw := width / 2
	var out [][]pt
	for _, l := range lines {
		pts := dedupe(l.pts)
		closed := l.closed
		if closed && len(pts) > 1 && pts[0] == pts[len(pts)-1] {
			pts = pts[:len(pts)-1]
		}
		n := len(pts)
		if n < 2 {
			continue
		}
		segments := n - 1
		if closed {
			segments = n
		}
		for i := 0; i < segments; i++ {
			a, b := pts[i], pts[(i+1)%n]
			dx, dy := b.X-a.X, b.Y-a.Y
			length := math.Hypot(dx, dy)
			if length == 0 {
				continue
			}
			nx, ny := -dy/length*hw, dx/length*hw
			out = append(out, orient([]pt{
				{a.X + nx, a.Y + ny}, {b.X + nx, b.Y + ny},
				{b.X - nx, b.Y - ny}, {a.X - nx, a.Y - ny},
			}))
		}
		for j := 0; j < n; j++ {
			interior := closed || (j > 0 && j < n-1)
			if interior {
				prev := pts[(j+n-1)%n]
				next := pts[(j+1)%n]
				if turnsSharply(prev, pts[j], next) {
					out = append(out, disc(pts[j], hw))
				}
			} else if roundCaps {
				out = append(out, disc(pts[j], hw))
			}
		}
	}
	return out
}

// dedupe drops consecutive coincident points, which carry no direction.
func dedupe(pts []pt) []pt {
	out := make([]pt, 0, len(pts))
	for _, p := range pts {
		if len(out) > 0 && out[len(out)-1] == p {
			continue
		}
		out = append(out, p)
	}
	return out
}

func turnsSharply(prev, at, next pt) bool {
	ax, ay := at.X-prev.X, at.Y-prev.Y
	bx, by := next.X-at.X, next.Y-at.Y
	norm := math.Hypot(ax, ay) * math.Hypot(bx, by)
	if norm == 0 {
		return false
	}
	return (ax*bx+ay*by)/norm < joinCos
}

// disc approximates a round join or cap with a 16-gon.
func disc(c pt, r float64) []pt {
	const sides = 16
	out := make([]pt, sides)
	for i := range out {
		t := 2 * math.Pi * float64(i) / sides
		out[i] = pt{c.X + r*math.Cos(t), c.Y + r*math.Sin(t)}
	}
	return orient(out)
}

// orient returns the polygon with positive signed area.
func orient(poly []pt) []pt {
	area := 0.0
	for i := range poly {
		j := (i + 1) % len(poly)
		area += poly[i].X*poly[j].Y - poly[j].X*poly[i].Y
	}
	if area >= 0 {
		return poly
	}
	out := make([]pt, len(poly))
	for i := range poly {
		out[len(poly)-1-i] = poly[i]
	}
	return out
}

// ---------------------------------------------------------------------------
// Coverage rasterization
// ---------------------------------------------------------------------------

// subSamples is the number of sub-scanlines per pixel row. Horizontal
// coverage is exact; the vertical samples give anti-aliased edges.
const subSamples = 4

// mask is the coverage of one shape over a sub-rectangle of the canvas.
type mask struct {
	x0, y0, w, h int
	cov          []float32
}

type edge struct {
	x0, y0, x1, y1 float64
	dir            int
}

type crossing struct {
	x   float64
	dir int
}

// rasterize accumulates the coverage of closed polygons (every polygon
// closes implicitly) clipped to the canvas, with the nonzero or even-odd
// rule.
func rasterize(polys [][]pt, evenOdd bool, canvasW, canvasH int) mask {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	var edges []edge
	for _, poly := range polys {
		n := len(poly)
		for i := 0; i < n && n > 1; i++ {
			a, b := poly[i], poly[(i+1)%n]
			minX, maxX = math.Min(minX, a.X), math.Max(maxX, a.X)
			minY, maxY = math.Min(minY, a.Y), math.Max(maxY, a.Y)
			if a.Y == b.Y {
				continue
			}
			dir := 1
			if b.Y < a.Y {
				dir = -1
			}
			edges = append(edges, edge{a.X, a.Y, b.X, b.Y, dir})
		}
	}
	x0 := int(math.Max(0, math.Floor(minX)))
	y0 := int(math.Max(0, math.Floor(minY)))
	x1 := int(math.Min(float64(canvasW), math.Ceil(maxX)))
	y1 := int(math.Min(float64(canvasH), math.Ceil(maxY)))
	if len(edges) == 0 || x1 <= x0 || y1 <= y0 {
		return mask{}
	}
	m := mask{x0: x0, y0: y0, w: x1 - x0, h: y1 - y0}
	m.cov = make([]float32, m.w*m.h)
	weight := float32(1.0 / subSamples)
	var xs []crossing
	for row := 0; row < m.h; row++ {
		y := float64(y0 + row)
		rowCov := m.cov[row*m.w : (row+1)*m.w]
		for s := 0; s < subSamples; s++ {
			sy := y + (float64(s)+0.5)/subSamples
			xs = xs[:0]
			for _, e := range edges {
				lo, hi := math.Min(e.y0, e.y1), math.Max(e.y0, e.y1)
				if sy < lo || sy >= hi {
					continue
				}
				x := e.x0 + (sy-e.y0)*(e.x1-e.x0)/(e.y1-e.y0)
				xs = append(xs, crossing{x, e.dir})
			}
			sort.Slice(xs, func(i, j int) bool { return xs[i].x < xs[j].x })
			wind := 0
			for k := 0; k+1 < len(xs); k++ {
				wind += xs[k].dir
				inside := wind != 0
				if evenOdd {
					inside = (k+1)%2 == 1
				}
				if inside {
					addSpan(rowCov, xs[k].x-float64(x0), xs[k+1].x-float64(x0), weight)
				}
			}
		}
	}
	return m
}

// addSpan adds weight times the horizontal coverage of [a,b) to a row.
func addSpan(row []float32, a, b float64, weight float32) {
	w := float64(len(row))
	a = math.Max(0, math.Min(a, w))
	b = math.Max(0, math.Min(b, w))
	if b <= a {
		return
	}
	first := int(math.Floor(a))
	last := int(math.Ceil(b)) - 1
	for i := first; i <= last && i < len(row); i++ {
		lo := math.Max(a, float64(i))
		hi := math.Min(b, float64(i+1))
		if hi > lo {
			row[i] += float32(hi-lo) * weight
		}
	}
}

// ---------------------------------------------------------------------------
// Canvas
// ---------------------------------------------------------------------------

// canvas is a premultiplied float RGBA buffer; paint order is source-over.
type canvas struct {
	w, h int
	pix  []float32
}

func newCanvas(w, h int) *canvas {
	return &canvas{w: w, h: h, pix: make([]float32, w*h*4)}
}

// composite source-over paints colour c with coverage*alpha through m.
func (cv *canvas) composite(m mask, c color, alpha float64) {
	if m.w == 0 || alpha <= 0 {
		return
	}
	r := float32(c.R) / 255
	g := float32(c.G) / 255
	b := float32(c.B) / 255
	for row := 0; row < m.h; row++ {
		for col := 0; col < m.w; col++ {
			cov := m.cov[row*m.w+col]
			if cov <= 0 {
				continue
			}
			a := cov * float32(alpha*c.A)
			if a <= 0 {
				continue
			}
			i := ((m.y0+row)*cv.w + m.x0 + col) * 4
			keep := 1 - a
			cv.pix[i] = r*a + cv.pix[i]*keep
			cv.pix[i+1] = g*a + cv.pix[i+1]*keep
			cv.pix[i+2] = b*a + cv.pix[i+2]*keep
			cv.pix[i+3] = a + cv.pix[i+3]*keep
		}
	}
}

// straight converts the premultiplied canvas to straight 8-bit RGBA.
func (cv *canvas) straight() []byte {
	out := make([]byte, cv.w*cv.h*4)
	for i := 0; i < len(out); i += 4 {
		a := cv.pix[i+3]
		if a <= 0 {
			continue
		}
		out[i] = to8(cv.pix[i] / a)
		out[i+1] = to8(cv.pix[i+1] / a)
		out[i+2] = to8(cv.pix[i+2] / a)
		out[i+3] = to8(a)
	}
	return out
}

func to8(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Round(float64(v) * 255))
}
