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
- **S7** — Flex mínimo (decisión del usuario tras medir): el motor implementa el
  contexto de formato flex — `flex-direction` (row/column), `flex-wrap` básico,
  `gap`/`row-gap`/`column-gap`, `flex` (grow/shrink/basis), `justify-content`
  (start/center/end/space-between) y `align-items` (start/center/stretch) — para
  que codeberg.org componga como Firefox: hero a 2 columnas, sección a 3,
  estadísticas a 4 y soporte a 3 filas. Objetivo medible: acercar la altura de
  4403px a los 1901px de Firefox y bajar las bandas 3–10 por debajo del 20% de
  diff (método de bandas con `magick compare -metric AE -fuzz 25%`, referencia
  Firefox claro vía `layout.css.prefers-color-scheme.content-override=1`).
- **S8** — Decoración de caja (pendiente, siguiente tras S7): `background-image`
  (repeat/position mínimos), `border` y `border-radius`.

## Tasks

| ID | Qué | Specs | Ola | Estado |
|---|---|---|---|---|
| T1 | Subrecursos CSS (`<link>`, `@import` remoto, caché, resolución de URLs relativas, orden de documento) | S2 | 1 (paralela) | ✅ 13 tests; codeberg ink 0,043 → 0,206 |
| T2 | `window` + APIs DOM mínimas para scripts | S4 | 1 (paralela) | ✅ 16 tests; codeberg sin errores de script |
| T3 | Texto a Pango + wrapping real (absorbe el bug de líneas cortadas) | S5 | 2 | ✅ 0f61753 (codeberg: 0 líneas cortadas; altura 3014→4123) |
| T4 | Imágenes (`<img>`: fetch, decode, pintura en caja) | S3 | 2 | ✅ 011bfc6 (16 tests; logo de Codeberg pintando, SVG degradan con aviso) |
| T5 | Medición de la Meta 1 + informe | S1,S6 | 3 | pendiente (parcialmente hecho: bandas medidas) |
| T6 | Flex mínimo: contexto de formato flex en `layout.rs` (+ campos en `css::Style` y mapping) | S7 | 4 | ✅ 38f725e (9 tests; objetivo medible NO alcanzado: queda grid) |

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
- **L2** (2026-10-22): Ola 1 cerrada (T1 subrecursos CSS + T2 window/location/navigator
  + cableado, commits 6dd5665/702e108/617a043; codeberg ink 0,043→0,206, sin errores
  de script) y T3 de la Ola 2 cerrado (commit 0f61753): texto medido/envuelto/pintado
  con Pango a 72dpi (1pt=1px), interlineado y tracking como atributos. 13 tests con
  RED observado en la base; suite completa 106 verdes + 1 ignorada. codeberg.org:
  de 503 píxeles cortados a **cero tinta en los 3 últimos píxeles**, altura
  3014→4123 (wrapping + interlineado reales), RSS 38,6→49,5MB. Diferencial sobre 40
  fixtures: 0 diffs estructurales. Limitación conocida para T5: `css::Style::
  font_family` conserva solo la primera familia de la pila CSS (las demás las
  resuelve fontconfig como fallback); con JetBrains Mono como única fuente instalada
  no es observable. Decisión de interfaz registrada por el usuario: navegación a
  puro teclado estilo luakit/vimb (vi), en `docs/objetivos.md`.
- **L3** (2026-10-22): **Ola 2 cerrada**. T3 (Pango, commit 0f61753) + T4 (imágenes,
  commit 011bfc6). T4: `src/img.rs` (fetch+decode+cache: PNG/JPEG/GIF vía gdk-pixbuf,
  WebP vía libwebp, tope 10MB, guarda de bombas de descompresión, SVG documentado como
  no soportado), `<img>` como elemento reemplazado (tamaño intrínseco, CSS sobre
  atributos, aspect-ratio con una sola dimensión, clamp a la caja de contenido),
  pintura premultiplicada en ambos caminos de salida. 16 tests (RED observado en la
  base: 0 píxeles de imagen); diferencial: páginas sin imágenes byte-idénticas. El
  worker pidió permiso para tocar `main.rs` y el padre aplicó la línea (cableado de la
  base del documento a `layout_with_subresources`) — los writers no tocan `main.rs`.
  Verificación en vivo: codeberg.org pinta el logo PNG (258×250, wordmark incluido);
  los 4 SVG de la página degradan con `browser-lite: skipping image ...` no fatal.
  Suites: 122 tests verdes + 1 ignorada; assess RDD `medium / under_budget`.
  Modelo de subagentes cambiado por el usuario: ahora
  `opencode-go/minimax-m3.1-flash-free` (MiniMax M3.1 Flash Free); Muse Spark 1.3
  cancelado. Pendiente: T5 (medición de la Meta 1 + informe).
- **L4** (2026-10-22): el usuario eligió "Flex primero (Recomendado)" tras ver la
  medición por bandas — "Flex mínimo (fila/columna + gap + flex-grow + wrap básico)
  en layout.rs. Objetivo medible: acercar la altura de 4403px a los 1901px de
  Firefox y bajar las bandas 3-10 por debajo del 20% de diff." Contexto de la
  medición: Firefox headless sirve el tema OSCURO (donde está la imagen de montaña
  del fondo que el usuario vio en NetSurf); la referencia clara se captura con
  `layout.css.prefers-color-scheme.content-override=1`. Gaps ordenados: (1) flex/grid,
  (2) background-image + decoración de caja (S8), (3) SVG y fuentes, (4)
  `prefers-color-scheme`. Nueva spec S7 y tarea T6.
- **L5** (2026-10-22): T6 (flex) entregado en 38f725e — **con el worker muriendo en
  mitad (resultado vacío del runtime) y el padre adoptando/corrigiendo su borrador**.
  Defectos encontrados y corregidos por el padre: (1) el bucle de colocación no
  sumaba el `gap` entre ítems; (2) `write_carrier` pegaba las dos declaraciones
  carrier sin `;` → el valor del gap nunca llegaba a libcss (probado con sonda:
  20px desaparecían); (3) `row-gap`/`column-gap` escribían ambos carriers en vez
  del suyo; (4) los ítems anónimos (sentinela `usize::MAX` para texto inline)
  revientan con `styles[-1]` en la página real — el test del worker tampoco cubría
  el caso; (5) el test de `space-between` esperaba 100px donde el spec deja 150px
  (300px libres / 2 juntas). Resultado: 15 suites verdes, codeberg.org renderiza
  sin panics (1000×4048, antes 4403). **Objetivo medible S7 NO alcanzado**: bandas
  3–9 siguen en 22–68% (meta <20%) y la altura queda lejos de 1901px. Causa
  medida: Codeberg compone sus columnas con **CSS grid** (`display:grid` +
  `grid-template` en homepage.css/index.css además de flex) — falta grid mínimo.
