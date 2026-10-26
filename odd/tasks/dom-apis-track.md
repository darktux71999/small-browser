# Track de APIs DOM reales (divergencia deliberada del oráculo)

Objetivo: interactividad real de página — empezando por el **banner ToU de
codeberg** (el JS lo muestra con APIs de DOM; nuestro motor lo oculta → es la
banda 1 del diff de Meta 1 al 51%). **Decisión del usuario (2026-10-25)**: se
abre el track («si abrimos el track de dom apis») bajo la filosofía «opción 1»:
esto es una **divergencia deliberada** — el motor Go tendrá éxito donde el
oráculo falla, y se registra como categoría propia en KNOWN_DIFFS (como
`var()`/SVG). La referencia ya NO es el oráculo (que no tiene estas APIs): son
**WHATWG DOM + Firefox**. Las 9 filas JS de KNOWN_DIFFS, cerradas hoy como
byte-idénticas, volverán a diferir en stderr por diseño (documentado al
cerrar cada unidad).

**Contexto Meta 1** (docs/objetivos.md): el track pertenece a «renderizar
codeberg.org como debe verse» — el banner es fidelidad visual y mejora la
métrica de éxito (diff vs Firefox). Meta 2 (UI de searxgo) no se toca.

**Retirada del oráculo** (decisión del usuario pendiente al llegar): al cerrar
este track, el diferencial contra Rust se reemplaza por capturas doradas propias
(Go vs Go) y el motor Rust se archiva. El paso de archivar requiere su OK.

## Specs

- **S1** — APIs DOM core por evidencia: `document.getElementById`,
  `querySelector(All)` (REUSAR el motor de selectores), `classList`
  (add/remove/toggle/contains), `innerHTML`/`textContent` get+set (parseo vía
  go/dom), `setAttribute`/`getAttribute`/`removeAttribute`, `createElement`,
  `appendChild`/`removeChild`, `closest`/`matches` si el script del banner los
  usa. La lista final sale de LEER el script real del banner + los 9 fixtures
  (evidencia, no invento). Semántica WHATWG; aproximaciones citadas.
- **S2** — Timers y eventos de nodos: `setTimeout`/`setInterval`/`clear*` con
  la cola ejecutada en el punto correcto del pipeline, `addEventListener` en
  nodos (no solo global), `document.cookie` get/set si lo usa la evidencia.
- **S3** — Aceptación: el **banner ToU aparece** en el render de codeberg
  (banda 1 colapsa hacia Firefox); KNOWN_DIFFS registra la categoría de
  divergencia DOM y las 9 filas JS quedan anotadas como re-divergentes por
  diseño; el diferencial se mide contra goldens propios (o se documenta el
  cambio de red si la retirada del oráculo está aprobada).

## Tasks

| T | Descripción | Spec | Ruta | Estado |
|---|---|---|---|---|
| T1 | APIs DOM core por evidencia (banner + fixtures) | S1 | muse-spark-1.3-contributor | ✅ (8+3 tests; 4 fixtures con blast radius registrado; bug cgo use-after-free cazado) |
| T2 | Timers + eventos de nodos + cookies (si evidencia) | S2 | muse-spark | pendiente |
| T3 | Aceptación (banner + registro + red de goldens) | S3 | muse-spark | pendiente |

## Log

- **L1** (2026-10-25): apertura del track por decisión del usuario («si
  abrimos el track de dom apis») con la pregunta de alcance respondida por
  docs/objetivos.md: el track pertenece a Meta 1 («como debe verse»); Meta 2 =
  UI de searxgo. Confirmado además que el oráculo ya no es necesario para
  features nuevas (su semántica está extraída; solo queda su red de regresión,
  reemplazable por goldens propios — archivar el Rust es decisión del usuario
  al cierre del track). Rama `feat/go-engine`; runner `go test ./...`.
- **L2** (2026-10-25): **T1 entregado** (muse-spark — `partial` solo porque los
  tests RED en vuelo del track hermano de métricas hacen que el módulo entero no
  compile ahora; su superficie propia va 3× verde). `go/js/dom.go`: driver
  `__bl_dom` sobre la arena go/dom viva + bootstrap WHATWG (wrappers de
  document/element, classList, atributos, innerHTML con parseo vía go/dom,
  matches/closest reusando el motor de selectores, DOMException). **Bug cgo real
  cazado**: `jsgoGoCall` liberaba slots de argv **prestados** → use-after-free
  que segfaulteaba en cualquier callback con string/objeto (los tests solo de
  números no lo detectan — los immediates no llevan refcount). Pipeline:
  `BindDOM` tras `Bind` (degradación graciosa) + gate `DomDirty` → recompute de
  estilos tras scripts conservando el orden del oráculo. Tests: 8 DOM + 3
  pipeline; los 26 js previos intactos (incluido el pin de la superficie del
  oráculo). **Blast radius registrado**: 4 fixtures cambian (cookie-echo/set
  ahora renderizan `sent: none`/`cookie requested`, js-dom ejecuta scripts,
  net-utf8); los otros 5 JS quedan igual por diseño. PNG vivo sin cambios (AE 0
  vs base). **Hallazgo para T2**: el banner sigue oculto porque su JS vive en un
  **bundle externo `<script src>`** que ni el oráculo ni nosotros fetchamos
  (paridad `src`) — mostrarlo exige OTRA divergencia deliberada: fetch de
  scripts externos + la superficie que use el bundle (localStorage/eventos…),
  con la nota S5 ampliada (ejecutar bundles de sitio = más superficie). Nota:
  `querySelectorAll` con sintaxis no soportada iguala vacío en vez de lanzar
  SyntaxError (documentado); innerHTML de `<noscript>` conserva el hijo raw-text.
