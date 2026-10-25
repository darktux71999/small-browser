# Custom properties y var() en el motor Go

Objetivo: desbloquear los 406 bordes de Codeberg (`border: 1px solid var(--color-*)`)
que libcss 0.9.2 descarta al parsear. **Solo motor Go** (el Rust se congela como
oráculo — divergencia intencionada documentada). Rama `feat/go-engine`; runner:
`go test ./...`. Construcción por partes siguiendo `docs/pareto.md`.

## Specs

- **S1** — Captura: el rewriter extrae las declaraciones `--*` y las que contienen
  `var(...)` ANTES de libcss (que las descarta), conservando propiedad, valor
  crudo, selector de la regla y contexto (media). El texto que llega a libcss
  pierde exactamente esas declaraciones y nada más (pin del texto reescrito).
- **S2** — Resolución: mapa de custom properties por elemento con herencia
  (padre → hijo, la propia manda), sustitución `var(--x[, fallback])` recursiva
  con fallbacks anidados, undefined sin fallback → unresolvable, ciclos →
  unresolvable. API lista para que la cascada manual la aplique.
- **S3** — Aplicación (fase 2): la cascada manual (extensión de la de
  border-radius) aplica las declaraciones resueltas por elemento con la
  prioridad y el orden del documento; los bordes con `var()` pintan. Aceptación:
  codeberg.org muestra los bordes del héroe; el diferencial del corpus no cambia
  (62/71, los 9 diffs JS conocidos).
- **S4** — `rem`/`em` en `border-radius` del motor Go (`.28571429rem` de los
  botones de Forgejo ≈ 4.57px con raíz 16px) — opcional, tras S3.

## Tasks

| T | Descripción | Spec | Ruta | Estado |
|---|---|---|---|---|
| T1 | Captura en el rewriter + resolutor (herencia + var()) | S1,S2 | delegado (mimo-v2.6-flash) | ✅ (63 PASS go/css; smoke `border: 1px solid var(--c)` → `1px solid red`) |
| T2 | Aplicación vía cascada manual + bordes de codeberg | S3 | delegado (qwen3.8-flash) | ✅ (73 PASS; corpus 62/71 sin cambios; live 1000x3175; borde pintado a píxel) |
| T3 | rem/em en radios | S4 | pendiente | pendiente |

## Log

- **L1** (2026-10-24): origen: libcss 0.9.2 descarta `var()` (406 bordes
  bloqueados — `odd/tasks/meta1-codeberg.md`, `docs/pareto.md`). Decisiones del
  usuario: var() solo en el motor Go; "no olvides delegar tareas y ir
  construyendo por partes segun el principio de pareto". Construcción en partes
  T1 → T2 → T3, cada una con su commit.
- **L2** (2026-10-24): **T1 entregado** (mimo-v2.6-flash). API para T2:
  `CustomDecl{Property,Value,Selector,Media string; Important bool; Order uint32}`,
  `CaptureSheet(css)`/`CaptureInline(decls)` → texto reescrito + capturas (libcss
  pierde exactamente esas declaraciones, pinneado), `NewCustomProperties(doc, sheets)`,
  `EffectiveMap` (herencia, propio manda, invalid → guaranteed-invalid),
  `Resolve(element, value)` recursivo (fallbacks anidados, indefinidos y ciclos →
  unresolvable=false). **Limitación registrada**: `Media` se captura pero no se
  evalúa en esta fase — T2 debe honrarlo reusando la evaluación de media
  existente (nunca reimplementarla). Inline `style=""` se captura por elemento
  con `CaptureInline` y se puja `inline:true, order:u32::MAX` (patrón
  grid/radius). 42+21 subtests en go/css; módulo 10/10; vet/gofmt limpios
  (7 ficheros con drift de gofmt previo corregidos solo-whitespace, verificados
  con `git diff -w`). **Consecuencia para la S7 de la migración**: cuando T2
  aplique los bordes var(), el Go divergirá A PROPÓSITO de la medición en vivo
  del oráculo (que descarta var() para siempre) — la paridad S7 se mide en la
  base sin var(); el corpus no cambia. `Risk: none`.
- **L3** (2026-10-24): **T2 entregado** (qwen3.8-flash, CERO rondas de
  corrección). `go/css/bordervar.go`: cada declaración border con `var()` se
  resuelve por elemento (`cp.Resolve`), se puja con el `cascadeBid` existente
  ordenado por (important, inline, especificidad, order) y se **reproduce a
  través del propio select de libcss** — una regla por bid, envuelta en la
  condición `@media` capturada, con una sonda `color:#dead00` que delata el
  veredicto de match de selector/media (libcss 0.9.2 no expone `css_query`, es
  la única vía sin reimplementar media). La expansión de shorthands queda
  single-sourced en libcss; `readComputedBorders` extraído de `mapStyle` se
  comparte por ambos caminos con máscaras por slot. No-resolubles → drop antes
  del bid (hermano válido gana). **Aceptación S3**: corpus **62/71 sin cambios**
  (los mismos 9 JS de KNOWN_DIFFS.md); live `wrote ... (1000x3175)` (+2px: los
  bordes ya ocupan espacio — divergencia intencional del oráculo congelado,
  3173); **prueba a píxel**: p(500,413) GO `srgb(205,227,226)` vs RUST blanco.
  73 PASS en go/css (63 previas intactas); módulo 10/10; vet/gofmt limpios.
  Límites documentados en bordervar.go: propiedades no-border con var() quedan
  capturadas sin aplicar (futura fase, `borderVarScope` es el punto de
  ampliación); overwrite de slots sin interleaving de orígenes (mismo modelo
  que grid/radius); drops conservadores si QuerySelectorAll acepta un selector
  que libcss rechaza o un `@media` anidado no parsea en mq3. `Risk: none`
  (ningún ítem 1-6: la extracción de mapStyle es internal, requested y
  behavior-pinned). **Proceso**: assess del writer (forma worktree) =
  `high_risk`; assess de la candidata committed (ce9d6cf..ad71cf3) =
  **`medium` / `under_budget` → review NO debido** (la porción queda pendiente
  hasta que un commit posterior alcance el presupuesto). Commits previos de
  feat/go-engine sin assess (lapse del padre, registrado aquí).
