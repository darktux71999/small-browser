//! S3 — `<img>` subresources: fetch, decode to RGBA, lay out and paint.
//!
//! The tests drive the public engine API only: parse a fixture, load its
//! images with an explicit base (the fixture directory for local pages, an HTTP
//! base for remote ones), lay out, and inspect the produced boxes or the
//! painted frame/PNG. Decoding is exercised through [`browser_lite::img::decode`]
//! with in-memory bodies, so the format coverage does not depend on fixtures
//! that a committed file extension would have to declare.

use std::io::{Read, Write};
use std::net::TcpListener;
use std::sync::mpsc::Receiver;
use std::thread;
use std::time::Duration;

use browser_lite::css::Style;
use browser_lite::css_libcss;
use browser_lite::dom::{Document, NodeId};
use browser_lite::img::{self, ImageLimits, ImageSet};
use browser_lite::layout::{self, Layout, LayoutBox};
use browser_lite::paint;

const VIEWPORT: f64 = 1000.0;

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

fn fixture(name: &str) -> String {
    std::fs::read_to_string(format!(
        "{}/test/data/{name}",
        env!("CARGO_MANIFEST_DIR")
    ))
    .unwrap_or_else(|e| panic!("fixture {name}: {e}"))
}

/// The base for the local fixtures: their containing directory, exactly what
/// `main` hands the engine for a local page.
fn fixture_base() -> String {
    format!("{}/test/data/", env!("CARGO_MANIFEST_DIR"))
}

fn load(name: &str) -> (Document, Vec<Style>) {
    let doc = Document::parse(&fixture(name));
    let styles = css_libcss::compute_with_subresources(&doc, &fixture_base(), VIEWPORT);
    (doc, styles)
}

fn images_of(doc: &Document) -> ImageSet {
    ImageSet::load(doc, &fixture_base())
}

fn node(doc: &Document, selector: &str) -> NodeId {
    doc.query_selector(selector)
        .unwrap_or_else(|| panic!("fixture has no {selector}"))
}

fn box_of<'a>(layout: &'a Layout, doc: &Document, selector: &str) -> &'a LayoutBox {
    let id = node(doc, selector);
    layout
        .boxes
        .iter()
        .find(|b| b.node == id)
        .unwrap_or_else(|| panic!("no box for {selector}"))
}

fn image_boxes(layout: &Layout) -> Vec<&LayoutBox> {
    layout.boxes.iter().filter(|b| b.tag == "img").collect()
}

// ---------------------------------------------------------------------------
// S3: download and decode
// ---------------------------------------------------------------------------

#[test]
fn png_is_fetched_and_decoded_to_rgba() {
    let doc = Document::parse(&fixture("img-basic.html"));
    let images = images_of(&doc);

    assert!(images.warnings().is_empty(), "{:?}", images.warnings());
    let image = images
        .image(node(&doc, "img"))
        .expect("img-basic.html references a decodable PNG");
    assert_eq!((image.width, image.height), (4, 3), "intrinsic pixel size");
    assert_eq!(image.rgba.len(), 4 * 3 * 4, "RGBA8 buffer is width*height*4");
    assert_eq!(&image.rgba[0..4], &[255, 0, 0, 255], "fixture is solid red");
    assert_eq!(images.fetched(), 1);
    assert_eq!(images.loaded(), 1);
    assert_eq!(images.failed(), 0);
}

#[test]
fn relative_src_resolves_against_the_http_base() {
    let body = std::fs::read(format!(
        "{}/test/data/img-red.png",
        env!("CARGO_MANIFEST_DIR")
    ))
    .expect("fixture png");
    let (origin, requests) = spawn_server(body);

    // A remote page resolves `src` against the document URL, like stylesheets.
    let doc = Document::parse("<body><img src=\"img-red.png\"></body>");
    let images = ImageSet::load(&doc, &format!("{origin}/sub/"));

    let image = images
        .image(node(&doc, "img"))
        .unwrap_or_else(|| panic!("remote image not loaded: {:?}", images.warnings()));
    assert_eq!((image.width, image.height), (4, 3));
    assert_eq!(
        requests.recv_timeout(Duration::from_secs(10)).expect("one request"),
        "/sub/img-red.png",
        "the relative URL is resolved against the document directory"
    );
}

#[test]
fn jpeg_gif_and_webp_decode_to_rgba() {
    // 2x2 solid red, one body per format; the system decoders are gdk-pixbuf
    // (JPEG/GIF) and libwebp.
    for (name, body) in [("jpeg", hex(JPEG)), ("gif", hex(GIF)), ("webp", hex(WEBP))] {
        let image = img::decode(&body).unwrap_or_else(|e| panic!("{name}: {e}"));
        assert_eq!((image.width, image.height), (2, 2), "{name}: pixel size");
        assert_eq!(image.rgba.len(), 2 * 2 * 4, "{name}: RGBA8 buffer");
        let px = &image.rgba[0..4];
        if name == "jpeg" {
            // Lossy: near-red is the contract, not an exact triple.
            assert!(px[0] > 200 && px[1] < 80 && px[2] < 80, "{name}: {px:?}");
        } else {
            assert_eq!(px, &[255, 0, 0, 255], "{name}: solid red");
        }
    }
}

#[test]
fn undecodable_bodies_are_reported_not_fatal() {
    let err = img::decode(b"this is not an image").expect_err("garbage must not decode");
    assert!(
        err.contains("image"),
        "the error should say what failed: {err:?}"
    );
    let err = img::decode(b"").expect_err("an empty body must not decode");
    assert!(!err.is_empty(), "empty bodies report a reason");

    let svg = b"<?xml version=\"1.0\"?><svg xmlns=\"http://www.w3.org/2000/svg\"></svg>";
    let err = img::decode(svg).expect_err("SVG is out of scope");
    assert!(err.contains("SVG"), "SVG failures name the format: {err:?}");
}

// ---------------------------------------------------------------------------
// S3: layout box sizing
// ---------------------------------------------------------------------------

#[test]
fn image_box_uses_the_intrinsic_size() {
    let (doc, styles) = load("img-basic.html");
    let images = images_of(&doc);
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);

    let before = box_of(&layout, &doc, "p");
    let image = box_of(&layout, &doc, "img");
    assert!((image.w - 4.0).abs() < 0.01, "intrinsic width, got {}", image.w);
    assert!((image.h - 3.0).abs() < 0.01, "intrinsic height, got {}", image.h);
    assert!(
        image.y >= before.y + before.h - 1.0,
        "the image flows after the preceding block: image.y={} block={}..{}",
        image.y,
        before.y,
        before.y + before.h
    );

    let after = layout
        .boxes
        .iter()
        .filter(|b| b.tag == "p")
        .nth(1)
        .expect("the trailing paragraph");
    assert!(
        after.y >= image.y + image.h - 0.5,
        "the trailing block starts below the image: {} vs {}",
        after.y,
        image.y + image.h
    );
}

#[test]
fn width_and_height_attributes_scale_the_box() {
    let (doc, styles) = load("img-attrs.html");
    let images = images_of(&doc);
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);

    let boxes = image_boxes(&layout);
    assert_eq!(boxes.len(), 2, "both images get a box");
    assert!(
        (boxes[0].w - 40.0).abs() < 0.01 && (boxes[0].h - 30.0).abs() < 0.01,
        "width/height attributes set the box, got {}x{}",
        boxes[0].w,
        boxes[0].h
    );
    // A single attribute keeps the 4:3 intrinsic ratio.
    assert!(
        (boxes[1].w - 8.0).abs() < 0.01 && (boxes[1].h - 6.0).abs() < 0.01,
        "width alone keeps the aspect ratio, got {}x{}",
        boxes[1].w,
        boxes[1].h
    );
}

#[test]
fn an_image_never_exceeds_the_content_box() {
    let (doc, styles) = load("img-clamp.html");
    let images = images_of(&doc);
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);

    let container = box_of(&layout, &doc, ".box");
    let image = box_of(&layout, &doc, "img");
    assert!(
        (container.w - 100.0).abs() < 0.01,
        "fixture container is 100px wide, got {}",
        container.w
    );
    assert!(
        image.w <= container.w + 0.01,
        "1200px image must be clamped to the 100px content box, got {}",
        image.w
    );
    assert!(
        (image.w - 100.0).abs() < 0.01,
        "the clamp keeps the box at the content width, got {}",
        image.w
    );
    // 1200x10 scaled to 100px wide is 0.83px tall: the ratio survives.
    assert!(
        (image.h - 10.0 * 100.0 / 1200.0).abs() < 0.05,
        "the clamp scales both axes, got height {}",
        image.h
    );
    assert!(
        image.x >= container.x - 0.5 && image.x + image.w <= container.x + container.w + 0.5,
        "the image box stays inside its container: {}..{} vs {}..{}",
        image.x,
        image.x + image.w,
        container.x,
        container.x + container.w
    );
}

// ---------------------------------------------------------------------------
// S3: both paint paths draw the image
// ---------------------------------------------------------------------------

#[test]
fn frame_path_paints_the_image() {
    let (doc, styles) = load("img-basic.html");
    let images = images_of(&doc);
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let frame = paint::render(&layout).expect("render");

    let image = box_of(&layout, &doc, "img");
    let [b, g, r, _] = pixel(&frame, centre_x(image), centre_y(image));
    assert!(
        r > 200 && g < 60 && b < 60,
        "the image box should be red, got rgb({r},{g},{b})"
    );

    // The old gap stays white: the pixels come from the image, not from a fill.
    let gap = pixel(&frame, (image.x as i32 + 200).min(frame.width - 1), centre_y(image));
    assert_eq!(gap, [255, 255, 255, 255], "nothing painted beside the image");
}

#[test]
fn png_path_paints_the_image() {
    let (doc, styles) = load("img-basic.html");
    let images = images_of(&doc);
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);

    let path = std::env::temp_dir().join(format!("browser-lite-img-{}.png", std::process::id()));
    paint::render_to_png(&layout, &path).expect("render_to_png");
    let mut file = std::fs::File::open(&path).expect("png exists");
    let mut png = cairo::ImageSurface::create_from_png(&mut file).expect("valid png");
    let _ = std::fs::remove_file(&path);

    let image = box_of(&layout, &doc, "img");
    let [b, g, r, _] = surface_pixel(&mut png, centre_x(image), centre_y(image));
    assert!(
        r > 200 && g < 60 && b < 60,
        "the PNG keeps the image pixels, got rgb({r},{g},{b})"
    );
}

#[test]
fn alpha_is_composited_over_the_page() {
    let doc = Document::parse(
        "<body style=\"margin:0\"><img src=\"img-alpha.png\"></body>",
    );
    let styles = css_libcss::compute_with_subresources(&doc, &fixture_base(), VIEWPORT);
    let images = images_of(&doc);
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let frame = paint::render(&layout).expect("render");

    let image = box_of(&layout, &doc, "img");
    let [b, g, r, _] = pixel(&frame, centre_x(image), centre_y(image));
    // 50% red over white: red stays saturated, green/blue land mid-way.
    assert!(
        r > 240 && (110..=150).contains(&g) && (110..=150).contains(&b),
        "half-transparent red over white, got rgb({r},{g},{b})"
    );
}

// ---------------------------------------------------------------------------
// S3: graceful degradation
// ---------------------------------------------------------------------------

#[test]
fn broken_images_leave_empty_boxes_and_one_warning_each() {
    let (doc, styles) = load("img-broken.html");
    let images = images_of(&doc);

    // Two references to the same missing file, plus one undecodable body: one
    // warning per resource, failures cached like successful fetches.
    assert_eq!(
        images.warnings().len(),
        2,
        "one warning per resource: {:?}",
        images.warnings()
    );
    assert_eq!(images.fetched(), 2, "the duplicate URL is fetched once");
    assert_eq!(images.failed(), 2);
    assert_eq!(images.loaded(), 0);
    assert!(
        images.warnings().iter().any(|w| w.contains("img-missing.png")),
        "the broken URL is named: {:?}",
        images.warnings()
    );
    assert!(
        images
            .warnings()
            .iter()
            .any(|w| w.contains("img-notimage.png")),
        "the undecodable body is named: {:?}",
        images.warnings()
    );

    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let boxes = image_boxes(&layout);
    assert_eq!(boxes.len(), 3, "every <img> still gets its box");
    for b in &boxes {
        assert!(b.image.is_none(), "failed images have no pixels");
        assert!(
            b.w == 0.0 && b.h == 0.0,
            "without size hints a failed image is a zero box, got {}x{}",
            b.w,
            b.h
        );
    }

    // Rendering continues and the surrounding text is still laid out.
    let frame = paint::render(&layout).expect("render survives broken images");
    assert!(frame.width > 0 && frame.height > 0);
    assert!(
        layout
            .boxes
            .iter()
            .any(|b| b.text.as_deref() == Some("text after")),
        "the trailing text is still laid out"
    );
}

#[test]
fn svg_is_unsupported_and_keeps_an_empty_box() {
    let (doc, styles) = load("img-svg.html");
    let images = images_of(&doc);

    assert_eq!(images.warnings().len(), 1, "{:?}", images.warnings());
    assert!(
        images.warnings()[0].contains("SVG"),
        "SVG failures name the format: {:?}",
        images.warnings()
    );
    assert!(images.image(node(&doc, "img")).is_none());

    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    let image = box_of(&layout, &doc, "img");
    // The width/height attributes still reserve the author's box.
    assert!(
        (image.w - 32.0).abs() < 0.01 && (image.h - 32.0).abs() < 0.01,
        "an empty box keeps the attribute size, got {}x{}",
        image.w,
        image.h
    );
    paint::render(&layout).expect("render survives an unsupported image");
}

#[test]
fn oversized_bodies_are_rejected_not_truncated() {
    let doc = Document::parse(&fixture("img-basic.html"));
    let limits = ImageLimits {
        max_bytes: 16,
        timeout: Duration::from_secs(10),
    };
    let images = ImageSet::load_with_limits(&doc, &fixture_base(), limits);

    assert_eq!(images.warnings().len(), 1, "{:?}", images.warnings());
    assert!(
        images.warnings()[0].contains("limit"),
        "the cap is reported: {:?}",
        images.warnings()
    );
    assert!(images.image(node(&doc, "img")).is_none());
    assert_eq!(images.failed(), 1);
}

#[test]
fn each_url_is_fetched_once_per_run() {
    let (doc, styles) = load("img-attrs.html");
    let images = images_of(&doc);

    // Both elements point at img-red.png.
    assert_eq!(images.fetched(), 1, "the URL is fetched once");
    assert_eq!(images.loaded(), 1, "the decode is cached");
    let layout = layout::layout_with_images(&doc, &styles, VIEWPORT, &images);
    for b in image_boxes(&layout) {
        assert!(b.image.is_some(), "both elements reuse the cached image");
    }
}

// ---------------------------------------------------------------------------
// PRESERVE: pages without images are untouched
// ---------------------------------------------------------------------------

#[test]
fn pages_without_images_lay_out_identically() {
    let (doc, styles) = load("hello.html");
    let plain = layout::layout(&doc, &styles, VIEWPORT);
    let with_set = layout::layout_with_images(&doc, &styles, VIEWPORT, &ImageSet::empty());

    assert_eq!(plain.boxes.len(), with_set.boxes.len());
    for (a, b) in plain.boxes.iter().zip(&with_set.boxes) {
        assert_eq!((a.node, &a.tag, a.x, a.y, a.w, a.h), (b.node, &b.tag, b.x, b.y, b.w, b.h));
        assert!(a.image.is_none() && b.image.is_none());
    }
    assert_eq!(plain.height, with_set.height);
    assert_eq!(plain.runs.len(), with_set.runs.len());
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn centre_x(b: &LayoutBox) -> i32 {
    (b.x + b.w / 2.0) as i32
}

fn centre_y(b: &LayoutBox) -> i32 {
    (b.y + b.h / 2.0) as i32
}

/// ARGB32 native-endian: [B, G, R, A] on little endian.
fn pixel(frame: &paint::Frame, x: i32, y: i32) -> [u8; 4] {
    let offset = ((y * frame.width + x) * 4) as usize;
    let p = &frame.pixels[offset..offset + 4];
    [p[0], p[1], p[2], p[3]]
}

fn surface_pixel(surface: &mut cairo::ImageSurface, x: i32, y: i32) -> [u8; 4] {
    let stride = surface.stride() as usize;
    let data = surface.data().expect("png pixel data");
    let offset = y as usize * stride + x as usize * 4;
    [data[offset], data[offset + 1], data[offset + 2], data[offset + 3]]
}

/// Serves `body` once for any request and reports the requested path.
fn spawn_server(body: Vec<u8>) -> (String, Receiver<String>) {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
    let port = listener.local_addr().expect("server address").port();
    let (sender, receiver) = std::sync::mpsc::channel();
    thread::spawn(move || {
        for stream in listener.incoming() {
            let Ok(mut stream) = stream else { break };
            let mut buffer = [0u8; 2048];
            let n = stream.read(&mut buffer).unwrap_or(0);
            let request = String::from_utf8_lossy(&buffer[..n]).into_owned();
            let path = request
                .lines()
                .next()
                .and_then(|line| line.split_whitespace().nth(1))
                .unwrap_or("/")
                .to_string();
            let _ = sender.send(path);
            let head = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                body.len()
            );
            let _ = stream.write_all(head.as_bytes());
            let _ = stream.write_all(&body);
            let _ = stream.flush();
            break;
        }
    });
    (format!("http://127.0.0.1:{port}"), receiver)
}

fn hex(text: &str) -> Vec<u8> {
    let digits: Vec<u8> = text.bytes().filter(|b| !b.is_ascii_whitespace()).collect();
    digits
        .chunks(2)
        .map(|pair| {
            u8::from_str_radix(std::str::from_utf8(pair).expect("ascii"), 16).expect("hex pair")
        })
        .collect()
}

/// 2x2 solid red JPEG (gdk-pixbuf's built-in loader).
const JPEG: &str = "\
    ffd8ffe000104a46494600010100000100010000ffdb0043000201010101010201010102\
    020202020403020202020504040304060506060605060606070908060709070606080b08\
    090a0a0a0a0a06080b0c0b0a0c090a0a0affdb004301020202020202050303050a070607\
    0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a\
    0a0a0a0a0a0a0a0a0a0a0a0a0a0affc00011080002000203012200021101031101ffc400\
    1f0000010501010101010100000000000000000102030405060708090a0bffc400b51000\
    02010303020403050504040000017d010203000411051221314106135161072271143281\
    91a1082342b1c11552d1f02433627282090a161718191a25262728292a3435363738393a\
    434445464748494a535455565758595a636465666768696a737475767778797a83848586\
    8788898a92939495969798999aa2a3a4a5a6a7a8a9aab2b3b4b5b6b7b8b9bac2c3c4c5c6\
    c7c8c9cad2d3d4d5d6d7d8d9dae1e2e3e4e5e6e7e8e9eaf1f2f3f4f5f6f7f8f9faffc400\
    1f0100030101010101010101010000000000000102030405060708090a0bffc400b51100\
    020102040403040705040400010277000102031104052131061241510761711322328108\
    144291a1b1c109233352f0156272d10a162434e125f11718191a262728292a3536373839\
    3a434445464748494a535455565758595a636465666768696a737475767778797a828384\
    85868788898a92939495969798999aa2a3a4a5a6a7a8a9aab2b3b4b5b6b7b8b9bac2c3c4\
    c5c6c7c8c9cad2d3d4d5d6d7d8d9dae2e3e4e5e6e7e8e9eaf2f3f4f5f6f7f8f9faffda00\
    0c03010002110311003f00f8be8a28afe533fdfc3fffd9";

/// 2x2 solid red GIF.
const GIF: &str = "\
    47494638376102000200810000ff00000000000000000000002c00000000020002000008\
    06000108041010003b";

/// 2x2 lossless red WebP (libwebp, since gdk-pixbuf has no WebP loader here).
const WEBP: &str = "\
    524946461c000000574542505650384c0f0000002f014000000710fd8ffe0722a2ff0100";

/// S3: the real CLI paints images. The one-line wiring in main.rs passes the
/// document base into layout, so a relative `src` resolves and the pixels land
/// in the output PNG. RED evidence: before the wiring the CLI rendered the
/// image area 100% white (layout::layout had no base to resolve URLs against).
#[test]
fn cli_paints_images() {
    let out = std::env::temp_dir().join("browser-lite-cli-images-test.png");
    let _ = std::fs::remove_file(&out);
    let fixture = format!("{}/test/data/img-basic.html", env!("CARGO_MANIFEST_DIR"));
    let status = std::process::Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .args(["--png", out.to_str().unwrap(), &fixture])
        .status()
        .expect("run browser-lite");
    assert!(status.success(), "browser-lite exited with {status}");
    let bytes = std::fs::read(&out).expect("PNG written");
    let img = browser_lite::img::decode(&bytes).expect("output PNG decodes");
    let red = img
        .rgba
        .chunks(4)
        .filter(|p| p[0] > 200 && p[1] < 50 && p[2] < 50)
        .count();
    assert!(red >= 12, "expected the 4x3 red fixture painted, got {red} red px");
    let _ = std::fs::remove_file(&out);
}
