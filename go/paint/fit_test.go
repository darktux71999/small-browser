package paint

import (
	"math"
	"testing"
)

// fitRect pins the xMidYMid meet placement the SVG layer relies on: the
// content keeps its aspect ratio and is centred in the box.
func TestFitRectMeetsAndCentres(t *testing.T) {
	tests := []struct {
		name                       string
		x, y, w, h                 float64
		iw, ih                     uint32
		wantX, wantY, wantW, wantH float64
	}{
		// The codeberg forgejo wordmark (800x300 viewBox) in a 30x30 box:
		// 30 wide, 11.25 tall, centred vertically.
		{"wide content in a square box", 10, 20, 30, 30, 800, 300, 10, 29.375, 30, 11.25},
		{"tall content in a wide box", 0, 0, 40, 10, 100, 200, 17.5, 0, 5, 10},
		{"matching aspect fills the box", 5, 5, 20, 10, 200, 100, 5, 5, 20, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, w, h := fitRect(tt.x, tt.y, tt.w, tt.h, tt.iw, tt.ih)
			if !near(x, tt.wantX) || !near(y, tt.wantY) || !near(w, tt.wantW) || !near(h, tt.wantH) {
				t.Fatalf("fit = (%v,%v,%v,%v), want (%v,%v,%v,%v)",
					x, y, w, h, tt.wantX, tt.wantY, tt.wantW, tt.wantH)
			}
		})
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
