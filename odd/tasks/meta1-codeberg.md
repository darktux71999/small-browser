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
  estadísticas a 4 y soporte a 3 filas. Objetivo medible: acercar la altura a la
  referencia Firefox y bajar las bandas 3–10 por debajo del 20% de diff (método
  de bandas con `magick compare -metric AE -fuzz 25%`, referencia Firefox claro
  vía `layout.css.prefers-color-scheme.content-override=1`). **Referencia
  re-baselada 2026-10-25 (L21): 1669px con el entorno de fuentes fijado**
  (Liberation/DejaVu/Noto instaladas vía KISS; el histórico «1901px» era
  artefacto del fallback monoespaciado que inflaba también a Firefox).
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
| T7 | Grid mínimo: contexto de formato grid (`layout.rs` + `css::Style` + mapping) | S8 | 4 | ✅ 77abd05 (worker muse-spark: 6 tests RED→GREEN; diferencial 53 idénticos, 5 = fixtures grid; assess: medium/under_budget) |
| T8 | Scroll/viewport: ventana fija + offset de scroll + recorte al pintar | S9 | 4 | ✅ worker (16 tests RED→GREEN; 15 salidas byte-idénticas; commit pendiente tras el seam con T7) |
| T9 | `background-image` mínimo (url, repeat, posición) — las montañas del héroe | S10 | 4 | ✅ (6 tests RED→GREEN; diferencial 58 idénticos + 3 fixtures nuevos; codeberg: montañas pintando) |
| T10 | Tipografía real: resolución de lista de familias + keywords genéricas + `@font-face` (ttf/otf) | Pareto #4 | 4 | ✅ (9 tests RED→GREEN; diferencial 61+1 esperado; 0 regresiones — falta instalar fuentes para verlo) |

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
- **L11** (2026-10-23): jornada del review + grid. (1) **Review nativo de
  21d90d2 (T8): APROBADO y autoridad quemada** (`gentle-ai.review-acknowledged/v1`)
  tras una corrección acotada: los lentes readability+reliability y el refutador
  marcaron el mismo CRITICAL determinista (R2-001/R3-1: al mover la pintura a
  `shell::run` desapareció la rama `browser-lite: paint failed: {err}` y un fallo
  de Cairo se reportaba como `window failed`) — corregido en 8785891 con
  `RunError::{Paint,Window}` + `diagnostic()`, exit code intacto, 2 pins
  (forecast 80 líneas = reales 80). (2) **T7 grid entregado por worker
  muse-spark** (tras cancelar el colgado de mimo): 6 tests RED→GREEN,
  diferencial 53/5, commit 77abd05, assess `medium/under_budget` → sin review.
  (3) **Descubrimiento de infraestructura**: el agente `review-validator`
  apuntaba a `opencode-go/minimax-m3.1-flash-free` (modelo **retirado** del
  proveedor) → `ModelUnavailableError` disfrazado de `provider_failed` en el
  transporte; reparado a `opencode-go/mimo-v2.6-flash` (tier bajo del pool).
  Lecciones: los rechazos `provider_failed` del transporte se miran en el log
  de OpenCode (`level=ERROR`, `Failed to drain Session`); la selección
  `intended-untracked` viaja en el flag de cada petición STATUS (schema
  `gentle-ai.review-intended-untracked-selection/v1`, fuente en
  `~/proyectos/gentle-ai/internal/cli/review_intended_untracked.go`); y la
  reconstrucción de corrección committed-only exige **worktree limpio** (el
  trabajo de grid se aparca con `git stash -u` durante la validación). (4)
  **Medición Meta 1**: codeberg.org baja de 4048px a **3173px** (Firefox claro:
  1901px) — grid restó 875px. Bandas 13–16 ya < 8% de diff; la brecha restante
  está en héroe/banda azul (5–12: 40–65% → faltan fuentes reales y decoración
  de caja) y en la altura del footer (17–23: más allá de la página de Firefox).
  (5) **Avisos no bloqueantes del review** (trabajo posterior, nunca reabren
  este candidato): R2-002 comentario obsoleto en `paint.rs:25-26`; R2-003 check
  tautológico de viewport en `shell.rs:226-231`; R2-004 retorno sin usar de
  `ScrollState::apply`; R2-005 ciclo de módulos paint⇄shell por las constantes
  de viewport; R3-2 decode de eventos SDL sin test de offsets; R3-3
  `fidelity-probe.py:capture_blitz` sigue capturando la ventana (recorte) en
  vez de `--png` completo.
- **L12** (2026-10-23): **bug del scroll encontrado y corregido** (reporte del
  usuario: «se ve bien pero no funciona el scroll»). Causa raíz: el bucle de
  frames terminaba con `SDL_WaitEventTimeout(&mut idle, 16)`, que guarda en su
  buffer el evento que lo despierta y el código lo **descartaba sin despachar**
  — cada pulsación solitaria (flechas, j/k, PgUp/PgDn, g/G) se tragaba al final
  de cada frame; solo sobrevivían ráfagas (por eso el cierre de ventana
  funcionaba). Fix 67fc20a: `SDL_Delay(16)` (no consume eventos; la pulsación
  queda en cola para el `SDL_PollEvent` del siguiente frame) + 2 pins unitarios
  del decode SDL3 (offsets key@28/mod@32, riesgo R3-2 del review) y del mapeo
  tecla→acción. Validado en ventana real por el usuario: «el scroll funciona!».
  `Risk: item 6` (el consumo de eventos en el bucle FFI no es testeable
  automáticamente; aceptación = prueba de teclado real) → verificador
  independiente lanzado (mimo-v2.6-flash) sobre el wiring del bucle.
- **L13** (2026-10-23): **review del slice (grid+scroll+docs): APROBADO y quemado**
  (lineage `review-8aa397f463c8623b`, target `bd57ba40…`) — sin CRITICAL: no
  hubo refutador ni corrección. Modelos diversificados por tarea a petición del
  usuario («si diversifica por las tareas solo no toques el orquestador»):
  risk→mimo-v2.6-flash, resilience→muse-spark-1.3-contributor,
  readability→deepseek-v4.1-flash, reliability→mimo-v2.6-flash,
  refuter→muse-spark-1.3-contributor, validator→deepseek-v4.1-flash
  (orquestador y modelo raíz intactos). **Avisos del review** (trabajo posterior
  separado, no reabren el candidato): R2-1 flag grid se pone y no se limpia +
  «grid container» decidido en 3 sitios; R2-2 cap 1000 duplicado en 3 sitios;
  R2-3 columna implícita en 2 capas; R2-4 mezcla de orígenes en el Bid del
  merge; R2-5 cascada manual duplicada; R2-6 doble marcador de ítem anónimo;
  R3-1 **row-span no aporta alto de fila** (grid colapsa si solo hay spanning
  items); R3-2 **min-width/max-width legítimos se confunden con gap** (carriers)
  y `column-gap` real queda ensombrecido; R3-3 la cascada grid ignora condiciones
  `@media`/`@supports` y `!important` es por bloque; R3-4 ítems grid que son
  flex/grid anidados ignoran el ancho de su celda; R3-5 `inline-grid` pasa a
  comportamiento block; R3-6 contratos de degradación sin tests de frontera;
  R3-7 el test del decode SDL es autorreferencial y el fix del bucle no tiene
  test; R3-8 `css::apply` marca grid por `grid-template-columns` sin `display`.
- **L14** (2026-10-23): **bug de la orilla corregido** (reporte del usuario:
  «tenemos inconsistencia en el redimensionado de la pagina entre letras salidas
  o en la orilla de la ventana»). Raíz = R3-4 del review confirmado en vivo: los
  ítems que son contexto flex/grid **anidado** se maquetaban con el ancho
  disponible del padre en vez de su celda (`layout_node` despachaba a
  `layout_flex_container`/`layout_grid_container` con `Forced::default()`), lo
  que empujaba grupos enteros fuera del lienzo («Sign in» en x=1068, «Register
  now.» en x=1414). Fix 14a5ad8: enhebrar `forced` hasta los contextos anidados
  (honrado por `content_box` como la rama de bloque); pins: flex-en-flex
  content-sized, flex-en-grid = su celda de 200px (RED 960/1000 → GREEN).
  Efecto medido: desbordes del lienzo en codeberg.org **25 → 4** (los 4
  restantes son cosméticos: «Register» 22px por padding derecho y 3 tarjetas a
  2px → cola de pulido). Diferencial byte-a-byte (base HEAD~1 vs actual sobre
  los 59 fixtures): **58 idénticos, 1 distinto** (`nested-contexts.html`, el
  fixture nuevo que ejercita la regla) → **0 regresiones**. Risk: none.
- **L15** (2026-10-23): **T9 `background-image` entregado** (worker mimo-v2.6-flash):
  url() resuelta en parse por libcss (absoluta contra la hoja), fetch/decode
  reusando `img.rs`, pintado detrás del contenido con mosaico a tamaño natural
  (`repeat` desde el origen de la caja, recorte a la cja, alpha intacto vía
  `rgba_surface`). Hallazgo FFI: `css_computed_background_image` devuelve 0x1
  tanto para NONE como para IMAGE — la única señal de «sin imagen» es el
  `lwc_string*` NULL. **Codeberg: las montañas translúcidas del héroe pintan**
  (banda del héroe 12.2% de píxeles cambiados, brillo medio 0.971→0.849, teal
  `srgb(148,203,200)` sustituyendo al blanco) y también el fondo de la sección
  de estadísticas. Diferencial: 58 idénticos + 3 fixtures nuevos + 1 delta de
  stderr esperado (fixture que declara background-image con SVG no soportado);
  0 regresiones. Suites: 165 passed / 0 failed. `Risk: none`. Pendiente de S10:
  `background-position`, `background-size`, `border`, `border-radius`; nota
  menor: las hojas inline `style=""` usan base sintética
  `browser-lite://inline/{id}` y sus URLs relativas de fondo no resuelven
  (aviso honesto, sin cambio de píxeles). Limpieza: /tmp (tmpfs 1GiB) se llenó
  con el scratch del worker — builds de scratch van al disco de home.
- **L16** (2026-10-23): **T10 tipografía real entregado** (worker mimo-v2.6-flash):
  lista completa de familias resuelta en orden de autora contra fontconfig
  (primer match real gana; genéricas mapeadas: sans-serif/system-ui→Sans,
  serif→Serif, monospace/ui-monospace→Mono, cursive/fantasy), `@font-face`
  extraído del camino libcss (mismo walker que los carriers), descarga con el
  loader existente, registro como app-font de fontconfig vía caché en home
  (fontconfig 2.18.3 **no** trae `FcConfigAppFontAddMemory`), ttf/otf soportados
  con aviso honesto para woff2 y `local()`. Bugs de libcss 0.9.2 encontrados y
  esquivados en el rewriter: (a) el byte de estatus de genérica es **basura**
  para declaraciones solo-nombre (varía por proceso) salvo que traigan genérica
  explícita → se añade la cola implícita `sans-serif`; (b) secuencias
  multi-identificador sin comillas con un identificador tipo genérico
  (`Liberation Serif`) se **descartan** en silencio → se normalizan con comillas.
  Tests: 9 (genéricas, stacks con fallback real, @font-face local, degradación
  con aviso, pin pre/code mono) bajo un fontconfig privado controlado.
  Diferencial: 61/62 idénticos en dump y png; el único delta es el aviso
  esperado de un fixture `local()`. Suites: 174 passed / 0 failed. **Hallazgo
  de entorno: la máquina tiene instalada UNA sola familia (JetBrains Mono)** —
  el gancho visual necesita instalar paquetes de fuentes (decisión del
  usuario). `Risk: item 6` (la aceptación visual no es probable sin más
  fuentes instaladas). Codeberg declara 5 `@font-face` CJK solo-woff2 →
  avisan una vez por familia y degradan (sin decoder woff2 barato).
- **L17** (2026-10-23): T10 **cerrado**: verificador independiente (muse-spark-1.3-
  contributor, primera tarea del pool nuevo) **5/5 PASS, 0 bloqueantes** —
  normalize_font_family semánticamente preservante, FFI de fontconfig sana
  (firmas contra headers instalados, patrones destruidos, NULLs checkeados),
  @font-face con caps (10MB/20s), escritura atómica, aviso por familia, sin
  fetch en páginas sin @font-face, split String/Vec sin divergencia, y el
  diferencial 61/62 creíble. Advisories: desc NULL sin checkar (preexistente),
  retorno de FcPatternAddString ignorado (degrada a false), `system-ui` no está
  en la lista GENERIC del rewriter (mapea igual a Sans). Decisión del usuario:
  **instalación de fuentes para después** («Dejarlo para después») — la
  maquinaria queda lista; al instalar paquetes (ttf-liberation/ttf-dejavu/
  noto-fonts) la tipografía real se verá sin más cambios de código.
| T11 | `border` (ancho/estilo/color por lado) + `border-radius` uniforme — resto de S10 | S10 | 4 | ✅ (11 tests RED→GREEN; diferencial 67 idénticos + 3 por diseño; 185/0 suite; codeberg idéntico — bloqueado por var()) |
- **L18** (2026-10-23): **T11 bordes/redondeos entregado** (muse-spark-1.3-
  contributor): anillo por lado (width/style/color desde los getters de libcss;
  thin/medium/thick→1/3/5px, CURRENT_COLOR→color de texto, `border-style:none`
  anula el ancho usado — regla CSS de used-value, absorbe el bug de libcss que
  reporta 2px sin declarar), `border-radius` con cascada manual (libcss no
  trae getter) con escalado por solape de esquinas CSS, clip de fondo/imagen/
  anillo en esquinas redondeadas, y degradación documentada: estilos no-solid
  → solid sin aviso, radios elípticos → valor horizontal. `--dump` documentado:
  `w` sin cambios (el borde vive dentro), `h` crece como el padding. Suites:
  **185 passed / 0 failed** (20 suites). Diferencial: 67/71 idénticos en dump y
  png; los 3 nuevos por diseño + net-latin1 con fallo idéntico; stderr idéntico
  en los 71. **Hallazgo de corpus**: codeberg.org queda byte-idéntico (0
  píxeles) porque **Forgejo escribe todos los bordes con `var(--color-*)`**
  (406 declaraciones) y libcss 0.9.2 descarta las declaraciones con var() —
  el desbloqueo real de bordes en la página son las **custom properties**
  (fila 14 del Pareto, que salta de media-baja a prioritaria por este dato).
  `Risk: none`. Pendientes medidos: var()/custom properties (el desbloqueo),
  rem/em en el lector de radios (los botones usan `.28571429rem`),
  background-position/size (resto de T9).
- **L19** (2026-10-23/24): **review en cadena del slice 497b1f7..HEAD: 3/3
  APROBADOS y quemados**. El slice acumulado (38 archivos/4.611 líneas) excedía
  el presupuesto del revisor (`lens_context_budget_exceeded`, nada creado) →
  receta del proveedor: candidatos más pequeños encadenados. Cada commit se
  revisó en su propio worktree temporal: **cándidato 1** = T10 tipografía
  (15 archivos/1.360 líneas, riesgo medium — aprobado; 1 BLOCKER del lente
  reliability sobre la caché de familias **refutado** por el refutador; avisos:
  vocabulario de genéricas duplicado, walkers a sincronizar, contrato de avisos
  a medio hacer, token_args=substring); **cándidato 2** = T1 fundación Go
  (7/1.065, high por el runner que lanza procesos — aprobado tras 1 reintento
  del lente reliability por `reviewer_result_not_admissible`; 12 avisos de
  legibilidad/cobertura); **cándidato 3** = T11 bordes (11/1.436, medium —
  aprobado; 11 avisos: 4 implementaciones de la expansión 1-4 valores, la
  lista thin/medium/thick en dos módulos, el predicado de visibilidad del anillo
  triplicado, y huecos de test en keywords/entradas inválidas/ramas de img).
  Lecciones: los worktrees temporales por commit eliminan además el roce del
  untracked; el presupuesto de contexto del revisor obliga a revisar por debajo
  de ~1.500-2.000 líneas por candidato.
- **L20** (2026-10-25): **Medición S7 con el motor Go** (qwen3.8-flash, tarea de
  medición sin ediciones). Go (HEAD 681a35e, con bordes var(), radios rem/em y
  fix noscript): **1000×3175** · Firefox 157 claro: **1000×1901** (reproduce
  byte a byte la referencia histórica) · AE página entera **17.47%**.
  **MÉTODO DE BANDAS (definición fijada aquí para que las futuras mediciones
  sean comparables)**: 24 bandas horizontales iguales sobre la altura de
  rejilla = max(altura motor, altura contenido referencia) → 3175/24 ≈ 132.3px
  por banda; diff% = píxeles cambiados / área de banda con
  `magick compare -metric AE -fuzz 25%`. La referencia Firefox es un recorte
  `magick -fuzz 12% -trim` de una captura de viewport 1000×3600 (headless 157
  no tiene `--full-page`); ese trim reproduce el 1901 histórico exacto. Ojo:
  el AE por bandas no es estrictamente aditivo (~2% de efecto de rebanado) —
  no mezclar totales de granularidades distintas.
  **Tabla (banda: % diff)**: 0:24.94 · 1:7.28 · 2:9.06 · 3:2.53 · 4:16.27 ·
  5:31.70 · 6:57.57 · 7:40.82 · 8:18.58 · 9:36.33 · 10:36.77 · 11:40.91 ·
  12:37.33 · 13:6.86 · 14:4.29 · 15:15.76 · 16:9.79 · 17:8.77 · 18:16.52 ·
  19:1.43 · 20:1.34 · 21:1.51 · 22:1.38 · 23:0.75.
  **Delta vs la última medición (era Rust, 3173px)**: el **héroe queda
  ARREGLADO** — bandas 3–4 bajan de 40–65% a **2.53/16.27%** (bordes var() +
  background-image); banda 8 = **18.58%**, el mejor número de banda azul
  jamás registrado. **Objetivo histórico (bandas 3–10 < 20%): NO cumplido** —
  exceden 5 (31.70), 6 (57.57), 7 (40.82), 9 (36.33), 10 (36.77); pasan 3, 4, 8.
  **Diagnóstico dominante**: ya no es decoración de cajas sino **ritmo
  vertical** — el motor Go pone el mismo contenido 150–500px más abajo que
  Firefox (fallback monoespaciado + line boxes más altos): el contenido idéntico
  cae desfasado y las bandas de texto 5–12 pagan el desfase. Secundarios: los
  4 SVG (logos), el banner ToU sin-JS donde Firefox muestra cookies, y ~1270px
  de footer de más bajo los 1901px de Firefox (bandas 19–23 ≈ 1%, correcto).
  **Palanca nº1 por datos**: tipografía real (fuentes del sistema — decisión
  diferida del usuario) + alineación de line-box/leading contra Firefox.
  Artefactos: /tmp/bl-m1 (side-by-side.png, diff-annotated.png, bands.csv,
  COMMANDS.sh con la reproducción exacta). Deriva viva: contador de users
  ±1 entre capturas (documentado en L15). `Risk: none` (solo medición).
- **L21** (2026-10-25): **Re-medición S7 con fuentes reales** (qwen3.8-flash) —
  **la hipótesis tipográfica queda FALSIFICADA** y con ella el «1901px» como
  referencia. Tras instalar Liberation/DejaVu/Noto (P7, recetas KISS en sprint):
  Go **1000×2936** (−239) · Firefox **1000×1669** (−232) · **brecha Go−FF
  +1267px sin cambiar** (era +1274) · AE página **17.24%** (era 17.47%). El
  fallback roto inflaba a AMBOS motores por igual — por eso el histórico
  «1901px de Firefox» era un artefacto del entorno de fuentes. Las fuentes sí
  arreglaron la cabecera: banda 0 (24.94→21.53), banda 4 (16.27→11.18), banda 3
  estable en 2.52. **Objetivo bandas 3–10 < 20%: NO cumplido** — 6 de 8 exceden
  (5:25.93, 6:55.40, 7:40.67, 8:25.61, 9:34.81, 10:35.64); la banda 8 empeoró
  (18.58→25.61). **Diagnóstico dominante confirmado como acumulación interna**:
  escaneo de fase rígida apenas se movió (d≈575→550 en el texto medio, con
  ~36% de mismatch a d=0) y los mejores offsets por zona crecen de forma no
  rígida (~750px en estadísticas, ~550 en texto medio, ~275/150 en la lista de
  repos) → **line-box/leading del propio motor acumulándose por documento**, no
  el entorno de fuentes (ya correcto y fijado como condición de referencia).
  La palanca siguiente es la computación de leading/line-height del motor contra
  las métricas de Firefox. Notas de método: al cambiar la altura, la rejilla de
  24 bandas re-bina los límites (los deltas por índice son aproximados; los
  robustos son AE total y el escaneo de fase); deriva viva del contador ±1 sin
  cuantificar. Artefactos: /tmp/bl-m2 (bands-delta.csv, phase-*.txt,
  COMMANDS.sh); /tmp/bl-m1 intacto como evidencia ANTES. **Decisión pendiente
  para el usuario**: el fix de leading cambia el layout de texto → divergirá del
  oráculo Rust (que comparte la misma conducta Pango) — ¿fix solo en Go
  (divergencia documentada) o también en el oráculo vía agente externo
  (preserva la paridad byte a byte del diferencial)? `Risk: none` (medición).
- **L22** (2026-10-25): **Medición M3 — veredicto S7 tras el leading** (parte
  T4 de `leading-fidelity.md`, ling-3.1-flash). Motor **1000×2297** vs Firefox
  **988×1669** · brecha **628px** (era 1267) · AE página **14.22%** · fase
  **d≈155-181** (era ~550). **Bandas 3-10: 4/8** (pasan 3,4,5,10 — mejor
  tendencia: P6 3/8, P8 2/8, M3 4/8); la banda 9 es fronteriza (21.80 fuzz0 /
  19.81 fuzz12). **S7 composición**: hero 2 columnas ✓ · sección about 4-across
  ✓ · estadísticas 4 columnas ✓ · **soporte 3 filas ✗** (compone apilado).
  Familia de defectos dominante (los tres fixes de leading no la tocan):
  **composición flex/grid en filas** — soporte `grid repeat(2,1fr)` apilado con
  botón 938×108 (20% del AE), nav `navbar-left` a 56px con enlaces envueltos +
  fondo blanco (el `--color-nav-bg` vive en `@layer codeberg-theme` dentro de
  `@media prefers-color-scheme:light` — la cascada var() no evalúa `@layer`),
  footer estirado 365 vs ~144. Gaps S10 de pintado: hero bg-image tiled a
  tamaño natural (FF: `no-repeat center bottom`), `linear-gradient` de cards
  sin pintar, bg azul blanco detrás de las estadísticas por el tiling. El
  veredicto honesto: la Meta 1 («componga como Firefox» + bandas <20%) sigue
  ABIERTA por la composición de filas — el siguiente frente está identificado
  con geografías exactas (dump + perfiles de tinta + muestras de píxel en
  /tmp/bl-m3/compose/). Artefactos: /tmp/bl-m3 (COMMANDS.sh, bands.csv,
  phase-*.csv, side-by-side.png, diff-annotated.png). Nota de método: trim
  `fuzz 0%` reproduce el 1669 clavado (el fuzz12 se come la franja viva de
  11px); el AE no es comparable entre generaciones por el re-binning (24
  bandas sobre max(alturas)). `Risk: none` (medición).
