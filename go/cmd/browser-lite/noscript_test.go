// End-to-end tests for the noscript policy (leading-fidelity S2,
// odd/tasks/leading-fidelity.md): the CLI's inline-style collection skips
// <style> elements inside <noscript> (the phantom ToU banner pattern), and
// the dump keeps the parsed noscript markup while painting nothing from it.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"browser-lite/dom"
)

const phantomBannerPage = `<!doctype html>
<html><head><style>.tw-hidden { display: none !important }</style></head>
<body>
<div style="height: 100px">top</div>
<noscript>
<style>#tou { display: flex !important }</style>
<div id="tou" class="tw-hidden"><div style="height: 100px">ToU banner text</div></div>
</noscript>
<div style="height: 100px">bottom</div>
</body></html>
`

// The same page with the noscript element removed: the phantom banner must
// cost the layout nothing, so its layout section is the reference twin.
const phantomBannerTwin = `<!doctype html>
<html><head><style>.tw-hidden { display: none !important }</style></head>
<body>
<div style="height: 100px">top</div>
<div style="height: 100px">bottom</div>
</body></html>
`

func TestLoadSheetsSkipsNoscriptStyles(t *testing.T) {
	doc := dom.Parse(`<html><head><style>.keep { color: red }</style></head><body>` +
		`<noscript><style>#tou { display: flex !important }</style>` +
		`<div id="tou" class="tw-hidden">ToU</div></noscript></body></html>`)
	var stderr bytes.Buffer
	sheets := loadSheets(doc, "https://example.org/", newLoader(&stderr))
	if stderr.String() != "" {
		t.Fatalf("stderr: %q", stderr.String())
	}
	// Preserve pin: exactly the one <style> outside noscript stays
	// collected; the noscript one is dropped.
	if len(sheets) != 1 {
		var texts []string
		for _, s := range sheets {
			texts = append(texts, s.stripped)
		}
		t.Fatalf("collected %d sheets %q, want only the one outside noscript", len(sheets), texts)
	}
	if !strings.Contains(sheets[0].stripped, ".keep") || strings.Contains(sheets[0].stripped, "#tou") {
		t.Fatalf("collected sheet = %q, want the .keep one without the noscript rules", sheets[0].stripped)
	}
}

// layoutSection cuts the dump at its LAYOUT part: the DOM section legitimately
// keeps the noscript markup (the P1 parse parity), the layout section must not
// carry anything the noscript painted.
func layoutSection(out string) string {
	i := strings.Index(out, "LAYOUT")
	if i < 0 {
		return out
	}
	return out[i:]
}

func writePage(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestDumpPhantomBannerIsNeverPainted(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	banner := writePage(t, dir, "banner.html", phantomBannerPage)
	code := run([]string{"--dump", banner}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr: %q", stderr.String())
	}
	out := stdout.String()

	// The phantom banner never paints: no box carries its text or element.
	if strings.Contains(layoutSection(out), "ToU banner text") {
		t.Fatalf("noscript banner text painted in the layout section:\n%s", out)
	}
	if strings.Contains(layoutSection(out), "noscript") {
		t.Fatalf("noscript content produced layout boxes:\n%s", out)
	}

	// And it costs zero vertical space: the twin page without the noscript
	// element has the identical layout section.
	var twinOut, twinErr bytes.Buffer
	twin := writePage(t, dir, "twin.html", phantomBannerTwin)
	if code := run([]string{"--dump", twin}, &twinOut, &twinErr); code != 0 {
		t.Fatalf("twin exit %d, stderr %q", code, twinErr.String())
	}
	if got, want := layoutSection(out), layoutSection(twinOut.String()); got != want {
		t.Errorf("layout with noscript differs from the twin without it:\n with: %q\n twin: %q", got, want)
	}

	// The DOM dump keeps the noscript contents as markup (the P1 parse
	// parity with the oracle stays intact).
	for _, want := range []string{"noscript", `div#tou.tw-hidden`, `#text "ToU banner text"`} {
		if !strings.Contains(out[:len(out)-len(layoutSection(out))], want) {
			t.Fatalf("DOM section lost the parsed noscript markup %q:\n%s", want, out)
		}
	}
}
