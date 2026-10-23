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
- **S8** — Grid mínimo (prioridad fijada por el usuario el 2026-10-23: «podemo
  empezar por grid minimo»): `display:grid` crea un contexto de formato grid;
  `grid-template-columns` con valores `px`, `fr`, `auto` y `repeat(n, pista)`;
  colocación de ítems en orden de fila (sparse); `grid-column: span N` (y
  `grid-row: span N` si es barato); separación entre pistas con los `gap`/
  `row-gap`/`column-gap` ya mapeados (carriers); filas de alto automático por
  contenido; estirado por defecto a la celda. Objetivo medible: bajar la altura
  de codeberg.org desde 4048px hacia los 1901px de Firefox y las bandas 3–10
  por debajo del 20% de diff.
- **S9** — Scroll / viewport («e ir estructurando el tema del scroll», mismo
  día): la ventana se abre a un tamaño de viewport práctico (1000×760), no a la
  altura del documento; el layout sigue siendo del documento completo; pintar
  recorta al viewport y desplaza por un offset de scroll vertical; scroll por
  teclado (↑/↓, PageUp/PageDown, Home/End y j/k/g/G estilo vi). El modo
  `--png` de documento completo **se preserva** (es la herramienta de medición
  contra Firefox).
- **S10** — Decoración de caja (pendiente, después de S8/S9): `background-image`
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
| T7 | Grid mínimo: contexto de formato grid (`layout.rs` + `css::Style` + mapping) | S8 | 4 | ⏸ worker colgado (~50 min sin escritura); sonda salvada en `/tmp/opencode/t7-salvage/`; reimplementar mañana |
| T8 | Scroll/viewport: ventana fija + offset de scroll + recorte al pintar | S9 | 4 | ✅ worker (16 tests RED→GREEN; 15 salidas byte-idénticas; commit pendiente tras el seam con T7) |

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
- **L6** (2026-10-22): diferencial byte-a-byte de T6 (base pre-flex `HEAD~2` vs actual,
  `--dump` sobre los 52 fixtures): **45 idénticos, 7 distintos, 0 regresiones**. Los 7
  diffs son flex pedido: los 6 fixtures `flex-*` (la base no sabe hacer flex) y
  `long.html`, que ya declaraba `display:flex; align-items:center;
  justify-content:center` y cuyo texto ahora se centra exactamente donde su CSS manda
  (antes salía arriba-izquierda porque el motor ignoraba flex). Risk de T6: none
  (ninguna diferencia no pedida; 15 suites verdes incluidas las de preservación).
- **L7** (2026-10-23): retomo de sesión. Preferencias del usuario: «como subajente
  deberia usar mimo-V2.6-flash» (ID verificado: `opencode-go/mimo-v2.6-flash`);
  «y si tiene la skill comprime la session» (respuesta: no existe skill de
  compresión en los directorios del usuario; la compresión la llevan runtime +
  Engram). Observación de ventana («la ventana la abres muy alargada… la
  solucion para renderizar algo en un espacio mas pequeno y poder visualizar
  todo es el scroll de la pagina») y orden de trabajo: «podemo empezar por grid
  minimo , he ir estructurando el tema del scroll». Nuevas specs S8 (grid
  mínimo), S9 (scroll/viewport) y decoración de caja movida a S10; tareas T7 y
  T8 delegadas en paralelo a `mimo-v2.6-flash` (superficies disjuntas: T7 toca
  css/layout/tests-grid; T8 toca shell/paint/main/tests-scroll).
- **L8** (2026-10-23): regla de modelos para delegación paralela, del usuario
  verbatim: «cuando tengas mas de una tarea diversifica entre modelos si es una
  tarea con mimo v2.6 flash bien si son 2 seria el este primer modelo + muse
  spark 1.3 contrubuitor + y si existe otra tarea mas deepseek v4.1 flash».
  IDs verificados activos: `opencode-go/mimo-v2.6-flash`,
  `opencode-go/muse-spark-1.3-contributor`, `opencode-go/deepseek-v4.1-flash`.
  Orden de asignación: 1ª tarea → mimo; 2ª → muse-spark; 3ª → deepseek; 4ª+
  recicla el pool en el mismo orden. T7/T8 (esta tanda) ya corrían ambas con
  mimo al dictarse la regla.
- **L9** (2026-10-23): T8 (scroll/viewport) entregado por el worker — `status:
  partial` solo porque la suite completa la rompe la sonda en vuelo de T7
  (`tests/grid.rs` con panic intencional), no su trabajo: sus 16 tests verdes
  (RED: 12 errores de compilación → GREEN), diferencial de 15 salidas
  (`--dump` stdout/stderr y `--png` bytes) byte-idénticas al baseline, y
  `SDL_VIDEODRIVER=dummy --frames 5` cerrando limpio. Ventana fija 1000×760 +
  `ScrollState` (clamp, page=viewport−60, línea 40px) + teclado ↑/↓/PgUp/PgDn/
  Home/End + j/k/g/G (ojo: los keycodes SDL3 son ASCII minúscula; `G` va por el
  modificador SHIFT). Riesgos residuales nombrados por el worker: offsets de
  `SDL_KeyboardEvent` hardcodeados verificados con sonda offsetof contra SDL
  3.4.18 (un bump de ABI rompería el parseo en silencio); teclado real no
  ejercitado (sin display). Assess del gate: `unassessable` (archivos sin
  trackear de T7 en vuelo) → inform only, self-verification del writer
  sostenida. Docs actualizadas por el padre: README (viewport + teclas),
  `tools/fidelity-probe.py` (aviso: `capture_blitz` ahora captura solo el
  viewport), `docs/font-svg-fidelity.md` (nota 1044×1036 obsoleta). Scratch
  del worker: `/tmp/opencode/t8-scratch` (limpieza a cargo del padre).
- **L10** (2026-10-22, cierre de jornada): T7 (grid) **colgado** — ~50 min sin
  tocar ningún archivo de `src/` (solo una sonda de 19 líneas con panic
  intencional, `tests/grid.rs`, mtime 22:27). Decisión del usuario verbatim:
  «mejor paralo y ve que le puedes recuperar si es que hizo algo y lo dejamos
  para manana». Recuperado en `/tmp/opencode/t7-salvage/`: `grid-probe.rs` (la
  sonda) y `grid-probe-harness-main.rs` (harness que exploraba cómo mapean
  `display:grid`, `grid-template-columns: 100px 1fr`, `gap: 10px` y
  `grid-column: span 2` a través de css_libcss — útil como punto de partida de
  mañana). La sonda se retiró de `tests/` (rompía la suite con su panic) tras
  copiarla. Hallazgo operativo: los subagentes corren **dentro del proceso de
  OpenCode** (solo `opencode` + `opencode serve` en el árbol de procesos) — el
  padre no puede cancelarlos sin matar la sesión; la cancelación se hizo/ha de
  hacerse desde la UI. T8 asegurado en commit con spot check
  (`cargo test --test scroll` → 16 passed, idéntico al reporte del worker) y
  seam (`cargo test` → 147 passed / 0 failed / 1 ignored en 16 suites).
  Grid queda para mañana, reimplementado por el padre (camino probado del flex)
  o relanzado — a decidir. Preferencia nueva del usuario para integraciones:
  al integrar, la preview se lanza **como ventana real** («si! pero cuando
  integres me lo lanzas como una ventana»).
  Assess del commit 98d7eb8 (RDD): `risk: high`, razón `process_boundary`
  (`src/shell.rs`, signal `shell_process`), 8 paths / 848 líneas → review nativo
  **pendiente**, diferido por parada explícita del usuario; primera acción de
  mañana (preflight STATUS con `--base-ref a059ef4 --committed-only`).
