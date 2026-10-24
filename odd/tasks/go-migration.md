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
| T2 | Paquetes `net` (fetch) + `img` (decode png/jpeg, límites y avisos idénticos) | S3 | 1 | delegado (mimo-v2.6-flash) |
| T3 | Pegamento CSS: libcss via cgo + rewriter (gap/font-family/@font-face) + `Style` + mapping de estilos computados | S3 | 1 | delegado (glm-5.3-flash-free) |
| T4 | `layout`: block + flex + grid + runs de texto (el joyero) | S3,S1 | 2 | pendiente |
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
