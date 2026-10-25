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
