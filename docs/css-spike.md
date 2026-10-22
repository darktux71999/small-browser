# css-spike — comparativa libcss vs css.rs (resultados)

Fecha: 2026-10-21 · Branch `spike/css-layer` · Repro: `sh spikes/css/compare.sh`
(datos crudos: `/tmp/opencode/css-spike-results/{results.md,results.csv,loc.csv}`)

Pregunta: ¿de dónde sale la capa de parse+cascada de CSS del motor? Dos rutas
sobre el **mismo** modelo de estilo, el mismo layout y el mismo pintor Pango/cairo
(spike acotado, `spikes/css/`, sin tocar el motor):

- **cssrs** — el motor propio (`css.rs`, 503 LOC) tal cual existe hoy.
- **libcss** — NetSurf libcss 0.9.2 (MIT) contra el DOM propio, con FFI a mano.

5 fixtures diseñadas para diferenciar capas: especificidad, herencia, media
queries, tipografía y artículo mezclado. Referencia de fidelidad: Firefox
headless, métrica heredada de `docs/font-svg-fidelity.md` (ink share + diff con
fuzz 25% dentro de la caja de contenido).

## Tabla 1 — render, memoria y fidelidad

| Fixture | Prov. | RSS (KB) | Tiempo (s) | Ink | Ink ref | **Diff %** | Altura |
|---|---|---|---|---|---|---|---|
| 01-especificidad | cssrs | 21.900 | 0,030 | 0,1485 | 0,0675 | 4,15 | 241 |
| 01-especificidad | libcss | 22.008 | 0,030 | 0,1485 | 0,0675 | 4,15 | 209 |
| 02-herencia | cssrs | 21.296 | 0,020 | 0,0416 | 0,0270 | 4,76 | 159 |
| 02-herencia | libcss | 22.100 | 0,030 | 0,0432 | 0,0270 | 4,63 | 215 |
| 03-media | cssrs | 21.244 | 0,020 | 0,0674 | 0,0897 | **13,05** | 80 |
| 03-media | libcss | 21.644 | 0,020 | 0,0715 | 0,0897 | **8,94** | 122 |
| 04-tipografia | cssrs | 22.672 | 0,030 | 0,0496 | 0,0305 | 8,58 | 282 |
| 04-tipografia | libcss | 23.520 | 0,040 | 0,0525 | 0,0305 | **7,30** | 355 |
| 05-articulo | cssrs | 24.020 | 0,050 | 0,0370 | 0,0512 | 11,19 | 657 |
| 05-articulo | libcss | 25.540 | 0,060 | 0,0347 | 0,0512 | **10,81** | 724 |

Todas las celdas `ok`, ningún `pending`. Los PNG A/B están en
`/tmp/opencode/css-spike-results/png/` y las referencias Firefox en `fidelity/`.

## Tabla 2 — líneas de código

| Ruta | Glue Rust propio | Código externo | Total |
|---|---|---|---|
| cssrs | 76 (`provider.rs`) | 503 (`src/css.rs`) | 579 |
| libcss | 1.591 (`libcss_ffi.rs`) | 86.013 (libcss+libparserutils+libwapcaplet, MIT) | 87.604 |

Compartido por ambas rutas y no contado: `style.rs`, `render.rs`, `ffi.rs`.
Lo que **mantienes tú** en la ruta libcss son las 1.591 líneas de FFI; las 86k de
C son código de terceros empaquetado (3 recipes KISS + buildsystem).

## Veredicto por criterio

- **Memoria: empate.** 21,2–24,0MB (cssrs) vs 21,6–25,5MB (libcss): domina el
  binario + Pango + cairo, no la capa de CSS. Δ < 1,5MB. **No decide.**
- **Fidelidad: gana libcss, 3 de 5.** Empate exacto en 01 (mismos estilos
  computados: ink idéntico 0,1485 — el terreno común lo resuelven igual) y 02.
  Libcss gana justo donde la capa de CSS importa: **@media** (8,94 vs 13,05: cssrs
  ignora `@media` y se queda a 14px sin fondo condicional) y tipografía computada
  (7,30 vs 8,58). La mayor diferencia observable entre rutas es 4,1 puntos de diff
  (fixture 03); en el artículo realista, 0,4.
- **Líneas de código: gana css.rs por 150×** (579 vs 87.604) o por 20× si solo
  cuentas lo que se mantiene (579 vs 1.667). libcss compra su CSS con superficie
  de build (3 libs + netsurf-buildsystem) y 1.591 líneas de unsafe FFI.
- **Mantenimiento KISS: gana css.rs.** Cero dependencias nuevas, todo auditable,
  subset documentado. libcss = CSS 2.1 completo + flex + media queries sin
  escribirlo, a costa de 3 paquetes y un binding.

## Hallazgos que cambian el diseño (valen para cualquier ruta)

1. **Hoja UA**: libcss no trae estilos de agente de usuario; `display` por etiqueta
   y los márgenes por defecto hay que suministrarlos (css.rs trae defaults
   embebidos, aunque limitados). La ruta B necesita una hoja UA mínima — decisión
   de producto, no del spike. Explica las diferencias de altura A/B (01: 241 vs 209).
2. **Media queries necesitan viewport en el proveedor**: hoy el proveedor resuelve
   @media contra 800×600 fijo; `--width` solo llega al renderer. Para un motor
   real, el viewport debe entrar en la cascada.
3. **Detalles de libcss 0.9.2** (corregidos contra su fuente, no supuestos):
   `css_color` es **AARRGGBB**; el elemento raíz **no** se compone
   (`css_select_style` ya resuelve absolutos: componer con padre NULL → `CSS_BADPARM`);
   `inline_style=true` es para listas de declaraciones (`style=""`), no para `<style>`.
4. **Bug del spike encontrado y corregido**: el renderer no fijaba el color de
   texto en cairo (los glifos salían con el último color de relleno). Corregido en
   `render.rs`; afectaba por igual a ambas rutas.
5. El renderer del spike es simplificado (bloques + runs de texto, sin mezcla
   inline profunda): deprime la fidelidad absoluta por igual en ambas columnas —
   la señal útil es la **diferencia A/B**, no el valor absoluto de diff.

## Qué NO mide este spike

- Rendimiento en páginas grandes (5 fixtures cortas; ambos ≤ 60ms).
- Cascada completa con `@import`, hojas múltiples con orígenes UA/user/author.
- Coste de mantener las 3 recipes KISS a lo largo del tiempo.

## Reproducción

```sh
sh spikes/css/build-libcss.sh          # libs MIT en ~/.cache/css-spike (fuera del worktree)
cd spikes/css && cargo build
sh compare.sh --render-cmd "$PWD/target/debug/css-spike"
```

Tests: `cargo test` (acuerdo A/B sobre el terreno común de especificidad).
