package shell_test

import (
	"testing"

	"browser-lite/shell"
)

// The window must not open at document height; both dimensions are fixed.
func TestViewportIsFixedPracticalSize(t *testing.T) {
	if shell.ViewportWidth != 1000 {
		t.Errorf("ViewportWidth = %d, want 1000", shell.ViewportWidth)
	}
	if shell.ViewportHeight != 760 {
		t.Errorf("ViewportHeight = %d, want 760", shell.ViewportHeight)
	}

	scroll := shell.New(3200.0, float64(shell.ViewportHeight))
	if scroll.Offset() != 0.0 {
		t.Errorf("fresh offset = %v, want 0 (starts at the top)", scroll.Offset())
	}
	if scroll.MaxOffset() != 2440.0 {
		t.Errorf("max offset = %v, want 2440 (document_height - viewport_height)", scroll.MaxOffset())
	}
}

func TestOffsetClampsBetweenZeroAndTheDocumentEnd(t *testing.T) {
	scroll := shell.New(3200.0, 760.0)
	if scroll.MaxOffset() != 2440.0 {
		t.Fatalf("max offset = %v, want 2440", scroll.MaxOffset())
	}

	scroll.SetOffset(10000.0)
	if scroll.Offset() != 2440.0 {
		t.Errorf("past the end: offset = %v, want 2440", scroll.Offset())
	}

	scroll.SetOffset(-50.0)
	if scroll.Offset() != 0.0 {
		t.Errorf("before the top: offset = %v, want 0", scroll.Offset())
	}

	scroll.SetOffset(1234.5)
	if scroll.Offset() != 1234.5 {
		t.Errorf("in range: offset = %v, want 1234.5", scroll.Offset())
	}
}

func TestShortDocumentNeverScrolls(t *testing.T) {
	for _, height := range []float64{500.0, 760.0} {
		scroll := shell.New(height, 760.0)
		if scroll.MaxOffset() != 0.0 {
			t.Errorf("doc %v: max offset = %v, want 0", height, scroll.MaxOffset())
		}
		for _, action := range []shell.ScrollAction{
			shell.LineDown, shell.PageDown, shell.Bottom,
		} {
			scroll.Apply(action)
			if scroll.Offset() != 0.0 {
				t.Errorf("doc %v: %v moved offset to %v, want 0", height, action, scroll.Offset())
			}
		}
	}
}

func TestLineAndPageStepsHaveThePinnedSizes(t *testing.T) {
	scroll := shell.New(3200.0, 760.0)

	scroll.Apply(shell.LineDown)
	scroll.Apply(shell.LineDown)
	scroll.Apply(shell.LineDown)
	if scroll.Offset() != 120.0 {
		t.Errorf("3x LineDown: offset = %v, want 120 (a line step is 40px)", scroll.Offset())
	}

	scroll.Apply(shell.LineUp)
	if scroll.Offset() != 80.0 {
		t.Errorf("LineUp: offset = %v, want 80 (mirrored step)", scroll.Offset())
	}

	scroll.Apply(shell.PageDown)
	if scroll.Offset() != 780.0 {
		t.Errorf("PageDown: offset = %v, want 780 (viewport 760 minus 60px overlap)", scroll.Offset())
	}
	scroll.Apply(shell.PageUp)
	if scroll.Offset() != 80.0 {
		t.Errorf("PageUp: offset = %v, want 80 (mirrored step)", scroll.Offset())
	}
}

func TestRepeatedPagingClampsAtBothEnds(t *testing.T) {
	scroll := shell.New(3200.0, 760.0)
	for i := 0; i < 100; i++ {
		scroll.Apply(shell.PageDown)
	}
	if scroll.Offset() != 2440.0 {
		t.Errorf("paging down: offset = %v, want 2440 (stops at the end)", scroll.Offset())
	}

	for i := 0; i < 100; i++ {
		scroll.Apply(shell.PageUp)
	}
	if scroll.Offset() != 0.0 {
		t.Errorf("paging up: offset = %v, want 0 (stops at the top)", scroll.Offset())
	}

	scroll.SetOffset(2400.0)
	scroll.Apply(shell.PageDown)
	if scroll.Offset() != 2440.0 {
		t.Errorf("last page: offset = %v, want 2440 (lands exactly on the end)", scroll.Offset())
	}
}

func TestHomeEndAndViGGJumpToTheEnds(t *testing.T) {
	scroll := shell.New(3200.0, 760.0)
	scroll.SetOffset(1000.0)

	scroll.Apply(shell.Top)
	if scroll.Offset() != 0.0 {
		t.Errorf("Top: offset = %v, want 0 (Home / g jump to the top)", scroll.Offset())
	}

	scroll.Apply(shell.Bottom)
	if scroll.Offset() != 2440.0 {
		t.Errorf("Bottom: offset = %v, want 2440 (End / G jump to the bottom)", scroll.Offset())
	}

	scroll.Apply(shell.Top)
	if scroll.Offset() != 0.0 {
		t.Errorf("Top again: offset = %v, want 0", scroll.Offset())
	}
}

func TestScrollKeysMapFromSDLKeycodes(t *testing.T) {
	lineUp := shell.ScrollAction(shell.LineUp)
	lineDown := shell.ScrollAction(shell.LineDown)
	pageUp := shell.ScrollAction(shell.PageUp)
	pageDown := shell.ScrollAction(shell.PageDown)
	top := shell.ScrollAction(shell.Top)
	bottom := shell.ScrollAction(shell.Bottom)
	cases := []struct {
		key       uint32
		modifiers uint16
		want      *shell.ScrollAction
	}{
		{shell.KeyUp, 0, &lineUp},
		{shell.KeyDown, 0, &lineDown},
		{shell.KeyPageUp, 0, &pageUp},
		{shell.KeyPageDown, 0, &pageDown},
		{shell.KeyHome, 0, &top},
		{shell.KeyEnd, 0, &bottom},
		{shell.KeyJ, 0, &lineDown},
		{shell.KeyK, 0, &lineUp},
		{shell.KeyJ, shell.ModShift, &lineDown},
		{shell.KeyK, shell.ModShift, &lineUp},
		{shell.KeyGLower, 0, &top},
		// Shift+g is `G` in vi: jump to the bottom, not to the top.
		{shell.KeyGLower, shell.ModShift, &bottom},
		{shell.KeyGUpper, 0, &bottom},
		// Keys with no vertical meaning stay unmapped.
		{shell.KeyQ, 0, nil},
		{0x68, 0, nil},       // 'h': no horizontal scrolling yet
		{0x6c, 0, nil},       // 'l'
		{0x40000029, 0, nil}, // Escape
	}
	for _, c := range cases {
		got, ok := shell.ActionFromKey(c.key, c.modifiers)
		if c.want == nil {
			if ok {
				t.Errorf("key %#x mod %#x: mapped to %v, want unmapped", c.key, c.modifiers, got)
			}
			continue
		}
		if !ok || got != *c.want {
			t.Errorf("key %#x mod %#x: got %v, want %v", c.key, c.modifiers, got, *c.want)
		}
	}
}

func TestMappedKeyActuallyMovesTheScrollState(t *testing.T) {
	// End to end: the keycode the shell parses drives the pure scroll model.
	scroll := shell.New(3200.0, 760.0)
	action, ok := shell.ActionFromKey(shell.KeyJ, 0)
	if !ok {
		t.Fatal("'j' must scroll")
	}
	scroll.Apply(action)
	if scroll.Offset() != 40.0 {
		t.Errorf("after 'j': offset = %v, want 40", scroll.Offset())
	}

	action, ok = shell.ActionFromKey(shell.KeyGLower, 0)
	if !ok {
		t.Fatal("'g' must scroll")
	}
	scroll.Apply(action)
	if scroll.Offset() != 0.0 {
		t.Errorf("after 'g': offset = %v, want 0", scroll.Offset())
	}
}

// The interactive path keeps stage-specific CLI diagnostics: a paint failure
// stays distinguishable from a window/SDL failure.
func TestPaintFailuresKeepThePaintDiagnostic(t *testing.T) {
	err := shell.PaintError("boom")
	if got := err.Diagnostic(); got != "browser-lite: paint failed: boom" {
		t.Errorf("diagnostic = %q, want %q", got, "browser-lite: paint failed: boom")
	}
}

func TestWindowFailuresKeepTheWindowDiagnostic(t *testing.T) {
	err := shell.WindowError("boom")
	if got := err.Diagnostic(); got != "browser-lite: window failed: boom" {
		t.Errorf("diagnostic = %q, want %q", got, "browser-lite: window failed: boom")
	}
}

// The Go key constants must equal the real SDL3 header values: letter
// keycodes are ASCII ('g' = 0x67), arrows/pages are
// SDL_SCANCODE_TO_KEYCODE(scancodes), SDL_KMOD_SHIFT is 0x3.
func TestKeyConstantsMatchSDLHeaders(t *testing.T) {
	if shell.KeyUp != 0x40000052 {
		t.Errorf("KeyUp = %#x, want 0x40000052", uint32(shell.KeyUp))
	}
	if shell.KeyDown != 0x40000051 {
		t.Errorf("KeyDown = %#x, want 0x40000051", uint32(shell.KeyDown))
	}
	if shell.KeyPageUp != 0x4000004B {
		t.Errorf("KeyPageUp = %#x, want 0x4000004B", uint32(shell.KeyPageUp))
	}
	if shell.KeyPageDown != 0x4000004E {
		t.Errorf("KeyPageDown = %#x, want 0x4000004E", uint32(shell.KeyPageDown))
	}
	if shell.KeyHome != 0x4000004A {
		t.Errorf("KeyHome = %#x, want 0x4000004A", uint32(shell.KeyHome))
	}
	if shell.KeyEnd != 0x4000004D {
		t.Errorf("KeyEnd = %#x, want 0x4000004D", uint32(shell.KeyEnd))
	}
	if shell.KeyJ != 0x6A || shell.KeyK != 0x6B {
		t.Errorf("KeyJ/KeyK = %#x/%#x, want 0x6a/0x6b", uint32(shell.KeyJ), uint32(shell.KeyK))
	}
	if shell.KeyGLower != 0x67 || shell.KeyGUpper != 0x47 {
		t.Errorf("KeyGLower/KeyGUpper = %#x/%#x, want 0x67/0x47",
			uint32(shell.KeyGLower), uint32(shell.KeyGUpper))
	}
	if shell.ModShift != 0x0003 {
		t.Errorf("ModShift = %#x, want 0x3", uint16(shell.ModShift))
	}
}
