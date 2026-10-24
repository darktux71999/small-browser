//! Real typography (T10, Pareto row #4): full `font-family` stack resolution,
//! generic keyword mapping, and `@font-face` registration.
//!
//! The machine's *system* fontconfig knows exactly one family (JetBrains
//! Mono), so `serif`, `sans-serif` and `monospace` would all collapse to the
//! same face and none of these rules could be observed on the production
//! config. The suite therefore runs every in-process layout against a private
//! fontconfig configuration (`FONTCONFIG_FILE`, written to a scratch dir on
//! the home disk and set before the first Pango context exists) that binds the
//! generic keywords to three tiny fixture fonts with deliberately different
//! advance widths:
//!
//! | CSS keyword family | fixture font    | advance |
//! |--------------------|-----------------|---------|
//! | serif              | T10 Serif Test  | 1400/2048 em |
//! | sans(s-serif)      | T10 Sans Test   |  800/2048 em |
//! | monospace          | T10 Mono Test   | 1100/2048 em |
//!
//! `@font-face` registers a fourth face through the engine's own pipeline
//! (T10 Webfont Test, 1700/2048 em). Everything asserted is observable
//! through the public interface: `compute` → `layout` → box metrics, or the
//! `--dump`/`--png` CLI. Expected widths are `chars * advance * 16px / 2048`
//! with a 2px tolerance for Pango's pixel rounding.

use std::path::PathBuf;
use std::process::Command;
use std::sync::{Mutex, MutexGuard, Once, OnceLock};

use browser_lite::css::Style;
use browser_lite::css_libcss;
use browser_lite::dom::Document;
use browser_lite::layout::{self, Layout, LayoutBox};

const VIEWPORT: f64 = 1000.0;
const EM: f64 = 16.0; // every fixture sets font-size: 16px
const TOL: f64 = 2.0; // Pango rounds layout metrics to whole pixels

/// Fixture font advances in units of 1/2048 em (see gen notes above).
const SERIF_ADV: f64 = 1400.0;
const SANS_ADV: f64 = 800.0;
const MONO_ADV: f64 = 1100.0;
const WEB_ADV: f64 = 1700.0;

/// Expected width of one fixture text line. Pango rounds every glyph advance
/// to whole pixels at 72dpi hinting (`chars * round(advance_px)`): the
/// measured values are exact integers like 19*11 = 209 for serif.
fn expect_width(chars: usize, advance: f64) -> f64 {
    chars as f64 * (advance * EM / 2048.0).round()
}

// ---------------------------------------------------------------------------
// Harness: fixtures, private fontconfig, engine entry points.
// ---------------------------------------------------------------------------

fn fixture(name: &str) -> String {
    std::fs::read_to_string(format!(
        "{}/test/data/{name}",
        env!("CARGO_MANIFEST_DIR")
    ))
    .unwrap_or_else(|e| panic!("fixture {name}: {e}"))
}

fn fixture_path(name: &str) -> PathBuf {
    PathBuf::from(format!("{}/test/data/{name}", env!("CARGO_MANIFEST_DIR")))
}

/// The base for local fixtures: their containing directory, exactly what
/// `main` hands the engine for a local page (same as `tests/background.rs`).
fn fixture_base() -> String {
    format!("{}/test/data/", env!("CARGO_MANIFEST_DIR"))
}

/// Scratch directory on the home disk (the /tmp tmpfs is 1 GiB and fills up).
fn scratch_dir() -> PathBuf {
    let home = std::env::var_os("HOME").expect("HOME");
    PathBuf::from(home).join(".cache/t10-typography-tests")
}

fn fontconfig_conf() -> PathBuf {
    scratch_dir().join("fonts.conf")
}

/// Write the private fontconfig configuration and export `FONTCONFIG_FILE`.
///
/// This must run before the first Pango/fontconfig call in this process
/// (fontconfig reads the file once, lazily, at first use); every test calls
/// it first, and `Once` makes the export a single point of truth.
///
/// The three generic fixture fonts are *copied* into the scratch dir, and the
/// config scans only that dir: the webfont fixture deliberately stays
/// outside it, so `T10 Webfont Test` exists only through the engine's own
/// `@font-face` registration and the font-face test cannot pass by accident.
fn ensure_fontconfig() {
    static ONCE: Once = Once::new();
    ONCE.call_once(|| {
        let dir = scratch_dir();
        let scanned = dir.join("fonts");
        std::fs::create_dir_all(&scanned).unwrap_or_else(|e| panic!("scratch {dir:?}: {e}"));
        for name in [
            "typography-serif-test.ttf",
            "typography-sans-test.ttf",
            "typography-mono-test.ttf",
        ] {
            std::fs::copy(fixture_path(name), scanned.join(name))
                .unwrap_or_else(|e| panic!("copy {name}: {e}"));
        }
        // fontconfig tests are case-insensitive on family, so the lowercase
        // keywords below also cover Pango's "Sans"/"Serif"/"Monospace".
        let conf = format!(
            r#"<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">
<fontconfig>
  <dir>{scanned}</dir>
  <cachedir>{dir}</cachedir>
  <match target="pattern">
    <test qual="any" name="family"><string>serif</string></test>
    <edit name="family" mode="assign" binding="strong"><string>T10 Serif Test</string></edit>
  </match>
  <match target="pattern">
    <test qual="any" name="family"><string>sans</string></test>
    <edit name="family" mode="assign" binding="strong"><string>T10 Sans Test</string></edit>
  </match>
  <match target="pattern">
    <test qual="any" name="family"><string>sans-serif</string></test>
    <edit name="family" mode="assign" binding="strong"><string>T10 Sans Test</string></edit>
  </match>
  <match target="pattern">
    <test qual="any" name="family"><string>monospace</string></test>
    <edit name="family" mode="assign" binding="strong"><string>T10 Mono Test</string></edit>
  </match>
</fontconfig>
"#,
            scanned = scanned.display(),
            dir = dir.display()
        );
        let path = fontconfig_conf();
        std::fs::write(&path, conf).unwrap_or_else(|e| panic!("write {path:?}: {e}"));
        std::env::set_var("FONTCONFIG_FILE", &path);
    });
}

/// Serialise every engine call inside this test binary: fontconfig is not
/// thread-safe, and `@font-face` registration (compute) can otherwise race
/// `FcFontMatch` (layout) in another test thread.
fn engine_lock() -> MutexGuard<'static, ()> {
    static LOCK: OnceLock<Mutex<()>> = OnceLock::new();
    LOCK.get_or_init(|| Mutex::new(()))
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// Compute styles without subresources (the plain `compute` path): family
/// stacks must resolve the same way there.
fn load(name: &str) -> (Document, Vec<Style>) {
    ensure_fontconfig();
    let _serialised = engine_lock();
    let doc = Document::parse(&fixture(name));
    let sheets = css_libcss::stylesheets(&doc);
    let styles = css_libcss::compute(&doc, &sheets, VIEWPORT);
    (doc, styles)
}

/// Compute styles with subresources (the `@font-face` path: the font file is
/// fetched and registered exactly like `main` does for a local page).
fn load_with_fonts(name: &str) -> (Document, Vec<Style>) {
    ensure_fontconfig();
    let _serialised = engine_lock();
    let doc = Document::parse(&fixture(name));
    let styles = css_libcss::compute_with_subresources(&doc, &fixture_base(), VIEWPORT);
    (doc, styles)
}

fn shape(doc: &Document, styles: &[Style]) -> Layout {
    let _serialised = engine_lock();
    layout::layout(doc, styles, VIEWPORT)
}

fn block<'a>(layout: &'a Layout, doc: &Document, selector: &str) -> &'a LayoutBox {
    let id = doc
        .query_selector(selector)
        .unwrap_or_else(|| panic!("fixture has no {selector}"));
    layout
        .boxes
        .iter()
        .find(|b| b.node == id)
        .unwrap_or_else(|| panic!("no box for {selector}"))
}

/// Width of the first text line inside a selector's block: what `--dump`
/// prints as the `#text` box width for these single-line fixtures.
fn width(layout: &Layout, doc: &Document, selector: &str) -> f64 {
    let target = block(layout, doc, selector);
    let center = target.y + target.h / 2.0;
    layout
        .boxes
        .iter()
        .filter(|b| {
            b.tag == "#text"
                && b.y + b.h / 2.0 >= target.y
                && b.y + b.h / 2.0 < target.y + target.h
        })
        .next()
        .unwrap_or_else(|| panic!("no text box inside {selector} (centre {center})"))
        .w
}

fn near(a: f64, b: f64) -> bool {
    (a - b).abs() <= TOL
}

// ---------------------------------------------------------------------------
// Generics: distinct keyword families must produce distinct metrics, and a
// stack whose first family is unknown must resolve to its generic tail.
// ---------------------------------------------------------------------------

#[test]
fn generic_families_produce_distinct_metrics() {
    let (doc, styles) = load("typography-generics.html");
    let layout = shape(&doc, &styles);

    let serif = width(&layout, &doc, ".serif");
    let sans = width(&layout, &doc, ".sans");
    let mono = width(&layout, &doc, ".mono");
    let default = width(&layout, &doc, ".default");

    assert!(near(serif, expect_width(19, SERIF_ADV)), "serif width {serif}, expected ~{}", expect_width(19, SERIF_ADV));
    assert!(near(sans, expect_width(19, SANS_ADV)), "sans width {sans}, expected ~{}", expect_width(19, SANS_ADV));
    assert!(near(mono, expect_width(19, MONO_ADV)), "monospace width {mono}, expected ~{}", expect_width(19, MONO_ADV));
    assert!(serif - sans > 10.0 && (serif - mono).abs() > 10.0 && mono - sans > 10.0,
        "generics must not collapse into one font: serif={serif} sans={sans} mono={mono}");
    assert!(near(default, sans), "the default family is sans: default={default} sans={sans}");
}

#[test]
fn stack_with_unknown_family_resolves_to_its_generic_tail() {
    let (doc, styles) = load("typography-generics.html");
    let layout = shape(&doc, &styles);

    let serif = width(&layout, &doc, ".serif");
    let sans = width(&layout, &doc, ".sans");
    let mono = width(&layout, &doc, ".mono");
    let stack_serif = width(&layout, &doc, ".stack-serif");
    let stack_sans = width(&layout, &doc, ".stack-sans");
    let stack_mono = width(&layout, &doc, ".stack-mono");

    assert!(near(stack_serif, serif),
        "\"NoSuchFont XYZ\", serif must fall through to serif: stack={stack_serif} serif={serif}");
    assert!(near(stack_sans, sans),
        "\"NoSuchFont XYZ\", sans-serif must fall through to sans: stack={stack_sans} sans={sans}");
    assert!(near(stack_mono, mono),
        "\"NoSuchFont XYZ\", monospace must fall through to monospace: stack={stack_mono} mono={mono}");
    assert!(stack_serif - stack_sans > 10.0,
        "the three stacks must not collapse into one font: serif={stack_serif} sans={stack_sans} mono={stack_mono}");
}

// ---------------------------------------------------------------------------
// Stacks: first match wins, unknown names are skipped (quoted and unquoted,
// multi-word names intact), an exhausted stack uses the sans default.
// ---------------------------------------------------------------------------

#[test]
fn unknown_named_family_falls_through_to_the_next_candidate() {
    let (doc, styles) = load("typography-stack.html");
    let layout = shape(&doc, &styles);

    let serif = width(&layout, &doc, ".serif");
    let sans = width(&layout, &doc, ".sans");
    let quoted = width(&layout, &doc, ".quoted-serif");
    let unquoted = width(&layout, &doc, ".unquoted-serif");
    let missing = width(&layout, &doc, ".missing-only");

    assert!(near(quoted, serif),
        "quoted unknown family then serif: quoted={quoted} serif={serif}");
    assert!(near(unquoted, serif),
        "unquoted unknown family then serif: unquoted={unquoted} serif={serif}");
    assert!(near(missing, sans),
        "an exhausted stack falls back to the sans default: missing={missing} sans={sans}");
}

#[test]
fn named_family_with_spaces_matches_whether_quoted_or_not() {
    let (doc, styles) = load("typography-stack.html");
    let layout = shape(&doc, &styles);

    let serif = width(&layout, &doc, ".serif");
    let sans = width(&layout, &doc, ".sans");
    let quoted = width(&layout, &doc, ".quoted-named");
    let unquoted = width(&layout, &doc, ".unquoted-named");
    let spaced = width(&layout, &doc, ".spaced-named");

    assert!(near(quoted, serif), "quoted family: {quoted} vs serif {serif}");
    assert!(near(unquoted, serif), "unquoted family: {unquoted} vs serif {serif}");
    assert!(near(quoted, unquoted), "quote style must not change the match");
    assert!(near(spaced, sans),
        "\"T10 Sans Test\", serif keeps its spaces and matches: {spaced} vs sans {sans}");
}

// ---------------------------------------------------------------------------
// @font-face: the declared family registers through the engine pipeline.
// ---------------------------------------------------------------------------

#[test]
fn font_face_registers_the_declared_family() {
    let (doc, styles) = load_with_fonts("typography-fontface.html");
    let layout = shape(&doc, &styles);

    let web = width(&layout, &doc, ".web");
    let fallback = width(&layout, &doc, ".fallback");
    let expected_web = expect_width(21, WEB_ADV);
    let expected_fallback = expect_width(21, SANS_ADV);

    assert!(near(web, expected_web),
        "@font-face must render with the declared family: width {web}, expected ~{expected_web}");
    assert!(web > fallback * 1.8,
        "the webfont must clearly differ from the fallback: web={web} fallback={fallback}");
    assert!(near(fallback, expected_fallback),
        "the control line keeps the named fallback: {fallback} vs ~{expected_fallback}");
}

#[test]
fn font_face_blocks_never_reach_the_cascade_and_following_rules_still_apply() {
    let (doc, styles) = load_with_fonts("typography-fontface-unsupported.html");
    // The rules written after the @font-face blocks still cascade: stripping
    // the at-rule must not eat the rest of the sheet.
    let p = &styles[doc.query_selector("p").unwrap()];
    assert!(
        p.color.r > 0.9 && p.color.g < 0.1 && p.color.b < 0.1,
        "p keeps the author's red from a rule after @font-face, got {:?}",
        p.color
    );

    let layout = shape(&doc, &styles);
    let rendered: Vec<f64> = layout
        .boxes
        .iter()
        .filter(|b| b.tag == "#text")
        .map(|b| b.w)
        .collect();
    assert!(rendered.len() >= 2 && rendered.iter().all(|w| *w > 0.0),
        "both paragraphs render their text, got widths {rendered:?}");
}

#[test]
fn unsupported_font_face_src_warns_on_stderr_and_the_page_still_renders() {
    ensure_fontconfig();
    let output = Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .arg("--dump")
        .arg(fixture_path("typography-fontface-unsupported.html"))
        .output()
        .expect("run browser-lite --dump");

    assert!(
        output.status.success(),
        "--dump exit: {:?} stderr: {}",
        output.status.code(),
        String::from_utf8_lossy(&output.stderr)
    );
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(
        stderr.contains("browser-lite: skipping @font-face") && stderr.contains("Woff2 Only"),
        "an all-woff2 @font-face degrades with one warning, got stderr: {stderr:?}"
    );
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(
        stdout.contains("LAYOUT "),
        "the page still lays out, got no LAYOUT line in stdout"
    );
}

// ---------------------------------------------------------------------------
// PRESERVE: the UA sheet's monospace for pre/code and the sans default.
// ---------------------------------------------------------------------------

#[test]
fn pre_and_code_keep_the_uas_monospace_family() {
    let (doc, styles) = load("typography-mono.html");

    // Cascade pin: the UA rule reaches both elements' computed style. The
    // Debug form is stable across `font_family` representations (String
    // before T10, list afterwards).
    for selector in ["pre", "code"] {
        let id = doc.query_selector(selector).unwrap_or_else(|| panic!("no {selector}"));
        let shown = format!("{:?}", styles[id].font_family).to_ascii_lowercase();
        assert!(
            shown.contains("monospace"),
            "{selector} keeps the UA sheet's monospace family, got {shown:?}"
        );
    }

    // Rendering pin: a block that says monospace measures the same as the
    // UA-sheet `pre`, and clearly wider than the sans control at these
    // glyph counts (103.1 vs 75.0 px).
    let layout = shape(&doc, &styles);
    let explicit = width(&layout, &doc, ".explicit");
    let stack = width(&layout, &doc, ".stack");
    let sans = width(&layout, &doc, ".sans");
    let pre = width(&layout, &doc, "pre");

    assert!(near(explicit, expect_width(12, MONO_ADV)),
        "explicit monospace width {explicit}, expected ~{}", expect_width(12, MONO_ADV));
    assert!(near(explicit, stack), "monospace stack tail: explicit={explicit} stack={stack}");
    assert!(near(explicit, pre), "the UA pre: explicit={explicit} pre={pre}");
    assert!(explicit - sans > 10.0,
        "monospace must not collapse into sans: explicit={explicit} sans={sans}");
    // The inline <code> renders inside its paragraph (which defaults to
    // sans); its own computed family is pinned above.
    assert!(width(&layout, &doc, ".inline-code") > 0.0, "inline code renders");
}

// ---------------------------------------------------------------------------
// --dump end to end: the CLI (child process, same private fontconfig) shows
// the resolved metrics as box widths.
// ---------------------------------------------------------------------------

#[test]
fn dump_shows_the_resolved_family_metrics() {
    ensure_fontconfig();
    let output = Command::new(env!("CARGO_BIN_EXE_browser-lite"))
        .arg("--dump")
        .arg(fixture_path("typography-generics.html"))
        .env("FONTCONFIG_FILE", fontconfig_conf())
        .output()
        .expect("run browser-lite --dump");
    assert!(
        output.status.success(),
        "--dump exit: {:?} stderr: {}",
        output.status.code(),
        String::from_utf8_lossy(&output.stderr)
    );

    // Seven identical strings in document order: serif, sans, mono, default,
    // then the three stacks with an unknown first family.
    let widths: Vec<f64> = String::from_utf8_lossy(&output.stdout)
        .lines()
        .filter(|line| {
            line.starts_with("  [") && line.contains("#text \"Hamburgefonstiv 123\"")
        })
        .map(|line| {
            line.split('[')
                .nth(1)
                .and_then(|rest| rest.split(']')
                    .next()
                    .and_then(|rect| rect.split_whitespace().nth(2))
                    .and_then(|w| w.parse::<f64>().ok()))
                .unwrap_or_else(|| panic!("no width in {line:?}"))
        })
        .collect();
    assert_eq!(widths.len(), 7, "seven styled lines in the dump: {widths:?}");

    let [serif, sans, mono, default, stack_serif, stack_sans, stack_mono] =
        widths.as_slice() else {
            panic!("expected seven widths, got {widths:?}")
        };
    assert!((serif - sans).abs() > 10.0 && (serif - mono).abs() > 10.0 && (mono - sans).abs() > 10.0,
        "generic keywords resolve to different fonts in --dump: {widths:?}");
    assert!(near(*default, *sans), "default == sans in --dump: {default} vs {sans}");
    assert!(near(*stack_serif, *serif), "stack tail serif in --dump: {stack_serif} vs {serif}");
    assert!(near(*stack_sans, *sans), "stack tail sans in --dump: {stack_sans} vs {sans}");
    assert!(near(*stack_mono, *mono), "stack tail monospace in --dump: {stack_mono} vs {mono}");
}
