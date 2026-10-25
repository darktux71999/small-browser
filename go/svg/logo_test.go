package svg

import (
	"os"
	"testing"
)

// The real codeberg.org logos (fetched from https://codeberg.org/assets/img/svg/)
// are copies under testdata/, pinned so the smoke test never needs the network.

func loadTestdata(t *testing.T, name string) *Document {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(body)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return doc
}

// inkStats counts opaque pixels and returns the mean straight colour of them.
func inkStats(rgba []byte) (count int, r, g, b float64) {
	for i := 0; i+3 < len(rgba); i += 4 {
		if rgba[i+3] < 200 {
			continue
		}
		count++
		r += float64(rgba[i])
		g += float64(rgba[i+1])
		b += float64(rgba[i+2])
	}
	if count > 0 {
		r, g, b = r/float64(count), g/float64(count), b/float64(count)
	}
	return count, r, g, b
}

func TestCodebergLogoRendersBlueInk(t *testing.T) {
	doc := loadTestdata(t, "cb-logo.svg")
	if w, h := doc.IntrinsicSize(); w != 16 || h != 16 {
		t.Fatalf("intrinsic = %vx%v, want 16x16", w, h)
	}
	_, _, rgba := doc.Render(256)
	count, r, g, b := inkStats(rgba)
	if count < 500 {
		t.Fatalf("logo ink = %d opaque pixels, want a visible mark", count)
	}
	// The mark is flat #2185d0 (33,133,208); the faint gradient shadow only
	// adds low-alpha pixels, which the opacity filter excludes.
	if !within(uint8(r), 33, 12) || !within(uint8(g), 133, 12) || !within(uint8(b), 208, 12) {
		t.Fatalf("dominant colour = (%.0f,%.0f,%.0f), want about #2185d0", r, g, b)
	}
}

func TestForgejoWordmarkRendersOrangeStrokesAndFills(t *testing.T) {
	doc := loadTestdata(t, "forgejo-wordmark.svg")
	w, h, rgba := doc.Render(512)
	if w != 512 || h != 192 {
		t.Fatalf("raster = %dx%d, want 512x192 (800x300 viewBox)", w, h)
	}
	count, _, _, _ := inkStats(rgba)
	// The wordmark is a stroked flame plus the deep-orange "Forgejo" text:
	// hundreds of thousands of pixels are not expected, a few thousand are.
	if count < 5000 {
		t.Fatalf("wordmark ink = %d opaque pixels, want a visible wordmark", count)
	}
	// The text glyph colour is #f50 (255,85,0): the pixel at the text
	// letter "F" stem is inside the deep-orange fill; sample the wordmark's
	// dominant orange instead of one pixel.
	orange := 0
	for i := 0; i+3 < len(rgba); i += 4 {
		if rgba[i+3] >= 250 && rgba[i] >= 250 && within(rgba[i+1], 85, 20) && rgba[i+2] <= 20 {
			orange++
		}
	}
	if orange < 2000 {
		t.Fatalf("#f50 pixels = %d, want the deep-orange wordmark fill", orange)
	}
}
