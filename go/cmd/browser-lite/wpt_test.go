package main

// WPT headless-mode tests (RED first): the hook traps completion and
// reports the right statuses for passing/failing/throwing subtests, the
// exit codes match the contract, and the embedded hook stays in sync
// with its canonical source at test/wpt-hook.js.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWPTPage(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.html")
	page := "<!doctype html><html><head><title>wpt</title></head><body>" + body + "</body></html>"
	if err := os.WriteFile(path, []byte(page), 0o644); err != nil {
		t.Fatalf("write page: %v", err)
	}
	return path
}

func runWPTFile(t *testing.T, path string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runWPT(path, 15000, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func wptJSON(t *testing.T, stdout string) wptResult {
	t.Helper()
	var raw string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "WPT JSON ") {
			raw = strings.TrimPrefix(line, "WPT JSON ")
		}
	}
	if raw == "" {
		t.Fatalf("no WPT JSON line in:\n%s", stdout)
	}
	var res wptResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("WPT JSON does not parse: %v\n%s", err, raw)
	}
	return res
}

func TestWPTHookMatchesCanonicalFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "wpt-hook.js"))
	if err != nil {
		t.Fatalf("read canonical hook: %v", err)
	}
	if wptHook != string(raw) {
		t.Errorf("wptHook differs from test/wpt-hook.js (edit the canonical file, then sync the const)")
	}
}

func TestWPTPassingPage(t *testing.T) {
	path := writeWPTPage(t, `<script>
test(function () { assert_equals(1 + 1, 2, "math"); }, "addition");
test(function () {
  var el = document.getElementById("x");
  assert_true(el !== null, "found");
  assert_equals(el.textContent, "hi", "text");
}, "dom");
</script><div id="x">hi</div>`)
	code, stdout, stderr := runWPTFile(t, path)
	if code != 0 {
		t.Errorf("exit %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"WPT HARNESS OK",
		"WPT PASS addition",
		"WPT PASS dom",
		"WPT SUMMARY total=2 pass=2 fail=0 timeout=0 notrun=0 precondition_failed=0",
		"WPT JSON ",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	res := wptJSON(t, stdout)
	if res.Harness != "OK" || len(res.Subtests) != 2 {
		t.Errorf("result = %+v, want harness OK with 2 subtests", res)
	}
}

func TestWPTFailingPage(t *testing.T) {
	path := writeWPTPage(t, `<script>
test(function () { assert_equals(1 + 1, 3, "math"); }, "addition");
test(function () { assert_true(true, "ok"); }, "fine");
</script>`)
	code, stdout, _ := runWPTFile(t, path)
	if code != 1 {
		t.Errorf("exit %d, want 1\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "WPT FAIL addition |") {
		t.Errorf("missing WPT FAIL line:\n%s", stdout)
	}
	if !strings.Contains(stdout, "WPT SUMMARY total=2 pass=1 fail=1 timeout=0 notrun=0 precondition_failed=0") {
		t.Errorf("bad summary:\n%s", stdout)
	}
	res := wptJSON(t, stdout)
	if res.Subtests[0].Status != "FAIL" || res.Subtests[0].Message == nil {
		t.Errorf("failing subtest = %+v, want FAIL with message", res.Subtests[0])
	}
}

func TestWPTThrowingSubtestIsFailNotHarnessError(t *testing.T) {
	path := writeWPTPage(t, `<script>
test(function () { throw new Error("boom"); }, "throws");
</script>`)
	code, stdout, _ := runWPTFile(t, path)
	if code != 1 {
		t.Errorf("exit %d, want 1\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "WPT HARNESS OK") {
		t.Errorf("throw must not break the harness:\n%s", stdout)
	}
	if !strings.Contains(stdout, "WPT FAIL throws |") {
		t.Errorf("missing FAIL line:\n%s", stdout)
	}
}

func TestWPTAsyncTestWithoutDoneIsTimeout(t *testing.T) {
	path := writeWPTPage(t, `<script>
var h = async_test("never done");
h.step(function () { assert_true(true, "step"); });
test(function () { assert_true(true, "sync"); }, "sync");
</script>`)
	code, stdout, _ := runWPTFile(t, path)
	if code != 1 {
		t.Errorf("exit %d, want 1\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "WPT TIMEOUT never done") {
		t.Errorf("missing TIMEOUT line:\n%s", stdout)
	}
}

func TestWPTPromiseTestResolvesViaMicrotask(t *testing.T) {
	path := writeWPTPage(t, `<script>
promise_test(function () { return Promise.resolve(1).then(function (v) {
  assert_equals(v, 1, "value");
}); }, "promise");
</script>`)
	code, stdout, _ := runWPTFile(t, path)
	if code != 0 {
		t.Errorf("exit %d, want 0\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "WPT PASS promise") {
		t.Errorf("missing PASS line:\n%s", stdout)
	}
}

func TestWPTNoTestsIsHarnessError(t *testing.T) {
	path := writeWPTPage(t, `<div>no scripts at all</div>`)
	code, stdout, _ := runWPTFile(t, path)
	if code != 1 {
		t.Errorf("exit %d, want 1\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "WPT HARNESS ERROR") {
		t.Errorf("missing HARNESS ERROR:\n%s", stdout)
	}
}

func TestWPTRealHarnessTrap(t *testing.T) {
	// A page shipping its own testharness.js-style API: defining
	// add_completion_callback must forward our completion hook, and the
	// numeric testharness statuses must map to names.
	path := writeWPTPage(t, `<script>
add_completion_callback = function (cb) { window.__cb = cb; };
test(function () { assert_true(true, "x"); }, "shadowed");
window.__cb([{ name: "real one", status: 0 }, { name: "real two", status: 1, message: "nope" }],
  { status: 0, message: undefined });
</script>`)
	code, stdout, _ := runWPTFile(t, path)
	if code != 1 {
		t.Errorf("exit %d, want 1\n%s", code, stdout)
	}
	for _, want := range []string{"WPT HARNESS OK", "WPT PASS real one", "WPT FAIL real two | nope"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestWPTMissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWPT(filepath.Join(t.TempDir(), "missing.html"), 15000, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "browser-lite:") {
		t.Errorf("stderr %q, want browser-lite error", stderr.String())
	}
}

func TestWPTInvalidTimeoutFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--wpt", "x.html", "--wpt-timeout-ms", "soon"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--wpt-timeout-ms") {
		t.Errorf("stderr %q, want timeout complaint", stderr.String())
	}
}

func TestWPTThrowsHelpers(t *testing.T) {
	path := writeWPTPage(t, `<script>
test(function () {
  assert_throws_js("InvalidCharacterError", function () {
    var e = new DOMException("x", "InvalidCharacterError");
    throw e;
  }, "string form");
  assert_throws_js({name: "TypeError"}, function () { null.x; }, "dict form");
  assert_throws_js(TypeError, function () { null.x; }, "constructor form");
}, "throws forms");
</script>`)
	code, stdout, _ := runWPTFile(t, path)
	if code != 0 {
		t.Errorf("exit %d, want 0\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "WPT PASS throws forms") {
		t.Errorf("missing PASS line:\n%s", stdout)
	}
}

func TestWPTSquash(t *testing.T) {
	if got := wptSquash("a\x00b\x1fc  d\n\te"); got != "a b c d e" {
		t.Errorf("wptSquash = %q", got)
	}
}
