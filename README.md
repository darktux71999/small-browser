# browser-lite

Navegador desktop ligero, desde cero, sobre el stack KISS ya instalado.

## Stack

| Pieza | Elección | Estado |
|---|---|---|
| Lenguaje | Rust 1.99 (musl) | instalado |
| JS engine | quickjs-ng 0.17 (`libqjs`) | instalado (paquete propio) |
| HTML5 + DOM | lexbor 3.0.1 | instalado (paquete propio) |
| CSS | parser subset propio + cascada | `src/css.rs` |
| Layout | block flow propio | `src/layout.rs` |
| Paint | cairo 1.18.6 | sistema |
| Ventana | SDL3 3.4.18 (FFI directo) | sistema |
| Red | curl (siguiente corte) | sistema |
| Storage | sqlite (siguiente corte) | sistema |
| Sandbox | libseccomp/bubblewrap (siguiente corte) | sistema |

Go se descartó: cgo en el límite DOM/JS es el camino caliente y encarece cada
acceso. Rust con FFI fino enlaza las librerías del sistema sin duplicarlas.

## Uso

```sh
cargo build
./target/debug/browser-lite test/data/demo.html        # ventana
./target/debug/browser-lite --dump test/data/demo.html # DOM + cajas
./target/debug/browser-lite --png out.png test/data/demo.html
```

## Estructura

```
src/dom.rs     árbol arena construido por lexbor + querySelector
src/css.rs     parser CSS subset, especificidad y cascada
src/layout.rs  block flow, wrapping de texto
src/paint.rs   cairo -> ARGB / PNG
src/shell.rs   ventana SDL3 (FFI)
src/js.rs      QuickJS: console.log, document.title, errores
src/sys.rs     FFI de liblexbor y libqjs
```

Regla de dependencias: `shell -> paint -> layout -> css -> dom`, y `js` no
depende de UI. Nada incluye `shell` hacia abajo.

## Estado

Corte 1 completo: parseo HTML5 real, cascada con especificidad y
`!important`, layout block, pintado cairo, ventana SDL3 y QuickJS evaluando
scripts con errores no fatales. `cargo test` cubre los 7 casos del corte.

Siguiente: navegación (curl/URL/historial), inline layout con estilos por
span, y proceso por tab + seccomp.
