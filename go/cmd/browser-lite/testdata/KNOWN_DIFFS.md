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
