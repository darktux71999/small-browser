package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEngine writes an executable shell script acting as a browser engine:
// on `--dump <file>` it prints dumpOut to stdout and dumpErr to stderr; when
// `--png <out> <file>` is present it also writes pngBytes to <out>.
// It returns the script path.
func fakeEngine(t *testing.T, dumpOut, dumpErr string, pngBytes []byte) string {
	t.Helper()
	return fakeEngineExit(t, 0, dumpOut, dumpErr, pngBytes)
}

// fakeEngineExit is fakeEngine with a fixed process exit code.
func fakeEngineExit(t *testing.T, code int, dumpOut, dumpErr string, pngBytes []byte) string {
	t.Helper()
	dir := t.TempDir()
	pngFile := ""
	if pngBytes != nil {
		pngFile = filepath.Join(dir, "png.bin")
		if err := os.WriteFile(pngFile, pngBytes, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\n" +
		"png_out=\"\"\n" +
		"prev=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--png\" ]; then png_out=\"$a\"; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"if [ -n \"$png_out\" ] && [ -n \"" + pngFile + "\" ]; then cp \"" + pngFile + "\" \"$png_out\"; fi\n" +
		"printf '%s' \"" + shQuote(dumpOut) + "\"\n" +
		"printf '%s' \"" + shQuote(dumpErr) + "\" >&2\n" +
		fmt.Sprintf("exit %d\n", code)
	path := filepath.Join(dir, "engine.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shQuote(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

// rowStatus extracts the status cell of a fixture row from a text report.
func rowStatus(report, fixture string) string {
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, fixture) {
			for _, status := range []string{statusIdentical, statusDiff, statusSkip} {
				if strings.Contains(line, status) {
					return status
				}
			}
		}
	}
	return ""
}

func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunIdenticalEngines(t *testing.T) {
	corpus := t.TempDir()
	writeFixture(t, corpus, "hello.html", "<title>hi</title><p>hello</p>")
	oracle := fakeEngine(t, "DOM\nhtml\n", "", []byte("PNGDATA"))
	candidate := fakeEngine(t, "DOM\nhtml\n", "", []byte("PNGDATA"))
	var out strings.Builder
	code := run([]string{
		"--oracle", oracle, "--candidate", candidate,
		"--corpus", corpus, "--report", "text",
	}, &out)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; output:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "hello.html") || !strings.Contains(out.String(), "identical") {
		t.Fatalf("expected identical row for hello.html; output:\n%s", out.String())
	}
}

func TestRunDetectsDumpStdoutDiff(t *testing.T) {
	corpus := t.TempDir()
	writeFixture(t, corpus, "a.html", "<p>a</p>")
	oracle := fakeEngine(t, "DOM\nhtml\n", "", []byte("PNGDATA"))
	candidate := fakeEngine(t, "DOM\nCHANGED\n", "", []byte("PNGDATA"))
	var out strings.Builder
	code := run([]string{
		"--oracle", oracle, "--candidate", candidate,
		"--corpus", corpus, "--report", "text",
	}, &out)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; output:\n%s", code, out.String())
	}
	got := out.String()
	if rowStatus(got, "a.html") != statusDiff || !strings.Contains(got, "dump-stdout") {
		t.Fatalf("expected DIFF with dump-stdout stream; output:\n%s", got)
	}
}

func TestRunDetectsPngDiff(t *testing.T) {
	corpus := t.TempDir()
	writeFixture(t, corpus, "a.html", "<p>a</p>")
	oracle := fakeEngine(t, "same\n", "", []byte("PNG-ORACLE"))
	candidate := fakeEngine(t, "same\n", "", []byte("PNG-CANDIDATE"))
	var out strings.Builder
	code := run([]string{
		"--oracle", oracle, "--candidate", candidate,
		"--corpus", corpus, "--report", "text",
	}, &out)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; output:\n%s", code, out.String())
	}
	if got := out.String(); rowStatus(got, "a.html") != statusDiff || !strings.Contains(got, "png") {
		t.Fatalf("expected DIFF with png stream; output:\n%s", got)
	}
}

func TestRunDetectsDumpStderrDiff(t *testing.T) {
	corpus := t.TempDir()
	writeFixture(t, corpus, "a.html", "<p>a</p>")
	oracle := fakeEngine(t, "same\n", "", []byte("PNG"))
	candidate := fakeEngine(t, "same\n", "js error: boom\n", []byte("PNG"))
	var out strings.Builder
	code := run([]string{
		"--oracle", oracle, "--candidate", candidate,
		"--corpus", corpus, "--report", "text",
	}, &out)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; output:\n%s", code, out.String())
	}
	if got := out.String(); rowStatus(got, "a.html") != statusDiff || !strings.Contains(got, "dump-stderr") {
		t.Fatalf("expected DIFF with dump-stderr stream; output:\n%s", got)
	}
}

func TestRunCandidateMissingIsClean(t *testing.T) {
	corpus := t.TempDir()
	writeFixture(t, corpus, "a.html", "<p>a</p>")
	var out strings.Builder
	code := run([]string{
		"--oracle", filepath.Join(t.TempDir(), "no-such-oracle"),
		"--candidate", filepath.Join(t.TempDir(), "no-such-candidate"),
		"--corpus", corpus, "--report", "text",
	}, &out)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for missing engines; output:\n%s", code, out.String())
	}
	if got := out.String(); !strings.Contains(got, "not built") {
		t.Fatalf("expected clean 'not built' report; output:\n%s", got)
	}
}

func TestRunJSONReport(t *testing.T) {
	corpus := t.TempDir()
	writeFixture(t, corpus, "a.html", "<p>a</p>")
	writeFixture(t, corpus, "b.html", "<p>b</p>")
	oracle := fakeEngine(t, "same\n", "", []byte("PNG"))
	candidate := fakeEngine(t, "same\n", "", []byte("PNG"))
	var out strings.Builder
	code := run([]string{
		"--oracle", oracle, "--candidate", candidate,
		"--corpus", corpus, "--report", "json",
	}, &out)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; output:\n%s", code, out.String())
	}
	var report struct {
		Oracle    string `json:"oracle"`
		Candidate string `json:"candidate"`
		Corpus    string `json:"corpus"`
		Results   []struct {
			Fixture string   `json:"fixture"`
			Status  string   `json:"status"`
			Diffs   []string `json:"diffs"`
		} `json:"results"`
		Summary struct {
			Total     int `json:"total"`
			Identical int `json:"identical"`
			Diff      int `json:"diff"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out.String()), &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if report.Summary.Total != 2 || report.Summary.Identical != 2 || report.Summary.Diff != 0 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	for _, r := range report.Results {
		if r.Status != "identical" {
			t.Fatalf("fixture %s status = %q, want identical", r.Fixture, r.Status)
		}
	}
}

func TestRunBadCorpusIsUsageError(t *testing.T) {
	var out strings.Builder
	code := run([]string{
		"--oracle", "x", "--candidate", "y",
		"--corpus", filepath.Join(t.TempDir(), "no-such-dir"),
	}, &out)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 for missing corpus; output:\n%s", code, out.String())
	}
}

func TestRunBadReportFlagIsUsageError(t *testing.T) {
	corpus := t.TempDir()
	var out strings.Builder
	code := run([]string{"--corpus", corpus, "--report", "yaml"}, &out)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 for bad --report; output:\n%s", code, out.String())
	}
}

func TestRunBothEnginesFailIdentically(t *testing.T) {
	corpus := t.TempDir()
	writeFixture(t, corpus, "bad.html", "<p>bad</p>")
	oracle := fakeEngineExit(t, 1, "", "browser-lite: bad input\n", nil)
	candidate := fakeEngineExit(t, 1, "", "browser-lite: bad input\n", nil)
	var out strings.Builder
	code := run([]string{
		"--oracle", oracle, "--candidate", candidate,
		"--corpus", corpus, "--report", "text",
	}, &out)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for identically failing engines; output:\n%s", code, out.String())
	}
	if got := out.String(); !strings.Contains(got, "identical") || rowStatus(got, "bad.html") != statusIdentical {
		t.Fatalf("expected identical row for identically failing engines; output:\n%s", got)
	}
}

func TestRunExitCodeDiffers(t *testing.T) {
	corpus := t.TempDir()
	writeFixture(t, corpus, "a.html", "<p>a</p>")
	oracle := fakeEngineExit(t, 0, "same\n", "", []byte("PNG"))
	candidate := fakeEngineExit(t, 1, "same\n", "", []byte("PNG"))
	var out strings.Builder
	code := run([]string{
		"--oracle", oracle, "--candidate", candidate,
		"--corpus", corpus, "--report", "text",
	}, &out)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for exit-code mismatch; output:\n%s", code, out.String())
	}
	if got := out.String(); rowStatus(got, "a.html") != statusDiff || !strings.Contains(got, "exit-code") {
		t.Fatalf("expected DIFF with exit-code stream; output:\n%s", got)
	}
}
