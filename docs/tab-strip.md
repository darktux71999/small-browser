# Visible tab strip — design

## Problem

Today a "tab" is an OS window (`blitz-tabs`). There is no visible strip and no
single window that contains chrome plus page. The window size and position are
controlled by the user's tiling WM (dwm), so the app cannot dock or position a
second strip window above the page: both would be tiled side by side.

## Constraints

- Blitz 0.2.4 renders exactly one `Document` per window. `WindowConfig` owns the
  window; there is no chrome layer or viewport offset API.
- `BlitzApplication` owns the winit loop; the embedder can wrap it (as
  `blitz-tabs` already does) but cannot composite an extra surface into a
  window.
- The WM tiles windows; explicit positions and sizes are ignored.
- Input is delivered to the document through `handle_ui_event`.

## Options

### A. Second Blitz document in a strip window

A separate always-on-top window renders the strip as HTML/CSS.

- Pros: reuses the HTML/CSS engine; strip is easy to style; no new toolkit.
- Cons: two windows per tab; under a tiling WM they tile side by side, not
  stacked; focus and z-order coordination; closing the page window must close
  the strip.

Verdict: not viable with dwm tiling.

### B. Native chrome composited in the same window

Draw the strip with cairo/vello and offset the page viewport below it.

- Pros: true browser chrome; one window; precise input; no DOM pollution.
- Cons: Blitz exposes no chrome layer or viewport offset. Implementing it means
  forking `blitz-shell`/`anyrender_vello` to render two scenes into one surface.
  High effort, upstream churn.

Verdict: correct long-term architecture, too expensive for the MVP.

### C. Strip as part of the page document (recommended for the MVP)

Inject a fixed strip into the document and offset the page with UA CSS:

```html
<div id="__blitz_chrome" style="position:fixed;left:0;top:0;width:100vw;height:44px;z-index:2147483647">
  ...tabs...
</div>
<style>html { padding-top: 44px; }</style>
```

- Pros: one window, one renderer, works under any WM; uses the same DOM and
  click path that already works; no new dependencies.
- Cons: the strip lives in the page DOM (scripts can see it); CSS collisions are
  possible; a hostile page could hide it. Mitigate with a unique id, a reserved
  prefix, and re-injection on navigation.

Verdict: fastest path to a visible strip; acceptable for a lightweight browser.

### D. egui (or similar) for chrome only

Embed a UI toolkit that renders into the same wgpu device.

- Pros: mature widgets, no DOM pollution.
- Cons: second render pass and input stack; integration with Blitz's renderer
  and event loop; heavier binary.

Verdict: revisit if option C becomes unmaintainable.

## Recommended design (option C)

- Chrome markup is injected after `<body>` and before page scripts run, so page
  code sees it from the start (documented, reserved prefix `__blitz_`).
- Height: 44 logical px, `position:fixed`, full width, `z-index` max.
- Page offset: inject `html { padding-top: 44px; }` as a UA rule.
- Each tab: title, close button (`×`), active highlight.
- Click handling: chrome elements get `addEventListener("click")` from the
  injected bootstrap; they call a native bridge:
  - `__blitz_activate(tab_id)`
  - `__blitz_close(tab_id)`
  - `__blitz_new()`
- The app owns the tab model (`src/tabs.rs`) and pushes state to the chrome via
  a JS function `__blitz_set_tabs(json)` after every change and navigation.
- Keyboard shortcuts stay in the embedder (`Ctrl+T/W/Tab`), unchanged.
- Titles come from `find_title_node()` on each document change.

## Migration steps

1. Add `build_chrome_js()` and the injected markup to `blitz_engine`.
2. Expose `__blitz_*` bridges through the QuickJS context opaque pointer.
3. In `blitz-tabs`, render the strip into every document and sync state on
   navigation/tab changes.
4. Remove the per-tab OS window mapping only if chrome and page share one
   window; until then, keep one window per tab and treat the strip as local
   chrome per window.

## Spike

`src/bin/tab-strip-spike.rs` draws three fake tabs with cairo over SDL3,
highlights the active one and handles click/close hit-testing. It validates
geometry, colors and hit areas without touching the browser. Run:

```sh
DISPLAY=:1 cargo run --bin tab-strip-spike
```

The spike is standalone on purpose: it must not change `blitz_engine` or the
tabbed app.
