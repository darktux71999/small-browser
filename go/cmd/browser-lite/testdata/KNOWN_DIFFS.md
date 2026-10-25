# Known differential diffs (S1 exception register)

The dual-engine differential (`go run ./cmd/differential --oracle
../target/debug/browser-lite --candidate <built binary> --corpus
../test/data --report text`) stands at **62/71 identical**. The 9
remaining diffs are all in one domain: the Rust oracle evaluates page
scripts with QuickJS, which the Go engine does not port (no JS runtime).
Every other stream matches, and the painted **PNG bytes are identical**
for all 9 fixtures — scripts never reach paint.

In each case the oracle prints `browser-lite: js error: ...` on stderr
and/or a script-mutated `title:` line on stdout, while the Go engine runs
the page without evaluating `<script>` and keeps the parsed title.

| Fixture | Differing streams | Oracle behavior | Go behavior instead |
|---|---|---|---|
| `cookie-echo.html` | dump-stderr, png-stderr | `browser-lite: js error: TypeError: not a function` | No script evaluation; stderr empty. Title identical (`cookie-echo`). |
| `cookie-set.html` | dump-stderr, png-stderr | `browser-lite: js error: TypeError: not a function` | No script evaluation; stderr empty. Title identical (`cookie-set`). |
| `demo.html` | dump-stdout, dump-stderr, png-stdout, png-stderr | `browser-lite: js error: SyntaxError: expecting ';'`; script mutates the title to `demo lite (js)` | No script evaluation; stderr empty, title stays `demo lite`. |
| `events.html` | dump-stderr, png-stderr | `browser-lite: js error: TypeError: not a function` | No script evaluation; stderr empty. Title identical (`Events`). |
| `js-dom.html` | dump-stderr, png-stderr | `browser-lite: js error: TypeError: not a function` | No script evaluation; stderr empty. Title identical (`QuickJS + Blitz`). |
| `js-window.html` | dump-stdout, png-stdout | Script runs clean and mutates the title to `window:true:true:true:true` | No script evaluation; title stays `window surface`. No stderr on either side. |
| `js-window-error.html` | dump-stdout, dump-stderr, png-stdout, png-stderr | `browser-lite: js error: TypeError: not a function`; partial script run mutates the title to `before-error` | No script evaluation; stderr empty, title stays `window error`. |
| `net-utf8.html` | dump-stderr, png-stderr | `browser-lite: js error: TypeError: not a function` | No script evaluation; stderr empty. Title identical (`net-utf8`). |
| `timers.html` | dump-stderr, png-stderr | `browser-lite: js error: ReferenceError: setTimeout is not defined` | No script evaluation (`setTimeout` does not exist); stderr empty. Title identical (`Timers`). |

Cause for all rows: the QuickJS runtime is not ported to the Go engine.
Closing any row requires porting script evaluation, which is outside the
current scope; until then these 9 diffs are accepted and expected.

## Category: noscript policy (leading-fidelity S2)

The Go engine no longer applies or renders `<noscript>` contents: a `<style>`
with a `<noscript>` ancestor is dropped from both author-stylesheet collection
paths (go/css `Stylesheets` and the CLI loader), and no boxes are produced for
a `<noscript>` subtree in any formatting context. This matches Firefox with
scripting enabled, which treats the contents as raw text and never applies
their CSS; the frozen Rust oracle still collects and lays them out, so **by
design** any fixture containing a `<noscript>` with styleable content diverges
from the oracle (the phantom ToU banner, ~200px on live codeberg, no longer
paints). The DOM parse keeps the markup as children (P1 parity); only style
collection and layout skip it. No corpus fixture uses `<noscript>`, so today
this category changes no dump. Spec and divergence decision:
`odd/tasks/leading-fidelity.md` (S2). Orthogonal to the line-height category
above and the 9 QuickJS rows.

## Category: line-height semantics (leading-fidelity S1)

The Go engine now applies CSS 2.1 line-height: the unitless factor scales
the **font-size** (Pango's absolute line-height attribute, value computed
by one shared helper in `go/text` used by both the measuring shaper and
the painter), not the font's natural logical height (ascent+descent+line
gap) that Pango's factor attribute scales. The Rust oracle shares the old
factor semantics and is frozen (`src/layout.rs:1887-1890`; the same fix is
briefed to the external agent, 2026-10-25), so **by design** every
text-heavy fixture's dump and PNG outputs now diverge from the oracle
until the same fix lands there. Measured blast radius on the corpus: 66/71
fixtures change. The fixtures that remain identical (no engine-side text
runs, or text only created by scripts the Go engine does not evaluate):
`border-radius`, `img-attrs`, `img-clamp`, `js-dom` (its only diffs stay
the stderr-only QuickJS rows above), `net-latin1`. Spec and divergence
decision: `odd/tasks/leading-fidelity.md` (S1). This category is
orthogonal to the 9 QuickJS rows above, which are unchanged.

## Category: margin collapsing (leading-fidelity S3)

The Go engine now collapses vertical margins per CSS 2.1 §8.3.1 in the
block formatting context only (adjacent siblings, parent/first-child and
parent/last-child pass-through at auto height, empty blocks; never across
borders, padding, line boxes, and never between flex/grid items). The
frozen Rust oracle never collapses margins anywhere, so **by design** any
fixture with adjoining vertical block margins now diverges from the oracle
in its layout stream (and any text-position-sensitive paint output).
Reference: `odd/tasks/leading-fidelity.md` (S3). Orthogonal to the two
categories above, which are unchanged.

## Category: row composition (meta1 S7)

The Go engine composes flex/grid rows sideways where the frozen Rust
oracle still stacks them. Six deliberate divergences, all from the frozen
oracle sharing the old behavior (the Go port was faithful until here):
(1) the manual grid cascade now gates `@media` blocks on the layout
viewport (an unconditional walk let a later `max-width` single-column rule
beat `repeat(2,1fr)` at 1000px); `@container`/`@supports` still apply
unconditionally — container queries stay unevaluated; (2) `place-self` /
`justify-self` / `align-self` map into the grid item and shrink non-auto
axes to content with start/center/end placement instead of stretching;
(3) `minmax(px,px|auto)` tracks clamp content between their bounds (a fr
extremum degrades to auto); (4) a row flex container's max-content is the
main-axis sum of its items (the old max sized e.g. `navbar-left` to 56px
and wrapped its links); (5) `white-space:nowrap` suppresses wrapping at
formatting-context roots (inline-run splitting inside block layout stays
unmodeled); (6) anonymous flex/grid items no longer inherit the
container's padding and margins (the old double-count forced a bogus
shrink+wrap and inflated rows). Measured blast radius on the corpus:
stream-identical to the registered state (70 DIFF + `net-latin1`
identical); only `nested-contexts.html` changes bytes within its already
DIFF dump/png streams (nav 100px→60px tall, auth column at its content
offset — the fix direction). Live codeberg.org: LAYOUT 1000x2297→1000x1942
(nav one row, support 2-column with right-aligned buttons, footer
365px→284px); whole-page AE 14.22%→9.81%, support-region AE 68474→24162.
Reference: `odd/tasks/meta1-codeberg.md` (S7/L22). Orthogonal to the three
categories above, which are unchanged.

## Category: background placement, gradients and cascade layers (meta1 S10)

The Go engine now (1) places background images from the computed
background-repeat/background-position (libcss 0.9.2 computes both; the
painter previously ignored them, so the codeberg hero tiled instead of
painting one bottom-anchored instance), (2) paints the two-stop vertical
linear-gradient subset that the rewriter captures out of libcss' reach (the
codeberg about-card's vendor gradient chain; stop positions are dropped —
the documented approximation of CSS Images 3 §3.4), and (3) orders the
background family's manual-cascade bids by @layer first-appearance rank
(CSS Cascade 5 §6.4.2: the layered codeberg-theme `#navbar` declaration
beats forgejo-theme's, so the nav paints #2185D0). The frozen Rust oracle
shares all three gaps (it tiles alike, paints no gradients, knows no
layers), so **by design** pages using these features diverge from it; the
spec pins live in `go/css` background_test.go/layer_test.go and the
`go/paint` background pins. The manual pass is gated on documents holding a
captured (removed) background declaration, and an inline style="" whose
every declaration was captured now finishes as an empty libcss sheet
instead of failing `css_stylesheet_append_data`. Corpus blast radius: none
— no fixture uses background-position/no-repeat/gradients/@layer/var()
backgrounds (70 DIFF + `net-latin1` identical, unchanged). Live
codeberg.org: hero top white with a single anchored mountain instance (was
tiled), about-card teal ramp (bottom sample srgb(20,76,74); Firefox
srgb(21,76,75)), navbar exactly srgb(33,133,208) (was white; Firefox's own
nav is the same blue). LAYOUT stays 1000x1942. Whole-page AE vs the frozen
/tmp/bl-m3 Firefox grid (re-derived 1000x1942 grid, `magick compare -fuzz
25%`): 9.81%→9.99% — the +0.18pp is live-page drift, fully attributed:
the reference's JS-driven ToU announcement banner (~100px, visible at its
capture, hidden now) inflates the nav band (+12397 px; the engine's nav
colour is identical to Firefox's own nav), the hero band improved (-11715
px), and the card band carries the dropped-stop approximation plus the
pre-existing unmodelled `top:-7em`/`max-width:950px` card geometry
(+2892 px). Reference: `odd/tasks/meta1-codeberg.md` (S10/L22). Orthogonal
to the categories above, which are unchanged.

## Category: constrained and relative boxes (meta1 S10/L24)

The Go engine now preserves and applies `max-width` (CSS 2.1 §10.4), including
auto-margin centering and computed `box-sizing:border-box`, and shifts
`position:relative` boxes by their insets without changing normal flow (CSS 2.1
§9.4.3). The frozen Rust oracle shares both gaps, so pages exercising these
declarations diverge in box geometry and potentially paint. The current
78-fixture corpus has no max-width sizing or relative-position declaration
(`css-media.html` uses max-width only as a media feature); old-Go vs new-Go is
byte-identical for all 78 fixtures. Reference: `odd/tasks/meta1-codeberg.md`
(L24). This category is orthogonal to the existing entries.

## Category: custom properties and var() (var-custom-properties S1-S3)

The Go engine captures and resolves CSS custom properties and `var()` values
(the 406 Codeberg `border: 1px solid var(--color-*)` declarations paint); the
frozen oracle's libcss 0.9.2 discards every declaration whose value contains
`var()`. Fixtures exercising custom properties (`var-border.html`) diverge by
design. See odd/tasks/var-custom-properties.md.

## Category: rem/em radius units (var-custom-properties S4)

The Go engine resolves `rem`/`em` in `border-radius` (Forgejo's
`.28571429rem` resolves to 4.57px at the 16px root); the frozen oracle
degrades non-px radius units to zero. `radius-rem-em.html` diverges by
design. See odd/tasks/var-custom-properties.md.
