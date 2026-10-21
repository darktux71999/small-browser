//! Smoke tests for the MVP slice: HTML parse, cascade, layout, JS runtime.

use browser_lite::{css, dom::Document, js::JsRuntime, layout};

#[test]
fn dom_builds_html5_tree() {
    let doc = Document::parse("<!doctype html><title>t</title><h1>Hi</h1><p>x</p>");
    assert!(doc.query_selector("html").is_some());
    assert!(doc.query_selector("head").is_some());
    assert!(doc.query_selector("body").is_some());
    let h1 = doc.query_selector("h1").unwrap();
    assert_eq!(doc.text_content(h1), "Hi");
}

#[test]
fn cascade_honours_specificity_and_important() {
    let doc = Document::parse("<style>p{color:black} #x{color:blue}</style><p id=x>x</p>");
    let source = doc
        .query_selector_all("style")
        .into_iter()
        .map(|id| doc.text_content(id))
        .collect::<String>();
    let sheet = css::Stylesheet::parse(&source);
    let styles = css::compute(&doc, &sheet);
    let id = doc.query_selector("#x").unwrap();
    assert_eq!(styles[id].color.b, 1.0);
}

#[test]
fn layout_stacks_blocks_vertically() {
    let doc = Document::parse("<body><p>one</p><p>two</p></body>");
    let sheet = css::Stylesheet::parse("");
    let styles = css::compute(&doc, &sheet);
    let layout = layout::layout(&doc, &styles, 800.0);
    let paragraphs: Vec<_> = layout
        .boxes
        .iter()
        .filter(|b| b.tag == "p")
        .collect();
    assert_eq!(paragraphs.len(), 2);
    assert!(paragraphs[1].y > paragraphs[0].y);
}

#[test]
fn important_beats_higher_specificity() {
    let doc = Document::parse(
        "<style>p{color:blue !important} #x{color:red}</style><p id=x>x</p>",
    );
    let source = doc
        .query_selector_all("style")
        .into_iter()
        .map(|id| doc.text_content(id))
        .collect::<String>();
    let sheet = css::Stylesheet::parse(&source);
    let styles = css::compute(&doc, &sheet);
    let id = doc.query_selector("#x").unwrap();
    assert_eq!(styles[id].color.b, 1.0);
}

#[test]
fn paint_produces_argb_frame() {
    let doc = Document::parse("<style>p{background:red}</style><p>hi</p>");
    let sheet = css::Stylesheet::parse(
        &doc.query_selector_all("style")
            .into_iter()
            .map(|id| doc.text_content(id))
            .collect::<String>(),
    );
    let styles = css::compute(&doc, &sheet);
    let layout = layout::layout(&doc, &styles, 200.0);
    let frame = browser_lite::paint::render(&layout).expect("render");
    assert_eq!(frame.width, 200);
    assert_eq!(frame.pixels.len(), (frame.width * frame.height * 4) as usize);
}

#[test]
fn js_runtime_evaluates_and_mutates_title() {
    let mut rt = JsRuntime::new("start").unwrap();
    rt.eval("console.log('hello', 2 + 2); document.title = 'done';", "t.js")
        .unwrap();
    assert_eq!(rt.title(), "done");
}

#[test]
fn js_errors_are_reported_not_fatal() {
    let mut rt = JsRuntime::new("t").unwrap();
    assert!(rt.eval("this is not js", "bad.js").is_err());
    assert!(rt.eval("1 + 1", "ok.js").is_ok());
}
