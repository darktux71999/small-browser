package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(name string) string {
	return filepath.Join("..", "..", "..", "test", "data", name)
}

func TestDumpHelloExact(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dump", fixture("hello.html")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr: %q", stderr.String())
	}
	// S3 (leading-fidelity): body/p margins now collapse per CSS 2.1 §8.3.1
	// (UA body 8px folds with the p's 16px block margins into one 16px
	// margin above the body border; the p sits at the body content top).
	// The old golden summed them (body at 8, p at 24, height 68).
	want := `DOM
#document
  html
    head
      title
        #text "hello"
      style
        #text "body { background: white; color: black; }"
    body
      p#t
        #text "hello lite"
LAYOUT 1000x55
  [   0.0    0.0 1000.0   39.0] html
  [   8.0   16.0  984.0   23.0] body
  [   8.0   16.0  984.0   23.0] p
  [   8.0   16.0   64.0   23.0] #text "hello lite"
title: hello
`
	if stdout.String() != want {
		t.Errorf("dump mismatch:\n got %q\nwant %q", stdout.String(), want)
	}
}

func TestDumpLongMeasuresFullDocument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dump", fixture("long.html")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	out := stdout.String()
	line, ok := "", false
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "LAYOUT ") {
			line, ok = strings.TrimSpace(l), true
		}
	}
	if !ok {
		t.Fatalf("no LAYOUT header in %q", out)
	}
	if line != "LAYOUT 1000x3200" {
		t.Errorf("LAYOUT header: %q, want 1000x3200", line)
	}
	boxes, maxBottom := 0, 0.0
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "  [") {
			continue
		}
		rest := strings.TrimPrefix(l, "  [")
		rect := rest[:strings.Index(rest, "]")]
		var x, y, w, h float64
		if _, err := fmt.Sscanf(rect, "%f %f %f %f", &x, &y, &w, &h); err != nil {
			t.Fatalf("box rect %q: %v", rect, err)
		}
		boxes++
		maxBottom = max(maxBottom, y+h)
	}
	if boxes == 0 {
		t.Fatal("dump lists no layout boxes")
	}
	if maxBottom < 3199.0 {
		t.Errorf("boxes reach only %v, want >= 3199", maxBottom)
	}
}

func TestPNGWritesFullDocument(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "long.png")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--png", out, fixture("long.html")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	want := fmt.Sprintf("wrote %s (1000x3200)\n", out)
	if stdout.String() != want {
		t.Errorf("wrote line: got %q want %q", stdout.String(), want)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read png: %v", err)
	}
	image, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if w, h := image.Bounds().Dx(), image.Bounds().Dy(); w != 1000 || h != 3200 {
		t.Errorf("png is %dx%d, want 1000x3200", w, h)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"--dump"}, {"--png"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v): exit %d, want 2", args, code)
		}
		want := "usage: browser-lite [--dump] [--png out.png] <file.html|http(s)://url>\n"
		if stderr.String() != want {
			t.Errorf("run(%v): stderr %q, want %q", args, stderr.String(), want)
		}
	}
}

func TestMissingFileError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dump", "/nonexistent.html"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	want := "browser-lite: /nonexistent.html: No such file or directory (os error 2)\n"
	if stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
}

// The interactive path renders N frames under the dummy video driver and
// exits 0: the headless smoke for the SDL3 window shell (no display needed).
func TestInteractiveFramesDummyDriver(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "browser-lite")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}
	smoke := exec.Command(binary, "--frames", "5", fixture("long.html"))
	smoke.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy", "SDL_AUDIODRIVER=dummy")
	var stdout, stderr bytes.Buffer
	smoke.Stdout, smoke.Stderr = &stdout, &stderr
	if err := smoke.Run(); err != nil {
		t.Fatalf("dummy-driver --frames 5: %v\nstdout: %q\nstderr: %q", err, stdout.String(), stderr.String())
	}
	if stdout.String() != "" {
		t.Errorf("stdout = %q, want empty (frames mode is silent)", stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}
