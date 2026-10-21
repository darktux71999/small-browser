//! SDL3 shell: one window, one frame from the paint backend.
//! Uses the system libSDL3 (3.4.18) through a small, explicit FFI surface.

#![allow(non_camel_case_types, non_snake_case)]

use std::ffi::CString;
use std::os::raw::{c_char, c_int, c_void};

use crate::paint::Frame;

type SDL_Window = c_void;
type SDL_Renderer = c_void;
type SDL_Texture = c_void;

const SDL_INIT_VIDEO: u32 = 0x0000_0020;
const SDL_PIXELFORMAT_ARGB8888: u32 = 0x1636_2004;
const SDL_TEXTUREACCESS_STREAMING: c_int = 1;
const SDL_EVENT_QUIT: u32 = 0x100;

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

pub fn run(frame: &Frame, title: &str, max_frames: Option<u32>) -> Result<(), String> {
    unsafe {
        if !SDL_Init(SDL_INIT_VIDEO) {
            return Err(error("SDL_Init"));
        }

        let c_title = CString::new(title).unwrap_or_default();
        let window = SDL_CreateWindow(
            c_title.as_ptr(),
            frame.width,
            frame.height,
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
            frame.width,
            frame.height,
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
                if event.kind() == SDL_EVENT_QUIT {
                    return finish(texture, renderer, window, Ok(()));
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
