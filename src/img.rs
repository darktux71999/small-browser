//! Image subresources: fetch, decode to RGBA and cache them for one run.
//!
//! `<img src>` resolves like a stylesheet (S2): an http(s) page resolves
//! against its final document URL, a local page against the directory of the
//! file. Every URL is fetched and decoded once per run and the result —
//! including a failure — is cached, so a page that repeats a logo costs one
//! request. `background-image` URLs (S10) ride the same fetch/decode/cache
//! machinery through [`ImageSet::load_background_images`].
//!
//! Decoding is headless (no X or Wayland display): gdk-pixbuf's built-in
//! PNG/JPEG/GIF loaders for those formats and libwebp for WebP. SVG is not
//! supported: the element keeps an empty box and the run logs one warning.
//!
//! A resource that fails (unreachable, over the size cap, unknown or
//! unsupported format) never aborts the render: it warns once through
//! `browser-lite:` on stderr, exactly like a broken stylesheet.

use std::collections::HashMap;
use std::fmt;
use std::path::{Component, Path, PathBuf};
use std::rc::Rc;
use std::time::Duration;

use url::Url;

use crate::css::Style;
use crate::dom::{Document, NodeId};

/// Hard cap on one image body. A body larger than this is rejected, not
/// truncated, so a runaway download cannot be mistaken for a broken image.
pub const MAX_IMAGE_BYTES: usize = 10 * 1024 * 1024;

/// Per-image transfer timeout, matching the stylesheet loader's order of
/// magnitude: an image must not stall the render forever.
pub const IMAGE_TIMEOUT: Duration = Duration::from_secs(20);

/// Decompression-bomb guard: a 100x100 PNG can declare 40000x40000 pixels.
pub const MAX_IMAGE_PIXELS: u64 = 40_000_000;

/// One decoded image: non-premultiplied RGBA8, top-down, `width * height * 4`
/// bytes. Cairo wants premultiplied ARGB32, so `paint` converts at draw time.
pub struct Image {
    pub width: u32,
    pub height: u32,
    pub rgba: Vec<u8>,
}

impl fmt::Debug for Image {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        // Never dump the pixel buffer: a 2000x2000 image is 16 MB of noise.
        write!(f, "Image({}x{})", self.width, self.height)
    }
}

/// Per-run limits, the image counterpart of `SheetLimits`.
#[derive(Debug, Clone, Copy)]
pub struct ImageLimits {
    /// Bodies larger than this are rejected instead of decoded.
    pub max_bytes: usize,
    pub timeout: Duration,
}

impl Default for ImageLimits {
    fn default() -> ImageLimits {
        ImageLimits {
            max_bytes: MAX_IMAGE_BYTES,
            timeout: IMAGE_TIMEOUT,
        }
    }
}

/// The images of one document, decoded and cached for the whole run.
///
/// Keyed by resolved URL for the cache and by node for the layout lookup: two
/// `<img>` elements pointing at the same file share one decode.
#[derive(Debug, Default)]
pub struct ImageSet {
    /// Nodes that referenced a resource, mapped to its resolved URL.
    urls: HashMap<NodeId, String>,
    /// `background-image` URL (as the style carries it: sheet-resolved by
    /// libcss, as authored by the legacy cascade) to its image; `None` caches
    /// a failure. Keyed by that URL because every element that writes the
    /// same URL shares one fetch through the resolved-URL cache below.
    backgrounds: HashMap<String, Option<Rc<Image>>>,
    /// Resolved URL to its image; `None` caches a failure so it is not retried.
    cache: HashMap<String, Option<Rc<Image>>>,
    /// Unique URLs that were attempted (successful or not).
    fetched: usize,
    warnings: Vec<String>,
}

impl ImageSet {
    /// No images: what a page without `<img>` (or `layout`'s plain entry
    /// point) lays out against.
    pub fn empty() -> ImageSet {
        ImageSet::default()
    }

    /// Fetch and decode every `<img src>` of `doc`, resolving relative URLs
    /// against `base` (the document URL or the page's directory).
    pub fn load(doc: &Document, base: &str) -> ImageSet {
        ImageSet::load_with_limits(doc, base, ImageLimits::default())
    }

    /// [`ImageSet::load`] with explicit limits, so tests can drive the caps.
    pub fn load_with_limits(doc: &Document, base: &str, limits: ImageLimits) -> ImageSet {
        let mut set = ImageSet::default();
        for (id, node) in doc.nodes.iter().enumerate() {
            if node.is_text || !node.tag.eq_ignore_ascii_case("img") {
                continue;
            }
            let Some(src) = node.attr("src").map(str::trim).filter(|s| !s.is_empty()) else {
                // No source is not a failed resource: nothing to report.
                continue;
            };
            let Some(url) = join_url(base, src) else {
                set.warn(format!("skipping image {src:?}: cannot resolve it against {base:?}"));
                continue;
            };
            set.urls.insert(id, url.clone());
            set.load_url(&url, &limits);
        }
        set
    }

    /// The decoded image for `node`, or `None` when it has none (no `src`,
    /// unreachable, undecodable, or unsupported).
    pub fn image(&self, node: NodeId) -> Option<Rc<Image>> {
        self.urls
            .get(&node)
            .and_then(|url| self.cache.get(url))
            .and_then(|image| image.clone())
    }

    /// Fetch and decode the `background-image` URLs of `styles` on top of the
    /// `<img>` images (S10), resolving them against `base` exactly like
    /// `<img src>` and sharing the same per-URL cache: a page that references
    /// the same file as an `<img>` and as a background decodes it once.
    pub fn load_background_images(self, styles: &[Style], base: &str) -> ImageSet {
        self.load_background_images_with_limits(styles, base, ImageLimits::default())
    }

    /// [`ImageSet::load_background_images`] with explicit limits, so tests can
    /// drive the caps.
    pub fn load_background_images_with_limits(
        mut self,
        styles: &[Style],
        base: &str,
        limits: ImageLimits,
    ) -> ImageSet {
        for style in styles {
            let Some(raw) = style.background_image.as_deref() else {
                continue;
            };
            if self.backgrounds.contains_key(raw) {
                continue;
            }
            let loaded = match join_url(base, raw) {
                Some(url) => self.load_url(&url, &limits).clone(),
                None => {
                    self.warn(format!(
                        "skipping background image {raw:?}: cannot resolve it against {base:?}"
                    ));
                    None
                }
            };
            self.backgrounds.insert(raw.to_string(), loaded);
        }
        self
    }

    /// The decoded image behind a `background-image` URL as the style
    /// carries it, or `None` when it was never requested or failed to load.
    pub fn background(&self, raw_url: &str) -> Option<Rc<Image>> {
        self.backgrounds
            .get(raw_url)
            .and_then(|image| image.clone())
    }

    /// The resolved URL `node` points at, when it had a usable `src`.
    pub fn url(&self, node: NodeId) -> Option<&str> {
        self.urls.get(&node).map(String::as_str)
    }

    /// One message per failed resource, without the `browser-lite:` prefix.
    pub fn warnings(&self) -> &[String] {
        &self.warnings
    }

    /// Unique URLs attempted in this run.
    pub fn fetched(&self) -> usize {
        self.fetched
    }

    /// Unique URLs that decoded.
    pub fn loaded(&self) -> usize {
        self.cache.values().filter(|image| image.is_some()).count()
    }

    /// Unique URLs that failed; the failures are cached too.
    pub fn failed(&self) -> usize {
        self.cache.values().filter(|image| image.is_none()).count()
    }

    fn warn(&mut self, message: String) {
        eprintln!("browser-lite: {message}");
        self.warnings.push(message);
    }

    /// Fetch and decode `url` once. `None` is cached as well, so a broken URL
    /// is reported once even when the page repeats it.
    fn load_url(&mut self, url: &str, limits: &ImageLimits) -> Option<Rc<Image>> {
        if let Some(cached) = self.cache.get(url) {
            return cached.clone();
        }
        self.fetched += 1;
        let result = match fetch(url, limits).and_then(|body| decode(&body)) {
            Ok(image) => Some(Rc::new(image)),
            Err(err) => {
                self.warn(format!("skipping image {url}: {err}"));
                None
            }
        };
        self.cache.insert(url.to_string(), result.clone());
        result
    }
}

/// Decode one image body to RGBA, sniffing the format from its bytes.
///
/// The `Content-Type` and the file extension are ignored on purpose: a
/// mislabelled body still renders, and a `.png` that is not a PNG fails with
/// the same message as any other unknown format.
pub fn decode(body: &[u8]) -> Result<Image, String> {
    if body.is_empty() {
        return Err("empty body".to_string());
    }
    if looks_like_svg(body) {
        return Err("SVG images are not supported".to_string());
    }
    let image = if is_webp(body) {
        webp::decode(body)?
    } else {
        pixbuf::decode(body)?
    };
    let pixels = u64::from(image.width) * u64::from(image.height);
    if pixels == 0 {
        return Err("image has no pixels".to_string());
    }
    if pixels > MAX_IMAGE_PIXELS {
        return Err(format!(
            "{pixels} pixels exceeds the {MAX_IMAGE_PIXELS} pixel limit"
        ));
    }
    Ok(image)
}

/// SVG is XML, so it starts with `<svg` after the prolog. Detected before
/// decoding because gdk-pixbuf has no SVG loader and would report a generic
/// "unknown format" instead of naming what is missing.
fn looks_like_svg(body: &[u8]) -> bool {
    let body = body.strip_prefix(&[0xEF, 0xBB, 0xBF]).unwrap_or(body);
    let start = body
        .iter()
        .position(|byte| !byte.is_ascii_whitespace())
        .unwrap_or(body.len());
    let text = &body[start..];
    if text.starts_with(b"<svg") {
        return true;
    }
    let xml_prolog = text.starts_with(b"<?xml")
        || text.starts_with(b"<!--")
        || text.starts_with(b"<!DOCTYPE");
    if !xml_prolog {
        return false;
    }
    let head = &text[..text.len().min(4096)];
    head.windows(4).any(|w| w.eq_ignore_ascii_case(b"<svg"))
}

fn is_webp(body: &[u8]) -> bool {
    body.len() >= 12 && &body[0..4] == b"RIFF" && &body[8..12] == b"WEBP"
}

// ---------------------------------------------------------------------------
// Fetching
// ---------------------------------------------------------------------------

/// Fetch one image body, honouring the size cap and timeout.
fn fetch(url: &str, limits: &ImageLimits) -> Result<Vec<u8>, String> {
    if url.starts_with("http://") || url.starts_with("https://") {
        let (bytes, _content_type) =
            crate::net::fetch_bytes_limited(url, limits.max_bytes, limits.timeout)?;
        return Ok(bytes);
    }
    let path = local_path(url).ok_or_else(|| format!("unsupported image URL {url:?}"))?;
    read_capped(&path, limits.max_bytes)
}

/// The filesystem path behind an image URL, if there is one.
fn local_path(url: &str) -> Option<PathBuf> {
    match Url::parse(url) {
        Ok(parsed) if parsed.scheme() == "file" => parsed.to_file_path().ok(),
        // Another scheme (`data:`, `about:`, ...): not a resource we can read.
        Ok(_) => None,
        // Not a URL: a plain filesystem path.
        Err(_) => Some(PathBuf::from(url)),
    }
}

/// Read a file, rejecting (not truncating) anything over `max_bytes`.
fn read_capped(path: &Path, max_bytes: usize) -> Result<Vec<u8>, String> {
    let meta = std::fs::metadata(path).map_err(|err| err.to_string())?;
    if meta.is_dir() {
        return Err("is a directory".to_string());
    }
    if meta.len() > max_bytes as u64 {
        return Err(format!(
            "{} bytes exceeds the {max_bytes} byte limit",
            meta.len()
        ));
    }
    let bytes = std::fs::read(path).map_err(|err| err.to_string())?;
    if bytes.len() > max_bytes {
        return Err(format!("body exceeds the {max_bytes} byte limit"));
    }
    Ok(bytes)
}

/// Resolve `rel` against `base`.
///
/// Mirrors the stylesheet loader's rule (`css_libcss::join_url`, private to
/// that module): `base` is an absolute URL (http(s), file, ...) or a filesystem
/// path. A path is a directory when it ends with a separator or names an
/// existing directory; otherwise its parent directory is the base. Relative
/// paths are normalised so the cache key is stable (`a/../b.png` == `b.png`).
fn join_url(base: &str, rel: &str) -> Option<String> {
    let rel = rel.trim();
    if rel.is_empty() {
        return None;
    }
    if Url::parse(rel).is_ok() {
        return Some(rel.to_string());
    }
    if let Ok(base_url) = Url::parse(base) {
        return base_url.join(rel).ok().map(|url| url.to_string());
    }
    if base.is_empty() {
        return None;
    }
    let base_path = Path::new(base);
    let dir = if base.ends_with('/') || base_path.is_dir() {
        base_path.to_path_buf()
    } else {
        base_path.parent().unwrap_or(base_path).to_path_buf()
    };
    let joined = if Path::new(rel).is_absolute() {
        PathBuf::from(rel)
    } else {
        dir.join(rel)
    };
    Some(normalize_path(&joined).to_string_lossy().into_owned())
}

/// Lexical `.`/`..` removal, without touching the filesystem.
fn normalize_path(path: &Path) -> PathBuf {
    let mut out = PathBuf::new();
    for component in path.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                if !out.pop() {
                    out.push("..");
                }
            }
            other => out.push(other.as_os_str()),
        }
    }
    out
}

// ---------------------------------------------------------------------------
// Decoders
// ---------------------------------------------------------------------------

/// PNG, JPEG and GIF through gdk-pixbuf's built-in loaders.
///
/// The streaming loader is used instead of `gdk_pixbuf_new_from_file` because
/// the body is already in memory, and it needs no display: gdk-pixbuf is a
/// rendering-independent library.
mod pixbuf {
    use std::ffi::CStr;
    use std::os::raw::{c_char, c_int, c_uchar, c_void};

    /// `GError` as published in the glib headers: quark, code, message.
    #[repr(C)]
    struct GError {
        _domain: u32,
        _code: c_int,
        message: *mut c_char,
    }

    #[link(name = "gdk_pixbuf-2.0")]
    extern "C" {
        fn gdk_pixbuf_loader_new() -> *mut c_void;
        fn gdk_pixbuf_loader_write(
            loader: *mut c_void,
            buf: *const c_uchar,
            count: usize,
            error: *mut *mut GError,
        ) -> c_int;
        fn gdk_pixbuf_loader_close(loader: *mut c_void, error: *mut *mut GError) -> c_int;
        fn gdk_pixbuf_loader_get_pixbuf(loader: *mut c_void) -> *mut c_void;
        fn gdk_pixbuf_get_width(pixbuf: *const c_void) -> c_int;
        fn gdk_pixbuf_get_height(pixbuf: *const c_void) -> c_int;
        fn gdk_pixbuf_get_rowstride(pixbuf: *const c_void) -> c_int;
        fn gdk_pixbuf_get_n_channels(pixbuf: *const c_void) -> c_int;
        fn gdk_pixbuf_get_pixels(pixbuf: *const c_void) -> *mut c_uchar;
    }

    #[link(name = "gobject-2.0")]
    extern "C" {
        fn g_object_unref(obj: *mut c_void);
    }

    #[link(name = "glib-2.0")]
    extern "C" {
        fn g_error_free(error: *mut GError);
    }

    pub fn decode(body: &[u8]) -> Result<super::Image, String> {
        unsafe {
            let loader = gdk_pixbuf_loader_new();
            if loader.is_null() {
                return Err("gdk-pixbuf: cannot create an image loader".to_string());
            }
            let mut error: *mut GError = std::ptr::null_mut();
            if gdk_pixbuf_loader_write(loader, body.as_ptr(), body.len(), &mut error) == 0 {
                let message = take_error(&mut error);
                g_object_unref(loader);
                return Err(message);
            }
            if gdk_pixbuf_loader_close(loader, &mut error) == 0 {
                let message = take_error(&mut error);
                g_object_unref(loader);
                return Err(message);
            }
            // `get_pixbuf` borrows the loader's pixbuf, so the pixels are copied
            // before the loader goes away.
            let pixbuf = gdk_pixbuf_loader_get_pixbuf(loader);
            let image = if pixbuf.is_null() {
                Err("unsupported or corrupt image data".to_string())
            } else {
                let width = gdk_pixbuf_get_width(pixbuf);
                let height = gdk_pixbuf_get_height(pixbuf);
                let channels = gdk_pixbuf_get_n_channels(pixbuf);
                let stride = gdk_pixbuf_get_rowstride(pixbuf);
                let pixels = gdk_pixbuf_get_pixels(pixbuf);
                if width <= 0 || height <= 0 || channels <= 0 || stride <= 0 || pixels.is_null() {
                    Err("unsupported or corrupt image data".to_string())
                } else {
                    Ok(copy_rgba(
                        pixels,
                        width as usize,
                        height as usize,
                        channels as usize,
                        stride as usize,
                    ))
                }
            };
            g_object_unref(loader);
            image
        }
    }

    /// gdk-pixbuf keeps non-premultiplied samples, packed in 1-4 channels.
    unsafe fn copy_rgba(
        pixels: *const c_uchar,
        width: usize,
        height: usize,
        channels: usize,
        stride: usize,
    ) -> super::Image {
        let mut rgba = Vec::with_capacity(width * height * 4);
        for row in 0..height {
            let row_pixels = std::slice::from_raw_parts(pixels.add(row * stride), width * channels);
            for px in row_pixels.chunks_exact(channels) {
                let (r, g, b, a) = match channels {
                    1 => (px[0], px[0], px[0], 255),
                    2 => (px[0], px[0], px[0], px[1]),
                    3 => (px[0], px[1], px[2], 255),
                    _ => (px[0], px[1], px[2], px[3]),
                };
                rgba.extend_from_slice(&[r, g, b, a]);
            }
        }
        super::Image {
            width: width as u32,
            height: height as u32,
            rgba,
        }
    }

    /// Take the message out of a `GError` and free it.
    unsafe fn take_error(error: &mut *mut GError) -> String {
        if error.is_null() || (*error).is_null() {
            return "unsupported or corrupt image data".to_string();
        }
        let message = CStr::from_ptr((**error).message)
            .to_string_lossy()
            .into_owned();
        g_error_free(*error);
        *error = std::ptr::null_mut();
        message
    }
}

/// WebP through libwebp: gdk-pixbuf ships WebP as an optional module that is
/// not installed here, and libwebp decodes RGBA in one call.
mod webp {
    use std::os::raw::{c_int, c_void};

    #[link(name = "webp")]
    extern "C" {
        fn WebPGetInfo(
            data: *const u8,
            data_size: usize,
            width: *mut c_int,
            height: *mut c_int,
        ) -> c_int;
        fn WebPDecodeRGBA(
            data: *const u8,
            data_size: usize,
            width: *mut c_int,
            height: *mut c_int,
        ) -> *mut u8;
        fn WebPFree(ptr: *mut c_void);
    }

    pub fn decode(body: &[u8]) -> Result<super::Image, String> {
        unsafe {
            let mut width: c_int = 0;
            let mut height: c_int = 0;
            if WebPGetInfo(body.as_ptr(), body.len(), &mut width, &mut height) == 0 {
                return Err("unsupported or corrupt WebP data".to_string());
            }
            if width <= 0 || height <= 0 {
                return Err("WebP image has no pixels".to_string());
            }
            let mut decoded_width: c_int = 0;
            let mut decoded_height: c_int = 0;
            let pixels = WebPDecodeRGBA(
                body.as_ptr(),
                body.len(),
                &mut decoded_width,
                &mut decoded_height,
            );
            if pixels.is_null() || decoded_width <= 0 || decoded_height <= 0 {
                return Err("unsupported or corrupt WebP data".to_string());
            }
            let len = decoded_width as usize * decoded_height as usize * 4;
            // WebP hands out non-premultiplied RGBA, the same layout `Image` uses.
            let rgba = std::slice::from_raw_parts(pixels, len).to_vec();
            WebPFree(pixels as *mut c_void);
            Ok(super::Image {
                width: decoded_width as u32,
                height: decoded_height as u32,
                rgba,
            })
        }
    }
}
