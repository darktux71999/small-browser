//! Standalone tab-strip spike: cairo drawing over an SDL3 window.
//!
//! Validates geometry, colors and hit-testing for the chrome layer described
//! in `docs/tab-strip.md`. It does not touch the browser engine.

use std::ffi::{CStr, CString};
use std::os::raw::{c_char, c_int, c_void};

use cairo::{Context, FontSlant, FontWeight, Format, ImageSurface};

const WIDTH: i32 = 1000;
const HEIGHT: i32 = 64;
const WINDOW_HEIGHT: i32 = 160;

type SDL_Window = c_void;
type SDL_Renderer = c_void;
type SDL_Texture = c_void;

const SDL_INIT_VIDEO: u32 = 0x20;
const SDL_PIXELFORMAT_ARGB8888: u32 = 0x1636_2004;
const SDL_TEXTUREACCESS_STREAMING: c_int = 1;
const SDL_EVENT_QUIT: u32 = 0x100;

#[repr(C, align(8))]
struct SdlEvent([u8; 128]);

#[repr(C)]
struct SdlRect {
    x: f32,
    y: f32,
    w: f32,
    h: f32,
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
        dstrect: *const SdlRect,
    ) -> bool;
    fn SDL_RenderPresent(renderer: *mut SDL_Renderer);
    fn SDL_PollEvent(event: *mut SdlEvent) -> bool;
    fn SDL_WaitEventTimeout(event: *mut SdlEvent, timeout_ms: c_int) -> bool;
    fn SDL_GetMouseState(x: *mut f32, y: *mut f32) -> u32;
    fn SDL_GetError() -> *const c_char;
}

#[derive(Clone)]
struct Tab {
    title: String,
}

struct Strip {
    tabs: Vec<Tab>,
    active: usize,
}

impl Strip {
    fn new() -> Strip {
        Strip {
            tabs: vec![
                Tab { title: "QuickJS + Blitz".into() },
                Tab { title: "Page B".into() },
                Tab { title: "Docs".into() },
            ],
            active: 0,
        }
    }

    fn tab_width(&self) -> f64 {
        let n = self.tabs.len().max(1) as f64;
        ((WIDTH as f64 - 40.0) / n).min(220.0)
    }

    fn tab_at(&self, x: f64) -> Option<usize> {
        if x < 8.0 {
            return None;
        }
        let width = self.tab_width();
        let index = ((x - 8.0) / width).floor() as usize;
        (index < self.tabs.len()).then_some(index)
    }

    fn close_hit(&self, x: f64, index: usize) -> bool {
        let width = self.tab_width();
        let left = 8.0 + index as f64 * width;
        let close_left = left + width - 28.0;
        x >= close_left && x <= close_left + 18.0
    }

    fn draw(&self) -> Vec<u8> {
        let mut surface = ImageSurface::create(Format::ARgb32, WIDTH, HEIGHT).unwrap();
        {
            let cr = Context::new(&surface).unwrap();
            cr.set_source_rgb(0.12, 0.13, 0.15);
            cr.paint().unwrap();

            let width = self.tab_width();
            for (i, tab) in self.tabs.iter().enumerate() {
                let x = 8.0 + i as f64 * width;
                let active = i == self.active;
                if active {
                    cr.set_source_rgb(0.20, 0.22, 0.25);
                    rounded_rect(&cr, x, 6.0, width - 6.0, HEIGHT as f64 - 6.0, 8.0);
                    cr.fill().unwrap();
                    cr.set_source_rgb(0.04, 0.69, 0.31);
                    cr.rectangle(x, 6.0, width - 6.0, 3.0);
                    cr.fill().unwrap();
                }
                cr.set_source_rgb(0.92, 0.93, 0.94);
                cr.select_font_face("Sans", FontSlant::Normal, FontWeight::Normal);
                cr.set_font_size(14.0);
                cr.move_to(x + 14.0, 38.0);
                cr.show_text(&tab.title).unwrap();

                if active {
                    cr.set_source_rgb(0.65, 0.68, 0.72);
                    cr.set_font_size(16.0);
                    cr.move_to(x + width - 26.0, 37.0);
                    cr.show_text("x").unwrap();
                }
            }
        }

        let width_bytes = WIDTH as usize * 4;
        let stride = surface.stride() as usize;
        let mut pixels = Vec::with_capacity(width_bytes * HEIGHT as usize);
        {
            let data = surface.data().unwrap();
            for row in 0..HEIGHT as usize {
                let start = row * stride;
                pixels.extend_from_slice(&data[start..start + width_bytes]);
            }
        }
        pixels
    }
}

fn rounded_rect(cr: &Context, x: f64, y: f64, w: f64, h: f64, r: f64) {
    cr.new_sub_path();
    cr.arc(x + w - r, y + r, r, -std::f64::consts::FRAC_PI_2, 0.0);
    cr.arc(x + w - r, y + h - r, r, 0.0, std::f64::consts::FRAC_PI_2);
    cr.arc(x + r, y + h - r, r, std::f64::consts::FRAC_PI_2, std::f64::consts::PI);
    cr.arc(x + r, y + r, r, std::f64::consts::PI, 1.5 * std::f64::consts::PI);
    cr.close_path();
}

fn main() {
    unsafe {
        if !SDL_Init(SDL_INIT_VIDEO) {
            eprintln!("SDL_Init: {}", error());
            return;
        }
        let title = CString::new("tab-strip-spike").unwrap();
        let window = SDL_CreateWindow(title.as_ptr(), WIDTH, WINDOW_HEIGHT, 0);
        if window.is_null() {
            eprintln!("SDL_CreateWindow: {}", error());
            return;
        }
        let renderer = SDL_CreateRenderer(window, std::ptr::null());
        let texture = SDL_CreateTexture(
            renderer,
            SDL_PIXELFORMAT_ARGB8888,
            SDL_TEXTUREACCESS_STREAMING,
            WIDTH,
            HEIGHT,
        );

        let mut strip = Strip::new();
        let mut pixels = strip.draw();
        let mut was_down = false;
        let dst = SdlRect { x: 0.0, y: 0.0, w: WIDTH as f32, h: HEIGHT as f32 };

        loop {
            SDL_UpdateTexture(
                texture,
                std::ptr::null(),
                pixels.as_ptr() as *const c_void,
                WIDTH * 4,
            );
            SDL_RenderClear(renderer);
            SDL_RenderTexture(renderer, texture, std::ptr::null(), &dst);
            SDL_RenderPresent(renderer);

            let mut event = SdlEvent([0; 128]);
            while SDL_PollEvent(&mut event) {
                if u32::from_ne_bytes(event.0[0..4].try_into().unwrap()) == SDL_EVENT_QUIT {
                    SDL_DestroyTexture(texture);
                    SDL_DestroyRenderer(renderer);
                    SDL_DestroyWindow(window);
                    SDL_Quit();
                    return;
                }
            }

            let mut mx = 0.0f32;
            let mut my = 0.0f32;
            let buttons = SDL_GetMouseState(&mut mx, &mut my);
            let down = buttons & 1 != 0;
            if down && !was_down {
                eprintln!("[spike] click at {mx:.0},{my:.0}");
                let x = mx as f64;
                let y = my as f64;
                if y < HEIGHT as f64 {
                    if let Some(index) = strip.tab_at(x) {
                        if strip.close_hit(x, index) && strip.tabs.len() > 1 {
                            strip.tabs.remove(index);
                            if strip.active >= strip.tabs.len() {
                                strip.active = strip.tabs.len() - 1;
                            }
                            println!("closed tab {index}, {} left", strip.tabs.len());
                        } else {
                            strip.active = index;
                            println!("active tab {index}: {}", strip.tabs[index].title);
                        }
                        pixels = strip.draw();
                    }
                }
            }
            was_down = down;

            let mut idle = SdlEvent([0; 128]);
            SDL_WaitEventTimeout(&mut idle, 16);
        }
    }
}

fn error() -> String {
    unsafe {
        let ptr = SDL_GetError();
        if ptr.is_null() {
            "unknown SDL error".into()
        } else {
            CStr::from_ptr(ptr).to_string_lossy().into_owned()
        }
    }
}
