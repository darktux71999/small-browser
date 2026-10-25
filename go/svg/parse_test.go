package svg

import (
	"math"
	"testing"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func hasPointNear(lines []polyline, x, y, tol float64) bool {
	for _, l := range lines {
		for _, p := range l.pts {
			if near(p.X, x, tol) && near(p.Y, y, tol) {
				return true
			}
		}
	}
	return false
}

func TestParseIntrinsicSize(t *testing.T) {
	tests := []struct {
		name string
		src  string
		w, h float64
	}{
		{"width and height attributes", `<svg width="16" height="24" viewBox="0 0 4 6"/>`, 16, 24},
		{"px suffix", `<svg width="16px" height="8px"/>`, 16, 8},
		{"viewBox only", `<svg viewBox="0 0 800 300"/>`, 800, 300},
		{"width scales the viewBox height", `<svg width="400" viewBox="0 0 800 300"/>`, 400, 150},
		{"nothing given falls back to 300x150", `<svg/>`, 300, 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			w, h := doc.IntrinsicSize()
			if !near(w, tt.w, 1e-9) || !near(h, tt.h, 1e-9) {
				t.Fatalf("size = %vx%v, want %vx%v", w, h, tt.w, tt.h)
			}
		})
	}
}

func TestParseRejectsNonSVG(t *testing.T) {
	for _, src := range []string{"", "<html></html>", "<svg><g></svg>"} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Fatalf("Parse(%q) must fail", src)
		}
	}
}

func TestParsePathLinesAndClose(t *testing.T) {
	lines := parsePath("M10 20 L30 20 L30 40 Z")
	if len(lines) != 1 || !lines[0].closed || len(lines[0].pts) != 3 {
		t.Fatalf("lines = %+v, want one closed triangle", lines)
	}
	if lines[0].pts[2] != (pt{30, 40}) {
		t.Fatalf("third point = %+v, want (30,40)", lines[0].pts[2])
	}
}

func TestParsePathRelativeAndImplicitCommands(t *testing.T) {
	tests := []struct {
		name string
		d    string
		want []pt
	}{
		{"relative moveto and lineto", "m10 10 l5 0", []pt{{10, 10}, {15, 10}}},
		{"implicit lineto after moveto", "M0 0 10 0 10 10", []pt{{0, 0}, {10, 0}, {10, 10}}},
		{"H and V", "M0 0 H10 V10 h-10", []pt{{0, 0}, {10, 0}, {10, 10}, {0, 10}}},
		{"compact numbers", "M.5-.5.5.5", []pt{{0.5, -0.5}, {0.5, 0.5}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := parsePath(tt.d)
			if len(lines) != 1 || len(lines[0].pts) != len(tt.want) {
				t.Fatalf("lines = %+v, want one polyline of %d points", lines, len(tt.want))
			}
			for i, p := range tt.want {
				got := lines[0].pts[i]
				if !near(got.X, p.X, 1e-9) || !near(got.Y, p.Y, 1e-9) {
					t.Fatalf("point %d = %+v, want %+v", i, got, p)
				}
			}
		})
	}
}

func TestParsePathCubicQuadraticAndSmooth(t *testing.T) {
	// Cubic from (0,0) to (10,0) with control points (0,10),(10,10): the
	// midpoint at t=0.5 is (5,7.5).
	if lines := parsePath("M0 0 C0 10 10 10 10 0"); !hasPointNear(lines, 5, 7.5, 0.01) {
		t.Fatalf("cubic midpoint missing: %+v", lines)
	}
	// Quadratic from (0,0) to (10,0) with control (5,10): midpoint (5,5).
	if lines := parsePath("M0 0 Q5 10 10 0"); !hasPointNear(lines, 5, 5, 0.01) {
		t.Fatalf("quadratic midpoint missing: %+v", lines)
	}
	// S reflects the previous second control point (10,10) about (10,0),
	// giving (10,-10); the second curve's midpoint is then (15,-7.5).
	lines := parsePath("M0 0 C0 10 10 10 10 0 S 20 -10 20 0")
	if !hasPointNear(lines, 15, -7.5, 0.01) {
		t.Fatalf("smooth cubic midpoint missing: %+v", lines)
	}
	// T reflects the previous quadratic control (5,10) about (10,0) to
	// (15,-10); the second curve's midpoint is (15,-5).
	lines = parsePath("M0 0 Q5 10 10 0 T 20 0")
	if !hasPointNear(lines, 15, -5, 0.01) {
		t.Fatalf("smooth quadratic midpoint missing: %+v", lines)
	}
}

func TestParsePathArcs(t *testing.T) {
	// Half circle centred on (10,0), radius 10, sweep-flag 1: the arc passes
	// through (10,-10), the point at angle 3pi/2 in screen coordinates.
	lines := parsePath("M0 0 A10 10 0 0 1 20 0")
	if !hasPointNear(lines, 10, -10, 0.01) {
		t.Fatalf("arc midpoint missing: %+v", lines)
	}
	last := lines[0].pts[len(lines[0].pts)-1]
	if last != (pt{20, 0}) {
		t.Fatalf("arc end = %+v, want exactly (20,0)", last)
	}
	// Packed flags ("0 0120 0": rotation 0, large-arc 0, sweep 1, end 20 0)
	// parse the same way as spaced flags: the same half circle.
	packed := parsePath("M0 0a10 10 0 0120 0")
	if !hasPointNear(packed, 10, -10, 0.01) || packed[0].pts[len(packed[0].pts)-1] != (pt{20, 0}) {
		t.Fatalf("packed arc flags misparsed: %+v", packed)
	}
}

func TestParsePathStopsAtInvalidData(t *testing.T) {
	lines := parsePath("M0 0 L10 0 X 5 5")
	if len(lines) != 1 || len(lines[0].pts) != 2 {
		t.Fatalf("lines = %+v, want the valid prefix only", lines)
	}
}

func TestCascadeOrder(t *testing.T) {
	// A class rule beats a type rule; the style attribute beats both;
	// presentation attributes lose to any author rule.
	src := `<svg viewBox="0 0 10 10"><style>circle{fill:red}.c{fill:blue}</style>` +
		`<circle class="c" fill="green" style="fill:#000" cx="5" cy="5" r="5"/>` +
		`<circle class="c" fill="green" cx="5" cy="5" r="5"/>` +
		`<circle fill="green" cx="5" cy="5" r="5"/></svg>`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var fills []string
	for _, el := range doc.root.children {
		if el.tag == "style" {
			continue
		}
		fills = append(fills, doc.props(el, nil)["fill"])
	}
	want := []string{"#000", "blue", "red"}
	if len(fills) != len(want) {
		t.Fatalf("fills = %v, want %v", fills, want)
	}
	for i := range want {
		if fills[i] != want[i] {
			t.Fatalf("fill %d = %q, want %q (all %v)", i, fills[i], want[i], fills)
		}
	}
}

func TestInheritanceAndOpacityNotInherited(t *testing.T) {
	doc, err := Parse([]byte(`<svg><g fill="#00f" opacity="0.5"><rect style="fill-opacity:0.25"/></g></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	g := doc.root.children[0]
	rect := g.children[0]
	props := doc.props(rect, doc.props(g, nil))
	if props["fill"] != "#00f" || props["fill-opacity"] != "0.25" {
		t.Fatalf("rect props = %v, want inherited fill and own fill-opacity", props)
	}
	if _, ok := props["opacity"]; ok {
		t.Fatalf("opacity must not inherit: %v", props)
	}
}
