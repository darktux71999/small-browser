package main

// T3 script-pipeline tests: the oracle (src/main.rs:75-107, src/js.rs:229-239)
// is the spec — inline bodies run in document order with `src` ignored,
// each failure emits its `js error:` line to stderr and execution continues,
// and lifecycle handlers fire (consume-once) after every eval.

import (
	"bytes"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"browser-lite/dom"
	"browser-lite/js"
)

// newScriptRuntime builds a bound runtime like the browser pipeline does:
// initial DOM title plus a file:// page URL (src/main.rs:84-98).
func newScriptRuntime(t *testing.T, title string) *js.Runtime {
	t.Helper()
	rt, err := js.New()
	if err != nil {
		t.Fatalf("js.New: %v", err)
	}
	t.Cleanup(rt.Close)
	if err := rt.Bind(title, "file:///tmp/t3probe/page.html"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return rt
}

// captureProcessStdout redirects fd 1 (where QuickJS console.log writes via
// C printf) into a pipe while fn runs, then returns everything written.
func captureProcessStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	fd := int(os.Stdout.Fd())
	saved, err := syscall.Dup(fd)
	if err != nil {
		t.Fatalf("dup: %v", err)
	}
	defer syscall.Close(saved)
	if err := syscall.Dup2(int(w.Fd()), fd); err != nil {
		t.Fatalf("dup2: %v", err)
	}
	fn()
	syscall.Dup2(saved, fd)
	w.Close()
	out, _ := io.ReadAll(r)
	r.Close()
	return string(out)
}

func TestScriptsRunInDocumentOrder(t *testing.T) {
	doc := dom.Parse(`<!doctype html><html><head><title>t</title></head><body>` +
		`<script>console.log("first");</script>` +
		`<script>console.log("second");</script></body></html>`)
	rt := newScriptRuntime(t, "t")
	var stderr bytes.Buffer
	got := captureProcessStdout(t, func() {
		runPageScripts(doc, rt, &stderr)
	})
	if want := "[js] first\n[js] second\n"; got != want {
		t.Errorf("console order:\n got %q\nwant %q", got, want)
	}
	if stderr.String() != "" {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestScriptErrorEmitsLineAndContinues(t *testing.T) {
	doc := dom.Parse(`<!doctype html><html><head><title>t</title></head><body>` +
		`<script>syntax error here</script>` +
		`<script>document.title = "after-failure";</script></body></html>`)
	rt := newScriptRuntime(t, "t")
	var stderr bytes.Buffer
	runPageScripts(doc, rt, &stderr)
	// Byte-exact oracle line (src/main.rs:104-106): the demo fixture's
	// second script fails the same way against the oracle itself.
	if want := "browser-lite: js error: SyntaxError: expecting ';'\n"; stderr.String() != want {
		t.Errorf("stderr:\n got %q\nwant %q", stderr.String(), want)
	}
	if got := rt.Title(); got != "after-failure" {
		t.Errorf("title after failing script = %q, want %q (script 2 must still run)", got, "after-failure")
	}
}

func TestScriptSrcIgnoredLikeOracle(t *testing.T) {
	// The oracle never fetches src (src/main.rs:99-107 reads text_content
	// only): src-only scripts are empty and skipped, src+body scripts run
	// their inline body. No fetch, no error line.
	doc := dom.Parse(`<!doctype html><html><head><title>src probe</title></head><body>` +
		`<script src="https://example.invalid/x.js"></script>` +
		`<script>   </script>` +
		`<script src="other.js">document.title = "embedded-ran";</script>` +
		`<script>document.title = document.title + "+inline";</script></body></html>`)
	rt := newScriptRuntime(t, "src probe")
	var stderr bytes.Buffer
	runPageScripts(doc, rt, &stderr)
	if stderr.String() != "" {
		t.Errorf("stderr = %q, want empty (src is ignored, never fetched)", stderr.String())
	}
	if got, want := rt.Title(), "embedded-ran+inline"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestDispatchConsumedOnceAcrossScripts(t *testing.T) {
	// __bl_dispatch consumes the handler list (src/js.rs:99-101), and eval
	// fires the lifecycle events after every script (src/js.rs:229-239):
	// the handler registered by script 1 fires once, not again after
	// script 2.
	doc := dom.Parse(`<!doctype html><html><head><title>t</title></head><body>` +
		`<script>addEventListener("DOMContentLoaded", function () { console.log("fired"); });</script>` +
		`<script>console.log("second");</script></body></html>`)
	rt := newScriptRuntime(t, "t")
	var stderr bytes.Buffer
	got := captureProcessStdout(t, func() {
		runPageScripts(doc, rt, &stderr)
	})
	if want := "[js] fired\n[js] second\n"; got != want {
		t.Errorf("dispatch across scripts:\n got %q\nwant %q", got, want)
	}
	if stderr.String() != "" {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestDumpTitleReflectsScriptsWindowFixture(t *testing.T) {
	// End-to-end through run(): the js-window fixture's shape
	// (`title: window:true:true:true:true`) is produced by the lifecycle
	// handler firing after the eval, with title re-read after scripts.
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dump", fixture("js-window.html")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr: %q", stderr.String())
	}
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if got, want := lines[len(lines)-1], "title: window:true:true:true:true"; got != want {
		t.Errorf("last dump line = %q, want %q", got, want)
	}
}
