# css-libcss — libcss pasa a ser la capa de parse+cascada del motor

Objetivo: ruta B elegida por el usuario: el motor calcula estilos con libcss
(NetSurf, MIT) contra su propio DOM, con hoja UA mínima y viewport dentro de la
cascada; después, migrar el texto a Pango. Branch `feat/css-libcss`; runner:
`cargo test` (motor) + `cargo test` (spikes/css, que comparte `css.rs`).
Mirror Engram: pendiente (la herramienta no está disponible en este runtime).

## Specs

- **S1** — El usuario eligió "B: libcss al motor (Recomendado)": "Primer paso:
  hoja UA mínima + viewport en la cascada, portar libcss_ffi del spike a src/ como
  módulo del motor y empaquetar las 3 libs MIT como recipes KISS (lexbor y quickjs
  ya los empaquetas tú). Luego Pango."
- **S2** — Recipes KISS para `libwapcaplet`, `libparserutils` y `libcss` (MIT) con
  las convenciones de `extra/lexbor` (version, sources, checksums, build, depends),
  construibles con `kiss b` e instalables con `kiss i`; el motor las enlaza desde
  el sistema (static + headers + .pc). `netsurf-buildsystem` como dependencia de
  build.
- **S3** — El motor calcula estilos con libcss: port de `spikes/css/src/libcss_ffi.rs`
  a `src/` como módulo, con **hoja UA mínima propia** (display por etiqueta,
  márgenes/tamaños por defecto — libcss no trae ninguna) y **viewport dentro de la
  cascada** (las `@media` se resuelven contra el ancho del viewport de la ventana,
  no contra 800×600 fijos).
- **S4** — `css::Style` se amplía con tipografía (font_family, font_weight, italic,
  line_height, text_align, letter_spacing) **sin romper la API existente**:
  `css::parse`/`css::compute` siguen con su firma y comportamiento (el spike los
  usa y debe seguir verde).
- **S5** — El comportamiento observable de los comandos existentes se mantiene
  salvo en los valores de estilo (que cambian a propósito): differential check de
  `--dump`/`--png` sobre `test/data/` contra una copia de la base en scratch; toda
  diferencia estructural que nadie pidió es regresión.
- **S6** — Después: migrar el texto del motor a Pango (interlineado, justificación,
  tracking, `@font-face` vía fontconfig), reutilizando `spikes/css/src/render.rs`
  como referencia.

## Tasks

| ID | Qué | Specs | Ruta | Estado |
|---|---|---|---|---|
| T1 | Recipes KISS (netsurf-buildsystem + libwapcaplet + libparserutils + libcss) validadas con `kiss b`; instala el padre | S2 | delegated | ✅ recipes en `sprint/extra` (sin commitear) + 4 paquetes instalados en el sistema |
| T2 | Port `libcss_ffi` → `src/` + hoja UA + viewport en cascada + `css::Style` ampliado + wiring + tests + differential check | S3,S4,S5 | delegated | ✅ cfafc50 (56 tests, differential 0 diffs estructurales) |
| T3 | Pango: migrar layout/paint del motor (usa el renderer del spike como referencia) | S6 | pendiente | siguiente corte |
| T4 | Docs + mediciones del motor real (RSS/fidelidad con el harness de `spikes/css/compare.sh`) | S1 | pendiente | |

## Log

- **L1** (2026-10-22): el usuario eligió "B: libcss al motor (Recomendado)" —
  "Primer paso: hoja UA mínima + viewport en la cascada, portar libcss_ffi del
  spike a src/ como módulo del motor y empaquetar las 3 libs MIT como recipes KISS
  (lexbor y quickjs ya los empaquetas tú). Luego Pango." Contexto: recomendación
  basada en `docs/css-spike.md` (memoria empatada; libcss gana fidelidad en 3/5,
  sobre todo `@media`; css.rs gana en LOC/KISS) y en la arquitectura del proyecto
  (cada capa es un componente de una sola responsabilidad: lexbor, quickjs, cairo,
  pango, curl, sqlite, seccomp).
- Decisión técnica aceptada: las 3 libs se empaquetan **static** (misma enlazada ya
  verificada por el spike) + headers + .pc; `netsurf-buildsystem` como dependencia
  de build. El FFI del motor acepta `CSS_LIB_PREFIX` para desarrollo antes del
  `kiss i` del padre.
- Novedades del entorno descubiertas aquí: `busybox install` no soporta `-C` (el
  buildsystem de NetSurf lo usa; shim necesario), y el build de estas libs exige
  `CC='cc -std=gnu17'` + `WARNFLAGS='-Wall -Wno-error'` con clang 22.
- **L2** (2026-10-22): T1 y T2 completados por workers acotados. T1: 4 recipes
  KISS (checksums blake3 de 66 hex, no sha512 como decía el brief) validadas con
  `kiss b`; efecto secundario documentado: `kiss b` con stdin no legible auto-elevó
  vía `doas` e instaló buildsystem+libwapcaplet — decisión: no deshacer, completar
  la instalación (`kiss i libparserutils libcss`); sistema final con libcss 0.9.2,
  libparserutils 0.2.5, libwapcaplet 0.4.3 (static+headers+.pc) y netsurf-buildsystem
  1.10. Recipes copiadas a `~/proyectos/sprint/extra/` sin commitear (repo ajeno en
  rama activa, lo commitea el usuario). T2: port + hoja UA + @media contra el
  viewport (1000px de layout) + `css::Style` tipográfico aditivo (commit cfafc50);
  differential check sobre 29 fixtures: 0 diffs de DOM/cajas/exit/stderr, solo
  valores de estilo (a propósito). Bug real encontrado y arreglado: los refcounts
  de libwapcaplet no son atómicos → selección de estilo serializada con mutex.
  Seam check del padre: motor enlazando desde /usr/lib sin `CSS_LIB_PREFIX`, suites
  del motor (51+) y del spike (1) verdes.
