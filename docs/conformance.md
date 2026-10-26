# browser-lite conformance ledger

Section-by-section walk through the specs on our shelf
(`/home/darktux/proyectos/web-standards/`, snapshots dated 2026-10-26 —
see `MANIFEST.md` there), recording how **this engine** behaves against each.
It is measured against the **spec text**, not against any other browser.
It is a living map, not a guarantee; the engine's runtime behaviour is the
source of truth. Re-check any row by running the engine against a page that
exercises the feature.

Shape note: the section-per-spec walk emulates the *shape* of the reference
project's compatibility doc (Nordstjernen `docs/HTML-compatibility.md`,
INSPIRATION ONLY — none of its data is copied here); every status below is
our own, earned from our own code and tests.

Snapshot: ledger v1, 2026-10-26. Spec snapshots: WHATWG living standards as
of 2026-10-26; W3C TR snapshots per MANIFEST (Media Queries L5 is a WD —
no TR exists).

## How to read this

- **Spec section** groups rows by the spec's own chapters (§1–§16 for HTML,
  per-module chapters for CSS). Row granularity is "one testable behaviour",
  not "one spec sentence" — the vast majority of rows are ABSENT, and that
  is the point: **the ledger is the work list**.
- **Requirement** is a short behaviour statement, not a quote. For the exact
  normative text, open the snapshot on the shelf.
- **Status** is one of the three definitions below — nothing else.
- **Evidence** is our code path or test. A row with no evidence is aspirational
  and must read ABSENT.

## Status definitions

- **done** — implemented and pinned by a test in this repo. The evidence
  column names the test.
- **partial** — works for a documented subset; the Notes column states the
  exact boundary (what works, what does not). Subset must itself be tested.
- **absent** — not implemented, or implemented but unverified against the
  spec (unverified counts as absent — no inflation).

## The rule (the ledger is the compass)

Every engine PR that implements a spec behaviour **updates its row here**
(status + evidence + notes) in the same change. The executable companion is
the WPT harness `tools/wpt-score.py` (with `tools/fidelity-probe.py` /
`tools/fidelity-bands.py` for rendering): ledger rows say *what the spec
asks*, WPT says *what the engine scores*. Neither replaces the other.

## Engine map (the paths referenced throughout)

| Concern | Source |
|---------|--------|
| HTML parse + DOM tree | `go/dom/dom.go` (parse over `golang.org/x/net/html`; stable node-id arena) |
| Script pipeline | `go/cmd/browser-lite/scripts.go` (inline-only, oracle byte-exact) |
| Stylesheets / cascade input | `go/cmd/browser-lite/sheets.go`, `go/css/sheet.go`, `go/css/rewriter.go`, `go/css/libcss.go` |
| Cascade / computed values | `go/css/compute.go`, `go/css/computedvar.go`, `go/css/mapping.go`, `go/css/selectctx.go` |
| Custom properties + `var()` | `go/css/custom.go`, `go/css/computedvar.go` |
| Media queries | `go/css/media.go` |
| Backgrounds / gradients | `go/css/background.go` |
| Fonts / `@font-face` | `go/css/fontface.go`, `go/text/family.go` |
| Text / line-height / shaping | `go/text/lineheight.go`, `go/text/shaper.go`, `go/css/*lineheight*` |
| Layout (block/inline/flex/grid/position) | `go/layout/layout.go`, `go/layout/flex.go`, `go/layout/grid.go`, `go/layout/absolute.go` |
| Paint | `go/paint/paint.go` |
| SVG subset | `go/svg/svg.go`, `go/svg/path.go` |
| Images | `go/img/img.go` |
| JS runtime + oracle surface | `go/js/js.go`, `go/js/bindings.go` (oracle 7-member surface) |
| JS DOM track | `go/js/dom.go` |
| Networking | `go/net/net.go`, `go/net/join.go`, `go/css/urljoin.go` |

## Row tally (counted across the tables below)

**18 done · 56 partial · 52 absent — 126 rows total.**
The histogram is the headline: over four-fifths of the map is partial-or-absent
subset work, and the absent column is the ordered backlog (§"Next work" at
the end names the head of the queue).

---

## WHATWG HTML (§1–§16, per the spec's own TOC)

### §1 Introduction

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §1 | Informative; engine targets standards-mode processing model | done | `go/dom/dom.go` (html/head/body implied around fragments) | Nothing to implement; records intent |

### §2 Common infrastructure

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §2.5 / WHATWG URL | URL parsing for page URL, `location` view, stylesheet joins | partial | `go/js/bindings.go` (SetURL), `go/css/urljoin.go` | Absolute-URL subset; WHATWG URL table has the gaps |
| §2 / Encoding | Decode byte stream to UTF-8 per Encoding | absent | — | Decode path unverified against the Encoding snapshot; snapshot is on the shelf, behaviour is not |
| §2.6 | Common microsyntaxes (numbers, dates, dimensions) | absent | — | No dedicated microsyntax parsing; values flow through unvalidated |

### §3 Semantics, structure, and APIs of HTML documents

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §3.2 | `html`/`head`/`body` implied around fragments | done | `go/dom/dom.go` Parse | Stable node ids, document order, root `#document` |
| §3.3 | `document.title` readable and writable | done | `go/js/bindings.go` (Title), `go/js/dom_test.go` | Byte-exact vs oracle |
| §3.2.5 | `base href` resolution for relative URLs | absent | — | Relative joins exist (`urljoin.go`) but no `base`-element pass |
| §3.2.3 / §3.2.6 | Global attributes / `document.body` beyond tagName | partial | `go/js/dom.go` (document.body) | `body.tagName` works; global-attribute reflection beyond class/id unmapped |

### §4 The elements of HTML

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §4.3 | Inline `<script>`: document order, `inline-{n}.js` naming (skips included), per-script error line, continue-on-error | done | `go/cmd/browser-lite/scripts.go`, `go/cmd/browser-lite/scripts_test.go` | Oracle byte-exact; never looks at `src` |
| §4.3 | External script `src` fetch + execution | absent | — | GAP (parity choice now superseded by the method pivot): not fetched, no error line; Fetch table owns the future |
| §4.2/§4.3 | `<style>` + external stylesheets applied to cascade | partial | `go/cmd/browser-lite/sheets.go`, `go/css/sheet.go` | Inline `<style>` wired; `link rel=stylesheet` fetch path unverified → absent half |
| §4.8 | `<img>`: source selection, fallback, SVG images | partial | `go/img/img.go`, `tests/images.rs` | Raster + SVG subset paint; `srcset`/sizes selection absent |
| §4.10 | Forms: controls, submission, validation | absent | — | No form layout, no submission, no constraint validation |
| §4.9 | Tables: auto layout, sections, spanning | absent | — | No table formatting context in `go/layout` |
| §4.5/§4.6 | Ruby, edits, embedded content beyond img | absent | — | No `ruby`, no `video`/`audio`/`canvas`/`iframe` rendering |
| §4.13 | Custom elements / declarative shadow DOM | absent | — | Parser treats unknown elements as plain nodes only |

### §5 Microdata

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §5 | Microdata extraction model | absent | — | Non-goal for the current engine; parser drops nothing but extracts nothing |

### §6 User interaction

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §6.4/§6.5 | Focus, activation behaviour, click sequences | absent | — | No focus ring, no activation dispatch verified |
| §6.7 | `contenteditable`, drag-and-drop, editing APIs | absent | — | No editing surface |

### §7 Loading web pages

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §7.1 | Browsing contexts, navigation, session history | absent | — | Single-document dump pipeline; no history stack |
| §7.2 | `location` navigation API | partial | `go/js/bindings.go` (read-only location view) | Read-only view seeded from page URL; navigation itself absent |
| §7.9 | Page show/hide, `load` event | absent | — | Only `DOMContentLoaded` listener registry exists; `load` never fires |
| §7.10 | Offline / application cache | absent | — | No offline surface |

### §8 Web app APIs

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §8.1 | Oracle 7-member web surface: `window`, `document.title`, `navigator.userAgent`, `location`, `console`, `add/removeEventListener` | done | `go/js/bindings.go`, `go/js/bindings_test.go` | Byte-exact vs oracle; T2-owned members stay missing (pinned by test) |
| §8.1 | DOM core track: `getElementById`, `querySelector(All)`, `createElement`, `innerHTML`/`textContent`, attributes, `classList`/`className`, `appendChild`/`removeChild`, `closest`/`matches`, `document.body` | partial | `go/js/dom.go`, `go/js/dom_test.go` | Track in progress; oracle-7 preserved byte-exact underneath |
| §8.1.7 | Event loop, microtasks, task sources | absent | — | Synchronous eval only; `DispatchPageEvents` fires DOMContentLoaded inline |
| §8.6 | Timers (`setTimeout`/`setInterval`) | absent | — | Deliberately missing per oracle; pinned `ReferenceError` in `go/js/dom_test.go` |
| §8.4 | `document.write` / dynamic markup insertion | absent | — | No document-open stream; `innerHTML` fragment path is approximated (see §13) |
| §8.1 | `DOMContentLoaded` listener registry + dispatch | done | `go/js/bindings.go`, `go/js/js.go` (EvalPage lifecycle) | `load` and per-node events remain absent (see §7) |

### §9 Communications

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §9.4 | `postMessage`, channels, transferables | absent | — | Single-context runtime; no messaging surface |
| §9.2/§9.3 | WebSockets / SSE | absent | — | No socket or event-stream transport in `go/net` |

### §10 Web workers

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §10 | Dedicated/shared workers, `importScripts` | absent | — | One main-thread QuickJS context; workers would need the Fetch row first |

### §11 Worklets

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §11 | Worklet execution contexts | absent | — | No paint/layout worklet surface; below workers in priority |

### §12 Web storage

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §12.3 | `document.cookie` read/write + store | absent | — | `document.cookie === undefined`; pinned in `go/js/dom_test.go` as T2-owned future |
| §12.4 | `localStorage` / `sessionStorage` | absent | — | No storage area implementation (`tests/storage.rs` covers the oracle side only) |

### §13 HTML syntax

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §13.2 | Tokenizer (tags, attributes, entities, comments) | done | `go/dom/dom.go` via `x/net/html` | Comments/doctypes dropped like the oracle |
| §13.3 | Tree construction: insertion modes, implied elements | partial | `go/dom/dom.go`, `go/dom/dom_test.go` | `html/head/body` implication done; foster parenting + adoption agency delegated to the dependency, unverified row-by-row |
| §13.3 | `noscript` with scripting disabled (element children, not raw text) | done | `go/dom/dom.go` parseNoscriptFragment, `noscript_test.go` (×3 packages) | Re-parses raw-text-only `noscript` as fragment; element-bearing keeps behaviour |
| §13.4 | Fragment parsing / `innerHTML` fidelity | partial | `go/dom/dom.go` | Approximated: plain-text stays one `#text` child; context-element quirks unverified |
| §13.2.6 | RCDATA/RAWTEXT (`title`, `textarea`, `style`, `script`) | partial | `go/dom/dom.go` (script text via TextContent) | Works through the dependency; `textarea` default-value semantics untested |
| §13.2.2 | Standards vs quirks mode switch | absent | — | No doctype-driven mode switch verified; engine behaves standards-only |

### §14 XML syntax

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §14 | XHTML parsing, namespaces, `DOMParser` XML mode | absent | — | HTML-only parser; namespaces unrepresented in the node arena |

### §15 Rendering

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §15.3 | Block/inline visual formatting driven by §15 rules | partial | `go/layout/layout.go`, `go/paint/paint.go` | Boxes paint; §15's bidirectional, replaced-element, and overflow rules not walked row-by-row |
| §15.2 | CSS integration (presentational hints vs style) | partial | `go/css/mapping.go` (family capture) | Mapped families wired; hint-vs-author precedence corners untested |
| §15.5 | Selection painting, focus rings, caret | absent | — | Inline spans preserved (`inline_spans_test`) but no selection model |
| §15.7 | Paged media / print | absent | — | No page boxes, no print pipeline |

### §16 Obsolete features

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| §16 | Obsolete elements/attributes behaviour | absent | — | Parsed as unknown nodes at most; no conforming obsolete rendering |

---

## WHATWG DOM

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| DOM §2 | Node tree ownership: document-order ids, `#document` root, parent/children | done | `go/dom/dom.go`, `go/dom/dom_test.go` | Arena model mirrors the oracle |
| DOM §2 | `Node`/`Document`/`Element` core surface | partial | `go/js/dom.go` | Subset only (see §8 DOM track); no `NodeType` constants, no `ownerDocument` |
| DOM §3 | Mutation algorithms (`appendChild`/`removeChild`) + `DomDirty` re-render flag | partial | `go/js/dom.go`, `go/js/dom_test.go` | Insert/remove work; full pre-insert validity checks absent |
| DOM §4 | Ranges and static ranges | absent | — | No range objects; selection depends on this |
| DOM §4.5 | Traversal (`NodeIterator`, `TreeWalker`, `NodeFilter`) | absent | — | Only selector-list traversal exists |
| DOM §2.10 | `querySelector(All)` descendant chains; `>` accepted as descendant | partial | `go/dom/dom.go`, `go/js/dom.go` | `tag`, `#id`, `.class`, `tag.class` + chains; `+`/`~`/attribute selectors absent |
| DOM §5 | Full event model (capture/bubble, per-node listeners) | partial | `go/js/bindings.go` | `DOMContentLoaded` registry only; per-node events absent like the oracle |
| DOM §6 | Mutation observers | absent | — | `DomDirty` flag covers the pipeline need; no observer records |
| DOM §7 | Shadow DOM, slots, `attachShadow` | absent | — | Flat tree only |
| DOM §8 | `innerHTML` parsing + serialization round-trip | partial | `go/dom/dom.go` | Parse side approximated; serialization fidelity untested |
| DOM §9 | `DOMParser`, `XMLSerializer` | absent | — | No parser objects exposed to scripts |
| DOM §2 | `getElementById` / `createElement` / attributes / `classList` | partial | `go/js/dom.go`, `go/js/dom_test.go` | Core track subset; `dataset`, `toggle` covered in tests |

## WHATWG URL

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| URL §4 | Basic URL parsing (absolute http(s)) | partial | `go/js/bindings.go` (SetURL), `go/css/urljoin.go` | Absolute URLs only; failure keeps previous value (oracle parity) |
| URL §5 | Serialization + `location` parts (`protocol/host/pathname`…) | partial | `go/js/bindings.go` location view | Read-only parts exposed; setters absent |
| URL §4.3 | Origin + same-origin computation | absent | — | No origin comparison in the engine; SOP unenforced (single-document) |
| URL §4.2 | IDN / punycode / percent-encode sets | absent | — | Unverified; non-ASCII URLs untested |

## WHATWG Infra

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Infra §5 | Strings, code points, ASCII helpers | done | pervasive `asciiLower` helpers (`go/dom`, `go/css`) | Native subset; no dedicated module needed |
| Infra §6 | Lists, maps, structs, JSON types | done | Go stdlib use across `go/*` | Primitives available natively; no dedicated module needed |
| Infra §7 | Namespaces, structured clone, time | absent | — | No clone/serialization layer; needed by workers/storage futures |

## WHATWG Encoding

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Enc §4 | UTF-8 decode path for documents | absent | — | Assumed-UTF-8 today; snapshot on shelf, behaviour unwalked |
| Enc §4.2 | Label-to-encoding mapping (`meta charset`, BOM sniffing) | absent | — | No label table; `meta charset` not honoured (verify before claiming) |
| Enc §5 | TextDecoder/TextEncoder streams | absent | — | No script-exposed codec surface |

## WHATWG Fetch

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Fetch §4 | Main fetch: methods, headers, bodies, statuses | partial | `go/net/net.go`, `go/img/img.go` | Top-level + image bytes move; no method/header/body fidelity |
| Fetch §3 | CORS, modes, credentials, `no-cors` | absent | — | Nothing enforced; single-origin assumption throughout |
| Fetch §5 | `fetch()` script API + promises integration | absent | — | No promise job queue (see §8 event-loop row) |
| Fetch §6 | Subresource integrity, referrer policy, MIME sniffing | absent | — | Bytes accepted as-is; sniffing unverified |

---

## CSS modules (one table per snapshot on the shelf)

### css-syntax-3 — tokenizing and parsing

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Syntax §4/§5 | Tokenizer + parser producing rules/declarations | partial | `go/css/rewriter.go`, `go/css/libcss.go` (real C parser + Go rewriter) | libcss owns grammar; rewriter pre-pass handles our extensions |
| Syntax §5.4 | Error recovery: drop bad declaration, keep rule | partial | `go/css/rewriter_test.go` | Pinned for known fixtures; exhaustive recovery untested |
| Syntax §8 | At-rules: `@media` gated, `@font-face` captured, rest preserved/dropped | partial | `go/css/media.go`, `go/css/fontface.go`, `go/css/layer_test.go` | `@media` viewport-gated; unknown at-rules need a row-by-row audit |

### css-cascade-5 — cascade, inheritance, conditional rules

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Cascade §6 | Origin/importance/specificity/order sorting | partial | `go/css/libcss.go`, `go/css/selectctx.go` | Delegated to libcss; our selector subset caps what sorting can see |
| Cascade §7 | Inheritance of inherited properties | partial | `go/css/compute.go`, `go/css/mapping.go` (family capture) | Mapped families inherit; unmapped properties silently don't |
| Cascade §8 | Conditional group rules (`@media`, `@supports`, `@layer`) | partial | `go/css/media.go` ForEachStyleRuleViewport | `@media` min/max-width/height + and/or/comma; `@supports`/`@layer` ordering unverified |

### css-values-4 — units, percentages, `var()`-adjacent math

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Values §5/§6 | Absolute/relative lengths, `px`, `em`/`rem` resolution | partial | `go/css/mapping.go`, `go/css/computedvar.go` | Root style populated so descendant `rem` resolves (`9be004c`) |
| Values §6.2 | `rem`/`em` inside `border-radius` and geometry | done | `go/css/radiusunit_test.go`, `go/css/bordervar.go` | Pinned; radius family resolves units correctly |
| Values §9 | `var()` substitution incl. fallbacks | partial | `go/css/custom.go`, `go/css/computedvar.go`, `go/css/custom_test.go` | Phase 1: complete `var(...)` calls captured/rewritten; nested-fallback chains untested |
| Values §10 | `calc()`/`min()`/`max()`/`clamp()` math | absent | — | No math-function evaluation; values must be literal |

### css-box-3 / css-box-4 — box model

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Box §2 | Content/padding/border/margin geometry | partial | `go/css/box_geometry_test.go`, `go/layout/layout.go` | Core geometry tested; `box-sizing` switch unverified → treat as absent half |
| Box §3 | Margin collapsing (block context) | partial | `go/layout/margin_collapse_test.go` | Block-axis collapsing done; float/positioned interplay absent |
| Box-4 §3 | Margin-trim, view-box, missed-size containment | absent | — | Level 4 on shelf for the future; nothing implemented |

### selectors-4 — selector matching

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Sel §3 | Type, `#id`, `.class`, `tag.class`, descendant chains | done | `go/dom/dom.go`, `go/js/dom.go`, `go/cmd/browser-lite/domindex_test.go`-adjacent | Exact oracle subset; pinned |
| Sel §8 | Child `>` / sibling `+` / `~` combinators | absent | — | `>` currently accepted *as a descendant separator* (documented oracle parity, not spec behaviour) |
| Sel §10+ | Pseudo-classes/elements, attribute selectors, `:has()` | absent | — | No pseudo or attribute matching; `:hover`/`:link` untested |

### css-flexbox-1 — flexible boxes

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Flex §4 | Row/column container + item collection | done | `go/layout/flex.go`, `go/layout/flex_test.go`, `tests/flex.rs` | Both axes incl. column-axis anonymous items (`80b87ee`) |
| Flex §5 | Flex lines, wrapping, gaps | partial | `go/layout/flex.go` flexLines | Wrapping exists; `gap` on main axis only (`mainGap`) — cross-gap unverified |
| Flex §8 | Main-axis sizing: bases, grow/shrink, free-space distribution | partial | `go/layout/flex.go` (flexBaseSize, lineMainSize) | Common cases; definite-vs-indefinite corners untested |
| Flex §9/§10 | Cross sizing + alignment (`align-items`, baselines) | partial | `go/layout/flex.go` (crossSizeIsAuto) | Auto cross sizes; baseline alignment absent |

### css-grid-1 / css-grid-2 — grid layout

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Grid-1 §5 | Explicit tracks, placement, auto flow | partial | `go/layout/grid.go`, `go/layout/grid_test.go`, `tests/grid.rs` | Basic grids incl. `@media(max-width)` template swap (`media.go` doc) |
| Grid-1 §11 | Gutters/gaps, alignment in grid areas | partial | `go/layout/grid.go` | Fixed-gap cases; `place-*` shorthand coverage untested |
| Grid-1 §12 | Implicit tracks, auto-placement algorithm | partial | `go/layout/grid.go` | Dense/sparse corners unverified |
| Grid-2 | Subgrid and Level-2 placement refinements | absent | — | Snapshot on shelf; nothing implemented |

### css-fonts-4 — fonts and `@font-face`

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Fonts §4 | `@font-face` rule capture | partial | `go/css/fontface.go`, `go/css/fontface_test.go` | Captured declarations; remote-font fetch + format negotiation absent |
| Fonts §5 | Font matching: family list, weight/style selection | partial | `go/text/family.go` | Mapped families resolve; full 3-step matching untested |
| Fonts §5.4 | First-available-font metrics, real baselines | done | `go/text/shaper.go`, `go/text/*_test.go` | Font-natural line-height + real baselines (`8a680ee`) |

### css-text-3 — line breaking and white space

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Text §3 | White-space processing (`normal`, `pre`, collapsing) | partial | `go/layout/inline.go`, `go/layout/rowcompose.go` | Collapsing in row composition; `pre`/`pre-wrap` unverified |
| Text §5 | Line breaking, wrapping opportunities | partial | `go/layout/rowcompose.go`, `go/layout/rowcompose_test.go` | Breaks composed; CJK/hyphenation absent |
| Text §5 | `line-height: normal` = font metrics | done | `go/text/lineheight.go`, `go/text/lineheight_test.go` | Natural metrics, not a magic number |
| Text §5 | Percentage `line-height` = factor of font size | done | `go/css/lineheight_pct_test.go` | Regression-pinned (`88e01b6`) |

### css-backgrounds-3 — backgrounds, borders, rounding

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| BG §3 | `background-color` + image slot capture (colour/`linear-gradient`) | partial | `go/css/background.go`, `go/css/background_test.go` | Rewriter-captured; gradient-or-colour slot discipline pinned |
| BG §3.4 (CSS Images 3 subset) | Two-stop vertical `linear-gradient()` | partial | `go/css/background.go` parseLinearGradient | Exact subset: two stops, vertical; angles/offsets/multi-stop absent |
| BG §3.7 | `background-position` / `background-size` / `repeat` placement | partial | `go/css/background.go`, `go/paint/paint.go` | Placement wired; `background-repeat`/`position` longhand corners noted in-code |
| BG §5 | `border-radius` incl. `rem`/`em` radii | done | `go/css/radiusunit_test.go` | Pinned (see values-4 row); SVG `rx`/`ry` still absent (see SVG) |

### css-color-4 — color functions and alpha

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Color §4 | `#rrggbbaa` + functional notation capture | partial | `go/css/background.go` colour paths, tests | Hex-with-alpha works; `rgb(... / a)` slash syntax untested |
| Color §9 | Compositing/alpha in paint | partial | `go/paint/paint.go`, `go/shell/toargb_test.go` | Opaque + alpha paths painted; premultiplication corners unverified |
| Color §14+ | `color-mix()`, `lab()`/`oklch()`, wide gamut | absent | — | No modern color math; sRGB pipeline only |

### css-position-3 — positioned layout

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Pos §6 | `position: relative` offsets | partial | `go/layout/relative_position_test.go` | Offset boxes; `top/left/%` corners in test only |
| Pos §9 | `position: absolute` + containing blocks | partial | `go/layout/absolute.go`, `go/layout/absolute_position_test.go` | Basic containing-block placement; shrink-to-fit + nested abspos corners open |
| Pos §7/§12 | `fixed` / `sticky` | absent | — | No viewport-fixed or scroll-sticky positioning |

### css-display-3 — box generation

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Display §2 | `block`/`inline`/`flex`/`grid` box generation | partial | `go/layout/layout.go`, `go/layout/doc.go` | Four contexts exist; `inline-block`, `table-*`, `contents` unverified |
| Display §4 | Anonymous box generation (flex/grid items, block-in-inline) | partial | `go/layout/flex.go` (column anonymous items `80b87ee`) | Flex anonymous items fixed; run-in/block-in-inline untested |
| Display §3 | `display: none` subtree suppression | absent | — | Unverified: no pinned test that `none` removes boxes from all three consumers |

### css-sizing-3 — intrinsic sizing

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Sizing §4 | Percentage widths against definite containers | partial | `go/layout/layout.go` + layout tests | Common cases; indefinite-percentage resolution untested |
| Sizing §5 | `min/max-content`, intrinsic measurement | partial | `go/layout/flex.go` maxContentWidth paths | Flex-item intrinsic widths; standalone intrinsic keywords absent |
| Sizing §4.3 | `aspect-ratio`, `min/max` clamps on replaced boxes | absent | — | No ratio boxes; `img` sizing is extrinsic-only |

### css-align-3 — alignment

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Align §9 | `justify-content` distribution | partial | `go/layout/flex.go` justifyOffset | Free-space offsets; `space-evenly` + overflow alignment untested |
| Align §6/§10 | `align-items` / `place-*` / self alignment | partial | `go/layout/grid.go`, `go/layout/flex.go` | Container-level subset; per-item `align-self`/baseline absent |

### css-variables-1 — custom properties

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| Vars §2 | `--*` declaration capture per element | partial | `go/css/custom.go`, `go/css/custom_test.go` | Phase 1 capture map; inheritance-through-tree corners untested |
| Vars §3 | `var()` substitution in values | partial | `go/css/computedvar.go`, `go/css/computedvar_test.go` | Effective-map + substitution; `@property`/typed registration absent |

### mediaqueries-4 (TR) / mediaqueries-5 (WD) — conditional application

| Spec section | Requirement | Status | Evidence | Notes |
|--------------|-------------|--------|----------|-------|
| MQ-4 §2 | Media types, comma lists, `and`/`or` chains, `min/max-width/height` | partial | `go/css/media.go`, `go/cmd/browser-lite/sheets`-adjacent | L3/L4 core works at viewport gate; `orientation`/`resolution` untested |
| MQ-5 | L5 features (`prefers-color-scheme`, `dynamic-range`, …) | absent | — | WD snapshot on shelf; nothing evaluated |

---

## Next work (head of the absent queue)

Ordered by spec-walk priority — the first engine PRs under the new method
should come from this list, top first:

1. **External subresources end-to-end** (HTML §4.3 `src`, Fetch §4–§6):
   fetch + execute external scripts, fetch linked stylesheets, `srcset`
   selection. Unlocks workers, remote fonts, and every fetch-gated row.
2. **Event loop + timers** (HTML §8.1.7, §8.6): microtask/task sources,
   `setTimeout`/`setInterval` — the single biggest JS-surface gap, and the
   gate for `fetch()` promises.
3. **Full box-model closure** (css-box-3 `box-sizing`, css-display-3
   `display: none`, css-sizing-3 `aspect-ratio`): small rows, high WPT yield.
4. **Selector combinators** (selectors-4 §8: `>`, `+`, `~`; then attribute
   selectors): `>`-as-descendant is documented parity debt — fixing it
   changes matching observably, so pin current behaviour first.
5. **Storage + cookies** (HTML §12): `document.cookie`, `localStorage` —
   unblocks real login flows in test pages.
6. **Tables and forms** (HTML §4.9/§4.10): the two missing layout contexts
   that dominate real-page WPT failures.
7. **Encoding honesty** (Encoding §4): honour `meta charset`/BOM or formally
   declare UTF-8-only with a failing-input test.

## Maintenance

- Row counts in the tally above must be updated by hand when rows are
  added or change status (keep the three-number line exact; counting the
  status column over the tables verifies it — keep this note free of literal
  table-row text so the count stays clean).
- Snapshot refreshes go to the shelf (`MANIFEST.md` rule), never into this
  file; a refresh that changes normative text for a `done` row re-opens that
  row to `partial` until re-verified.
