# browser-lite — parallel work plan (wave 1)

## State: what already works

Do not redo any of this.

- `browser-lite` (legacy engine): lexbor HTML, own CSS cascade, block layout,
  cairo paint, SDL3 window, QuickJS eval. Used for `--dump`/`--png`.
- `blitz-lite`: Blitz render (Stylo + Taffy + Vello) + QuickJS on the same
  `BaseDocument`. Serves local files and `http(s)://` URLs.
- DOM API: `getElementById`, `querySelector`, `body`, `activeElement`,
  `createElement`, `appendChild`, `setAttribute`, `getAttribute`, `textContent`,
  `tagName`, `id`, `addEventListener`/`removeEventListener`, click bubbling.
- Events: real mouse clicks (hit test + bubbling), keyboard (`keydown`/`keyup`),
  focus on mousedown, `setTimeout`/`clearTimeout`.
- `blitz-tabs`: one window per tab, `Ctrl+T/W/Tab`, link navigation through a
  queued `NavigationProvider`, `Alt+Left/Right` per-tab history.
- Models with tests: `src/history.rs`, `src/tabs.rs` (7 tests).
- 7 unit tests on the legacy engine.

Current limitations (targets of this wave):

- Body background does not fill the viewport; the window shows the desktop.
- `net::fetch` is synchronous and ignores charset/status codes.
- No cookies, no persistent storage.
- No visible tab strip (tabs are OS windows).
- No sandbox per tab.

## Hard rules for parallel agents

1. **One owner per file.** Stay inside your task's "owns" list.
2. Do not run repository-wide formatters, generators, or dependency updates.
3. Never commit or push. Leave changes in the working tree.
4. Technical artifacts (code, comments, docs) in English.
5. Scratch files go outside the repo, under `/tmp/opencode`.
6. Do not change `Cargo.toml` unless your task owns it.
7. Build and test with the target dir on disk (`/tmp` is a 1 GB tmpfs):

```sh
cd /home/darktux/browser-lite
CARGO_TARGET_DIR=/home/darktux/.cache/bl-target cargo build --bin blitz-tabs
CARGO_TARGET_DIR=/home/darktux/.cache/bl-target cargo test --lib
DISPLAY=:1/           # X display for visual checks
```

## File ownership (wave 1)

| Area | Files | Owner |
| --- | --- | --- |
| Networking v2 | `src/net.rs`, `src/url.rs`, `src/bin/blitz-lite.rs`, `test/data/net-*.html` | Agent A |
| Storage | `src/storage.rs` (new), `tests/storage.rs` (new), `Cargo.toml` | Agent B |
| Paint/window polish — done | `src/blitz_engine.rs`, `src/bin/blitz-tabs.rs`, `test/data/*.html` | Agent C |
| Tab strip design | `docs/tab-strip.md` (new), `src/bin/tab-strip-spike.rs` (new) | Agent D |
| Frozen | `src/dom.rs`, `src/css.rs`, `src/layout.rs`, `src/paint.rs`, `src/shell.rs`, `src/history.rs`, `src/tabs.rs`, `src/nav.rs` | nobody |

`Cargo.toml` is shared by A and B: coordinate by adding dependencies only
(`curl`, `url`, `winit`, `rusqlite`), never removing or reformatting.

## T4 (Agent A) — Networking v2

Owns: `src/net.rs`, `src/url.rs`, `src/bin/blitz-lite.rs`, `test/data/net-*.html`.

Requirements:

- Parse `Content-Type` charset (`utf-8` default) and decode the body
  accordingly; today the caller does lossy UTF-8.
- Return an error for HTTP status >= 400 with the status line.
- Add `fetch_async(url) -> std::sync::mpsc::Receiver<Result<Vec<u8>, String>>`
  so the UI is not blocked; keep `fetch` for compatibility.
- Expose the final URL after redirects: `FetchResult { final_url, bytes }`.
- Do not add cookies yet (T8).

Acceptance:

- `curl`-equivalent behavior against a local server: 200, 404, redirect, and a
  `text/html; charset=iso-8859-1` fixture.
- `blitz-lite http://127.0.0.1:8765/test/data/net-404.html` prints a readable
  error and exits non-zero.
- `blitz-lite` still opens local files.

Verify:

```sh
cd /home/darktux/browser-lite
setsid python3 -m http.server 8765 >/tmp/opencode/net.log 2>&1 </dev/null &
DISPLAY=:1 CARGO_TARGET_DIR=/home/darktux/.cache/bl-target \
  cargo run --bin blitz-lite -- http://127.0.0.1:8765/test/data/net-404.html
```

## T5 (Agent B) — Storage

Owns: `src/storage.rs` (new), `tests/storage.rs` (new), `Cargo.toml`.

Scope: SQLite-backed persistence, no UI integration.

API:

```rust
pub struct Store { /* Connection */ }
impl Store {
    pub fn open(path: &str) -> Result<Store, String>;
    pub fn open_memory() -> Result<Store, String>;
    pub fn push_history(&self, url: &str, title: &str) -> Result<(), String>;
    pub fn recent_history(&self, limit: usize) -> Result<Vec<HistoryEntry>, String>;
    pub fn clear_history(&self) -> Result<(), String>;
    pub fn set_cookie(&self, domain: &str, name: &str, value: &str) -> Result<(), String>;
    pub fn cookies_for(&self, domain: &str) -> Result<Vec<(String, String)>, String>;
}
```

- Schema created on open (`CREATE TABLE IF NOT EXISTS`).
- `open_memory()` for tests.
- No panic on invalid SQL; return `Err`.

Acceptance: `cargo test --test storage` covers insert/read/clear and cookie
round-trip.

## T6 (Agent C) — Paint and window polish

Owns: `src/blitz_engine.rs`, `src/bin/blitz-tabs.rs`, `test/data/*.html`.

1. **Canvas background.** Pages whose `body` has a background currently show
   the desktop below the content. Investigate Blitz's canvas painting; if there
   is no propagation, implement a workaround that keeps the body color visible
   (for example, an injected `html` background derived from `body`, or a
   renderer clear color). Do not hardcode white.
2. **Window title.** `View::init` already reads `find_title_node`, but a
   document swapped in by navigation keeps the old title. After `load_into`,
   update `view.window.set_title(...)` from the new document.
3. Add `<title>` to every fixture under `test/data/`.

Acceptance: `test/data/nav-a.html` and `nav-b.html` show their titles; a page
with `body { background: #eef4ff }` paints the full viewport.

## T7 (Agent D) — Visible tab strip design spike

Owns: `docs/tab-strip.md` (new), `src/bin/tab-strip-spike.rs` (new).

Deliverable: a design that can be reviewed, not production code.

- Compare three approaches: (a) chrome rendered as a second Blitz document in a
  strip window, (b) a native winit/cairo strip docked above the page, (c) an
  embedded UI toolkit (egui) owning only the strip.
- Cover: input routing, focus, resize, per-tab titles, close buttons, and how
  it coexists with the existing `WindowConfig` API.
- Spike binary: a standalone winit window drawing 3 fake tabs with cairo.
  It must build and run without touching other files.

## Wave 2 (after wave 1 lands)

- T8 Wire storage into tabs: history and cookies (`src/bin/blitz-tabs.rs`,
  `src/net.rs`, `src/storage.rs`).
- T9 Async loading in tabs (`src/bin/blitz-tabs.rs`, `src/net.rs`).
- T10 Sandbox per tab: `seccomp`/`bubblewrap` design and implementation.
- T11 Packaging: release profile, strip, measure binary and RSS, document
  limits.

## Hand-off prompts

### Agent A (T4)

```text
You are working in /home/darktux/browser-lite, a Rust lightweight browser.

Read plan.md and odd/tasks/browser-lite.md first. Your task is T4 (networking
v2). You own ONLY:
- src/net.rs
- src/url.rs
- src/bin/blitz-lite.rs
- test/data/net-*.html

Do not edit Cargo.toml, src/blitz_engine.rs, src/bin/blitz-tabs.rs or the
legacy engine. Do not commit or push.

Constraints: system libcurl via the `curl` crate; no new runtime deps. Build:
CARGO_TARGET_DIR=/home/darktux/.cache/bl-target cargo build --bin blitz-lite.
Verify against a local python http.server on DISPLAY=:1.

Report: files changed, commands, observed results, blockers.
```

### Agent B (T5)

```text
You are working in /home/darktux/browser-lite, a Rust lightweight browser.

Read plan.md first. Your task is T5 (storage). You own ONLY:
- src/storage.rs (new)
- tests/storage.rs (new)
- Cargo.toml (add rusqlite only; touch nothing else)

Implement the Store API from plan.md over system SQLite. No UI integration.
Do not commit or push. Tests:
CARGO_TARGET_DIR=/home/darktux/.cache/bl-target cargo test --test storage

Report: files changed, schema, tests, blockers.
```

### Agent C (T6)

```text
You are working in /home/darktux/browser-lite, a Rust lightweight browser.

Read plan.md first. Your task is T6 (paint/window polish). You own ONLY:
- src/blitz_engine.rs
- src/bin/blitz-tabs.rs
- test/data/*.html

Fix the canvas background and the window title on navigation, and add <title>
to fixtures. Do not touch src/net.rs, src/storage.rs, src/history.rs or
src/tabs.rs. Do not commit or push.

Verify visually on DISPLAY=:1 with ImageMagick:
DISPLAY=:1 import -window root /tmp/opencode/check.png

Report: files changed, screenshots taken, blockers.
```

## Known traps

- Blitz 0.2.4 `Document` is `Deref<Target = BaseDocument>` + `as_any_mut`.
- `EventState` is private: override `handle_ui_event`, do not implement
  `EventHandler`.
- `blitz-html` needs `ua_stylesheets: Some(..)` to inject `DEFAULT_CSS`.
- `set_node_text` only applies to `Text` nodes; `textContent` on an element is
  remove-children + text node + append.
- QuickJS-ng 0.17 `JSValue` is a 16-byte by-value struct; `JS_Eval` needs a
  NUL-terminated source.
- Replacing `view.doc` requires copying the old viewport or the page renders
  blank.
- Our `handle_ui_event` bypasses Blitz's default click action; links are
  resolved manually in `follow_link`.
