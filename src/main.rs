use std::path::{Path, PathBuf};
use std::process::ExitCode;

use browser_lite::{css_libcss, dom, js, layout, paint, shell};
use dom::{Document, NodeId};

/// Layout width, also the viewport that `@media` is resolved against.
const VIEWPORT_WIDTH: f64 = 1000.0;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let mut png: Option<PathBuf> = None;
    let mut file: Option<PathBuf> = None;
    let mut dump = false;
    let mut frames: Option<u32> = None;

    let mut i = 0;
    while i < args.len() {
        match args[i].as_str() {
            "--png" => {
                i += 1;
                png = args.get(i).map(PathBuf::from);
            }
            "--dump" => dump = true,
            "--frames" => {
                i += 1;
                frames = args.get(i).and_then(|v| v.parse().ok());
            }
            "--help" | "-h" => {}
            other => file = Some(PathBuf::from(other)),
        }
        i += 1;
    }

    let Some(file) = file else {
        eprintln!("usage: browser-lite [--dump] [--png out.png] file.html");
        return ExitCode::from(2);
    };

    let html = match std::fs::read_to_string(&file) {
        Ok(html) => html,
        Err(err) => {
            eprintln!("browser-lite: {}: {err}", file.display());
            return ExitCode::from(1);
        }
    };

    let doc = Document::parse(&html);
    if dump {
        dump_dom(&doc);
    }

    let sheets = css_libcss::stylesheets(&doc);
    let styles = css_libcss::compute(&doc, &sheets, VIEWPORT_WIDTH);

    let title = doc
        .query_selector("title")
        .map(|id| doc.text_content(id))
        .unwrap_or_default();

    let mut runtime = match js::JsRuntime::new(&title) {
        Ok(rt) => rt,
        Err(err) => {
            eprintln!("browser-lite: {err}");
            return ExitCode::from(1);
        }
    };
    for (n, script_id) in doc.query_selector_all("script").into_iter().enumerate() {
        let source = doc.text_content(script_id);
        if source.trim().is_empty() {
            continue;
        }
        if let Err(err) = runtime.eval(&source, &format!("inline-{n}.js")) {
            eprintln!("browser-lite: js error: {err}");
        }
    }

    let layout = layout::layout(&doc, &styles, VIEWPORT_WIDTH);

    match png {
        Some(path) => {
            if let Err(err) = paint::render_to_png(&layout, &path) {
                eprintln!("browser-lite: paint failed: {err}");
                return ExitCode::from(1);
            }
            println!(
                "wrote {} ({}x{})",
                path.display(),
                layout.width,
                layout.height.ceil()
            );
        }
        None if dump => dump_layout(&layout),
        None => {
            let frame = match paint::render(&layout) {
                Ok(frame) => frame,
                Err(err) => {
                    eprintln!("browser-lite: paint failed: {err}");
                    return ExitCode::from(1);
                }
            };
            if let Err(err) = shell::run(&frame, &runtime.title(), frames) {
                eprintln!("browser-lite: window failed: {err}");
                return ExitCode::from(1);
            }
        }
    }

    if dump {
        println!("title: {}", runtime.title());
    }
    ExitCode::SUCCESS
}

fn dump_dom(doc: &Document) {
    println!("DOM");
    print_node(doc, doc.root, 0);
}

fn print_node(doc: &Document, id: NodeId, depth: usize) {
    let node = &doc.nodes[id];
    let pad = "  ".repeat(depth);
    if node.is_text {
        let text = node.text.trim();
        if !text.is_empty() {
            println!("{pad}#text {text:?}");
        }
        return;
    }
    let id_attr = node.attr("id").map(|v| format!("#{v}")).unwrap_or_default();
    let class_attr = node
        .attr("class")
        .map(|v| format!(".{}", v.replace(' ', ".")))
        .unwrap_or_default();
    println!("{pad}{}{id_attr}{class_attr}", node.tag);
    for &child in &node.children {
        print_node(doc, child, depth + 1);
    }
}

fn dump_layout(layout: &layout::Layout) {
    println!("LAYOUT {}x{}", layout.width, layout.height.ceil());
    for b in &layout.boxes {
        let label = match &b.text {
            Some(text) => format!("#text {text:?}"),
            None => b.tag.clone(),
        };
        println!(
            "  [{:6.1} {:6.1} {:6.1} {:6.1}] {}",
            b.x, b.y, b.w, b.h, label
        );
    }
}

#[allow(dead_code)]
fn _path_hint(_: &Path) {}
