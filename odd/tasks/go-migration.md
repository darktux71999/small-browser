# Migración del motor a Go (dual-engine)

Objetivo: portar el motor de browser-lite de Rust a Go usando el motor Rust
existente como **oráculo de referencia**: cada módulo Go se acepta solo cuando
sus salidas casan con las del oráculo. Rama `feat/go-engine`. Test runner:
`go test ./go/...` + `go run ./go/cmd/differential` (corpus `test/data/`).

## Specs
- **S1** — Aceptación dual-engine: para cada fixture de `test/data/`, el motor Go
  produce `--dump` y `--png` **byte-idénticos** a los del motor Rust. Cualquier
  diferencia exige explicación y visto bueno explícito antes de aceptar.
- **S2** — Mismas librerías C via cgo (libcss 0.9.2 + libwapcaplet, Pango,
  Cairo, SDL3, QuickJS más adelante): Go reemplaza el pegamento, no los
  motores; mediciones y píxeles comparables. El texto de usuario de cada
  petición: «Migrar el motor a Go ahora».
- **S3** — Conducta portada = la del motor actual: HTML→DOM, cascada CSS
  (libcss + los trucos del rewriter: carriers de gap, normalización de
  font-family, extracción de @font-face), layout block/flex/grid + runs de
  texto, tipografía real (stacks + @font-face), fondos, bordes/redondeos
  (T11), viewport 1000×760 con scroll, CLI `--dump`/`--png`/ventana idéntica.
- **S4** — Tests portados 1:1 (19 suites / 174 tests, mismos fixtures y mismas
  expectativas) en `go/...`; verdes con `go test`.
- **S5** — Sanidad: `go test -race ./go/...` verde (el pipeline de descargas va
  concurrente); sin archivos de scratch dentro del worktree.
- **S6** — El motor Rust se mantiene compilable como oráculo hasta la
  aceptación completa; su destino (conservar como referencia o retirarlo) es
  decisión posterior del usuario.
- **S7** — Paridad de Meta 1: el motor Go reproduce la medición de codeberg.org
  del oráculo (altura y bandas dentro de tolerancia cero de bytes en `--png`).

## Tasks
| T | Unidad | Specs | Ola | Estado |
|---|---|---|---|---|
| T1 | Esqueleto `go/` (go.mod), paquete `dom` (parser HTML5) + runner diferencial `cmd/differential` | S1,S3 | 1 | ✅ (17 tests; oráculo-vs-oráculo 71/71 idénticos) |
| T2 | Paquetes `net` (fetch) + `img` (decode png/jpeg, límites y avisos idénticos) | S3 | 1 | ✅ (83 ejecuciones de test; paridad de strings byte a byte) |
| T3 | Pegamento CSS: libcss via cgo + rewriter (gap/font-family/@font-face) + `Style` + mapping de estilos computados | S3 | 1 | ✅ (31 tests; glm murió a mitad → completado por muse-spark con 6 causas raíz) |
| T4a | `text` (shaper Pango) + `layout` bloque/caja/runs | S3,S1 | 2 | ✅ (17 tests; diferencial humo 14 fixtures idénticos) |
| T4b | `layout`: contexto flex (fila/columna, wrap, gap, grow, justify, align) | S3,S1 | 2 | delegado (muse-spark-1.3-contributor) |
| T4c | `layout`: contexto grid (px/fr/auto/repeat, span, gap) | S3,S1 | 2 | pendiente |
| T5 | `paint`: Cairo via cgo + CLI `--png`/`--dump` | S1,S3 | 3 | pendiente |
| T6 | `shell`: SDL3 via cgo (headers reales, sin offsets a mano) + scroll | S3 | 3 | pendiente |
| T7 | Tests portados + diferencial completo + paridad Meta 1 | S4,S5,S7 | 4 | pendiente |

## Log
- **L1** (2026-10-23): petición del usuario verbatim: «es muy tarde? para pensar
  en migrar todo a go? o lo que esta escrito en rust a GO?» y decisión
  «Migrar el motor a Go ahora» (estrategia dual-engine con el motor Rust como
  oráculo, elegida de las opciones presentadas). Contexto medido al decidir:
  motor 10.347 líneas Rust + 4.681 de tests (19 suites/174 tests), 64 bloques
  extern "C" escritos a mano. Arquitectura acordada: `go/` con paquetes
  dom/css/net/img/layout/paint/shell/cmd, mismas C libs via cgo, aceptación
  por diferencial byte a byte contra el oráculo. Ola 1 lanzada en paralelo
  (3 unidades, modelos del pool: muse-spark-1.3-contributor, mimo-v2.6-flash,
  glm-5.3-flash-free).
- **L2** (2026-10-23): **T1 entregado** (muse-spark-1.3-contributor): `go/go.mod`
  (module browser-lite, go 1.25 — x/net v0.58.0 lo exige), `go/dom` (parser
  HTML5 con el subconjunto exacto de selectores de `src/dom.rs`, incluido el
  quirk del `>` como descendiente y tags ASCII-case-insensitive) y
  `go/cmd/differential` (compara 6 streams + bytes de png por fixture,
  política de fallo-idéntico-cuenta-como-idéntico — el oráculo mismo sale
  con exit 1 en `net-latin1.html` — y normaliza la línea `wrote <path>`).
  **Validación clave: oráculo-vs-oráculo sobre los 71 fixtures → 71 idénticos,
  exit 0** (la herramienta de aceptación S1 está probada de extremo a extremo).
  17 tests Go (7 dom + 10 differential), vet limpio. Nota: `go 1.25` en go.mod
  (toolchain requerido por x/net).
- **L3** (2026-10-24): **ola 1 completa** (T1 ✅ T2 ✅ T3 ✅). T3 murió a mitad con
  `glm-5.3-flash-free` (segundo modelo gratuito que se nos cae en una unidad
  larga → fuera del pool, sustituido por `qwen3.8-flash` por decisión del
  usuario) y lo completó `muse-spark` (T3b) tras 4 correcciones del padre
  (función duplicada en glue.h; definiciones reales movidas a glue.c — varios
  TUs de cgo incluyen la cabecera y Go llama a esas funciones; el cookie de
  selección convertido en **handle de registro** codificado con nodePtr porque
  cgo prohíbe punteros Go sin fijar y el Pinner no cubre la clausura transitiva;
  off-by-one del harness de test con `HasPrefix(html[i:i+N])`). Las **6 causas
  raíz** de T3b: texto de `<style>/<script>/<title>` huérfano como hermano (cero
  hojas), elementos void rompiendo la pila, atributos sin parseo de comillas,
  sintesis html/head/body estilo lexbor (libcss blockifica los root elements),
  defaults `New()` vs cero, `Url::join` (Rust) vs `JoinPath` (Go), null pending
  URL en imports, y cookie con base 8192 (el runtime aborta con punteros
  < minLegalPointer 4096). **Hallazgo**: libcss 0.9.2 resuelve `medium`/`thick`
  a **2px/4px** (no 3px/5px de la tabla legacy) — un test corregido con
  evidencia del oráculo (src/css_libcss.rs:2728-2748). 31/31 en go/css, módulo
  entero verde, vet y -race limpios. Ola 2 siguiente: T4a layout block+texto.
- **L4** (2026-10-24): **T4a entregado** (muse-spark): `go/text` (shaper Pango
  via cgo, 72dpi, familias por fontconfig con el mapa de genéricas, atributos
  de interlineado/tracking, `ResolveFamily` first-match) + `go/layout` (cajas,
  modelo de caja con la semántica T11: el borde vive dentro de `w`, `h` crece
  como el padding; formato de bloque, `flush_text` con cajas `#text`, `<img>`,
  runs para pintar). Flex/grid caen a bloque plano (pinneado por test) — es la
  brecha de T4b/T4c. 17 tests; módulo entero verde (7 paquetes); vet/race/gofmt
  limpios. **Diferencial de humo del propio worker: 14 fixtures byte-idénticos
  contra el `--dump` del oráculo** en la ruta de bloque. Detalle fino: Pango
  reporta la primera línea con un bearing de ~2.2px a 16px — las aserciones de
  origen exacto son incorrectas y el oráculo absorbe el mismo valor.
