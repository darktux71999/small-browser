package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// boxLine matches one geometry line of --dump output:
// "  [   x    y    w    h] tag".
var boxLine = regexp.MustCompile(`^\s*\[\s*(-?[\d.]+)\s+(-?[\d.]+)\s+([\d.]+)\s+([\d.]+)\]\s+(\S+)`)

// dumpBox returns the origin and size of the first geometry line of the
// given tag with a positive size, and fails when there is none.
func dumpBox(t *testing.T, dump, tag string) (x, y, w, h float64) {
	t.Helper()
	for _, line := range bytes.Split([]byte(dump), []byte("\n")) {
		m := boxLine.FindSubmatch(line)
		if m == nil || string(m[5]) != tag {
			continue
		}
		bw, _ := strconv.ParseFloat(string(m[3]), 64)
		bh, _ := strconv.ParseFloat(string(m[4]), 64)
		if bw <= 0 || bh <= 0 {
			continue
		}
		x, _ = strconv.ParseFloat(string(m[1]), 64)
		y, _ = strconv.ParseFloat(string(m[2]), 64)
		return x, y, bw, bh
	}
	t.Fatalf("no painted %s box in dump:\n%s", tag, dump)
	return 0, 0, 0, 0
}

// pngPixel renders fixture to a PNG and returns the 8-bit RGB at (x, y).
func pngPixel(t *testing.T, fixtureName string, x, y int) [3]uint8 {
	t.Helper()
	out := filepath.Join(t.TempDir(), "frame.png")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--png", out, fixture(fixtureName)}, &stdout, &stderr); code != 0 {
		t.Fatalf("--png exit %d, stderr %q", code, stderr.String())
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read png: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	r, g, b, _ := img.At(x, y).RGBA()
	return [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

// dumpOf runs --dump over fixture and returns its stdout.
func dumpOf(t *testing.T, fixtureName string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--dump", fixture(fixtureName)}, &stdout, &stderr); code != 0 {
		t.Fatalf("--dump exit %d, stderr %q", code, stderr.String())
	}
	return stdout.String()
}

// RED for gap 1, end to end: an inline <svg> in a text run is a 16x16
// box on the line and paints its own pixels at that box.
func TestInlineSVGPaintsAtItsLineBox(t *testing.T) {
	dump := dumpOf(t, "inline-svg.html")
	x, y, w, h := dumpBox(t, dump, "svg")
	if w != 16 || h != 16 {
		t.Fatalf("inline svg box = %vx%v, want 16x16", w, h)
	}
	got := pngPixel(t, "inline-svg.html", int(x)+8, int(y)+8)
	if want := [3]uint8{0xcc, 0x22, 0x22}; got != want {
		t.Fatalf("inline svg centre pixel = %v, want %v (the rect fill)", got, want)
	}
}

// RED for gap 2, end to end: an <img> inside an inline-block gets a box and
// paints the decoded image there.
func TestImageInInlineBlockPaints(t *testing.T) {
	dump := dumpOf(t, "probe-svg-img.html")
	x, y, w, h := dumpBox(t, dump, "img")
	// probe-svg-image.svg paints a #aa5522 rectangle at x 120..220, y 20..100
	// of its 240x120 viewBox; sample its centre in proportion to the box.
	got := pngPixel(t, "probe-svg-img.html", int(x+w*0.7), int(y+h*0.5))
	if want := [3]uint8{0xaa, 0x55, 0x22}; got != want {
		t.Fatalf("image-in-inline-block pixel = %v, want %v", got, want)
	}
}
