package svg

import (
	"math"
	"testing"
)

// render parses src and rasterizes it at maxSide, failing the test on any
// error so each caller can assert on pixels alone.
func render(t *testing.T, src string, maxSide int) (int, int, []byte) {
	t.Helper()
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	w, h, rgba := doc.Render(maxSide)
	if len(rgba) != w*h*4 {
		t.Fatalf("raster %dx%d has %d bytes", w, h, len(rgba))
	}
	return w, h, rgba
}

func pixelAt(w int, rgba []byte, x, y int) [4]uint8 {
	i := (y*w + x) * 4
	return [4]uint8{rgba[i], rgba[i+1], rgba[i+2], rgba[i+3]}
}

func within(got, want uint8, tol int) bool {
	return math.Abs(float64(got)-float64(want)) <= float64(tol)
}

func TestRenderCircleAndTriangle(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">` +
		`<circle cx="30" cy="30" r="20" fill="#ff0000"/>` +
		`<polygon points="60,10 90,10 75,40" fill="#0000ff"/></svg>`
	w, h, px := render(t, src, 100)
	if w != 100 || h != 100 {
		t.Fatalf("raster = %dx%d, want 100x100", w, h)
	}
	if c := pixelAt(w, px, 30, 30); c[3] != 255 || !within(c[0], 255, 2) || c[2] != 0 {
		t.Fatalf("circle centre = %v, want opaque red", c)
	}
	if c := pixelAt(w, px, 75, 20); c[3] != 255 || c[2] < 250 || c[0] != 0 {
		t.Fatalf("triangle interior = %v, want opaque blue", c)
	}
	if c := pixelAt(w, px, 95, 95); c[3] != 0 {
		t.Fatalf("empty corner = %v, want transparent", c)
	}
}

func TestRenderEdgeIsAntiAliased(t *testing.T) {
	// The rect's left edge at x=10.5 splits pixel column 10 in half: its
	// coverage is 0.5, not the 0 or 255 a hard rasterizer would give.
	src := `<svg viewBox="0 0 100 100"><rect x="10.5" y="0" width="40" height="100" fill="#000"/></svg>`
	w, _, px := render(t, src, 100)
	if a := pixelAt(w, px, 10, 50)[3]; !within(a, 128, 4) {
		t.Fatalf("half-covered pixel alpha = %d, want about 128", a)
	}
	if a := pixelAt(w, px, 11, 50)[3]; a != 255 {
		t.Fatalf("interior pixel alpha = %d, want 255", a)
	}
}

func TestRenderViewBoxScalesIntoRaster(t *testing.T) {
	// A 50x50 rect in a 100-unit viewBox painted at a 200px raster covers
	// the top-left quarter: user (25,25) is pixel (50,50).
	src := `<svg width="200" height="200" viewBox="0 0 100 100"><rect width="50" height="50" fill="#000"/></svg>`
	w, h, px := render(t, src, 200)
	if w != 200 || h != 200 {
		t.Fatalf("raster = %dx%d, want 200x200", w, h)
	}
	if pixelAt(w, px, 50, 50)[3] != 255 {
		t.Fatal("top-left quarter must be inked")
	}
	if pixelAt(w, px, 150, 150)[3] != 0 {
		t.Fatal("bottom-right quarter must stay empty")
	}
}

func TestRenderRasterKeepsViewBoxAspect(t *testing.T) {
	w, h, _ := render(t, `<svg viewBox="0 0 800 300"><rect width="1" height="1"/></svg>`, 512)
	if w != 512 || h != 192 {
		t.Fatalf("raster = %dx%d, want 512x192 (viewBox aspect)", w, h)
	}
}

func TestRenderGroupTransform(t *testing.T) {
	src := `<svg viewBox="0 0 100 100"><g transform="translate(50 0)">` +
		`<rect width="50" height="100" fill="#000"/></g></svg>`
	w, h, px := render(t, src, 100)
	if pixelAt(w, px, 75, 50)[3] != 255 {
		t.Fatal("translated rect must cover x 50..100")
	}
	if pixelAt(w, px, 25, 50)[3] != 0 {
		t.Fatal("untranslated area must stay empty")
	}
	_ = h
}

func TestRenderStrokeWidth(t *testing.T) {
	src := `<svg viewBox="0 0 100 100"><line x1="10" y1="50" x2="90" y2="50" stroke="#000" stroke-width="10"/></svg>`
	w, _, px := render(t, src, 100)
	if pixelAt(w, px, 50, 50)[3] != 255 {
		t.Fatal("stroke centre must be inked")
	}
	if pixelAt(w, px, 50, 60)[3] != 0 {
		t.Fatal("pixel 10 units below the stroke must be empty")
	}
}

func TestRenderFillNoneAndOpacity(t *testing.T) {
	src := `<svg viewBox="0 0 100 100">` +
		`<rect x="0" y="0" width="100" height="100" fill="none" stroke="#f00" stroke-width="4"/>` +
		`<rect x="0" y="0" width="100" height="100" fill="#000" fill-opacity="0.5"/></svg>`
	w, _, px := render(t, src, 100)
	if c := pixelAt(w, px, 50, 50); !within(c[3], 128, 2) {
		t.Fatalf("fill-opacity 0.5 alpha = %d, want about 128", c[3])
	}
}

func TestRenderCurrentColor(t *testing.T) {
	src := `<svg viewBox="0 0 10 10" style="color:#ff0000"><rect width="10" height="10" fill="currentColor"/></svg>`
	w, _, px := render(t, src, 10)
	if c := pixelAt(w, px, 5, 5); c[0] != 255 || c[1] != 0 || c[3] != 255 {
		t.Fatalf("currentColor pixel = %v, want opaque red", c)
	}
}

func TestRenderGradientFlatAverage(t *testing.T) {
	// The gradient is approximated by the average of its stop colours:
	// (255,0,0) and (0,0,255) average to (128,0,128).
	src := `<svg viewBox="0 0 10 10"><defs><linearGradient id="g">` +
		`<stop offset="0" stop-color="#ff0000"/><stop offset="1" stop-color="#0000ff"/>` +
		`</linearGradient></defs><rect width="10" height="10" fill="url(#g)"/></svg>`
	w, _, px := render(t, src, 10)
	c := pixelAt(w, px, 5, 5)
	if !within(c[0], 128, 2) || c[1] != 0 || !within(c[2], 128, 2) || c[3] != 255 {
		t.Fatalf("gradient pixel = %v, want about (128,0,128,255)", c)
	}
}

func TestRenderMissingPaintServerDegrades(t *testing.T) {
	// A dangling url() without fallback paints nothing; with a fallback it
	// paints the fallback colour. Nothing crashes.
	src := `<svg viewBox="0 0 10 10">` +
		`<rect width="5" height="10" fill="url(#missing)"/>` +
		`<rect x="5" width="5" height="10" fill="url(#missing) #00ff00"/></svg>`
	w, _, px := render(t, src, 10)
	if pixelAt(w, px, 2, 5)[3] != 0 {
		t.Fatal("dangling url() without fallback must paint nothing")
	}
	if c := pixelAt(w, px, 7, 5); c[1] != 255 || c[3] != 255 {
		t.Fatalf("fallback pixel = %v, want opaque green", c)
	}
}

func TestRenderFillRuleEvenOdd(t *testing.T) {
	outer := "M0 0H10V10H0Z"
	inner := "M3 3H7V7H3Z"
	nonzero := `<svg viewBox="0 0 10 10"><path d="` + outer + inner + `" fill="#000"/></svg>`
	evenodd := `<svg viewBox="0 0 10 10"><path d="` + outer + inner + `" fill="#000" fill-rule="evenodd"/></svg>`
	// Both squares wind the same way, so nonzero fills the hole; evenodd
	// punches it out.
	w, _, px := render(t, nonzero, 10)
	if pixelAt(w, px, 5, 5)[3] != 255 {
		t.Fatal("nonzero must fill the inner square")
	}
	w, _, px = render(t, evenodd, 10)
	if pixelAt(w, px, 5, 5)[3] != 0 {
		t.Fatal("evenodd must leave the inner square empty")
	}
}
