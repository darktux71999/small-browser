// Package shell opens the fixed-viewport SDL3 window on a laid-out document
// and scrolls it from the keyboard, mirroring the Rust oracle (src/shell.rs).
//
// The window opens at ViewportWidth x ViewportHeight no matter how tall the
// document is: the layout still measures the whole page, the painter clips to
// the viewport, and the keyboard scrolls a ScrollState.
//
// All SDL3 access goes through the real C headers via cgo: event fields are
// read by C helpers compiled against <SDL3/SDL.h>, never by hand-rolled
// struct offsets (the oracle's hand-written FFI had an ABI bug class exactly
// there; cgo ends it by construction).
package shell

/*
#cgo pkg-config: sdl3
#include <SDL3/SDL.h>
#include <stdlib.h>
#include <string.h>

// Event field readers compiled against the real SDL3 headers: the Go side
// never assumes a struct layout or offset.
static Uint32 shell_event_type(SDL_Event *e) { return (Uint32)e->type; }
static Uint32 shell_event_key(SDL_Event *e) { return (Uint32)e->key.key; }
static Uint16 shell_event_mod(SDL_Event *e) { return (Uint16)e->key.mod; }

// SDL3 returns C bool; normalize to int so the Go side stays comparison-safe.
static int shell_init(void) { return SDL_Init(SDL_INIT_VIDEO) ? 1 : 0; }
static void shell_quit(void) { SDL_Quit(); }
static void shell_delay(Uint32 ms) { SDL_Delay(ms); }
static int shell_poll(SDL_Event *e) { return SDL_PollEvent(e) ? 1 : 0; }
static int shell_update_texture(SDL_Texture *t, void *pixels, int pitch) {
	return SDL_UpdateTexture(t, NULL, pixels, pitch) ? 1 : 0;
}
static int shell_render_clear(SDL_Renderer *r) { return SDL_RenderClear(r) ? 1 : 0; }
static int shell_render_texture(SDL_Renderer *r, SDL_Texture *t) {
	return SDL_RenderTexture(r, t, NULL, NULL) ? 1 : 0;
}
static void shell_present(SDL_Renderer *r) { SDL_RenderPresent(r); }
static const char *shell_error(void) { return SDL_GetError(); }
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"unsafe"

	"browser-lite/layout"
	"browser-lite/paint"
)

// Fixed interactive viewport: the window opens at this practical size instead
// of the document height, and only this many pixels are ever painted.
const (
	ViewportWidth  = 1000
	ViewportHeight = 760
)

// LineStep scrolls arrow keys and vi j/k by this many CSS pixels (roughly
// one text line).
const LineStep = 40.0

// PageOverlap keeps this many pixels of the previous page visible: a page
// step is the viewport height minus PageOverlap.
const PageOverlap = 60.0

// SDL3 event types and keycodes, values verified against the installed
// SDL 3.4.18 headers. Letter keycodes are the ASCII value of the lower-case
// letter whatever the modifiers are, so vi G arrives either as shifted `g`
// or (platform dependent) as the upper-case code; both are handled in
// ActionFromKey.
const (
	EventQuit    = 0x100
	EventKeyDown = 0x300

	KeyUp       = 0x40000052
	KeyDown     = 0x40000051
	KeyPageUp   = 0x4000004B
	KeyPageDown = 0x4000004E
	KeyHome     = 0x4000004A
	KeyEnd      = 0x4000004D
	KeyJ        = 0x6A
	KeyK        = 0x6B
	KeyGLower   = 0x67
	KeyGUpper   = 0x47
	KeyQ        = 0x71

	// ModShift is SDL_KMOD_SHIFT (any Shift key down).
	ModShift = 0x0003
)

// ScrollAction is a keyboard command that moves the vertical scroll offset.
type ScrollAction int

const (
	// LineUp is Arrow up, k.
	LineUp ScrollAction = iota
	// LineDown is Arrow down, j.
	LineDown
	// PageUp is PageUp.
	PageUp
	// PageDown is PageDown.
	PageDown
	// Top is Home, g: jump to the top of the document.
	Top
	// Bottom is End, G: jump to the bottom of the document.
	Bottom
)

// ScrollState is the vertical scroll state, clamped to
// [0, max(0, documentHeight - viewportHeight)].
//
// This is a pure unit: the shell feeds it SDL keys, the painter only draws
// the offset it is told to draw.
type ScrollState struct {
	offset         float64
	maxOffset      float64
	viewportHeight float64
}

// New starts a document at the top; the range shrinks to 0 when the document
// fits inside the viewport.
func New(documentHeight, viewportHeight float64) *ScrollState {
	maxOffset := documentHeight - viewportHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	return &ScrollState{viewportHeight: viewportHeight, maxOffset: maxOffset}
}

// Offset is the current scroll offset in CSS pixels from the top.
func (s *ScrollState) Offset() float64 { return s.offset }

// MaxOffset is the largest legal offset:
// max(0, documentHeight - viewportHeight).
func (s *ScrollState) MaxOffset() float64 { return s.maxOffset }

// SetOffset sets an absolute offset, clamped into the scrollable range.
func (s *ScrollState) SetOffset(offset float64) {
	s.offset = clamp(offset, 0, s.maxOffset)
}

// Apply runs one keyboard command and returns the resulting offset.
func (s *ScrollState) Apply(action ScrollAction) float64 {
	switch action {
	case LineUp:
		s.SetOffset(s.offset - LineStep)
	case LineDown:
		s.SetOffset(s.offset + LineStep)
	case PageUp:
		s.SetOffset(s.offset - s.pageStep())
	case PageDown:
		s.SetOffset(s.offset + s.pageStep())
	case Top:
		s.SetOffset(0)
	case Bottom:
		s.SetOffset(s.maxOffset)
	}
	return s.offset
}

// pageStep is a page step: the viewport height minus PageOverlap of context,
// never below one pixel.
func (s *ScrollState) pageStep() float64 {
	step := s.viewportHeight - PageOverlap
	if step < 1 {
		step = 1
	}
	return step
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ActionFromKey maps an SDL3 virtual key code and its modifiers to a scroll
// command; ok is false when the key does not scroll and the shell ignores
// it. It covers the arrow keys, PageUp/PageDown, Home/End and the vi
// navigation j/k/g/G.
func ActionFromKey(key uint32, modifiers uint16) (action ScrollAction, ok bool) {
	switch key {
	case KeyUp:
		return LineUp, true
	case KeyDown:
		return LineDown, true
	case KeyPageUp:
		return PageUp, true
	case KeyPageDown:
		return PageDown, true
	case KeyHome:
		return Top, true
	case KeyEnd:
		return Bottom, true
	case KeyK:
		return LineUp, true
	case KeyJ:
		return LineDown, true
	case KeyGUpper:
		return Bottom, true
	case KeyGLower:
		if modifiers&ModShift != 0 {
			return Bottom, true
		}
		return Top, true
	}
	return 0, false
}

// Kind classifies a failure from the interactive window path, so the CLI
// keeps its stage-specific diagnostics.
type Kind int

const (
	// KindPaint marks a painting failure (Cairo render path).
	KindPaint Kind = iota
	// KindWindow marks a window, renderer, texture, or event plumbing
	// failure (SDL path).
	KindWindow
)

// RunError is a classified failure from the interactive window path.
type RunError struct {
	kind Kind
	msg  string
}

// PaintError classifies a viewport painting failure.
func PaintError(msg string) *RunError { return &RunError{kind: KindPaint, msg: msg} }

// WindowError classifies an SDL/window plumbing failure.
func WindowError(msg string) *RunError { return &RunError{kind: KindWindow, msg: msg} }

// Diagnostic is the CLI line for this failure, keeping the historical
// `browser-lite: <stage> failed: {err}` messages.
func (e *RunError) Diagnostic() string {
	if e.kind == KindPaint {
		return "browser-lite: paint failed: " + e.msg
	}
	return "browser-lite: window failed: " + e.msg
}

func (e *RunError) Error() string { return e.Diagnostic() }

func windowError(what string) *RunError {
	ptr := C.shell_error()
	detail := "unknown SDL error"
	if ptr != nil {
		detail = C.GoString(ptr)
	}
	return WindowError(fmt.Sprintf("%s: %s", what, detail))
}

// Run opens the fixed-viewport window on l and runs the event loop until the
// window closes (or maxFrames frames have been shown, for headless smoke
// tests). maxFrames nil runs until the window closes.
//
// The whole document is laid out, but only one viewportful is painted: the
// initial frame is the document top, and the scroll keys (arrows,
// PageUp/PageDown, Home/End, vi j/k/g/G) re-paint the window further down
// through ScrollState. --png/--dump never reach this path.
func Run(l *layout.Layout, title string, maxFrames *uint32) error {
	// SDL3 requires every video/event call on ONE OS thread. Lock this
	// goroutine to its thread first: without this, Go migrates the loop
	// across threads at scheduling points (every blocking cgo call such as
	// SDL_Delay is one), which in the wild lost keyboard events (Xlib is
	// not thread-safe) and issued GL calls with no current context —
	// surfacing as window-move smearing and Mesa "glBindTexture/
	// glPixelStorei/glTexSubImage2D called without a rendering context".
	runtime.LockOSThread()
	scroll := New(l.Height, float64(ViewportHeight))
	offset := scroll.Offset()
	frame, err := paint.Render(l, &offset)
	if err != nil {
		return PaintError(err.Error())
	}
	if frame.Width != ViewportWidth || frame.Height != ViewportHeight {
		return PaintError(fmt.Sprintf("viewport frame is %dx%d, expected %dx%d",
			frame.Width, frame.Height, ViewportWidth, ViewportHeight))
	}

	if C.shell_init() == 0 {
		return windowError("SDL_Init")
	}

	ctitle := C.CString(title)
	defer C.free(unsafe.Pointer(ctitle))
	window := C.SDL_CreateWindow(ctitle, C.int(ViewportWidth), C.int(ViewportHeight), 0)
	if window == nil {
		C.shell_quit()
		return windowError("SDL_CreateWindow")
	}

	renderer := C.SDL_CreateRenderer(window, nil)
	if renderer == nil {
		err := windowError("SDL_CreateRenderer")
		C.SDL_DestroyWindow(window)
		C.shell_quit()
		return err
	}

	texture := C.SDL_CreateTexture(renderer,
		C.SDL_PIXELFORMAT_ARGB8888, C.SDL_TEXTUREACCESS_STREAMING,
		C.int(ViewportWidth), C.int(ViewportHeight))
	if texture == nil {
		err := windowError("SDL_CreateTexture")
		C.SDL_DestroyRenderer(renderer)
		C.SDL_DestroyWindow(window)
		C.shell_quit()
		return err
	}
	// Ordered teardown: SDL resources must be destroyed in dependency
	// order (texture -> renderer -> window) BEFORE SDL_Quit tears down
	// the video subsystem. Destroying them after SDL_Quit (e.g. via
	// deferred destroys running after a quit-first helper returns) lands
	// GL deletes with no current context — the Mesa "glDeleteTextures
	// called without a rendering context" noise at exit. Every exit path
	// below calls cleanup explicitly; there are no deferred SDL destroys.
	cleanup := func() {
		C.SDL_DestroyTexture(texture)
		C.SDL_DestroyRenderer(renderer)
		C.SDL_DestroyWindow(window)
		C.shell_quit()
	}

	// argb is the reusable premultiplied upload buffer for the fixed-size
	// viewport texture. SDL_UpdateTexture copies the pixels on return, so the
	// buffer is safe to repaint frame after frame: this removes the per-frame
	// 3 MB (ViewportWidth x ViewportHeight x 4) allocation that toARGB used to
	// make on every scroll repaint. Peak RSS is bound by the glibc arena
	// (which already retained the freed buffer), so this chiefly cuts the
	// allocation rate and GC pressure rather than the high-water mark.
	var argb []byte
	upload := func(f paint.Frame) error {
		need := f.Width * f.Height * 4
		if len(f.Pixels) < need {
			return WindowError("SDL_UpdateTexture: empty frame")
		}
		if cap(argb) < need {
			argb = make([]byte, need)
		}
		buf := argb[:need]
		toARGBInto(f, buf)
		if C.shell_update_texture(texture, unsafe.Pointer(&buf[0]), C.int(f.Width*4)) == 0 {
			return windowError("SDL_UpdateTexture")
		}
		return nil
	}
	if err := upload(frame); err != nil {
		cleanup()
		return err
	}

	var frames uint32
	for {
		if maxFrames != nil && frames >= *maxFrames {
			break
		}
		var event C.SDL_Event
		for C.shell_poll(&event) != 0 {
			switch C.shell_event_type(&event) {
			case C.Uint32(EventQuit):
				cleanup()
				return nil
			case C.Uint32(EventKeyDown):
				action, ok := ActionFromKey(
					uint32(C.shell_event_key(&event)),
					uint16(C.shell_event_mod(&event)))
				if !ok {
					continue
				}
				before := scroll.Offset()
				scroll.Apply(action)
				// Re-paint only when the offset really moved; the
				// clamped ends and unmapped keys stay put.
				if scroll.Offset() == before {
					continue
				}
				next := scroll.Offset()
				nf, err := paint.Render(l, &next)
				if err != nil {
					cleanup()
					return PaintError(err.Error())
				}
				frame = nf
				if err := upload(frame); err != nil {
					cleanup()
					return err
				}
			}
		}

		if C.shell_render_clear(renderer) == 0 {
			cleanup()
			return windowError("SDL_RenderClear")
		}
		if C.shell_render_texture(renderer, texture) == 0 {
			cleanup()
			return windowError("SDL_RenderTexture")
		}
		C.shell_present(renderer)
		frames++

		// Sleep between frames WITHOUT consuming events. The oracle's
		// previous SDL_WaitEventTimeout stored the wake-up event in a
		// buffer that was never dispatched, so every lone key press was
		// swallowed here and scrolling never fired. SDL_Delay leaves the
		// event queued for the next SDL_PollEvent drain above.
		C.shell_delay(16)
	}

	cleanup()
	return nil
}

// toARGBInto converts a straight-RGBA8 frame to the premultiplied ARGB8888
// bytes the streaming texture expects, writing into dst (which must hold at
// least Width*Height*4 bytes), mirroring the oracle's rgba_surface
// conversion (truncating channel*alpha/255). Native-endian words are stored
// little-endian here; Cairo copies the same layout when the oracle uploads
// its raw surface rows. The caller may reuse dst across frames.
func toARGBInto(f paint.Frame, dst []byte) {
	need := f.Width * f.Height * 4
	if len(dst) < need || len(f.Pixels) < need {
		return
	}
	for i := 0; i < f.Width*f.Height; i++ {
		r, g, b, a := uint32(f.Pixels[i*4]), uint32(f.Pixels[i*4+1]),
			uint32(f.Pixels[i*4+2]), uint32(f.Pixels[i*4+3])
		word := (a << 24) | ((r * a / 255) << 16) | ((g * a / 255) << 8) | (b * a / 255)
		binary.LittleEndian.PutUint32(dst[i*4:], word)
	}
}
