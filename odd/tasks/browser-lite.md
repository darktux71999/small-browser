# browser-lite

Objetivo: navegador desktop ligero desde cero, reutilizando el stack KISS
instalado. Decisión de lenguaje: **Rust** (FFI fino a lexbor/QuickJS; Go se
descarta por el coste de cgo en el camino caliente DOM/JS).

- Branch: main (repo local nuevo, sin remoto)
- Test runner: `cargo test` + `--dump` determinista
- Stack: rust 1.99, quickjs-ng 0.17 (libqjs), lexbor 3.0.1, cairo 1.18.6
- Reuso del sistema: sdl3, curl, sqlite, pango/freetype, seccomp

## Specs

- S1: `browser-lite --dump file.html` parsea HTML con lexbor y emite el árbol
  DOM + cajas de layout de forma determinista a stdout.
- S2: el parser HTML respeta HTML5 (elementos implícitos `html/head/body`,
  cierre automático de `<p>`), delegando en lexbor, no en un parser propio.
- S3: la cascada CSS aplica especificidad `(id, class, tag)`, orden de origen
  (UA < autor < inline) y `!important`; soporta `display`, `margin`,
  `padding`, `width`, `height`, `color`, `background`, `font-size`.
- S4: layout block flow: cada caja block ocupa el ancho del contenedor menos
  márgenes, apila verticalmente y respeta padding; el texto se envuelve por
  ancho disponible.
- S5: `browser-lite --png out.png file.html` pinta el layout con cairo
  (rectángulos + texto) y escribe un PNG válido.
- S6: los `<script>` inline se evalúan con QuickJS en orden; `console.log`
  escribe en stdout; un error JS se reporta y no aborta el render.
- S7: `cargo test` cubre S1-S6 con fixtures en `test/data`.

## Tasks

- T1 [S1,S2] inline — DOM + lexbor FFI — DONE
- T2 [S3] inline — parser CSS subset + cascada — DONE
- T3 [S4] inline — layout block — DONE
- T4 [S5] inline — paint cairo + PNG + ventana SDL3 — DONE
- T5 [S6] inline — QuickJS runtime — DONE
- T6 [S7] inline — 7 tests + fixtures demo/nojs — DONE

## Log

L1 (usuario, literal): "quiero implementar un navegador desde cero ligero hay
que elegir los componentes correctos por ejemplo me gustaria usar quickjs como
motor es pequeno y moderno hace falta el resto de componentes y que tecnologias
serian buenas a escojer a llenar huecos vamos revisando esta parte y como seria
la estructuta"

L2: objetivo MVP = Desktop ligero. Stack = Opción A (quickjs + lexbor, resto
reuso KISS). Implementado paquetes KISS `extra/quickjs` y `extra/lexbor`,
instalados correctamente.

L3: usuario autoriza Go o Rust "el que mejor convenga". Decisión Rust: cgo
descarta Go en el hot path; Rust con FFI fino evita duplicar QuickJS/lexbor y
mantiene el binario ligero.

L4: desviación consciente — el parser CSS de esta primera slice es propio
(subset) en vez de lexbor/css, porque el binding de lexbor/css exige layout de
structs inestable y no bloquea el hito; el módulo `css` mantiene la frontera
para sustituirlo por lexbor/css sin tocar layout.

L5 (evidencia): `cargo test` 7/7 verdes. `--dump` muestra árbol HTML5 real
(html/head/body implícitos) y cajas block apiladas. `--png` produce PNG
1000x328 con h1 verde, `#lead` rojo por !important y card con padding.
Ventana SDL3 abierta en DISPLAY=:1 y verificada por screenshot.
Hallazgos: quickjs-ng 0.17 usa JSValue de 16 bytes por valor (no puntero
etiquetado) y JS_Eval exige fuente NUL-terminada aunque reciba longitud; ambos
documentados en `src/sys.rs` y `src/js.rs`.
