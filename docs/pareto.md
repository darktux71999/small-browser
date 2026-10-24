# Pareto de renderizado — el 20% que cubre el ~80% de las páginas

Referencia de prioridades para browser-lite. Evita la ingeniería inversa ciega
(un sitio a la vez) y ancla las decisiones en **datos de uso real**.

## Fuentes canónicas

| Fuente | Qué aporta |
|---|---|
| [HTTP Archive Web Almanac — CSS](https://almanac.httparchive.org/) (2020–2025) | % de páginas que usan cada propiedad y valor |
| [MDN Browser Compatibility Report](https://mdn-web-dna.s3-us-west-2.amazonaws.com/MDN-Browser-Compatibility-Report-2020.pdf) | dolores reales de layout: flexbox 39%, grid 35%, responsive 31%, forms 25%, scrolling 23% |
| caniuse / Chrome Platform Status / [webstatus.dev](https://webstatus.dev) | uso en pageviews por feature (flexbox 97% global) |
| [AreWeBrowserYet](https://arewebrowseryet.com/metrics/browser-feature/) | cobertura de motores ponderada por popularidad (flexbox 0.83, grid 0.40) |
| *Web Browser Engineering* (Panchekha & Harrelson) | orden de construcción incremental probado en un motor real |

## El 20% vital (orden de impacto × presencia en páginas)

| # | Feature | Presencia medida | browser-lite | Prioridad |
|---|---|---|---|---|
| 1 | Modelo de caja + `display` block/inline/none | prácticamente 100% | ✅ | — |
| 2 | Cascada: selectores, especificidad, `!important`, herencia | 100% | ✅ (libcss) | — |
| 3 | Texto inline: wrap, `font-size/weight/family`, `line-height`, `text-align` | top de propiedades | ✅ familias resueltas vía fontconfig (stacks + genéricas) | ✅ |
| 4 | **`@font-face` / webfonts** | ~50%+ de páginas (`font-display: swap` en 49.6%) | ⚠️ ttf/otf ✅ (app-fonts fontconfig); woff2/local() ❌ con aviso | media-alta |
| 5 | Fondos: `background-color/image/repeat/position/size` | 96/90/85/91/82% | ⚠️ color+image+repeat ✅, position/size ❌ | media-alta |
| 6 | `border` / `border-radius` | 71% / 65% (px dominante 65–71%) | ⚠️ plain-CSS ✅; bloqueado en páginas reales por `var()` y unidades rem | media-alta |
| 7 | **Flexbox** | 97% de pageviews | ✅ mínimo (gap, grow, wrap) | — |
| 8 | **Grid** | ~50–90% y creciendo | ✅ mínimo (px/fr/auto/repeat, span) | media (avisos R3-*) |
| 9 | `position` rel/abs/fixed/sticky + `z-index` + `overflow` | Baseline desde v1; sticky en todo nav moderno | ❌/parcial | **alta** |
| 10 | Media queries (`min/max-width`) | ~80% (width y display, los más usados) | ✅ en libcss; ❌ en la cascada manual de grid (R3-3) | media |
| 11 | Gradientes (`linear-gradient`) | ~73% de páginas | ❌ | media |
| 12 | `box-shadow` | muy común en tarjetas/botones | ❌ | media |
| 13 | Pseudo-elementos `::before/::after` | iconos (Font Awesome solo: 8.5–9.3% de páginas) | ❌ | media |
| 14 | Custom properties (`var()`) | colores en 1 de 5 páginas; **Codeberg: 406 bordes con `var()` que libcss descarta** | ❌ | **alta** (dato de corpus, subió de media-baja) |
| 15 | Opacity / transform / filter (`blur` 4%) | base de transiciones | parcial (alpha ✅) | media-baja |
| 16 | Transiciones / animaciones | `transition` 85–90%, `animation` 70–77% | ❌ | baja (pulido, no estructura) |
| 17 | Tablas y listas semánticas | sitios de contenido/docs | ❌/parcial | media-baja |

## Reglas de decisión

1. **Estructura antes que pulido**: lo que cambia dónde están las cajas
   (3–9, 14) pesa más que lo que las embellece (11–12, 15–16).
2. **Medir, no suponer**: cada unidad se cierra con medición (bandas vs
   Firefox claro) y diferencial byte-a-byte, como en T6/T8/T9.
3. **Un sitio de referencia no es el corpus**: codeberg.org y searxgo son
   metas de la hoja de ruta, pero la prioridad viene del Almanac, no de lo que
   le falte a Codeberg en particular.
4. **Avisos del review = deuda de calidad** priorizada por esta tabla
   (R3-1/R3-2/R3-3 tocan features de las filas 8 y 10).
5. Revisar esta tabla con cada nuevo Almanac anual.

## Estado del arte (2026-10-23)

- Completado: modelo de caja, cascada libcss, texto con Pango, imágenes
  `<img>` (PNG/JPEG), fondos color+image+repeat, flex mínimo, grid mínimo,
  viewport 1000×760 con scroll, media queries en la ruta libcss.
- Mayor brecha visual hoy: **webfonts** (todo cae en monoespaciada), luego
  bordes/redondeos y gradiente del héroe.
- Mayor brecha estructural hoy: **position/sticky + overflow**, y la cascada
  manual de grid que ignora `@media` (R3-3).
