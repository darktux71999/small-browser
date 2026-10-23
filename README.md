# browser-lite

Navegador desktop ligero, desde cero, sobre el stack KISS ya instalado.

## Stack

| Pieza | Elección | Estado |
|---|---|---|
| Lenguaje | Rust 1.99 (musl) | instalado |
| JS engine | quickjs-ng 0.17 (`libqjs`) | instalado (paquete propio) |
| HTML5 + DOM | lexbor 3.0.1 | instalado (paquete propio) |
| CSS | libcss 0.9.2 (NetSurf, MIT): parse + select + cascada | `src/css_libcss.rs`; `src/css.rs` legado |
| Layout | block flow propio | `src/layout.rs` |
| Paint | cairo 1.18.6 | sistema |
| Ventana | SDL3 3.4.18 (FFI directo) | sistema |
| Red | curl (siguiente corte) | sistema |
| Storage | sqlite (siguiente corte) | sistema |
| Sandbox | libseccomp/bubblewrap (siguiente corte) | sistema |

Go se descartó: cgo en el límite DOM/JS es el camino caliente y encarece cada
acceso. Rust con FFI fino enlaza las librerías del sistema sin duplicarlas.

## Motor

Motor propio sobre el stack KISS del sistema (lexbor + libcss + cairo + SDL3),
con QuickJS ejecutando los scripts sobre el DOM propio. El experimento con el
motor Blitz (Stylo + Taffy + Vello) se retiró: costaba ~180MB por pestaña al
traer el stack completo de un navegador moderno. La capa de CSS es libcss 0.9.2
(NetSurf, MIT) mediante FFI fino: parse, selectores, especificidad, herencia y
`@media` resueltas contra el ancho real del viewport, más una hoja UA mínima
propia porque libcss no trae ninguna. `src/css.rs` queda como subset legado que
usa el spike comparativo (`spikes/css/`). Las tres libs estáticas
(`libcss`, `libparserutils`, `libwapcaplet`) se enlazan con `build.rs`; durante
el desarrollo `CSS_LIB_PREFIX` apunta a un prefijo privado.

## Uso

```sh
cargo build
./target/debug/browser-lite test/data/demo.html        # ventana
./target/debug/browser-lite --dump test/data/demo.html # DOM + cajas
./target/debug/browser-lite --png out.png test/data/demo.html
# la ventana abre un viewport fijo de 1000x760 con scroll: ↑/↓, PgUp/PgDn,
# Home/End y j/k/g/G (vi). `--png` sigue capturando el documento completo.
```

## Estructura

```
src/dom.rs     árbol arena construido por lexbor + querySelector
src/css_libcss.rs  FFI libcss 0.9.2: parse, select, cascada, hoja UA, @media
src/css.rs     subset CSS legado (lo usa el spike y sirve de referencia)
src/layout.rs  block flow, wrapping de texto
src/paint.rs   cairo -> ARGB / PNG
src/shell.rs   ventana SDL3 (FFI)
src/js.rs      QuickJS: console.log, document.title, errores
src/sys.rs     FFI de liblexbor y libqjs
```

Regla de dependencias: `shell -> paint -> layout -> css -> dom`, y `js` no
depende de UI. Nada incluye `shell` hacia abajo.

## Estado

Corte 1 completo en el motor propio: lexbor parsea HTML5, libcss calcula los
estilos (hoja UA mínima incluida), `layout.rs` hace block flow y `paint.rs`
pinta con cairo (PNG o ventana SDL3). QuickJS muta el DOM (`getElementById`,
`createElement`, `appendChild`, `setAttribute`, `textContent`, `querySelector`)
y hay eventos `addEventListener('click')` con bubbling. 7 tests verdes en el
motor propio.

Módulos de aplicación listos: `tabs` (pestañas + historial), `cookies`,
`storage`, `net` (charset, redirecciones, async) y `sandbox` (seccomp +
bubblewrap). Próximo corte: migrar el texto a Pango (interlineado,
justificación, tracking, `@font-face` vía fontconfig), reutilizando
`spikes/css/src/render.rs` como referencia.
