# Objetivos del motor

Pausa: 2026-10-22. Este documento fija hacia dónde va `browser-lite`, qué funciona
hoy y qué falta, para retomarlo sin rehacer la investigación.

## Meta 1 — renderizar codeberg.org como debe verse

Por qué: benchmark real, moderadamente complejo (HTML grande, CSS moderno, JS
inline), y buen punto de partida — no es la web más minimalista, pero tampoco un
laberinto.

**Estado hoy** (probe del 2026-10-22, commit `cfafc50`+): el motor carga
`https://codeberg.org` de verdad (curl/TLS + charset + redirecciones), parsea con
lexbor, aplica estilos con libcss (hoja UA + `@media` contra el viewport) y pinta
block-flow con cairo → contenido completo y jerarquía visible (títulos, listas,
footer, idiomas), 1000×2647, **35MB RSS** (frente a ~180MB/pestaña del experimento
Blitz, retirado). La ventana SDL3 abre y cierra limpiamente.

**Qué falta, por orden** (actualizado tras la Ola 1):

1. **Subrecursos CSS — HECHO** (Ola 1): `<link>` y `@import` se descargan y
   cascadan en orden de documento; en codeberg.org el ink pasa de 0,043 → 0,206.
   **Imágenes pendientes**: quedan huecos donde van logos e iconos.
2. **`window` y APIs DOM mínimas — HECHO** (Ola 1): `window` (alias del global),
   `location`, `navigator`, `addEventListener`; los scripts de Codeberg ya no
   reportan errores.
3. **Layout flex/grid — nuevo gap** (visible en el render): la navegación sale en
   columna en vez de fila y las columnas apiladas. Lo necesitan Codeberg y la UI
   de searxgo (Meta 2).
4. **Bug de wrapping**: el envoltorio es por recuento de caracteres
   (`layout.rs:152`) y corta líneas anchas; lo absorbe la migración a Pango.
5. **Pango para el texto** (interlineado, justificación, tracking, `@font-face` vía
   fontconfig): tarea T3 de `odd/tasks/css-libcss.md`; el renderer del spike
   (`spikes/css/src/render.rs`) ya tiene el camino hecho.

**Criterio de éxito**: comparar contra Firefox headless con la métrica ya probada
(ink share + diff con fuzz 25% dentro de la caja de contenido —
`spikes/css/fidelity.py`, método documentado en `docs/font-svg-fidelity.md`).

## Interfaz (decisión tomada)

La navegación del navegador será **a puro teclado, estilo luakit o vimb,
inspirada en vi**: modos, comandos y keybindings vi-like, sin depender del ratón.
Aplica cuando toque construir la UI (tras la Meta 1); nada del motor actual lo
contradice (la shell SDL3 es solo el lienzo).

## Meta 2 — renderizar la UI que genera searxgo

El caso de uso real que justifica el motor: la UI que genera **searxgo** (su
programa). Ni NetSurf ni chawan la renderizan bien — solo a medias; con flags de
texto plano/HTML se ve, pero ese no es el punto. Consecuencia de diseño: el motor
necesita la parte de CSS que a esos les falta (probablemente flex/grid y selectores
modernos — por confirmar con captura de la página real).

Próximo paso concreto al retomar: congelar una página real de searxgo como fixture
en `test/data/`, medirla con la sonda de fidelidad y listar los gaps de CSS que
arroja (eso define el alcance de la Meta 2).

## Pila actual (todo KISS, instalado en el sistema)

| Capa | Componente | Estado |
|---|---|---|
| HTML → DOM | lexbor 3.0.1 | en uso |
| CSS parse + cascada | libcss 0.9.2 + libparserutils + libwapcaplet (MIT) | en uso (hoja UA propia + viewport en la cascada) |
| CSS subset propio | `src/css.rs` | legado, lo usa el spike como comparación |
| Layout | block flow propio (`src/layout.rs`) | en uso |
| Texto | layout propio | **pendiente migrar a Pango** |
| Pintura | cairo 1.18 | en uso |
| JS | QuickJS 0.17 + puente DOM | en uso (le falta `window`) |
| Red | curl (charset, redirecciones, async) | en uso |
| Ventana | SDL3 | en uso |
| Storage / cookies / sandbox | sqlite / jar propio / seccomp+bubblewrap | escritos, fuera del camino de render |

Memoria medida: ~20MB (página local), 35MB (codeberg.org). Binario debug ~20MB.

## Dónde está cada cosa

- `src/` — el motor (`dom`, `css_libcss`, `layout`, `paint`, `js`, `net`, `shell`,
  `css` legado, más `storage`, `cookies`, `sandbox`, `tabs`, `history`).
- `spikes/css/` — spike comparativo css.rs vs libcss + harness de medición
  (`compare.sh`, `fidelity.py`) y fixtures de diferenciación.
- `odd/tasks/*.md` — logbooks ODD: `css-spike.md` (comparativa) y `css-libcss.md`
  (adopción de libcss, con la verificación independiente y sus advisories abiertos:
  `vw`/`vh` invertidos en libcss 0.9.2 y `rem` sin el font-size del raíz).
- `docs/` — `api-comparativa.md` (por qué libcss), `css-spike.md` (números),
  `font-svg-fidelity.md` (método de sonda), este documento.
- Recipes KISS de las libs: `~/proyectos/sprint/extra/{netsurf-buildsystem,
  libwapcaplet,libparserutils,libcss}`.

## Estado del árbol al pausar

- Branch `feat/css-libcss`: motor con libcss verificado (sin bloqueadores),
  64 tests verdes, worktree limpio.
- Este cierre: soporte de URLs http(s) en `main.rs` (vía `net::fetch_text`) y este
  documento.
- Se puede borrar `~/.cache/css-spike` cuando se quiera: el motor ya enlaza las
  libs del sistema desde `/usr/lib`.
