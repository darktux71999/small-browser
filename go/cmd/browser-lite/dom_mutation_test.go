package main

// T1 (dom-apis-track S1) pipeline tests: DOM mutations from page scripts are
// live on the arena (dumpDOM reads it), and the style re-render after
// scripts (main.go's DomDirty gate) makes class-driven visibility real in
// layout. The banner case is the S3 preview at pipeline level.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"browser-lite/dom"
	"browser-lite/js"
)

// newDOMRuntime binds the oracle surface plus the live-DOM surface, like
// run() does.
func newDOMRuntime(t *testing.T, doc *dom.Document, title string) *js.Runtime {
	t.Helper()
	rt := newScriptRuntime(t, title)
	if err := rt.BindDOM(doc); err != nil {
		t.Fatalf("BindDOM: %v", err)
	}
	return rt
}

// dumpShowsDomMutation: the js-dom fixture's scripts now succeed (no stderr)
// and their created nodes reach layout and the title line.
func TestDumpShowsDomMutation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dump", fixture("js-dom.html")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	// Deliberate divergence (dom-apis-track T1): the oracle errors on
	// getElementById, so its stderr carries the TypeError and its layout
	// never sees the script-built nodes. Ours runs them clean.
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty (scripts succeed now)", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"#text \"QuickJS + Blitz\"",
		"#text \"Este nodo lo creó QuickJS y lo pintó Blitz.\"",
		"title: QuickJS + Blitz",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dump lacks %q\nfull dump:\n%s", want, out)
		}
	}
}

// bannerPinRendersVisible: the codeberg ToU pattern (hidden behind
// .tw-hidden{display:none}, shown by querySelector+classList.remove) ends up
// VISIBLE in layout — the re-render proof. The control without the toggle
// script keeps the banner out of layout.
func TestBannerPinRendersVisible(t *testing.T) {
	const page = `<!doctype html><html><head><title>t</title>` +
		`<style>.tw-hidden{display:none}</style></head><body>` +
		`<div id="tou-announcement" class="tw-hidden"><span>ToU v2</span></div>` +
		`%s</body></html>`
	cases := []struct {
		name       string
		script     string
		wantBanner bool
	}{
		{"toggle shows", `<script>document.querySelector("#tou-announcement").classList.remove("tw-hidden");</script>`, true},
		{"control stays hidden", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "banner.html")
			if err := os.WriteFile(file, []byte(strings.Replace(page, "%s", tc.script, 1)), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			var stdout, stderr bytes.Buffer
			code := run([]string{"--dump", file}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr.String())
			}
			if stderr.String() != "" {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			layout := stdout.String()[strings.Index(stdout.String(), "LAYOUT"):]
			if got := strings.Contains(layout, "ToU v2"); got != tc.wantBanner {
				t.Errorf("banner in layout = %v, want %v\n%s", got, tc.wantBanner, layout)
			}
		})
	}
}

// domDirtyGatesRecompute: a script that only reads leaves DomDirty false;
// the class toggle flips it (unit-level pin of the main.go gate).
func TestDomDirtyGatesRecompute(t *testing.T) {
	doc := dom.Parse(`<!doctype html><html><head><title>t</title></head>` +
		`<body><div id="a" class="tw-hidden">x</div></body></html>`)
	rt := newDOMRuntime(t, doc, "t")
	var stderr bytes.Buffer
	runPageScripts(doc, rt, &stderr)
	if rt.DomDirty() {
		t.Errorf("DomDirty after read-only scripts, want false")
	}
	_ = stderr
	doc2 := dom.Parse(`<!doctype html><html><head><title>t</title></head>` +
		`<body><div id="a" class="tw-hidden">x</div>` +
		`<script>document.getElementById("a").classList.remove("tw-hidden");</script></body></html>`)
	rt2 := newDOMRuntime(t, doc2, "t")
	var stderr2 bytes.Buffer
	runPageScripts(doc2, rt2, &stderr2)
	if stderr2.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr2.String())
	}
	if !rt2.DomDirty() {
		t.Errorf("DomDirty after classList.remove, want true")
	}
	if got := doc2.Nodes[0].Tag; got != "#document" {
		t.Errorf("arena root = %q", got)
	}
}
