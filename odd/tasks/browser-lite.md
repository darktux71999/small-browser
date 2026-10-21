# browser-lite

Objetivo: navegador desktop ligero desde cero, reutilizando el stack KISS
instalado. Decisión de lenguaje: **Rust** (FFI fino a lexbor/QuickJS; Go se
descarta por el coste de cgo en el camino caliente DOM/JS).

- Branch: main (repo local nuevo, sin remoto)
- Test runner: `cargo test` + `--dump` determinista
- Stack: rust 1.99, quickjs-ng 0.17 (libqjs), lexbor 3.0.1, cairo 1.18.6
- Reuso del sistema: sdl3, curl, sqlite, pango/freetype, seccomp

## Specs

- S1: `browser-lite --dump file.html` parsea HTML con lexbor y emite el árbol
  DOM + cajas de layout de forma determinista a stdout.
- S2: el parser HTML respeta HTML5 (elementos implícitos `html/head/body`,
  cierre automático de `<p>`), delegando en lexbor, no en un parser propio.
- S3: la cascada CSS aplica especificidad `(id, class, tag)`, orden de origen
  (UA < autor < inline) y `!important`; soporta `display`, `margin`,
  `padding`, `width`, `height`, `color`, `background`, `font-size`.
- S4: layout block flow: cada caja block ocupa el ancho del contenedor menos
  márgenes, apila verticalmente y respeta padding; el texto se envuelve por
  ancho disponible.
- S5: `browser-lite --png out.png file.html` pinta el layout con cairo
  (rectángulos + texto) y escribe un PNG válido.
- S6: los `<script>` inline se evalúan con QuickJS en orden; `console.log`
  escribe en stdout; un error JS se reporta y no aborta el render.
- S7: `cargo test` cubre S1-S6 con fixtures en `test/data`.
- S8: `blitz-lite file.html` renderiza con Blitz (HTML/CSS/layout/paint de
  Stylo+Taffy+Vello) usando la GPU del sistema.
- S9: QuickJS ejecuta los `<script>` inline sobre el DOM de Blitz: soporta
  `document.getElementById/body/createElement/querySelector`, `appendChild`,
  `setAttribute`, `textContent`, y las mutaciones se reflejan en el render.
- S10: `element.addEventListener(type, fn)` registra listeners y un click real
  (winit -> UiEvent::MouseUp) burbujea target->root y ejecuta el handler, que
  puede mutar el DOM y verse en el siguiente frame.
- S11: `Store` persiste historial y cookies en SQLite del sistema: `open`
  crea el esquema, `open_memory` da una base en memoria, `push_history` /
  `recent_history` / `clear_history` gestionan el historial, `set_cookie` /
  `cookies_for` las cookies por dominio. Todo fallo se devuelve como `Err`,
  nunca panic.
- S12: el cliente HTTP decodifica el cuerpo con el charset declarado en
  `Content-Type` (UTF-8 por defecto; ISO-8859-1/windows-1252 y UTF-16
  soportados), falla con `Err` y la línea de estado cuando el estado HTTP es
  >= 400, expone la URL final tras las redirecciones y ofrece una variante
  asíncrona que descarga en un hilo y entrega el resultado por un canal.
- S13: `CookieJar` guarda cookies de `Set-Cookie` y decide cuáles enviar a
  cada URL: cookies de host y de dominio, prefijo de ruta y caducidad por
  marca de tiempo absoluta. `Secure` y `HttpOnly` se leen pero todavía no se
  aplican. El jar persiste y se recupera a través del `Store` de SQLite, y
  `net::fetch_with_cookies` envía la cabecera `Cookie` y captura cada
  `Set-Cookie` de la respuesta, incluidas las de una cadena de redirecciones.
- S14: el renderer se puede sandboxear en dos capas independientes: un
  filtro seccomp que decide qué syscalls puede pedir el proceso, y un
  envoltorio bubblewrap que lo ejecuta en namespaces privados con el
  sistema de archivos en sólo lectura y sin red. El filtro se instala con
  `no_new_privs` y `tsync` y es irreversible, así que se prueba en un
  proceso hijo. El diseño completo está en `docs/sandbox.md`.
- S15: un probe de fidelidad compara, con capturas, lo que dibuja el motor
  con lo que dibuja un navegador de referencia (Firefox headless) sobre un
  corpus de fixtures de SVG y de fuentes. Mide la cantidad de tinta de cada
  render y su diferencia, y produce comparativas lado a lado. El informe
  vive en `docs/font-svg-fidelity.md`.

## Tasks

- T1 [S1,S2] inline — DOM + lexbor FFI — DONE
- T2 [S3] inline — parser CSS subset + cascada — DONE
- T3 [S4] inline — layout block — DONE
- T4 [S5] inline — paint cairo + PNG + ventana SDL3 — DONE
- T5 [S6] inline — QuickJS runtime — DONE
- T6 [S7] inline — 7 tests + fixtures demo/nojs — DONE
- T7 [S8,S9] inline — Blitz renderer + puente DOM QuickJS — DONE
- T8 [S10] inline — eventos click + bubbling + addEventListener — DONE
- T9 [S11] inline — storage SQLite (Store + tests) — DONE
- T10 [S12] inline — networking v2 (charset, status, URL final, async) — DONE
- T11 [S13] inline — cookie jar + persistencia (CookieJar, Store) — DONE
- T12 [S14] inline — sandbox: filtro seccomp + aislamiento bubblewrap — DONE
- T13 [S15] inline — probe de fidelidad SVG/fuentes (corpus + informe) — DONE

## Log

L1 (usuario, literal): "quiero implementar un navegador desde cero ligero hay
que elegir los componentes correctos por ejemplo me gustaria usar quickjs como
motor es pequeno y moderno hace falta el resto de componentes y que tecnologias
serian buenas a escojer a llenar huecos vamos revisando esta parte y como seria
la estructuta"

L2: objetivo MVP = Desktop ligero. Stack = Opción A (quickjs + lexbor, resto
reuso KISS). Implementado paquetes KISS `extra/quickjs` y `extra/lexbor`,
instalados correctamente.

L3: usuario autoriza Go o Rust "el que mejor convenga". Decisión Rust: cgo
descarta Go en el hot path; Rust con FFI fino evita duplicar QuickJS/lexbor y
mantiene el binario ligero.

L4: desviación consciente — el parser CSS de esta primera slice es propio
(subset) en vez de lexbor/css, porque el binding de lexbor/css exige layout de
structs inestable y no bloquea el hito; el módulo `css` mantiene la frontera
para sustituirlo por lexbor/css sin tocar layout.

L5 (evidencia): `cargo test` 7/7 verdes. `--dump` muestra árbol HTML5 real
(html/head/body implícitos) y cajas block apiladas. `--png` produce PNG
1000x328 con h1 verde, `#lead` rojo por !important y card con padding.
Ventana SDL3 abierta en DISPLAY=:1 y verificada por screenshot.
Hallazgos: quickjs-ng 0.17 usa JSValue de 16 bytes por valor (no puntero
etiquetado) y JS_Eval exige fuente NUL-terminada aunque reciba longitud; ambos
documentados en `src/sys.rs` y `src/js.rs`.

L6: usuario elige QuickJS como motor tras comparar con Boa/vibey-script.
Se implementa `src/blitz_engine.rs`: wrapper `JsDocument` (Deref a
BaseDocument + poll que ejecuta scripts) y puente QuickJS con funciones
nativas `__dom_*` más un bootstrap JS que expone una clase Element mínima.

L7 (evidencia): `blitz-lite test/data/js-dom.html` abre ventana, QuickJS crea
un h1 y un p, y Blitz los pinta (captura verificada). RSS 140 MB. Hallazgos:
blitz-html necesita `ua_stylesheets: Some(..)` para inyectar DEFAULT_CSS o
todo colapsa; el trait Document de 0.2.4 es Deref<Target=BaseDocument>;
`set_node_text` solo aplica a nodos Text, así que `textContent` = remove
children + create_text_node + append; los scripts corren antes del primer
layout para que el frame inicial ya tenga las mutaciones.

L8 (evidencia): click real vía XTest sobre `test/data/events.html`: la cadena
fue [10,8,1,0] (button, body, html, document), el handler corrió y el texto
pasó de "waiting" a "clicked: click" (captura verificada). El trait
EventHandler de Blitz 0.2.4 no es implementable fuera de la crate porque
EventState no se exporta: se intercepta handle_ui_event y se hace hit-test con
set_hover_to + get_hover_node_id, construyendo la cadena con Node::parent.

L9 (T5 storage, plan.md wave 1): `src/storage.rs` nuevo con `Store` sobre
SQLite del sistema (rusqlite 0.40 enlazado a libsqlite3 del sistema, sin
`bundled`): `open` / `open_memory` crean el esquema con `CREATE TABLE IF NOT
EXISTS` para `history` y `cookies`; `push_history` / `recent_history` (más
reciente primero, con límite) / `clear_history`; `set_cookie` (upsert por
dominio+nombre) / `cookies_for` (ordenado por nombre). `HistoryEntry` se
define aquí: `src/history.rs` es el modelo de navegación por pestaña y queda
congelado. Todo error de SQL se devuelve como `Err`, sin panic.
Evidencia: RED observado (`error[E0583]: file not found for module storage`),
GREEN con `cargo test --test storage` 6/6. Desviación de ownership: se
añade una línea `pub mod storage;` en `src/lib.rs`, que ningún agente posee
en la wave 1, porque el módulo no es visible sin registrarla.
Pendiente: T5 no se integra en la UI (eso es T8 en wave 2).

L10 (T4 networking v2, plan.md wave 1): `src/net.rs` reescrito sobre el
mismo libcurl. `fetch` conserva su firma; `fetch_result` devuelve
`FetchResult { final_url, bytes }` con la URL tras las redirecciones;
`fetch_text` decodifica con el charset de `Content-Type` (UTF-8 por defecto,
ISO-8859-1/US-ASCII/windows-1252 decodificados como windows-1252, UTF-16 con
o sin BOM, BOM UTF-8 eliminado); `fetch_async` / `fetch_result_async` /
`fetch_text_async` descargan en un hilo y entregan el resultado por
`std::sync::mpsc`. Un estado >= 400 devuelve `Err` con la línea de estado real
(capturada en `header_function`; en una cadena de redirecciones gana la
última). `src/bin/blitz-lite.rs` usa ya `fetch_text` y sale con código 1 y
mensaje legible ante un error HTTP.
Evidencia: RED observado (E0432 por la API nueva) y GREEN con
`cargo test --test net` 11/11, con un servidor HTTP mínimo dentro del propio
test (200, 404, 302 y charset). Manual con `python3 -m http.server 8765`:
`blitz-lite http://127.0.0.1:8765/test/data/net-404.html` imprime
`HTTP/1.0 404 File not found` y sale con 1. Con un servidor que declara
`charset=iso-8859-1`, la página muestra "café" correctamente (captura), y
tras un 302 el título de la ventana es el de la página final.
Notas: `test/data/net-404.html` NO existe a propósito — la aceptación exige
que esa URL devuelva 404. `net::fetch` ahora falla con estados >= 400 (antes
devolvía el cuerpo de error), así que `src/bin/blitz-tabs.rs:49`, que aún
usa `fetch` + UTF-8 lossy, debería migrar a `fetch_text`. El charset
desconocido cae a UTF-8 con aviso en stderr, para no dejar la página en
blanco.

L11 (T11 cookie jar, plan.md wave 1.5): `src/cookies.rs` nuevo con `Cookie` y
`CookieJar`: `set_from_header(url, valor)` parsea `Set-Cookie`
(nombre=valor; Domain; Path; Secure; HttpOnly; Expires; Max-Age), `header_for(url)`
devuelve la cabecera `Cookie` de las que casan, y `store`/`load` persisten a
través del `Store`. Casamiento: host-only (host exacto) y de dominio
(suffix con punto), prefijo de ruta con la regla de los límites de segmento
(RFC 6265), y caducidad por marca de tiempo absoluta; `Max-Age` gana sobre
`Expires`. `Secure` y `HttpOnly` se guardan pero no se aplican todavía.
`net::fetch_with_cookies(url, &CookieJar)` envía la cabecera `Cookie` y
devuelve los `Set-Cookie` capturados como pares `(url, valor)`, atribuyendo
cada cookie a la URL cuya respuesta la trajo: en una cadena de redirecciones
el `Location` sólo pasa a ser la URL actual cuando empieza la respuesta
siguiente.
En `src/storage.rs` la tabla `cookies` crece con `path`, `host_only`,
`secure`, `http_only` y `expires` (clave primaria dominio+nombre+ruta) y hay
una migración que borra y recrea la tabla antigua; `set_cookie`/`cookies_for`
siguen igual. Nuevos: `store_cookie`, `load_cookies`, `clear_cookies`.
Evidencia: RED observado (E0432 por `browser_lite::cookies`) y GREEN con
`cargo test --test cookies` 10/10 (set, eco, ámbito de dominio, ámbito de
ruta, caducidad, persistencia tras reabrir el `Store`, y captura de
`Set-Cookie` en una redirección). Pendiente: integración con la UI (eso es
T8); `document.cookie` aún no existe en la API del DOM.

L12 (T10 sandbox, plan.md wave 2): `src/sandbox.rs` nuevo con FFI fino a
libseccomp 2.6.1 del sistema (sin dependencia Rust nueva) y dos capas
independientes. Capa L1, filtro seccomp: `Policy::DenyDangerous` (por
defecto se permite todo salvo la lista `DANGEROUS_SYSCALLS`: execve,
ptrace, mount, setns, bpf, init_module, keyctl, userfaultfd, io_uring,
personality, setuid...) y `Policy::AllowOnlyRenderer` (sólo la lista
`RENDERER_SYSCALLS`, todo lo demás EPERM). El filtro se carga con
`no_new_privs` y `tsync`, y es irreversible: por eso los tests lo ejecutan
en un proceso hijo (el binario de test se re-ejecuta a sí mismo con
`BLITZ_SANDBOX_PROBE=install`). Capa L2, bubblewrap: `isolated_command` /
`spawn_isolated` arrancan el renderer con `--ro-bind / /`, `--tmpfs /tmp`,
`--unshare-all` (pid, ipc, uts, cgroup y **red**), `--die-with-parent` y
`--new-session`. `docs/sandbox.md` tiene el modelo de amenazas, la
comparación seccomp/Landlock/bubblewrap, las fases L1/L2/L3 (el navegador se
queda con la red, el protocolo de IPC y la política) y lo que falta.
Evidencia: RED no aplica (tarea nueva con tests nuevos), GREEN con
`cargo test --test sandbox` 8/8. Medido: el filtro deja el proceso en
`Seccomp: 2` y `execve` falla (no puede lanzar `/bin/true`); el hijo
sandboxed arranca, imprime y sale limpio; dentro del sandbox no hay ninguna
ruta (`/proc/net/route` sólo con la cabecera) ni interfaces del host.
Pendiente: el renderer sigue en el proceso del navegador, así que el filtro
aún no se aplica a nada: el cableado necesita `src/bin/blitz-tabs.rs` y un
proceso propio para el renderer (fase L2 del diseño). La lista
`RENDERER_SYSCALLS` todavía no se ha validado contra una carga real de
página (hace falta un `strace`).

L13 (T12 probe de fidelidad, plan.md): corpus de 12 fixtures en
`test/data/probe-*.html` (5 de SVG, 7 de fuentes) más `tools/fidelity-probe.py`
y `tools/x11shot.py`, y el informe en `docs/font-svg-fidelity.md`. El probe
abre cada fixture en `blitz-lite`, captura **la ventana** (no el escritorio)
con `import -window <id>` — este entorno no tiene `xdotool` ni `xwininfo`, así
que el árbol de ventanas se recorre con Xlib por ctypes — y renderiza lo
mismo con Firefox headless; compara dentro de la caja de contenido de la
referencia, con "tinta" (pixeles no de fondo) y diferencia con 25% de fuzz.
Hallazgos: **el SVG no se dibuja en absoluto** (5/5: formas, path, gradientes,
`<text>`, y como `<img>`/background queda una caja vacía); `line-height` se
ignora (1, 1.5 y 2 dan el mismo espacio, y el interlineado por defecto es
mayor que el de Firefox, así que el ritmo vertical difiere); también se
ignoran `text-indent`, `text-align: justify`, `text-transform` y
`font-variant-ligatures: none`; y el `@font-face` con `url(file://...)` no se
aplica (la línea cae a la familia por defecto, mientras Firefox sí usa el
archivo). Lo que **sí** coincide: tamaño de fuente (px/em/rem/%/palabras
clave), peso (300-900 eligen las mismas caras), cursiva, `letter-spacing`,
`word-spacing`, `text-align` izquierda/centro/derecha, el fallback a glifo
ausente (tofu igual que Firefox) y las ligaduras OpenType del Nerd Font.
Límite del entorno: sólo hay una familia instalada (JetBrains Mono), así que
la selección de familias no se puede comparar; por eso el fixture de
`@font-face` usa un *archivo* distinto (ExtraBold) para que la diferencia
se vea. Nota: las primeras capturas mostraban el escritorio bajo la página;
era un build anterior al arreglo del fondo de T6, ya no se reproduce.