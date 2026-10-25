package paint_test

import (
	"os"
	"path/filepath"
	"testing"

	"browser-lite/css"
	"browser-lite/img"
	"browser-lite/layout"
	"browser-lite/paint"
)

func red() *css.Color { return &css.Color{R: 1, G: 0, B: 0, A: 1} }

func px(f paint.Frame, x, y int) [4]byte {
	i := (y*f.Width + x) * 4
	return [4]byte{f.Pixels[i], f.Pixels[i+1], f.Pixels[i+2], f.Pixels[i+3]}
}

func TestBackgroundFillPixel(t *testing.T) {
	l := &layout.Layout{
		Width:  10,
		Height: 10,
		Boxes: []layout.LayoutBox{
			{X: 2, Y: 2, W: 6, H: 6, Background: red()},
		},
	}
	f, err := paint.Render(l, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if f.Width != 10 || f.Height != 10 {
		t.Fatalf("frame is %dx%d, want 10x10", f.Width, f.Height)
	}
	if got := px(f, 0, 0); got != [4]byte{255, 255, 255, 255} {
		t.Errorf("outside box: got %v, want white", got)
	}
	if got := px(f, 2, 2); got != [4]byte{255, 0, 0, 255} {
		t.Errorf("box corner: got %v, want red", got)
	}
	if got := px(f, 7, 7); got != [4]byte{255, 0, 0, 255} {
		t.Errorf("box far corner: got %v, want red", got)
	}
	if got := px(f, 8, 8); got != [4]byte{255, 255, 255, 255} {
		t.Errorf("past box: got %v, want white", got)
	}
}

func TestBackgroundImageTilesWithAlpha(t *testing.T) {
	tile := &img.Image{Width: 2, Height: 2, RGBA: []byte{
		255, 0, 0, 255, // opaque red
		0, 0, 255, 128, // half blue
		0, 255, 0, 0, // transparent
		0, 0, 0, 255, // opaque black
	}}
	l := &layout.Layout{
		Width:  4,
		Height: 4,
		Boxes: []layout.LayoutBox{
			{X: 0, Y: 0, W: 4, H: 4, BackgroundImage: tile,
				Background: &css.Color{R: 1, G: 1, B: 1, A: 1}},
		},
	}
	f, err := paint.Render(l, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Natural-size tiling: pixel (2,0) repeats tile (0,0).
	if a, b := px(f, 0, 0), px(f, 2, 0); a != b {
		t.Errorf("tile does not repeat: (0,0)=%v (2,0)=%v", a, b)
	}
	if got := px(f, 0, 0); got != [4]byte{255, 0, 0, 255} {
		t.Errorf("opaque tile pixel: got %v, want red", got)
	}
	// Half blue over white: (127,127,255,255).
	if got := px(f, 1, 0); got != [4]byte{127, 127, 255, 255} {
		t.Errorf("alpha tile pixel: got %v, want (127,127,255,255)", got)
	}
	// Transparent tile pixel shows the background through.
	if got := px(f, 0, 1); got != [4]byte{255, 255, 255, 255} {
		t.Errorf("transparent tile pixel: got %v, want white", got)
	}
	if got := px(f, 3, 3); got != [4]byte{0, 0, 0, 255} {
		t.Errorf("tiled black pixel: got %v, want black", got)
	}
}

func TestRoundedCornersClipBackground(t *testing.T) {
	l := &layout.Layout{
		Width:  10,
		Height: 10,
		Boxes: []layout.LayoutBox{
			{X: 0, Y: 0, W: 10, H: 10, Background: red(),
				BorderRadius: [4]css.Radius{css.RadiusPx(5)}},
		},
	}
	f, err := paint.Render(l, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := px(f, 0, 0); got != [4]byte{255, 255, 255, 255} {
		t.Errorf("rounded corner: got %v, want white", got)
	}
	if got := px(f, 5, 5); got != [4]byte{255, 0, 0, 255} {
		t.Errorf("box center: got %v, want red", got)
	}
}

func TestInsetRingPaintsInside(t *testing.T) {
	l := &layout.Layout{
		Width:  6,
		Height: 6,
		Boxes: []layout.LayoutBox{
			{X: 0, Y: 0, W: 6, H: 6,
				BorderWidth: [4]float64{2, 0, 0, 0},
				BorderStyle: [4]css.BorderKind{css.BorderSolid},
				BorderColor: [4]css.Color{{R: 1, A: 1}}},
		},
	}
	f, err := paint.Render(l, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := px(f, 1, 0); got != [4]byte{255, 0, 0, 255} {
		t.Errorf("top ring: got %v, want red", got)
	}
	if got := px(f, 1, 3); got != [4]byte{255, 255, 255, 255} {
		t.Errorf("below ring: got %v, want white", got)
	}
}

func TestDrawImageScalesPixels(t *testing.T) {
	dot := &img.Image{Width: 1, Height: 1, RGBA: []byte{255, 0, 0, 255}}
	l := &layout.Layout{
		Width:  2,
		Height: 2,
		Boxes: []layout.LayoutBox{
			{X: 0, Y: 0, W: 2, H: 2, Image: dot},
		},
	}
	f, err := paint.Render(l, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if got := px(f, x, y); got != [4]byte{255, 0, 0, 255} {
				t.Errorf("pixel (%d,%d): got %v, want red", x, y, got)
			}
		}
	}
}

func TestScrollClipsToViewport(t *testing.T) {
	l := &layout.Layout{
		Width:  1000,
		Height: 2000,
		Boxes: []layout.LayoutBox{
			{X: 8, Y: 100, W: 984, H: 50, Background: red()},
		},
	}
	offset := 100.0
	scrolled, err := paint.Render(l, &offset)
	if err != nil {
		t.Fatalf("Render scrolled: %v", err)
	}
	if scrolled.Width != 1000 || scrolled.Height != 760 {
		t.Fatalf("viewport is %dx%d, want 1000x760", scrolled.Width, scrolled.Height)
	}
	if got := px(scrolled, 50, 0); got != [4]byte{255, 0, 0, 255} {
		t.Errorf("document row 100 at viewport top: got %v, want red", got)
	}
	top := 0.0
	unscrolled, err := paint.Render(l, &top)
	if err != nil {
		t.Fatalf("Render top: %v", err)
	}
	if got := px(unscrolled, 50, 100); got != [4]byte{255, 0, 0, 255} {
		t.Errorf("document row 100: got %v, want red", got)
	}
	past := 1000000.0
	empty, err := paint.Render(l, &past)
	if err != nil {
		t.Fatalf("Render past end: %v", err)
	}
	if got := px(empty, 50, 20); got != [4]byte{255, 255, 255, 255} {
		t.Errorf("past the document: got %v, want white", got)
	}
}

func TestRenderPaintsTextRun(t *testing.T) {
	// Smoke for the painter's text path (S1 rewire): showRun rebuilds the
	// Pango layout through the shared absolute line-height helper, so the
	// run must still paint ink on the white page.
	l := &layout.Layout{
		Width:  200,
		Height: 40,
		Runs: []layout.TextRun{{
			X: 8, Y: 8, Width: 184, Text: "hello lite",
			Style: css.Style{
				FontFamily: "Sans",
				FontSize:   16,
				LineHeight: 1.2,
				Color:      css.Color{R: 0, G: 0, B: 0, A: 1},
			},
		}},
	}
	f, err := paint.Render(l, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	ink := 0
	for i := 0; i+3 < len(f.Pixels); i += 4 {
		if f.Pixels[i] != 255 || f.Pixels[i+1] != 255 || f.Pixels[i+2] != 255 || f.Pixels[i+3] != 255 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatalf("text run painted no ink")
	}
}

func TestRenderPNGWritesFile(t *testing.T) {
	l := &layout.Layout{
		Width:  4,
		Height: 3,
		Boxes: []layout.LayoutBox{
			{X: 0, Y: 0, W: 4, H: 3, Background: red()},
		},
	}
	path := filepath.Join(t.TempDir(), "tiny.png")
	if err := paint.RenderPNG(l, path); err != nil {
		t.Fatalf("RenderPNG: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read png: %v", err)
	}
	if string(raw[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Errorf("missing PNG magic: %q", raw[:8])
	}
}

// pxNear reports whether a frame pixel matches want within tol per channel.
func pxNear(t *testing.T, f paint.Frame, x, y int, want [4]byte, tol int32, label string) {
	t.Helper()
	got := px(f, x, y)
	for c := 0; c < 4; c++ {
		d := int32(got[c]) - int32(want[c])
		if d < 0 {
			d = -d
		}
		if d > tol {
			t.Errorf("%s: (%d,%d) = %v, want %v +/-%d", label, x, y, got, want, tol)
			return
		}
	}
}

// Background-image placement (meta1 S10 paint fidelity): background-repeat
// and background-position place the image inside the box instead of tiling
// it at natural size from the top-left corner. Percentages position the
// image's p% point at the same point of the free space (CSS Backgrounds 3
// §4.4 — where libcss computes the right/bottom keywords as 100%), and a
// plain length offsets the image from the box origin (CSS 2.1 §8.14.1).
func TestBackgroundImageNoRepeatRightBottom(t *testing.T) {
	tile := &img.Image{Width: 2, Height: 2, RGBA: []byte{
		255, 0, 0, 255, // TL red
		0, 0, 255, 255, // TR blue
		0, 255, 0, 255, // BL green
		0, 0, 0, 255, // BR black
	}}
	white := &css.Color{R: 1, G: 1, B: 1, A: 1}
	pct := func(v float64) css.BackgroundPosition {
		return css.BackgroundPosition{Value: v, Percent: true}
	}
	l := &layout.Layout{
		Width:  6,
		Height: 6,
		Boxes: []layout.LayoutBox{{
			X: 0, Y: 0, W: 6, H: 6,
			Background:          white,
			BackgroundImage:     tile,
			BackgroundRepeat:    css.BackgroundRepeatNoRepeat,
			BackgroundPositionX: pct(100),
			BackgroundPositionY: pct(100),
		}},
	}
	f, err := paint.Render(l, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// no-repeat right bottom: the 2x2 image sits at the box's bottom-right.
	if got := px(f, 4, 4); got != [4]byte{255, 0, 0, 255} {
		t.Errorf("image top-left: got %v, want red", got)
	}
	if got := px(f, 5, 5); got != [4]byte{0, 0, 0, 255} {
		t.Errorf("image bottom-right: got %v, want black", got)
	}
	// Nothing repeats: the rest of the box shows the background colour.
	for _, p := range [][2]int{{0, 0}, {3, 3}, {4, 3}, {0, 5}, {5, 0}} {
		if got := px(f, p[0], p[1]); got != [4]byte{255, 255, 255, 255} {
			t.Errorf("no-repeat leaves white at (%d,%d): got %v", p[0], p[1], got)
		}
	}
}

func TestBackgroundImageRepeatXAndPxOffset(t *testing.T) {
	tile := &img.Image{Width: 2, Height: 2, RGBA: []byte{
		255, 0, 0, 255,
		0, 0, 255, 255,
		0, 255, 0, 255,
		0, 0, 0, 255,
	}}
	white := &css.Color{R: 1, G: 1, B: 1, A: 1}
	l := &layout.Layout{
		Width:  6,
		Height: 14,
		Boxes: []layout.LayoutBox{
			{
				X: 0, Y: 0, W: 6, H: 6,
				Background:       white,
				BackgroundImage:  tile,
				BackgroundRepeat: css.BackgroundRepeatRepeatX,
			},
			{
				X: 0, Y: 8, W: 6, H: 6,
				Background:          white,
				BackgroundImage:     tile,
				BackgroundRepeat:    css.BackgroundRepeatNoRepeat,
				BackgroundPositionX: css.BackgroundPosition{Value: 1},
				BackgroundPositionY: css.BackgroundPosition{Value: 2},
			},
		},
	}
	f, err := paint.Render(l, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// repeat-x tiles along the row at the box top but never below it.
	if got := px(f, 4, 0); got != [4]byte{255, 0, 0, 255} {
		t.Errorf("repeat-x row 2: got %v, want red", got)
	}
	if got := px(f, 0, 2); got != [4]byte{255, 255, 255, 255} {
		t.Errorf("repeat-x below the row: got %v, want white", got)
	}
	// A px position offsets the image from the box's top-left origin.
	if got := px(f, 1, 10); got != [4]byte{255, 0, 0, 255} {
		t.Errorf("px offset image origin: got %v, want red", got)
	}
	if got := px(f, 0, 8); got != [4]byte{255, 255, 255, 255} {
		t.Errorf("px offset leaves the corner white: got %v", got)
	}
}

// A two-stop vertical linear-gradient paints an interpolated ramp over the
// box's background colour (CSS Images 3 §3.4 subset, alpha included): the
// codeberg.org about-cards' #144B49B2 → #144B49 ramp over white.
func TestBackgroundLinearGradientRamp(t *testing.T) {
	teal := css.Color{R: 20.0 / 255, G: 75.0 / 255, B: 73.0 / 255}
	l := &layout.Layout{
		Width:  4,
		Height: 10,
		Boxes: []layout.LayoutBox{{
			X: 0, Y: 0, W: 4, H: 8,
			Background: &css.Color{R: 1, G: 1, B: 1, A: 1},
			BackgroundGradient: &css.LinearGradient{
				From: css.Color{R: teal.R, G: teal.G, B: teal.B, A: 178.0 / 255},
				To:   css.Color{R: teal.R, G: teal.G, B: teal.B, A: 1},
			},
		}},
	}
	f, err := paint.Render(l, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Pixel centres sample t = (y+0.5)/H; alpha ramps 178/255 → 255/255.
	// Composite over white: c = C·a + 255·(1−a).
	pxNear(t, f, 1, 0, [4]byte{87, 126, 125, 255}, 4, "ramp top")
	pxNear(t, f, 1, 4, [4]byte{51, 99, 97, 255}, 4, "ramp middle")
	pxNear(t, f, 1, 7, [4]byte{24, 78, 76, 255}, 4, "ramp bottom")
	// The channel ramp is monotonic: teal strengthens downwards.
	top, mid, bot := px(f, 1, 0), px(f, 1, 4), px(f, 1, 7)
	if !(top[0] > mid[0] && mid[0] > bot[0]) {
		t.Errorf("red channel not monotonic: top %d mid %d bottom %d", top[0], mid[0], bot[0])
	}
	// Outside the box stays the white page.
	if got := px(f, 0, 9); got[0] != 255 {
		t.Errorf("outside gradient: got %v, want white", got)
	}
}
