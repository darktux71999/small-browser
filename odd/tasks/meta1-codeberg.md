# meta1-codeberg — renderizar codeberg.org como debe verse

Objetivo: Meta 1 de `docs/objetivos.md` — que `https://codeberg.org` se vea
comparable a Firefox headless. Branch `feat/meta1-codeberg`; runner: `cargo test`
(motor) + `cd spikes/css && cargo test` (comparte `css.rs`) + smoke real contra
codeberg.org. Mirror Engram: pendiente.

## Specs

- **S1** — Meta 1: "la primera meta es renderizar codeberg.org que es
  relativamente sencilla no es la web más minimalista pero creo que es un buen
  punto de partida". Criterio de éxito: la métrica de fidelidad contra Firefox
  headless (ink share + diff fuzz 25%, método `spikes/css/fidelity.py`) mejora de
  forma clara respecto al render actual (sin CSS externo), y no hay líneas
  cortadas por el borde.
- **S2** — Subrecursos CSS: el motor descarga las hojas `<link rel="stylesheet">`
  y los `@import` remotos y las aplica **en orden de documento** junto a las
  inline (origen author). URLs relativas resueltas contra la URL final del
  documento; en ficheros locales, contra el directorio del fichero. Caché por URL
  dentro de la ejecución, tope de tiempo/tamaño, y una hoja inaccesible no rompe
  el render (degrada, no aborta).
- **S3** — Imágenes: `<img>` se descarga, decodifica (PNG/JPEG/GIF/WebP con las
  libs del sistema ya instaladas) y se pinta en su caja (tamaño intrínseco, o
  `width`/`height` si vienen). Fallo de descarga/decodificado = hueco, no aborta.
- **S4** — `window` y APIs DOM mínimas: los scripts inline dejan de fallar con
  `ReferenceError: window is not defined`. `window` expone como mínimo
  `addEventListener`, `location` (lectura) y `document`/`console` existentes; los
  errores de script siguen siendo reportados y no fatales. El puente DOM actual no
  se rompe.
- **S5** — Texto con Pango: interlineado, justificación, tracking y familias vía
  fontconfig, y wrapping por métricas reales — el bug actual de líneas cortadas
  (envoltura por recuento de caracteres, `layout.rs:152`) desaparece como parte de
  esta migración, no se parchea aparte. Verificado visualmente con codeberg.org.
- **S6** — Informe de cierre: fidelidad antes/después + RSS + lo que queda fuera.

## Tasks

| ID | Qué | Specs | Ola | Estado |
|---|---|---|---|---|
| T1 | Subrecursos CSS (`<link>`, `@import` remoto, caché, resolución de URLs relativas, orden de documento) | S2 | 1 (paralela) | ✅ 13 tests; codeberg ink 0,043 → 0,206 |
| T2 | `window` + APIs DOM mínimas para scripts | S4 | 1 (paralela) | ✅ 16 tests; codeberg sin errores de script |
| T3 | Texto a Pango + wrapping real (absorbe el bug de líneas cortadas) | S5 | 2 | pendiente |
| T4 | Imágenes (`<img>`: fetch, decode, pintura en caja) | S3 | 2 | pendiente |
| T5 | Medición de la Meta 1 + informe | S1,S6 | 3 | pendiente |

Nota de planificación (aceptada): el bug de wrapping NO se arregla en la ruta de
texto actual porque T3 la reemplaza; si tras Pango persistieran líneas cortadas,
sería bug de anchura de caja y se corrige ahí.

## Log

- **L1** (2026-10-22): el usuario dijo "muy bien ya puedes seguir", retomando
  `docs/objetivos.md`: "la primera meta es renderizar codeberg.org que es
  relativamente sencilla no es la web más minimalista pero creo que es un buen
  punto de partida y luego nos meteremos con la ui que genera mi programa searxgo
  que este no lo puede renderizar ni netsurf ni chawan solo lo hacen a medias si
  meto flags de texto puro html ya se ve pero no es el punto".
- Diagnóstico previo (L1): el wrapping actual es por recuento de caracteres
  (`flush_text`, `layout.rs:152`: `max_chars = width / char_w`), de ahí las
  líneas cortadas en el render de codeberg.org; lo absorbe T3 (Pango).
