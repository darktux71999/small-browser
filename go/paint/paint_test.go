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
