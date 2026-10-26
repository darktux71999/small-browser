# Runtime JS: QuickJS via cgo

Objetivo: dar interactividad real al motor Go y cerrar los 9 diffs JS del
diferencial. **Decisión del usuario (2026-10-25)**: «va ser el runtime
quickjs-cjo» — QuickJS embebido via cgo (no goja). Motivo de peso: el oráculo
Rust **usa QuickJS** → mismo motor = mensajes de error y semántica byte a byte
con el oráculo, y los 9 fixtures cierran de verdad. Rama `feat/go-engine`;
runner `go test ./...` + runner diferencial. QuickJS ya está en el sistema
(`/usr/include/quickjs.h`, `qjs`; receta `extra/quickjs` en sprint por si hay
que reconstruir). Pool: **«mejor reusemos muse por el momento»** (haiku
aparcado por coste).

## Specs

- **S1** — QuickJS embebido via cgo (receta probada de `go/css`: bindings a las
  C headers reales, handles de registro para JS↔Go, ni un puntero Go en C,
  cookies con base ≥8192). Eval de scripts con captura de errores y formato de
  aviso **byte-idéntico al oráculo** (`browser-lite: js error: ...` — el
  formato exacto vive en `src/` del oráculo, que es la spec).
- **S2** — Superficie de bindings DOM/window **por evidencia**: los 9 fixtures
  JS + los scripts reales de codeberg. Famílias mínimas: `document`
  (getElementById, querySelector(All), title, cookie get/set), `window`
  (propiedades leídas por los fixtures), nodos (createElement, appendChild,
  innerHTML/textContent), timers (setTimeout/setInterval), eventos
  (addEventListener + dispatch básico). **La superficie que implementa el
  oráculo ES la spec** — no inventar APIs que el oráculo no tenga.
- **S3** — Pipeline de scripts: recolección inline + `src` en orden de
  documento, ejecución tras el parseo (aproximación documentada del
  parser-blocking), mutaciones de DOM/título → re-layout + re-pintado, avisos
  de error en stderr.
- **S4** — Aceptación: los **9 fixtures JS** (cookie-echo, cookie-set, demo,
  events, js-dom, js-window, js-window-error, net-utf8, timers) pasan a
  **idénticos** en el diferencial; `KNOWN_DIFFS.md` queda solo con las
  categorías no-JS registradas; el conteo del diferencial sube. La ventana
  interactiva responde a los scripts de la página.
- **S5** — Seguridad consciente (item 2 de alto riesgo de la casa): ejecutar JS
  de sitios reales **sin sandbox** es superficie de ataque real. Mitigación
  inicial: superficie DOM acotada + sin acceso a red ni FS desde JS + límite de
  tiempo de evaluación. La decisión de sandbox real queda para el usuario.

## Tasks

| T | Descripción | Spec | Ruta | Estado |
|---|---|---|---|---|
| T1 | `go/js`: bindings QuickJS + eval + captura de errores (formato oráculo) | S1 | delegado (muse-spark-1.3-contributor) | ✅ (6 tests/13 subtests; goldens byte-idénticos al oráculo; 12 paquetes verdes) |
| T2 | Superficie JS del oráculo (7 miembros) + errores byte-exactos | S2 | muse-spark | ✅ (13 tests; paridad de superficie oracle↔Go = conjuntos iguales) |
| T3 | Pipeline de scripts (orden, re-render, stderr) | S3 | muse-spark | pendiente |
| T4 | Aceptación diferencial + cierre KNOWN_DIFFS | S4 | padre (inline) | ✅ (**streams JS byte-idénticos**; js-dom idéntico total; residual = geometría registrada) |

## Log

- **L1** (2026-10-25): decisiones del usuario: «va ser el runtime quickjs-cjo»
  (QuickJS via cgo sobre goja — paridad byte a byte con el oráculo, que ya usa
  QuickJS) y «mejor reusemos muse por el momento» (haiku aparca por coste:
  output $0.50/M). Contexto: QuickJS instalado en el sistema; los 9 fixtures
  JS del corpus son la aceptación (KNOWN_DIFFS.md). Programado: migración de
  provider a `opencode-go` al reiniciar su tope (~20 min) — «lo que esté lo
  que no déjalo de openrouter» (nemotron-supervl se queda: su fila de pago es
  solo OpenRouter). Pool actual: muse (trabajo) · luna (sondas) · nemotron
  (visión) · haiku (aparcado) · orquestador intocado.
- **L2** (2026-10-25): **T1 entregado** (muse-spark — reintento tras la muerte
  por la llave de OpenRouter; conservó los restos RED del intento muerto y los
  completó). `go/js/`: núcleo de motor QuickJS via cgo — `glue.c` (definiciones
  propias, `Static_assert` del layout de JSValue, trampolín de callbacks Go,
  interrupt handler) + `js.go` (API para T2: `New/Close/Eval(src,filename)/
  Get(h,prop)/Call(h,args...)/SetGlobalFunc(name,fn)/Free(h)/SetTimeLimit(d)`,
  `Value{Kind,B,N,S,H}`, handles de registro, `JSError` con prefijo del oráculo).
  **Aceptación clave: errores byte-idénticos** — los goldens se verificaron
  contra libqjs 0.17.0 con la receta `JS_ToCString` del propio oráculo (no leyendo
  `.message` — ese es el truco para la paridad): `js error: SyntaxError:
  expecting ';'` (demo) y `js error: TypeError: not a function` (js-window-error)
  `cmp`-iguales. Límite de evaluación por Eval (10s por defecto, interrupt →
  `InternalError: interrupted`, mensaje nativo sin fixture — solo el prefijo
  queda pinneado). 12 paquetes verdes; vet/gofmt limpios. Lección de cgo:
  `//export` rechaza `const void*` — los headers compartidos declaran `void*`.
  `Risk: item 2 (S5)` — JS sin sandbox (mitigación acotada: sin red/FS, tope de
  tiempo); el sandbox real es decisión del usuario. Restos de scratch
  (/tmp/jsprobe, /tmp/jscmp) retirados por el padre.
- **L3** (2026-10-25): **T2 entregado — y el hallazgo que redefine S2/S4**
  (muse-spark). **La superficie JS del oráculo son SOLO 7 miembros**
  (`src/js.rs`, verificado por grep): `console` (log/error/warn),
  `document.title`, `window===globalThis`, `location` de solo lectura
  (href/protocol/host/hostname/pathname/toString), `navigator.userAgent`, y
  `add/removeEventListener` + `__bl_dispatch` (consume-once, dup-once,
  handleEvent, aislamiento de errores). **NO existen** getElementById,
  querySelector, cookies, timers, innerHTML ni APIs de nodos. Evidencia dura:
  los 9 fixtures JS **fallan contra el propio oráculo** salvo `js-window` (pasa
  limpio) y `demo` (1er script ok + SyntaxError): los otros seis producen
  `TypeError: not a function` / `ReferenceError: setTimeout is not defined` —
  líneas de error que el motor Go debe reproducir **byte a byte** (los goldens
  están pinneados). Por tanto: **el "gap JS" es de paridad de errores, no de
  APIs** — T4 cierra los 9 fixtures reproduciendo esos errores, y las APIs de
  DOM reales (la "interactividad real") son una **divergencia deliberada
  posterior** (como var()/SVG) que requiere decisión explícita de alcance.
  Semánticas finas replicadas con refs: los eventos lifecycle saltan tras cada
  eval aunque el script falle y el error del script gana (js.rs:229-239); las
  listas de handlers se consumen por dispatch (js.rs:99-101); listeners no
  fun y duplicados se ignoran (js.rs:63-71); set_url solo absoluto, host con
  puerto, path vacío → "/", about:blank → pathname "blank" (js.rs:194-217);
  title() relee con fallback al último valor bueno (js.rs:241-250). 13 tests
  nuevos; 19/19 en go/js; 12 paquetes verdes. `Risk: item 2 (S5)` sin cambios.
- **L4** (2026-10-25): **T4 ejecutado por el padre (inline) — RUNTIME JS
  CONSUMADO** (decisión del usuario: «opcion 1 — paridad primero»). Diferencial
  completo de 6 cadenas sobre 80 fixtures: los 9 fixtures JS **ya no difieren
  en stderr** (las líneas `js error:` son byte-idénticas) ni en las líneas
  `title:` mutadas por scripts; `js-dom.html` quedó **idéntico del todo**. Lo
  que aún difiere en esos fixtures (`dump-stdout`, `png-stdout`, `png`) es
  **geometría de layout** de las categorías ya registradas (line-height S1,
  var(), SVG/átomos) — no es dominio JS. Interpretación honesta de S4: el gap
  JS está **cerrado en esencia** (streams JS byte-idénticos); la idénticalidad
  total de los 6 streams es imposible mientras convivan las divergencias
  intencionales del motor nuevo (que NO se revierten). KNOWN_DIFFS.md cierra
  las 9 filas JS con banner RESOLVED y las conserva como registro histórico.
  **Track futuro (opción 1 del usuario)**: APIs de DOM reales
  (getElementById/timers/cookies/innerHTML — el oráculo no tiene ninguna) como
  **divergencia deliberada** con decisión de alcance explícita cuando se abra.
