# browser-lite — parallel work plan

## What exists today

- `browser-lite` (legacy engine): lexbor HTML, own CSS cascade, block layout,
  cairo paint, SDL3 window, QuickJS `eval` with `console`/`document.title`.
- `blitz-lite` (current engine): Blitz render (Stylo + Taffy + Vello) and a
  persistent QuickJS context that mutates Blitz's `BaseDocument`.
- DOM API exposed to page scripts: `getElementById`, `querySelector`, `body`,
  `createElement`, `appendChild`, `setAttribute`, `getAttribute`, `textContent`,
  `tagName`, `id`, `addEventListener`/`removeEventListener`, `click` bubbling.
- 7 unit tests on the legacy engine; the Blitz path is verified by screenshot.

Commands:

```sh
cd /home/darktux/browser-lite
CARGO_TARGET_DIR=/home/darktux/.cache/bl-target cargo build --bin blitz-lite
DISPLAY=:1 /home/darktux/.cache/bl-target/debug/blitz-lite test/data/events.html
CARGO_TARGET_DIR=/home/darktux/.cache/bl-target cargo test --test smoke
```

## Hard rules for parallel agents

1. **One owner per file.** Do not edit files outside your task's "owns" list.
2. Do not run repository-wide formatters, generators, or dependency updates.
3. Never commit or push. Leave changes in the working tree for review.
4. Keep technical artifacts (code, comments, docs) in English.
5. Scratch files go outside the repo, under `/tmp/opencode`.
6. Do not change `Cargo.toml` unless your task explicitly owns it.

## File ownership map

| Area | Files | Owner |
| --- | --- | --- |
| Network + navigation | `src/net.rs`, `src/url.rs`, `src/bin/blitz-lite.rs`, `src/lib.rs`, `Cargo.toml` | Agent A |
| Engine core (timers, keyboard, focus) | `src/blitz_engine.rs`, `src/sys.rs` | Agent B |
| Tabs + history (landed) | `src/history.rs`, `src/tabs.rs`, `src/bin/blitz-tabs.rs` | Agent A |
| Legacy engine | `src/dom.rs`, `src/css.rs`, `src/layout.rs`, `src/paint.rs`, `src/shell.rs` | frozen |
| Tests/fixtures | `tests/`, `test/data/` | whoever needs a fixture, one new file per task |
| This plan | `plan.md` | Agent A |

## Wave 1 — two independent tracks

### T1 (Agent A) — Networking and URL loading — DONE

Goal: `blitz-lite <url>` loads over HTTP(S) and renders it; local files keep
working. Implemented in `src/net.rs` (curl) and `src/url.rs`; verified with a
local server and screenshot.

Owns: `src/net.rs` (new), `src/url.rs` (new), `src/bin/blitz-lite.rs`,
`Cargo.toml`.

Interface:

```rust
// src/net.rs
pub fn fetch(url: &str) -> Result<Vec<u8>, String>;

// src/url.rs
pub fn is_http(url: &str) -> bool;
pub fn resolve(base: &str, href: &str) -> Option<String>;
```

Acceptance:

- `blitz-lite http://127.0.0.1:8765/test/data/js-dom.html` renders the page.
- Redirects followed, 20 s timeout, `browser-lite/0.1` user agent.
- HTTP errors return a readable message, no panic.
- Local file mode unchanged.

Verify:

```sh
cd /home/darktux/browser-lite
python3 -m http.server 8765 &
DISPLAY=:1 /home/darktux/.cache/bl-target/debug/blitz-lite \
  http://127.0.0.1:8765/test/data/js-dom.html
```

### T2 (Agent B) — Timers, keyboard, focus — DONE (verified)

Goal: page scripts get `setTimeout`/`clearTimeout` and keyboard events.

Owns: `src/blitz_engine.rs`, `src/sys.rs`.

Scope:

- JS bootstrap: timer queue and `setTimeout`/`clearTimeout`.
- Rust: a tick entry point called from `Document::poll`; if a timer is due,
  run it and return `true` so Blitz requests a redraw.
- Keyboard: handle `UiEvent::KeyDown`/`KeyUp`, build the target chain from the
  focused node, dispatch `keydown`/`keyup` with `{key, code}`.
- Focus: on `MouseDown`, set focus to the hit node; `document.activeElement`.
- New FFI only if strictly required; document it in `src/sys.rs`.

Acceptance:

- `setTimeout(() => document.getElementById("out").textContent = "later", 300)`
  updates the DOM without further input.
- A keydown listener receives the pressed key (`a`, `Enter`).
- No change to the existing click path.

Verify manually with ImageMagick screenshots; the window runs on `DISPLAY=:1`.

Fixture: `test/data/timers.html` (new file, owned by Agent B).

## Wave 2 — after Wave 1 merges

- T3 Tabs + history: new `src/bin/blitz-tabs.rs`, one `BlitzApplication` window
  per tab, in-memory history. New files only.
- T4 Storage: SQLite for history/cookies/localStorage; new `src/storage.rs`.
- T5 Packaging: release profile, strip, measure binary/RSS, document limits.
- T6 Default actions: link navigation on click, form submit, `preventDefault`.

## Hand-off prompt for Agent B (copy-paste)

```text
You are working in /home/darktux/browser-lite, a Rust lightweight browser.

Read plan.md first, then odd/tasks/browser-lite.md.

Your task is T2 (timers, keyboard, focus). You own ONLY:
- src/blitz_engine.rs
- src/sys.rs
- test/data/timers.html (new)

Do not edit Cargo.toml, src/net.rs, src/url.rs, src/bin/, or the legacy engine.

Constraints:
- Blitz 0.2.4, QuickJS-ng 0.17 (libqjs). JSValue is a 16-byte by-value struct.
- JS_Eval needs NUL-terminated source (use CString).
- The document lives in a Box<BaseDocument>; callbacks use the QuickJS context
  opaque pointer.
- Scripts run before the first layout; timers must work afterwards.
- Never commit or push. Build with:
  CARGO_TARGET_DIR=/home/darktux/.cache/bl-target cargo build --bin blitz-lite
- Verify on DISPLAY=:1 with ImageMagick `import -window root out.png`.
- Add no new dependencies.

Report: files changed, commands run, observed results, and any blocker.
```

## Known traps (do not rediscover)

- `blitz-html` only injects `DEFAULT_CSS` when `ua_stylesheets` is `Some(..)`.
- `Document` in 0.2.4 is `Deref<Target = BaseDocument>` + `as_any_mut`.
- `EventHandler` is not implementable outside the crate (`EventState` private);
  override `handle_ui_event` instead.
- `set_node_text` only works on `NodeData::Text`; `textContent` on elements is
  remove-children + text node + append.
- `/tmp` is a 1 GB tmpfs: always build with `CARGO_TARGET_DIR` on disk.
