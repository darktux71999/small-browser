package svg

import (
	"math"
	"strings"
)

// Each curve is flattened into this many chords. The logos are small (the
// largest raster is 2048 px) and a 24-chord cubic deviates by well under a
// pixel at that size.
const curveSteps = 24

// ellipseSteps is the polygon resolution of circles and ellipses.
const ellipseSteps = 64

// pt is a point in user units.
type pt struct{ X, Y float64 }

// polyline is one flattened subpath. closed subpaths also close the fill
// and the stroke's join at the start point.
type polyline struct {
	pts    []pt
	closed bool
}

const pathCommands = "MmLlHhVvCcSsQqTtAaZz"

// parsePath flattens path data to polylines in user space. Parsing stops at
// the first malformed command and keeps everything before it, as browsers
// do. A path must start with a moveto; anything else paints nothing.
func parsePath(d string) []polyline {
	sc := &scanner{s: d}
	var out []polyline
	open := false
	var cx, cy, sx, sy float64 // current point and subpath start
	var lcx, lcy float64       // last control point, for S/T reflection
	var prev byte              // previous command, uppercase
	var cmd byte

	ensure := func() {
		if !open {
			out = append(out, polyline{pts: []pt{{cx, cy}}})
			open = true
		}
	}
	add := func(p pt) {
		ensure()
		last := &out[len(out)-1]
		last.pts = append(last.pts, p)
	}

	for {
		sc.skipSpace()
		if sc.eof() {
			break
		}
		c := sc.s[sc.i]
		if isLetter(c) {
			if strings.IndexByte(pathCommands, c) < 0 {
				break
			}
			cmd = c
			sc.i++
		} else if cmd == 0 || cmd == 'Z' || cmd == 'z' {
			break
		}
		if cmd == 0 || (len(out) == 0 && cmd != 'M' && cmd != 'm') {
			break
		}
		rel := cmd >= 'a'
		up := cmd
		if rel {
			up = cmd - ('a' - 'A')
		}
		ok := true
		switch up {
		case 'M':
			var p pt
			p, ok = sc.point(rel, cx, cy)
			if ok {
				out = append(out, polyline{pts: []pt{p}})
				open = true
				cx, cy, sx, sy = p.X, p.Y, p.X, p.Y
				if rel {
					cmd = 'l'
				} else {
					cmd = 'L'
				}
			}
		case 'L':
			var p pt
			p, ok = sc.point(rel, cx, cy)
			if ok {
				add(p)
				cx, cy = p.X, p.Y
			}
		case 'H':
			var x float64
			x, ok = sc.num()
			if ok {
				if rel {
					x += cx
				}
				add(pt{x, cy})
				cx = x
			}
		case 'V':
			var y float64
			y, ok = sc.num()
			if ok {
				if rel {
					y += cy
				}
				add(pt{cx, y})
				cy = y
			}
		case 'C':
			var c1, c2, p pt
			c1, c2, p, ok = sc.threePoints(rel, cx, cy)
			if ok {
				cubicTo(add, pt{cx, cy}, c1, c2, p)
				lcx, lcy = c2.X, c2.Y
				cx, cy = p.X, p.Y
			}
		case 'S':
			var c2, p pt
			c2, p, ok = sc.twoPoints(rel, cx, cy)
			if ok {
				c1 := pt{cx, cy}
				if prev == 'C' || prev == 'S' {
					c1 = pt{2*cx - lcx, 2*cy - lcy}
				}
				cubicTo(add, pt{cx, cy}, c1, c2, p)
				lcx, lcy = c2.X, c2.Y
				cx, cy = p.X, p.Y
			}
		case 'Q':
			var q, p pt
			q, p, ok = sc.twoPoints(rel, cx, cy)
			if ok {
				quadTo(add, pt{cx, cy}, q, p)
				lcx, lcy = q.X, q.Y
				cx, cy = p.X, p.Y
			}
		case 'T':
			var p pt
			p, ok = sc.point(rel, cx, cy)
			if ok {
				q := pt{cx, cy}
				if prev == 'Q' || prev == 'T' {
					q = pt{2*cx - lcx, 2*cy - lcy}
				}
				quadTo(add, pt{cx, cy}, q, p)
				lcx, lcy = q.X, q.Y
				cx, cy = p.X, p.Y
			}
		case 'A':
			ok = arcCommand(sc, rel, &cx, &cy, add)
		case 'Z':
			if open {
				out[len(out)-1].closed = true
				open = false
			}
			cx, cy = sx, sy
		}
		if !ok {
			break
		}
		prev = up
	}
	return out
}

// point reads an absolute or relative coordinate pair.
func (sc *scanner) point(rel bool, cx, cy float64) (pt, bool) {
	x, ok := sc.num()
	if !ok {
		return pt{}, false
	}
	y, ok := sc.num()
	if !ok {
		return pt{}, false
	}
	if rel {
		x, y = x+cx, y+cy
	}
	return pt{x, y}, true
}

func (sc *scanner) twoPoints(rel bool, cx, cy float64) (pt, pt, bool) {
	a, ok := sc.point(rel, cx, cy)
	if !ok {
		return pt{}, pt{}, false
	}
	b, ok := sc.point(rel, cx, cy)
	return a, b, ok
}

func (sc *scanner) threePoints(rel bool, cx, cy float64) (pt, pt, pt, bool) {
	a, b, ok := sc.twoPoints(rel, cx, cy)
	if !ok {
		return pt{}, pt{}, pt{}, false
	}
	c, ok := sc.point(rel, cx, cy)
	return a, b, c, ok
}

// cubicTo appends the chords of a cubic from p0 to p3, excluding p0.
func cubicTo(add func(pt), p0, p1, p2, p3 pt) {
	for k := 1; k <= curveSteps; k++ {
		t := float64(k) / curveSteps
		mt := 1 - t
		a, b, c, d := mt*mt*mt, 3*mt*mt*t, 3*mt*t*t, t*t*t
		add(pt{
			a*p0.X + b*p1.X + c*p2.X + d*p3.X,
			a*p0.Y + b*p1.Y + c*p2.Y + d*p3.Y,
		})
	}
}

// quadTo raises a quadratic to a cubic exactly and flattens it.
func quadTo(add func(pt), p0, q, p2 pt) {
	c1 := pt{p0.X + 2.0/3*(q.X-p0.X), p0.Y + 2.0/3*(q.Y-p0.Y)}
	c2 := pt{p2.X + 2.0/3*(q.X-p2.X), p2.Y + 2.0/3*(q.Y-p2.Y)}
	cubicTo(add, p0, c1, c2, p2)
}

// arcCommand reads one elliptical arc and appends its chords. Endpoint
// parameters convert to centre parameters as in SVG 1.1 F.6.5.
func arcCommand(sc *scanner, rel bool, cx, cy *float64, add func(pt)) bool {
	rx, ok := sc.num()
	if !ok {
		return false
	}
	ry, ok := sc.num()
	if !ok {
		return false
	}
	rot, ok := sc.num()
	if !ok {
		return false
	}
	largeArc, ok := sc.flag()
	if !ok {
		return false
	}
	sweep, ok := sc.flag()
	if !ok {
		return false
	}
	end, ok := sc.point(rel, *cx, *cy)
	if !ok {
		return false
	}
	for _, p := range arcPoints(pt{*cx, *cy}, rx, ry, rot, largeArc, sweep, end) {
		add(p)
	}
	*cx, *cy = end.X, end.Y
	return true
}

// arcPoints flattens an arc from p1 to p2, excluding p1 and including p2.
// Coincident endpoints draw nothing; a zero radius degrades to a line.
func arcPoints(p1 pt, rx, ry, rotDeg float64, largeArc, sweep bool, p2 pt) []pt {
	if p1 == p2 {
		return nil
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []pt{p2}
	}
	phi := rotDeg * math.Pi / 180
	cosPhi, sinPhi := math.Cos(phi), math.Sin(phi)

	dx2, dy2 := (p1.X-p2.X)/2, (p1.Y-p2.Y)/2
	x1p := cosPhi*dx2 + sinPhi*dy2
	y1p := -sinPhi*dx2 + cosPhi*dy2

	lambda := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		rx, ry = rx*s, ry*s
	}
	rx2, ry2 := rx*rx, ry*ry
	num := rx2*ry2 - rx2*y1p*y1p - ry2*x1p*x1p
	den := rx2*y1p*y1p + ry2*x1p*x1p
	coef := 0.0
	if den != 0 {
		coef = math.Sqrt(math.Max(0, num/den))
	}
	if largeArc == sweep {
		coef = -coef
	}
	cxp := coef * rx * y1p / ry
	cyp := -coef * ry * x1p / rx
	centreX := cosPhi*cxp - sinPhi*cyp + (p1.X+p2.X)/2
	centreY := sinPhi*cxp + cosPhi*cyp + (p1.Y+p2.Y)/2

	ux, uy := (x1p-cxp)/rx, (y1p-cyp)/ry
	vx, vy := (-x1p-cxp)/rx, (-y1p-cyp)/ry
	theta1 := vectorAngle(1, 0, ux, uy)
	dtheta := math.Mod(vectorAngle(ux, uy, vx, vy), 2*math.Pi)
	if !sweep && dtheta > 0 {
		dtheta -= 2 * math.Pi
	} else if sweep && dtheta < 0 {
		dtheta += 2 * math.Pi
	}

	steps := int(math.Ceil(math.Abs(dtheta) / (math.Pi / 32)))
	if steps < 2 {
		steps = 2
	}
	out := make([]pt, 0, steps)
	for k := 1; k <= steps; k++ {
		if k == steps {
			out = append(out, p2)
			break
		}
		t := theta1 + dtheta*float64(k)/float64(steps)
		x := rx * math.Cos(t)
		y := ry * math.Sin(t)
		out = append(out, pt{
			centreX + cosPhi*x - sinPhi*y,
			centreY + sinPhi*x + cosPhi*y,
		})
	}
	return out
}

// vectorAngle is the signed angle from (ux,uy) to (vx,vy), in radians.
func vectorAngle(ux, uy, vx, vy float64) float64 {
	dot := ux*vx + uy*vy
	norm := math.Hypot(ux, uy) * math.Hypot(vx, vy)
	if norm == 0 {
		return 0
	}
	angle := math.Acos(math.Max(-1, math.Min(1, dot/norm)))
	if ux*vy-uy*vx < 0 {
		angle = -angle
	}
	return angle
}

// shapeLines returns the user-space geometry of a basic shape, or nil for an
// element that is not a shape or has no area or length to draw.
func shapeLines(el *element) []polyline {
	attr := func(name string) float64 {
		v, _ := parseLength(el.attrs[name])
		return v
	}
	switch el.tag {
	case "path":
		return parsePath(el.attrs["d"])
	case "rect":
		x, y, w, h := attr("x"), attr("y"), attr("width"), attr("height")
		if w <= 0 || h <= 0 {
			return nil
		}
		return []polyline{{pts: []pt{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}, closed: true}}
	case "circle":
		r := attr("r")
		return ellipse(attr("cx"), attr("cy"), r, r)
	case "ellipse":
		return ellipse(attr("cx"), attr("cy"), attr("rx"), attr("ry"))
	case "line":
		return []polyline{{pts: []pt{{attr("x1"), attr("y1")}, {attr("x2"), attr("y2")}}}}
	case "polyline", "polygon":
		nums := scanNumbers(el.attrs["points"])
		if len(nums)%2 == 1 {
			nums = nums[:len(nums)-1]
		}
		if len(nums) < 4 {
			return nil
		}
		pts := make([]pt, 0, len(nums)/2)
		for i := 0; i < len(nums); i += 2 {
			pts = append(pts, pt{nums[i], nums[i+1]})
		}
		return []polyline{{pts: pts, closed: el.tag == "polygon"}}
	}
	return nil
}

func ellipse(cx, cy, rx, ry float64) []polyline {
	if rx <= 0 || ry <= 0 {
		return nil
	}
	pts := make([]pt, ellipseSteps)
	for i := range pts {
		t := 2 * math.Pi * float64(i) / ellipseSteps
		pts[i] = pt{cx + rx*math.Cos(t), cy + ry*math.Sin(t)}
	}
	return []polyline{{pts: pts, closed: true}}
}
