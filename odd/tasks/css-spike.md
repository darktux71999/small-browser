# css-spike — comparativa libcss vs css.rs antes de elegir la capa de CSS

Objetivo: prototipo acotado que renderice las mismas 5 páginas con dos proveedores de
estilo (css.rs propio vs libcss de NetSurf) sobre un mismo renderer Pango/cairo, y
produzca la tabla de memoria, fidelidad y líneas de código para decidir. Branch
`spike/css-layer`; runner: `cargo test` (crate `spikes/css`) + `spikes/css/compare.sh`.

## Specs

- **S1** — "Antes de tocar el motor: prototipo acotado de libcss vs css.rs renderizando
  las mismas 5 páginas con Pango/cairo, con tabla de memoria, fidelidad y líneas de
  código." El spike NO modifica `src/`, `tools/`, `tests/` ni `test/` existentes; vive
  en `spikes/css/` y se puede borrar sin tocar el motor.
- **S2** — Dos proveedores de estilo sobre el MISMO modelo (`spikes/css/src/style.rs`),
  el mismo layout y el mismo pintor Pango/cairo: (A) `browser_lite::css` (`css::parse`
  + `css::compute`), (B) libcss 0.9.2 (`css_stylesheet_*`, `css_select_ctx_*`,
  `css_select_style`, `css_computed_style_compose`). Toda diferencia de render debe
  venir del proveedor, nunca del pipeline.
- **S3** — 5 páginas (`spikes/css/fixtures/*.html`) diseñadas para diferenciar capas de
  CSS (especificidad, herencia, @media, tipografía, mezcla realista), renderizadas con
  ambos proveedores → PNG A/B por página.
- **S4** — Tabla comparativa con: memoria (RSS máximo por render), fidelidad (mismo
  método que `docs/font-svg-fidelity.md`: referencia Firefox headless, ink share y
  diff con fuzz 25% dentro de la caja de contenido) y líneas de código (glue Rust por
  ruta + código externo que trae cada una).
- **S5** — Informe `docs/css-spike.md` con la tabla y un veredicto por criterio
  (memoria, fidelidad, LOC, mantenimiento KISS). El informe no decide por el usuario.
- **S6** — Reproducible desde cero: script que construye libcss + libparserutils +
  libwapcaplet en un prefijo fuera del worktree (`~/.cache/css-spike`) y corre la
  comparación completa. Nada dentro del worktree queda sucio.

## Tasks

| ID | Qué | Specs | Ruta | Estado |
|---|---|---|---|---|
| T1 | `spikes/css/build-libcss.sh`: netsurf-buildsystem + 3 libs en `~/.cache/css-spike`, smoke test C | S6 | inline | ✅ 0f48b9b (smoke OK) |
| T2 | Crate `spikes/css`: modelo de estilo, renderer Pango/cairo (bloques + texto), proveedor A (css.rs), 5 fixtures, bin `css-spike` | S1,S2,S3 | inline | ✅ 263d623 |
| T3 | Proveedor B (FFI libcss: stylesheet→select→compose→estilo) + test de acuerdo A/B (RED→GREEN) | S2,S3 | delegated (razón: backstop de contexto) | ✅ 263d623 (RED observado → GREEN) |
| T4 | Harness de medición: RSS por render, LOC por ruta, fidelidad vs Firefox | S4 | delegated (unidad paralela a T3, superficies disjuntas) | ✅ eb64f2a |
| T5 | Informe `docs/css-spike.md`: tabla + veredicto por criterio | S5 | inline | ✅ 73c7db5 |

## Log

- **L1** (2026-10-21): el usuario eligió la opción "Spike comparativo primero" —
  "Antes de tocar el motor: prototipo acotado de libcss vs css.rs renderizando las
  mismas 5 páginas con Pango/cairo, con tabla de memoria, fidelidad y líneas de
  código." Contexto: decisión de la capa de parse+cascada aplazada con "Estudiar las
  tres APIs primero"; informe previo en `docs/api-comparativa.md` (commit 0b3a224).
- Aceptado: el modelo de estilo del spike es propio (`SpikeStyle`), más ancho que
  `css::Style` (añade tipografía: font-family/weight/style, line-height, text-align,
  letter-spacing) para que las diferencias de capa sean visibles; ambos proveedores se
  mapean a él. El renderer es simplificado (bloques + runs de texto, sin flex ni
  inline mixto profundo); la simplificación afecta igual a ambos proveedores y se
  documenta en el informe.
- Aceptado: fixtures nuevos en `spikes/css/fixtures/` (no los de `test/data/`, que
  estaban diseñados para sondas de fuentes/SVG) para estresar especificidad, herencia,
  media queries y tipografía.
- **L2** (2026-10-21): el usuario preguntó "aun se esta usando blitz?" y ante el
  hallazgo de que el crate no compilaba sin `blitz-engine` (`nav.rs` usaba
  `blitz_traits` sin `cfg`) eligió "Borrar Blitz del repo (Recomendado)" —
  "Borrar blitz_engine.rs, los bins blitz-lite/blitz-tabs, nav.rs y las deps del
  Cargo.toml… también se limpia la caché de 1.8GB". Hecho en commit 7304050
  (desbloquea T2). S1 se mantiene: la limpieza de Blitz fue unidad aparte
  autorizada, el spike no tocó `src/`.
- T3 (delegado) corrigió el brief contra la fuente de libcss 0.9.2: `css_color`
  es AARRGGBB; el elemento raíz no se compone (componer con padre NULL →
  `CSS_BADPARM`); `inline_style=true` es para listas de declaraciones. Halló y el
  padre corrigió un bug del renderer T2 (el color de texto no se fijaba en cairo).
  `cargo test` del spike sale 101 en la fase --doc porque el toolchain no trae
  rustdoc: el crate lleva `doctest = false` como el motor.
- Resultados (ver `docs/css-spike.md`): memoria empatada (21-25MB ambos),
  fidelidad a favor de libcss en 3/5 (peor caso 13,05 vs 8,94 en @media; empate
  exacto en especificidad), LOC 579 (css.rs) vs 87.604 (libcss+glue). Hallazgo de
  diseño: libcss no trae hoja UA y las @media necesitan el viewport en la cascada.
- **L3** (2026-10-22): assess RDD del slice (base `master`): riesgo `high` con
  razones nombradas (`executable_mode` en `spikes/css/compare.sh`;
  `process_boundary`/`shell_source` en `spikes/css/build-libcss.sh`). Revisión
  ofrecida con lentes `review-risk,review-resilience,reliability`; el usuario
  eligió "Omitir esta vez" → `declined_this_candidate` (candidate-scoped; entrega
  bajo política ordinaria). Tier por commit: 0b3a224/0f48b9b/73c7db5 docs+tooling,
  7304050 refactor pedido por el usuario, 263d623+eb64f2a spike autocontenido.
