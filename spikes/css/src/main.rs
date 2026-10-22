//! CLI del spike.
//!
//!   css-spike --fixture <f.html> --provider cssrs|libcss --out <o.png> [--width N]
//!   css-spike --all --provider cssrs|libcss --out-dir <dir> [--fixtures-dir <dir>]
//!
//! Imprime una línea de resumen por render para que el harness de medición (T4)
//! la parsee.

use std::env;
use std::fs;
use std::path::PathBuf;
use std::process::exit;

use browser_lite::dom::Document;
use css_spike::provider::{stylesheets, CssRsProvider, LibcssProvider, Provider};
use css_spike::render;

const USAGE: &str = "css-spike — comparativa de proveedores de estilo (odd/tasks/css-spike.md)

USO:
  css-spike --fixture <f.html> --provider cssrs|libcss --out <o.png> [--width N]
  css-spike --all --provider cssrs|libcss --out-dir <dir> [--fixtures-dir <dir>] [--width N]

OPCIONES:
  --fixture <ruta>      página HTML a renderizar
  --all                 renderiza todos los *.html del directorio de fixtures
  --provider <nombre>   cssrs (motor propio) | libcss (NetSurf)
  --out <ruta.png>      PNG de salida (un fixture)
  --out-dir <dir>       directorio de salida (--all)
  --fixtures-dir <dir>  por defecto, fixtures/ junto al binario
  --width <px>          ancho del viewport (800 por defecto)
  --help                esta ayuda
";

struct Opts {
    fixture: Option<PathBuf>,
    all: bool,
    provider: String,
    out: Option<PathBuf>,
    out_dir: Option<PathBuf>,
    fixtures_dir: PathBuf,
    width: f64,
}

fn main() {
    let args: Vec<String> = env::args().skip(1).collect();
    if args.iter().any(|a| a == "--help" || a == "-h") {
        print!("{USAGE}");
        return;
    }
    let default_fixtures = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("fixtures");
    let mut opts = Opts {
        fixture: None,
        all: false,
        provider: String::new(),
        out: None,
        out_dir: None,
        fixtures_dir: default_fixtures,
        width: 800.0,
    };
    let mut it = args.iter();
    while let Some(arg) = it.next() {
        let mut value = |name: &str| -> String {
            it.next()
                .unwrap_or_else(|| die(&format!("falta el valor de {name}")))
                .clone()
        };
        match arg.as_str() {
            "--fixture" => opts.fixture = Some(PathBuf::from(value("--fixture"))),
            "--all" => opts.all = true,
            "--provider" => opts.provider = value("--provider"),
            "--out" => opts.out = Some(PathBuf::from(value("--out"))),
            "--out-dir" => opts.out_dir = Some(PathBuf::from(value("--out-dir"))),
            "--fixtures-dir" => opts.fixtures_dir = PathBuf::from(value("--fixtures-dir")),
            "--width" => {
                opts.width = value("--width").parse().unwrap_or_else(|_| die("--width inválido"))
            }
            other => die(&format!("opción desconocida: {other}")),
        }
    }

    let provider: Box<dyn Provider> = match opts.provider.as_str() {
        "cssrs" => Box::new(CssRsProvider),
        "libcss" => Box::new(LibcssProvider),
        "" => die("falta --provider"),
        other => die(&format!("proveedor desconocido: {other}")),
    };

    let jobs: Vec<(PathBuf, PathBuf)> = if opts.all {
        let mut files: Vec<PathBuf> = fs::read_dir(&opts.fixtures_dir)
            .unwrap_or_else(|e| die(&format!("fixtures: {e}")))
            .filter_map(|e| e.ok().map(|e| e.path()))
            .filter(|p| p.extension().map(|x| x == "html").unwrap_or(false))
            .collect();
        files.sort();
        let out_dir = opts.out_dir.unwrap_or_else(|| die("falta --out-dir"));
        fs::create_dir_all(&out_dir).unwrap_or_else(|e| die(&format!("out-dir: {e}")));
        files
            .into_iter()
            .map(|f| {
                let stem = f.file_stem().unwrap().to_string_lossy().to_string();
                let out = out_dir.join(format!("{stem}-{}.png", provider.name()));
                (f, out)
            })
            .collect()
    } else {
        let fixture = opts.fixture.unwrap_or_else(|| die("falta --fixture"));
        let out = opts.out.unwrap_or_else(|| die("falta --out"));
        vec![(fixture, out)]
    };

    for (fixture, out) in jobs {
        let html = fs::read_to_string(&fixture)
            .unwrap_or_else(|e| die(&format!("{}: {e}", fixture.display())));
        let doc = Document::parse(&html);
        let sheets = stylesheets(&doc);
        let styles = provider.compute(&doc, &sheets);
        let info = render::render_to_png(&doc, &styles, out.to_str().unwrap(), opts.width)
            .unwrap_or_else(|e| die(&format!("render: {e}")));
        println!(
            "RESULT provider={} fixture={} nodes={} sheets={} ops={} height={:.0} png={}",
            provider.name(),
            fixture.file_name().unwrap().to_string_lossy(),
            doc.nodes.len(),
            sheets.len(),
            info.ops,
            info.height,
            out.display()
        );
    }
}

fn die(msg: &str) -> ! {
    eprintln!("css-spike: {msg}");
    eprintln!("{USAGE}");
    exit(2);
}
