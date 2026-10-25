# Leading / fidelidad vertical del motor Go

Objetivo: cerrar la brecha vertical de codeberg.org vs Firefox (**+1267px**,
medida L21 de `odd/tasks/meta1-codeberg.md`) con los tres fixes del diagnóstico
P9a. **Decisión del usuario (2026-10-25)**: estrategia «**Ambos, convención
rem/em**» — fix en el motor Go ahora + brief para el agente externo con el
mismo contrato en el oráculo Rust; la paridad byte a byte se restaura cuando su
rama llegue y el hueco temporal vive en `KNOWN_DIFFS.md`. Rama `feat/go-engine`;
runner `go test ./...` + runner diferencial (con divergencias documentadas).

## Specs

- **S1** — line-height CSS: el factor de `line-height` escala el **font-size**
  (CSS 2.1), no la altura natural de la fuente (ascent+descent+linegap) como hoy
  hace Pango vía factor. Fix: `pango_attr_line_height_new_absolute(round(size ×
  lineHeight))` como **helper único compartido** por `go/text` y `go/paint` (el
  attr está hoy duplicado). Evidencia P9a: **−457px** en vivo (2936→2479 en el
  experimento copy-patch); tests con multiplicación entera (20px×1.2=24;
  16px×1.5=24); el primer run acepta `pos.y` negativo (half-leading CSS, −1.9
  medido) — actualizar la aserción `TestMeasureSingleLine`. El oráculo comparte
  el bug (`src/layout.rs:1887-1890`).
- **S2** — política `<noscript>`: los `<style>` dentro de `<noscript>` NO se
  recogen como CSS de autor ni sus contenidos se pintan (Firefox con JS ni los
  aplica ni los muestra; el banner fantasma ToU cuesta ~200px). El parseo DOM de
  `noscript` como markup se conserva (paridad con el oráculo, fix P1).
- **S3** — colapso de márgenes verticales: implementar el colapso CSS (hermanos
  adyacentes, padre/hijo primero/último, bloques vacíos) en el formato de
  bloque; ~150–230px recuperables (378px netos medidos). flex/grid no colapsan
  (CSS).
- **S4** — cierre de medición: repetir la banda S7 con el método fijado (L20);
  objetivo bandas 3–10 < 20% y la brecha de altura acercándose a los ~300–400px
  residuales que el diagnóstico predice (wraps, ceil por run, estiramientos).

## Tasks

| T | Descripción | Spec | Ruta | Estado |
|---|---|---|---|---|
| T1 | line-height absoluto (helper único text+paint) + KNOWN_DIFFS | S1 | delegado (ling-3.1-flash) | ✅ (179+108 subtests en verde; vivo 1000x2479 = −457px; differential 5 idénticos registrados) |
| T2 | política noscript (colección + pintado) | S2 | delegado (qwen3.8-flash) | ✅ (187+113 subtests verdes; vivo 1000x2317 = −162px; banner verificado ausente) |
| T3 | colapso de márgenes verticales | S3 | delegado | pendiente |
| T4 | re-medición S7 + informe | S4 | medición | pendiente |

## Log

- **L1** (2026-10-25): origen: diagnóstico **P9a** (muse-spark) — tres causas
  cuantificadas de la brecha de 1267px: (1) factor de line-height de Pango
  escala la altura natural de la fuente, no el font-size (−457px; oráculo lo
  comparte en src/layout.rs:1887-1890); (2) `<style>` dentro de `<noscript>`
  recogido como CSS de autor → banner fantasma (~200px; oráculo lo comparte);
  (3) márgenes verticales sin colapsar en ningún sitio (378px netos, ~150-230
  recuperables; oráculo lo comparte). El exceso de footer es las mismas causas.
  Presupuesto: ~860-950 de 1267; residuo ~300-400 (ceil por run, wraps,
  paddings, estiramiento de flex). **Blast radius**: 66/71 dumps del corpus
  cambian (solo border-radius, img-attrs, img-clamp, js-dom, net-latin1 se
  mantienen). **Decisión del usuario** ante la pregunta de estrategia de
  paridad: **«Ambos, convención rem/em (Recomendado)»**.
- **L2** (2026-10-25): **S1 aterrizada** (T1, ling-3.1-flash): line-height
  absoluto vía helper único `text.LineHeightAbsolute` (go/text/lineheight.go)
  compartido por shaper (`Measure`) y paint (`showRun`) — las dos inserciones
  duplicadas del attr ahora usan `pango_attr_line_height_new_absolute`. Pines
  integrales (20px×1.2=24, 16px×1.5=24, 16px×1.0=16) + el half-leading negativo
  del primer run (−1.8994140625 / −2 pinneados); `TestMeasureSingleLine`
  actualizado. **Efecto vivo: `LAYOUT 1000x2479` (−457px exactos sobre 2936)**.
  Differential: **66/71 salidas cambian por diseño**, los 5 registrados se
  mantienen (border-radius, img-attrs, img-clamp, net-latin1 idénticos +
  js-dom solo-stderr sin cambios) — coincide con el blast radius de L1 y con el
  registro KNOWN_DIFFS (categoría «line-height semantics»). Golden
  `TestDumpHelloExact` re-baselineado tras la escalación del worker (opción 1
  del usuario: superficie extendida a `go/cmd/browser-lite/main_test.go`; ningún
  otro test pinnea ese dump). Árbol completo verde: 179 tests + 108 subtests,
  10/10 paquetes; vet/gofmt limpios. **Residual registrado**: `normal`
  font-dependiente (Firefox ~1.40em para Noto vs el default 1.2 del motor) queda
  como ajuste futuro — S1 sólo fija la semántica del factor. Pines y dependiente
  de métricas de fuente instalada (Noto Sans): y-pins; alturas son
  font-independientes por construcción. `Risk: none`.
- **L3** (2026-10-25): **S2 aterrizada** (T2, qwen3.8-flash). Regla única de
  ancestros `css.HasNoscriptAncestor` (go/css/compute.go:73, usada en
  Stylesheets():58 y sheets.go:87) + regla única de salto `isNoscript`
  (go/layout/layout.go:265) en los DOS puntos de paso compartidos:
  `layoutNode` (:141) y `collectInlineText` (:409) — este segundo era necesario
  porque el texto inline nunca llega a `layoutNode` (probado en RED: dos modos
  de fuga). Los `<style>` dentro de `<noscript>` no se recogen; las subramas de
  `<noscript>` no generan cajas ni runs en ningún contexto. El parseo DOM se
  conserva intacto (pines P1 verdes). **Efecto vivo: `LAYOUT 1000x2317` (−162px
  sobre 2479)**; el banner fantasma está verificablemente ausente (cero cajas,
  cero texto ToU tras el header LAYOUT; nav arranca en y=0). Corpus sin
  fixtures con noscript (grep: 0 de 71) → el differential queda **idéntico al
  estado registrado de T1** (4 idénticos + js-dom solo-stderr). KNOWN_DIFFS:
  categoría «noscript policy» añadida, la de line-height intacta. 8 tests nuevos
  con secuencia RED de dos etapas (helper correcto + regla sin cablear);
  187+113 subtests, 10/10 paquetes; vet/gofmt limpios. Nota: el costo real del
  banner fue −162px (predicción −178/−200) — el residuo se cuantifica en T4.
  `Risk: none`.
