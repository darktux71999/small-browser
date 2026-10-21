# Comparativa de APIs: ¿de dónde sale el HTML/CSS del motor?

Fecha: 2026-10-21. Investigación read-only sobre tres candidatos para la capa de
parse + cascada del motor (el layout de píxeles, el texto con Pango y la pintura
con cairo siguen siendo propios en todas las rutas):

1. **chawan** (Nim, Unlicense) — motor HTML/CSS de un navegador TUI ya instalado
2. **libs de NetSurf** (C, MIT) — libcss + hubbub + libdom + libparserutils + libwapcaplet
3. **glue MacSurf/NeoSurf** — la receta "NetSurf + QuickJS" probada en producción

Fuentes leídas: clones temporales en `/tmp/opencode/api-survey/{chawan,libcss,
libhubbub,libdom,libparserutils,macsurf}` (chawan 185 ficheros .nim, libcss 246 C,
libdom 308 C, macsurf 2.599 ficheros) + documentación pública de macsurf.org,
github.com/mplsllc/macsurf, github.com/CobaltBSD/neosurf, sr.ht/~sircmpwn/visurf.

---

## 1. Resumen ejecutivo

| | chawan html/css | libs NetSurf | MacSurf/NeoSurf |
|---|---|---|---|
| Lenguaje / licencia | Nim / Unlicense | C / MIT | C / GPL-2 (árbol) |
| Parse HTML | `chame0` (genérico, portable) | hubbub o libdom/hubbub | ídem (vendored) |
| DOM | **acoplado a un QuickJS forked** (Node = JSRef) | libdom (747 fns) o DOM propio | libdom + binding JS a mano |
| CSS parse+cascada | lógica portable (~3.300 LOC) | **libcss, enlazable contra DOM propio** | libcss + reescritor propio |
| Layout | celdas de terminal (1/64 celda) | **no está en las libs** (GPL-2 en el árbol) | layout de NetSurf + grid propio |
| JS binding | el DOM *es* objetos JS | no incluido | 17.872 líneas escritas a mano |
| Coste de integración | portar a Rust (FFI no viable) | FFI medio: 36 callbacks + shim lwc | no reutilizable tal cual |

**Veredicto corto**: todas las rutas convergen en *tu DOM + tu layout de píxeles +
Pango + QuickJS*. La única decisión real es **de dónde sale el parse+cascada de CSS**:
`libcss` (MIT, enlazable contra tu DOM), portar la lógica de chawan a Rust
(Unlicense), o quedarte con `css.rs`.

---

## 2. chawan (Nim, Unlicense)

Inventario: `src/html/` ~15.000 LOC (dom.nim 7.992, env 1.152, event 1.132,
form 1.192, chadombuilder 472…), `src/css/` ~13.250 LOC (layout 3.359,
cssvalues 2.841, cssparser 1.950, color 1.054, csstree 842, render 729,
cascade 570, mediaquery 472, box 447, sheet 435, match 330, cell 128, lunit 95).

**Acoplamiento — no hay costura limpia:**
- El DOM es un grafo de objetos JS: `Node = JSRef[NodeObj]` (`chawan/src/html/dom.nim:311`),
  asignado por `JS_NewForeignObject` de un **QuickJS forked** (`chawan/lib/quickjs/quickjs.c:6548`,
  compilado con `-DCHA_BUILD`). Toda la vida de un nodo pasa por ahí (`jsref.nim:16-49`).
- Hasta el tokenizador de CSS importa el runtime JS: `cssparser.nim:6` → `js/jstypes` → `js/quickjs`.
- Infraestructura propia entrelazada: `io/console`, `server/*` (URLs de `@import`),
  `utils/luwrap` (saltos de línea, usa `js/libunicode`+`js/libregexp`).
- Build global: `nim.cfg:5-13` (`--mm:arc`, `--include:"utils/myaddr"`) → el C emitido
  no es una ABI plana.

**DOM**: punteros comprimidos (`internalNext/Prev` codifican sibling/root/lastChild,
`dom.nim:300-323`), atributos ordenados por átomo internado (`catom.nim`).

**Cobertura CSS**: selectores completos (tipo/id/clase/attr/:is/:not/:where/:nth-*,
combinadores), cascada correcta con orden UA/user/author + `!important` + capas
(`cascade.nim:469-498`), media queries (`mediaquery.nim:28-37`), 35 unidades.
**Falta**: solo 3 at-rules (`@import`, `@media`, `@layer`; `cssparser.nim:128-131`) —
no `@supports`, `@font-face`, `@keyframes`, `@container`; enum cerrado de ~78
propiedades; sin `transform`, transiciones/animaciones, `gap`/`grid-*`; **sin
layout grid** (`layout.nim:3320-3323`).

**Layout**: geometría en celdas de terminal, `LUnit` = int32 con 6 bits fraccionarios
= 1/64 de celda (`lunit.nim:1,9`); la grilla de caracteres la produce después
`render.nim`. Un renderer de píxeles tendría que reescribir medida de texto, saltos
de línea (`LUContext` de `luwrap`), pintura/z-index y elementos reemplazados.
El árbol de cajas (block/inline/table/flex, `layout.nim:2105-3262`) es reusable en principio.

**Veredicto**: compilarlo como librería C **no es viable** (QuickJS forked como
allocador + ARC + macros). **Portar selectivamente a Rust sí**: lo más valioso es
`chame0` (parser HTML5 genérico, dominio público), luego `cascade.nim` + `match.nim` +
`cssparser.nim` + `sheet.nim` (~3.300 LOC de lógica spec-shaped), y de `layout.nim`
solo los algoritmos (descartando LUnit/strwidth/luwrap/render). `dom.nim` no se porta.

---

## 3. libs de NetSurf (C, MIT)

Versiones: libcss 0.9.2, libhubbub 0.3.8, libdom 0.4.2, libparserutils 0.2.5,
libwapcaplet 0.4.3.

**Secuencia mínima de llamadas:**

```c
// A) HTML -> DOM (vía binding de libdom sobre hubbub)
dom_hubbub_parser_create(&params, &parser, &doc);   // libdom/bindings/hubbub/parser.h:69
dom_hubbub_parser_parse_chunk(parser, data, len);   // :83
dom_hubbub_parser_completed(parser);                // :91

// B) hojas de estilo
css_stylesheet_create(&params, &sheet);             // params.url y .resolve son OBLIGATORIOS
css_stylesheet_append_data(sheet, data, len);       // tolerar CSS_NEEDDATA
css_stylesheet_data_done(sheet);                    // puede devolver CSS_IMPORTS_PENDING

// C) cascada
css_select_ctx_create(&ctx);
css_select_ctx_append_sheet(ctx, sheet, origin, media_query);
css_select_style(ctx, node, &unit_ctx, &media, inline, &handler, pw, &results);
css_computed_style_compose(parent, child, &unit_ctx, &out);  // ¡paso obligatorio!
```

Detalle crítico: `css_select_style` **no hereda**; hay que hacer el compose manual
(`libcss/src/select/select.c:1246-1253`).

**Superficie**: libcss = 18 entry points + 110 getters `css_computed_*` sobre 126
propiedades; hubbub = 8 fns de parser + 18 callbacks de árbol; libdom = **747 fns**;
libparserutils = buffers/vectores/codecs. Sin hooks de allocador (malloc plano),
longitudes en coma fija 22:10 (`fpmath.h:19-23`).

**El hallazgo accionable**: `css_select_style` toma `void *node` opaco + un handler de
36 callbacks → **libcss se puede conducir desde tu propio DOM sin libdom**, evitando
las 747 funciones y los vtables `static inline` de libdom (inbindiables con bindgen).

**Trampas**:
- `libwapcaplet` (lwc) es **header-only**: `lwc_string_ref/unref/isequal` son macros y
  `static` → no se enlazan; hay que reimplementar/shim. Singleton global **sin lock**.
- Los arrays que se entregan a libcss deben salir de malloc de libc.
- Build: todas las libs exigen `netsurf-buildsystem` (paquete aparte) y traen `-Werror`.

**Gaps**: las libs **no** exportan layout, pintura, imágenes ni scripting. El box
layout de NetSurf vive en el árbol del navegador (`content/handlers/html/layout.c`,
`box_construct.c`, `table.c`, `layout_flex.c`…) y **es GPL-2** (`layout.h:6-8`),
mientras las libs son MIT. Tomar solo las libs mantiene MIT; tomar el layout
impondría GPL-2 — lo que refuerza el plan de layout propio con Pango.

**Cobertura libcss**: CSS 2.1 + partes de 3: media queries completas (rangos, or/not,
`prefers-color-scheme`, pointer/hover), selectores con `:not`, `:nth-*`, flexbox,
multicolumna, `calc()` sin resolver. **Falta**: `var()`/custom properties,
`@supports`, `@keyframes`; grid solo como valor de `display` sin propiedades `grid-*`.

**Coste FFI Rust**: ~900–1.200 items de bindgen + ~1.500–2.500 líneas de wrappers
seguros (110 getters tipados son la masa). Sin libdom, el slice más barato y creíble.

---

## 4. MacSurf y NeoSurf/visurf

- MacSurf **no** es "5 libs + shell": ha vendido el **árbol completo de NetSurf 3.12**
  (`macsurf/browser/netsurf/` con content/, desktop/, frontends/…) y su código propio
  son ~50 ficheros `frontends/macos9/` más decenas de miles de líneas de edits dentro
  del motor. Las libs vendored están parcheadas (`libdom/src/core/dom_string.c:948`).
- **macQJS no es compatible con la API de Duktape**: es un port C89 de QuickJS con la
  API nativa `JS_*` (~63.000 líneas). El swap Duktape→QuickJS significó reimplementar
  el seam de 10 funciones de NetSurf (`content/handlers/javascript/js.h:58-118`:
  `js_initialise/newheap/exec/fire_event/handle_new_element/…`) y escribir a mano un
  **binding DOM de 17.872 líneas** (`frontends/macos9/javascript/macsurf_qjs.c`).
  El nsgenbind de NetSurf ya no se usa.
- Truco reutilizable: las APIs de conveniencia del DOM (`classList`, `className`) se
  escriben como **cadenas de JS eval'd sobre los prototipos**, con un núcleo C mínimo
  de primitivas (attr/traversal/parse): `qjs_helper_fn` (`:5719`), `qjs_el_install_js_helpers` (`:5747`).
- Superficie mínima de binding DOM observada: traversal, atributos, `textContent`,
  `innerHTML` (parse+serialización reales), `createElement(NS)/createTextNode/
  createComment/createDocumentFragment`, `querySelector(All)` acotado, `document.cookie`,
  eventos + orden `DOMContentLoaded`/`load`.
- Layout: box layout de NetSurf + `layout_grid.c` propio (1.782 líneas, con IDs de
  propiedades privados `CSS_PROP_MACSURF_GRID_*` y un reescritor de CSS de 6.576 líneas:
  la "CSS moderna" de MacSurf es reescritura de texto, no implementación de spec).
- Updates tras JS: "reconvert" = rebox + relayout completo, con debounce (`html.c:4860-4914`).
- **NeoSurf (GPL-2) es el modelo estructural**: `libneosurf` como shared library
  (SOVERSION 14) que traga todo el motor, y un contrato de embebido de **6 vtables**
  (`neosurf_table { misc, clipboard, window, fetch, bitmap, layout }` →
  `neosurf_register()` → `neosurf_init()` → `browser_window_create()`). Su frontend
  **visurf** (Drew DeVault, Wayland + cairo + pango, keybindings vi) son **13 ficheros**
  con un `plotter_table` de 13 slots en ~440 líneas de cairo. NeoSurf sigue con
  Duktape (`#TODO: quickjs/wasm3`).

---

## 5. Qué se lleva de cada uno (independientemente de la ruta elegida)

1. **El contrato de 6 vtables de NeoSurf + `plotter_table` de 13 slots de visurf**:
   el diseño exacto de una shell delgada sobre un motor — directamente trasladable a
   la separación `shell -> paint -> layout -> css -> dom` de browser-lite.
2. **La lista de MacSurf del binding DOM mínimo** (traversal, attrs, textContent,
   innerHTML, createElement*, querySelector acotado, eventos + DOMContentLoaded): el
   alcance realista de nuestro puente `__dom_*` con QuickJS.
3. **El truco de helpers como JS eval'd** sobre prototipos: barato para ampliar la
   superficie del DOM sin escribir C/Rust por cada método.
4. **`css_computed_style_compose` en dos pasos** (o su equivalente): la herencia no es
   gratis en ningún diseño de cascada.
5. **Aviso de licencia**: el layout de NetSurf (flex incluido) es GPL-2; solo las libs
   son MIT.

---

## 6. Rutas descartadas

- **chawan como librería C**: sin costura — el DOM depende de un QuickJS forked como
  allocador, ARC de Nim, macros y flags globales de compilación.
- **libdom**: 747 funciones con vtables `static inline` inbindiables; se evita usando
  tu DOM con `css_select_style(void *node, …)`.
- **Layout de NetSurf**: GPL-2 contagiaría el proyecto; además es layout de cajas
  clásico que hay que mantener, y Pango ya cubre nuestro pain real (texto).

## 7. Decisión pendiente

La comparativa no decide por sí sola; las tres opciones vivas para parse+cascada son:

- **A. libcss (MIT) contra tu DOM propio**: CSS real (2.1 + flex + media queries) sin
  escribirlo; coste: 5 paquetes KISS (libcss, hubbub, parserutils, wapcaplet +
  buildsystem), 36 callbacks, shim de lwc, coma fija 22:10.
- **B. Portar a Rust la lógica de chawan (Unlicense)**: `cascade`+`match`+`cssparser`+
  `sheet` (~3.300 LOC), cero dependencias nuevas y cero quirks de FFI; cobertura menor
  (78 propiedades, 3 at-rules) y trabajo de port.
- **C. `css.rs` propio + Pango**: cero trabajo nuevo de CSS, subset documentado;
  máxima fidelidad limitada.

## Fuentes

- chawan: github.com/sourcehut-mirrors/chawan (mirror de sr.ht/~bptato/chawan)
- NetSurf libs: github.com/netsurf-browser/{libcss,libhubbub,libdom,libparserutils}
- MacSurf: github.com/mplsllc/macsurf, macsurf.org/story.html, macsurf.org/architecture.html
- NeoSurf/visurf: github.com/CobaltBSD/neosurf, sr.ht/~sircmpwn/visurf,
  drewdevault.com/blog/visurf-announcement
