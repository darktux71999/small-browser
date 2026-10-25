package svg

import "testing"

func TestParseColor(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want color
		ok   bool
	}{
		{"hex 3", "#abc", color{170, 187, 204, 1}, true},
		{"hex 6", "#2185d0", color{0x21, 0x85, 0xd0, 1}, true},
		{"hex 8 with alpha", "#aabbcc80", color{0xaa, 0xbb, 0xcc, 128.0 / 255}, true},
		{"rgb function", "rgb(255, 0, 10)", color{255, 0, 10, 1}, true},
		{"rgba function", "rgba(0,0,0,0.5)", color{0, 0, 0, 0.5}, true},
		{"named color", "red", color{255, 0, 0, 1}, true},
		{"named color is case-insensitive", "Blue", color{0, 0, 255, 1}, true},
		{"transparent", "transparent", color{0, 0, 0, 0}, true},
		{"none is not a color", "none", color{}, false},
		{"unknown name", "notacolor", color{}, false},
		{"malformed hex", "#12", color{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseColor(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if got.R != tt.want.R || got.G != tt.want.G || got.B != tt.want.B || !near(got.A, tt.want.A, 0.01) {
				t.Fatalf("color = %+v, want %+v", got, tt.want)
			}
		})
	}
}
