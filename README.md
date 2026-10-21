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

## Dos motores

- `browser-lite` — motor propio (lexbor + CSS subset + cairo + SDL3). Ligero,
  sin dependencias pesadas, útil como fallback y para `--dump/--png`.
- `blitz-lite` — render real con Blitz (Stylo + Taffy + Vello) y QuickJS
  ejecutando los scripts sobre el DOM de Blitz.

## Uso

```sh
cargo build
./target/debug/browser-lite test/data/demo.html        # ventana
./target/debug/browser-lite --dump test/data/demo.html # DOM + cajas
./target/debug/browser-lite --png out.png test/data/demo.html
cargo run --bin blitz-lite -- test/data/js-dom.html   # Blitz + QuickJS
cargo run --bin blitz-lite -- https://example.com      # carga por HTTP(S)
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

Corte 1 completo en el motor propio. Corte 2: `blitz-lite` renderiza con
Blitz y QuickJS muta el DOM (`getElementById`, `createElement`,
`appendChild`, `setAttribute`, `textContent`, `querySelector`), y eventos
`addEventListener('click')` con bubbling. 7 tests
verdes en el motor propio; el puente Blitz se verifica por captura.

Siguiente: eventos (`addEventListener`/click), timers y navegación real.
