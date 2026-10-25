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
| T1 | Captura en el rewriter + resolutor (herencia + var()) | S1,S2 | delegado (mimo-v2.6-flash) | en vuelo |
| T2 | Aplicación vía cascada manual + bordes de codeberg | S3 | delegado (qwen3.8-flash) | pendiente |
| T3 | rem/em en radios | S4 | pendiente | pendiente |

## Log

- **L1** (2026-10-24): origen: libcss 0.9.2 descarta `var()` (406 bordes
  bloqueados — `odd/tasks/meta1-codeberg.md`, `docs/pareto.md`). Decisiones del
  usuario: var() solo en el motor Go; "no olvides delegar tareas y ir
  construyendo por partes segun el principio de pareto". Construcción en partes
  T1 → T2 → T3, cada una con su commit.
