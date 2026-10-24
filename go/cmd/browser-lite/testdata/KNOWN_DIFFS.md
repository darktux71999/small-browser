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
