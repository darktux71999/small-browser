package shell

import (
	"testing"

	"browser-lite/paint"
)

// TestToARGBIntoPinsExactConversion pins the byte-exact ARGB8888 (little
// endian) transformation that toARGB performed per frame, now written into a
// caller-provided buffer. Existing scroll/upload behavior must not change.
func TestToARGBIntoPinsExactConversion(t *testing.T) {
	f := paint.Frame{
		Pixels: []byte{
			255, 0, 0, 255, // opaque red   -> mem [0,0,255,255]
			0, 0, 0, 0, // transparent  -> mem [0,0,0,0]
			255, 255, 255, 128, // half white -> mem [128,128,128,128]
		},
		Width:  3,
		Height: 1,
	}
	buf := make([]byte, f.Width*f.Height*4)
	toARGBInto(f, buf)
	want := []byte{0, 0, 255, 255, 0, 0, 0, 0, 128, 128, 128, 128}
	for i := range want {
		if buf[i] != want[i] {
			t.Errorf("byte %d = 0x%02x, want 0x%02x", i, buf[i], want[i])
		}
	}
}

// TestToARGBIntoReuseIsClean verifies the reusable-buffer contract: writing
// a second frame into the same buffer fully overwrites it, so no stale pixels
// from a previous scroll repaint can leak into the next texture upload.
func TestToARGBIntoReuseIsClean(t *testing.T) {
	first := paint.Frame{
		Pixels: []byte{255, 0, 0, 255},
		Width:  1, Height: 1,
	}
	second := paint.Frame{
		Pixels: []byte{0, 0, 255, 255}, // opaque blue -> mem [255,0,0,255]
		Width:  1, Height: 1,
	}
	buf := make([]byte, 4)
	toARGBInto(first, buf)
	toARGBInto(second, buf)
	want := []byte{255, 0, 0, 255}
	for i := range want {
		if buf[i] != want[i] {
			t.Errorf("after reuse: byte %d = 0x%02x, want 0x%02x (stale pixel?)", i, buf[i], want[i])
		}
	}
}
