//! SDL3 shell: one fixed-size window, one viewportful of the document.
//! Uses the system libSDL3 (3.4.18) through a small, explicit FFI surface.
//!
//! The window opens at [`VIEWPORT_WIDTH`] x [`VIEWPORT_HEIGHT`] no matter how
//! tall the document is (S9): the layout still measures the whole page, the
//! painter clips to the viewport, and the keyboard scrolls a [`ScrollState`].

#![allow(non_camel_case_types, non_snake_case)]

use std::ffi::CString;
use std::os::raw::{c_char, c_int, c_void};

use crate::layout::Layout;
use crate::paint;

type SDL_Window = c_void;
type SDL_Renderer = c_void;
type SDL_Texture = c_void;

const SDL_INIT_VIDEO: u32 = 0x0000_0020;
const SDL_PIXELFORMAT_ARGB8888: u32 = 0x1636_2004;
const SDL_TEXTUREACCESS_STREAMING: c_int = 1;
const SDL_EVENT_QUIT: u32 = 0x100;
const SDL_EVENT_KEY_DOWN: u32 = 0x300;

// SDL3 keycodes (headers `SDL_keycode.h`/`SDL_events.h`, values verified
// against the installed SDL 3.4.18 with `offsetof`). Letter keycodes are the
// ASCII value of the lower-case letter whatever the modifiers are, so `G`
// arrives either as shifted `g` or (platform dependent) as the upper-case
// code; both are handled in `scroll_action_from_key`.
const SDLK_UP: u32 = 0x4000_0052;
const SDLK_DOWN: u32 = 0x4000_0051;
const SDLK_PAGEUP: u32 = 0x4000_004B;
const SDLK_PAGEDOWN: u32 = 0x4000_004E;
const SDLK_HOME: u32 = 0x4000_004A;
const SDLK_END: u32 = 0x4000_004D;
const KEY_J: u32 = 0x6A;
const KEY_K: u32 = 0x6B;
const KEY_G_LOWER: u32 = 0x67;
const KEY_G_UPPER: u32 = 0x47;
const SDL_KMOD_SHIFT: u16 = 0x0003;

/// Fixed interactive viewport: the window opens at this practical size instead
/// of the document height, and only this many pixels are ever painted.
pub const VIEWPORT_WIDTH: i32 = 1000;
pub const VIEWPORT_HEIGHT: i32 = 760;

/// Arrow keys and vi `j`/`k` scroll one step of this many CSS pixels
/// (roughly one text line).
pub const LINE_STEP: f64 = 40.0;

/// PageUp/PageDown keep this many pixels of the previous page visible: a page
/// step is `viewport_height - PAGE_OVERLAP`.
pub const PAGE_OVERLAP: f64 = 60.0;

/// A keyboard command that moves the vertical scroll offset.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ScrollAction {
    /// Arrow up, `k`.
    LineUp,
    /// Arrow down, `j`.
    LineDown,
    /// PageUp.
    PageUp,
    /// PageDown.
    PageDown,
    /// Home, `g`: jump to the top of the document.
    Top,
    /// End, `G`: jump to the bottom of the document.
    Bottom,
}

/// Vertical scroll state, clamped to `[0, max(0, document_height - viewport_height)]`.
///
/// This is a pure unit: the shell feeds it SDL keys, the painter only draws
/// the offset it is told to draw.
#[derive(Clone, Copy, Debug)]
pub struct ScrollState {
    offset: f64,
    max_offset: f64,
    viewport_height: f64,
}

impl ScrollState {
    /// A document starts at the top; the range shrinks to `0` when the
    /// document fits inside the viewport.
    pub fn new(document_height: f64, viewport_height: f64) -> ScrollState {
        ScrollState {
            offset: 0.0,
            max_offset: (document_height - viewport_height).max(0.0),
            viewport_height,
        }
    }

    /// Current scroll offset in CSS pixels from the top of the document.
    pub fn offset(&self) -> f64 {
        self.offset
    }

    /// Largest legal offset: `max(0, document_height - viewport_height)`.
    pub fn max_offset(&self) -> f64 {
        self.max_offset
    }

    /// Sets an absolute offset, clamped into the scrollable range.
    pub fn set_offset(&mut self, offset: f64) {
        self.offset = offset.clamp(0.0, self.max_offset);
    }

    /// Applies one keyboard command and returns the resulting offset.
    pub fn apply(&mut self, action: ScrollAction) -> f64 {
        let delta = match action {
            ScrollAction::LineUp => -LINE_STEP,
            ScrollAction::LineDown => LINE_STEP,
            ScrollAction::PageUp => -self.page_step(),
            ScrollAction::PageDown => self.page_step(),
            ScrollAction::Top => {
                self.set_offset(0.0);
                return self.offset;
            }
            ScrollAction::Bottom => {
                self.set_offset(self.max_offset);
                return self.offset;
            }
        };
        self.set_offset(self.offset + delta);
        self.offset
    }

    /// A page step: the viewport height minus [`PAGE_OVERLAP`] of context,
    /// never below one pixel.
    fn page_step(&self) -> f64 {
        (self.viewport_height - PAGE_OVERLAP).max(1.0)
    }
}

/// Maps an SDL3 virtual key code (`SDL_KeyboardEvent.key`) and its modifiers
/// to a scroll command; `None` means "this key does not scroll" and the shell
/// ignores it. Covers the arrow keys, PageUp/PageDown, Home/End and the vi
/// navigation `j`/`k`/`g`/`G`.
pub fn scroll_action_from_key(key: u32, modifiers: u16) -> Option<ScrollAction> {
    match key {
        SDLK_UP => Some(ScrollAction::LineUp),
        SDLK_DOWN => Some(ScrollAction::LineDown),
        SDLK_PAGEUP => Some(ScrollAction::PageUp),
        SDLK_PAGEDOWN => Some(ScrollAction::PageDown),
        SDLK_HOME => Some(ScrollAction::Top),
        SDLK_END => Some(ScrollAction::Bottom),
        KEY_K => Some(ScrollAction::LineUp),
        KEY_J => Some(ScrollAction::LineDown),
        KEY_G_UPPER => Some(ScrollAction::Bottom),
        KEY_G_LOWER if modifiers & SDL_KMOD_SHIFT != 0 => Some(ScrollAction::Bottom),
        KEY_G_LOWER => Some(ScrollAction::Top),
        _ => None,
    }
}

/// SDL_Event is a 56-byte union; a padded buffer avoids binding every variant.
#[repr(C, align(8))]
struct SdlEvent([u8; 128]);

impl SdlEvent {
    fn new() -> SdlEvent {
        SdlEvent([0; 128])
    }
    fn kind(&self) -> u32 {
        u32::from_ne_bytes(self.0[0..4].try_into().unwrap())
    }
    /// `SDL_KeyboardEvent.key` (SDL 3.4.18, `offsetof` verified): type 0,
    /// reserved 4, timestamp 8, windowID 16, which 20, scancode 24, key 28,
    /// mod 32, raw 34, down 36, repeat 37.
    fn key(&self) -> u32 {
        u32::from_ne_bytes(self.0[28..32].try_into().unwrap())
    }
    /// `SDL_KeyboardEvent.mod` (16-bit `SDL_Keymod` mask at offset 32).
    fn modifiers(&self) -> u16 {
        u16::from_ne_bytes(self.0[32..34].try_into().unwrap())
    }
}

#[link(name = "SDL3")]
extern "C" {
    fn SDL_Init(flags: u32) -> bool;
    fn SDL_Quit();
    fn SDL_CreateWindow(title: *const c_char, w: c_int, h: c_int, flags: u64) -> *mut SDL_Window;
    fn SDL_DestroyWindow(window: *mut SDL_Window);
    fn SDL_CreateRenderer(window: *mut SDL_Window, name: *const c_char) -> *mut SDL_Renderer;
    fn SDL_DestroyRenderer(renderer: *mut SDL_Renderer);
    fn SDL_CreateTexture(
        renderer: *mut SDL_Renderer,
        format: u32,
        access: c_int,
        w: c_int,
        h: c_int,
    ) -> *mut SDL_Texture;
    fn SDL_DestroyTexture(texture: *mut SDL_Texture);
    fn SDL_UpdateTexture(
        texture: *mut SDL_Texture,
        rect: *const c_void,
        pixels: *const c_void,
        pitch: c_int,
    ) -> bool;
    fn SDL_RenderClear(renderer: *mut SDL_Renderer) -> bool;
    fn SDL_RenderTexture(
        renderer: *mut SDL_Renderer,
        texture: *mut SDL_Texture,
        srcrect: *const c_void,
        dstrect: *const c_void,
    ) -> bool;
    fn SDL_RenderPresent(renderer: *mut SDL_Renderer);
    fn SDL_PollEvent(event: *mut SdlEvent) -> bool;
    fn SDL_WaitEventTimeout(event: *mut SdlEvent, timeout_ms: c_int) -> bool;
    fn SDL_GetError() -> *const c_char;
}

/// Opens the fixed-viewport window on `layout` and runs the event loop until
/// the window closes (or `max_frames` frames have been shown).
///
/// The whole document is laid out, but only one viewportful is painted: the
/// initial frame is the document top, and the scroll keys (arrows,
/// PageUp/PageDown, Home/End, vi `j`/`k`/`g`/`G`) re-paint the window further
/// down through [`ScrollState`]. `--png`/`--dump` never reach this path.
pub fn run(layout: &Layout, title: &str, max_frames: Option<u32>) -> Result<(), String> {
    let mut scroll = ScrollState::new(layout.height, f64::from(VIEWPORT_HEIGHT));
    let mut frame = paint::render_viewport(layout, scroll.offset())?;
    if frame.width != VIEWPORT_WIDTH || frame.height != VIEWPORT_HEIGHT {
        return Err(format!(
            "viewport frame is {}x{}, expected {VIEWPORT_WIDTH}x{VIEWPORT_HEIGHT}",
            frame.width, frame.height
        ));
    }

    unsafe {
        if !SDL_Init(SDL_INIT_VIDEO) {
            return Err(error("SDL_Init"));
        }

        let c_title = CString::new(title).unwrap_or_default();
        let window = SDL_CreateWindow(
            c_title.as_ptr(),
            VIEWPORT_WIDTH,
            VIEWPORT_HEIGHT,
            0, // SDL_WINDOW_RESIZABLE off for now
        );
        if window.is_null() {
            SDL_Quit();
            return Err(error("SDL_CreateWindow"));
        }

        let renderer = SDL_CreateRenderer(window, std::ptr::null());
        if renderer.is_null() {
            SDL_DestroyWindow(window);
            SDL_Quit();
            return Err(error("SDL_CreateRenderer"));
        }

        let texture = SDL_CreateTexture(
            renderer,
            SDL_PIXELFORMAT_ARGB8888,
            SDL_TEXTUREACCESS_STREAMING,
            VIEWPORT_WIDTH,
            VIEWPORT_HEIGHT,
        );
        if texture.is_null() {
            SDL_DestroyRenderer(renderer);
            SDL_DestroyWindow(window);
            SDL_Quit();
            return Err(error("SDL_CreateTexture"));
        }

        let pitch = frame.width * 4;
        if !SDL_UpdateTexture(
            texture,
            std::ptr::null(),
            frame.pixels.as_ptr() as *const c_void,
            pitch,
        ) {
            return Err(error("SDL_UpdateTexture"));
        }

        let mut frames = 0u32;
        loop {
            if let Some(max) = max_frames {
                if frames >= max {
                    break;
                }
            }
            let mut event = SdlEvent::new();
            while SDL_PollEvent(&mut event) {
                match event.kind() {
                    SDL_EVENT_QUIT => return finish(texture, renderer, window, Ok(())),
                    SDL_EVENT_KEY_DOWN => {
                        if let Some(action) = scroll_action_from_key(event.key(), event.modifiers())
                        {
                            let before = scroll.offset();
                            scroll.apply(action);
                            // Re-paint only when the offset really moved; the
                            // clamped ends and unmapped keys stay put.
                            if scroll.offset() != before {
                                match paint::render_viewport(layout, scroll.offset()) {
                                    Ok(next) => frame = next,
                                    Err(err) => return finish(texture, renderer, window, Err(err)),
                                }
                                if !SDL_UpdateTexture(
                                    texture,
                                    std::ptr::null(),
                                    frame.pixels.as_ptr() as *const c_void,
                                    pitch,
                                ) {
                                    return finish(
                                        texture,
                                        renderer,
                                        window,
                                        Err(error("SDL_UpdateTexture")),
                                    );
                                }
                            }
                        }
                    }
                    _ => {}
                }
            }

            if !SDL_RenderClear(renderer) {
                return Err(error("SDL_RenderClear"));
            }
            if !SDL_RenderTexture(
                renderer,
                texture,
                std::ptr::null(),
                std::ptr::null(),
            ) {
                return Err(error("SDL_RenderTexture"));
            }
            SDL_RenderPresent(renderer);
            frames += 1;

            let mut idle = SdlEvent::new();
            SDL_WaitEventTimeout(&mut idle, 16);
        }

        finish(texture, renderer, window, Ok(()))
    }
}

fn finish(
    texture: *mut SDL_Texture,
    renderer: *mut SDL_Renderer,
    window: *mut SDL_Window,
    result: Result<(), String>,
) -> Result<(), String> {
    unsafe {
        SDL_DestroyTexture(texture);
        SDL_DestroyRenderer(renderer);
        SDL_DestroyWindow(window);
        SDL_Quit();
    }
    result
}

fn error(what: &str) -> String {
    unsafe {
        let ptr = SDL_GetError();
        let detail = if ptr.is_null() {
            "unknown SDL error".to_string()
        } else {
            std::ffi::CStr::from_ptr(ptr).to_string_lossy().into_owned()
        };
        format!("{what}: {detail}")
    }
}
