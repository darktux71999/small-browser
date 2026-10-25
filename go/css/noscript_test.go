// Tests for the noscript stylesheet-collection policy (leading-fidelity S2,
// odd/tasks/leading-fidelity.md). A browser with scripting enabled treats
// <noscript> contents as raw text: Firefox never applies CSS from inside a
// <noscript> and never renders its contents. The DOM parse keeps the markup
// (the P1 parity with the oracle stays intact — see go/dom/noscript_test.go);
// only the collection of author stylesheets must skip the subtree, so the
// phantom ToU banner (a noscript-only <style> beating .tw-hidden) never
// wins the cascade.
package css

import (
	"strings"
	"testing"
)

func TestStylesheetsSkipsNoscriptStyles(t *testing.T) {
	// A <style> inside a <noscript> is not an author stylesheet; a <style>
	// outside it still is (the pre-fix collection behavior is preserved for
	// everything the noscript rule does not cover).
	const html = `<style>.outer { color: red }</style>` +
		`<noscript><style>#t { display: flex !important }</style><div id="t" class="tw-hidden">No JS</div></noscript>`
	d := parseFakeHTML(html)
	sheets := Stylesheets(d)
	if len(sheets) != 1 {
		t.Fatalf("collected %d sheets %q, want only the one outside noscript", len(sheets), sheets)
	}
	if !strings.Contains(sheets[0], ".outer") {
		t.Fatalf("collected sheet = %q, want the .outer one", sheets[0])
	}
}

func TestStylesheetsKeepsStylesOutsideNoscript(t *testing.T) {
	// Preserve pin: two styles, one before and one after the noscript
	// element, both stay collected, in document order; the noscript one is
	// the only casualty.
	const html = `<style>.a { color: red }</style>` +
		`<noscript><style>.hidden { color: green }</style></noscript>` +
		`<style>.b { color: blue }</style>`
	sheets := Stylesheets(parseFakeHTML(html))
	if len(sheets) != 2 || !strings.Contains(sheets[0], ".a") || !strings.Contains(sheets[1], ".b") {
		t.Fatalf("collected sheets = %q, want [.a] then [.b]", sheets)
	}
}

func TestNoscriptStyleNeverWinsTheCascade(t *testing.T) {
	// The phantom-banner pattern end-to-end on the cascade: with the
	// noscript sheet collected, #t (specificity 100, !important) beats
	// .tw-hidden and paints a flex container. With the S2 rule the sheet
	// is not an author stylesheet, so .tw-hidden keeps display: none.
	s := styleOf(t,
		`<style>.tw-hidden { display: none !important }</style>`+
			`<noscript><style>#t { display: flex !important }</style><div id="t" class="tw-hidden">No JS</div></noscript>`,
		"#t")
	if s.Display != DisplayNone {
		t.Fatalf("#t display: %v, want none (the noscript stylesheet must not be collected)", s.Display)
	}
}

func TestHasNoscriptAncestorWalksStrictAncestors(t *testing.T) {
	d := parseFakeHTML(
		`<style>.a { color: red }</style>` +
			`<noscript><div><style>.b { color: blue }</style><p>x</p></div></noscript>`)
	ids := d.QuerySelectorAll("style")
	if len(ids) != 2 {
		t.Fatalf("fixture styles: %d, want 2", len(ids))
	}
	if HasNoscriptAncestor(d, ids[0]) {
		t.Errorf("the outer style must have no noscript ancestor")
	}
	if !HasNoscriptAncestor(d, ids[1]) {
		t.Errorf("a style nested deeper inside a noscript still has one as an ancestor")
	}
}
