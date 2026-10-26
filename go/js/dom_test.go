package js

// T1 (dom-apis-track S1) RED tests: real DOM core through the T1/T2 runtime.
// One test per API group; each asserts observable JS behavior only. These
// fail until BindDOM installs the document/element surface.

import (
	"strings"
	"testing"

	"browser-lite/dom"
)

// mustBindDOM builds a runtime with the oracle surface plus the DOM surface
// over the parsed page, like the browser pipeline does.
func mustBindDOM(t *testing.T, html string) (*Runtime, *dom.Document) {
	t.Helper()
	doc := dom.Parse(html)
	rt := MustNew(t)
	t.Cleanup(rt.Close)
	if err := rt.Bind("t", "about:blank"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := rt.BindDOM(doc); err != nil {
		t.Fatalf("BindDOM: %v", err)
	}
	return rt, doc
}

const domProbeHTML = `<!doctype html><html><head><title>t</title></head><body>` +
	`<div id="a" class="x y"><span id="b" class="y">hi</span></div>` +
	`<p id="c">tail</p></body></html>`

// evalStr evaluates src and returns the coerced string (String(...)).
func evalStr(t *testing.T, rt *Runtime, src string) string {
	t.Helper()
	v, err := rt.Eval(`String(`+src+`)`, "test.js")
	if err != nil {
		t.Fatalf("Eval(%s): %v", src, err)
	}
	if v.Kind != KindString {
		t.Fatalf("Eval(%s) kind = %v, want string", src, v.Kind)
	}
	return v.S
}

// traversal: getElementById / querySelector(All) reuse the go/dom selector
// engine (document order, descendant chains, null on miss, detached nodes
// invisible to document queries).
func TestDOMTraversal(t *testing.T) {
	rt, _ := mustBindDOM(t, domProbeHTML)
	for src, want := range map[string]string{
		`document.getElementById("b").textContent`:                      "hi",
		`document.getElementById("missing")`:                            "null",
		`document.querySelector("#a .y").id`:                            "b",
		`document.querySelector("p").id`:                                "c",
		`document.querySelector(".nope")`:                               "null",
		`document.querySelectorAll(".y").length`:                        "2",
		`document.querySelectorAll("#a .y").length`:                     "1",
		`document.querySelectorAll("#a .y")[0].id`:                      "b",
		`document.querySelectorAll("div span")[0].id`:                   "b",
		`document.querySelector("div").tagName`:                         "DIV",
		`document.getElementById("a") === document.querySelector("#a")`: "true",
	} {
		if got := evalStr(t, rt, src); got != want {
			t.Errorf("Eval(%s) = %q, want %q", src, got, want)
		}
	}
	// Created-but-detached nodes stay invisible to document queries.
	if got := evalStr(t, rt, `(function(){ const e = document.createElement("span"); e.id = "ghost";
		return String(document.getElementById("ghost")); })()`); got != "null" {
		t.Errorf("detached node visible to getElementById: %q", got)
	}
	// Non-element nodes never match: the body has no id-bearing text hit.
	if got := evalStr(t, rt, `document.querySelectorAll("#text").length`); got != "0" {
		t.Errorf("text-node selector hit: %q", got)
	}
}

// classList: add/remove/toggle(+force)/contains over the class attribute,
// shared with className; bad tokens throw InvalidCharacterError (DOM §4.9).
func TestDOMClassList(t *testing.T) {
	rt, _ := mustBindDOM(t, domProbeHTML)
	steps := []struct{ src, want string }{
		{`document.getElementById("b").classList.contains("y")`, "true"},
		{`document.getElementById("b").classList.contains("x")`, "false"},
		{`(function(){ const c = document.getElementById("c").classList; c.add("p","q","p");
			return document.getElementById("c").className; })()`, "p q"},
		{`(function(){ const e = document.getElementById("a"); e.classList.remove("x","zz");
			return e.className; })()`, "y"},
		{`(function(){ const c = document.getElementById("a").classList;
			const r1 = c.toggle("y"); const r2 = c.toggle("y"); return r1 + "," + r2 + "," + c.contains("y"); })()`,
			"false,true,true"},
		{`(function(){ const c = document.getElementById("a").classList;
			const r1 = c.toggle("z", true); const r2 = c.toggle("z", false); return r1 + "," + r2; })()`,
			"true,false"},
		{`(function(){ document.getElementById("a").className = "m n";
			const c = document.getElementById("a").classList; return c.contains("m") + "," + c.contains("n"); })()`,
			"true,true"},
		{`(function(){ document.getElementById("a").setAttribute("class", "s");
			return document.getElementById("a").classList.contains("s"); })()`, "true"},
	}
	for _, st := range steps {
		if got := evalStr(t, rt, st.src); got != st.want {
			t.Errorf("Eval = %q, want %q\nsrc: %s", got, st.want, st.src)
		}
	}
	for _, bad := range []string{
		`document.getElementById("a").classList.add("")`,
		`document.getElementById("a").classList.add("a b")`,
		`document.getElementById("a").classList.toggle("a\tb")`,
	} {
		_, err := rt.Eval(bad, "test.js")
		if err == nil || !strings.HasPrefix(err.Error(),
			"browser-lite: js error: InvalidCharacterError:") {
			t.Errorf("Eval(%s) err = %v, want InvalidCharacterError", bad, err)
		}
	}
}

// attributes: set/get/remove/hasAttribute plus createElement validation and
// tagName casing (WHATWG: HTML tagName is ASCII-uppercase).
func TestDOMAttributes(t *testing.T) {
	rt, _ := mustBindDOM(t, domProbeHTML)
	steps := []struct{ src, want string }{
		{`document.getElementById("a").getAttribute("id")`, "a"},
		{`document.getElementById("a").getAttribute("missing")`, "null"},
		{`document.getElementById("a").hasAttribute("class")`, "true"},
		{`(function(){ const e = document.getElementById("a"); e.setAttribute("data-n", "7");
			return e.getAttribute("data-n"); })()`, "7"},
		{`(function(){ const e = document.getElementById("a"); e.setAttribute("id", "a");
			e.removeAttribute("class"); return e.hasAttribute("class") + "," + e.className; })()`, "false,"},
		{`document.createElement("SECTION").tagName`, "SECTION"},
		{`document.createElement("Div").tagName`, "DIV"},
	}
	for _, st := range steps {
		if got := evalStr(t, rt, st.src); got != st.want {
			t.Errorf("Eval = %q, want %q\nsrc: %s", got, st.want, st.src)
		}
	}
	for _, bad := range []string{`document.createElement("")`, `document.createElement("a b")`,
		`document.createElement("a<b")`} {
		_, err := rt.Eval(bad, "test.js")
		if err == nil || !strings.HasPrefix(err.Error(),
			"browser-lite: js error: InvalidCharacterError:") {
			t.Errorf("Eval(%s) err = %v, want InvalidCharacterError", bad, err)
		}
	}
}

// innerHTML: parse/serialize round-trip through the fragment parser (WHATWG
// fragment case approximated with x/net/html, like go/dom's noscript
// re-parse); void elements drop end tags, text escapes.
func TestDOMInnerHTML(t *testing.T) {
	rt, _ := mustBindDOM(t, domProbeHTML)
	if got := evalStr(t, rt, `document.getElementById("a").innerHTML`); got != `<span class="y" id="b">hi</span>` {
		t.Errorf("innerHTML get = %q", got)
	}
	steps := []struct{ src, want string }{
		{`(function(){ const e = document.getElementById("c"); e.innerHTML = "<b>bo</b>ld";
			return e.innerHTML; })()`, `<b>bo</b>ld`},
		{`(function(){ const e = document.createElement("div"); e.innerHTML = "<br><p>a&amp;b</p>";
			return e.innerHTML; })()`, `<br><p>a&amp;b</p>`},
		{`(function(){ const e = document.createElement("div"); e.innerHTML = "";
			return e.innerHTML + "," + e.textContent; })()`, `,`},
		{`(function(){ const e = document.createElement("div"); e.textContent = "a<b";
			return e.innerHTML; })()`, `a&lt;b`},
	}
	for _, st := range steps {
		if got := evalStr(t, rt, st.src); got != st.want {
			t.Errorf("Eval = %q, want %q\nsrc: %s", got, st.want, st.src)
		}
	}
}

// treeMutation: appendChild adopts (moves) nodes, removeChild detaches and
// returns the node; cycles and strangers throw (HierarchyRequestError /
// NotFoundError); textContent set swaps children for one text node.
func TestDOMTreeMutation(t *testing.T) {
	rt, _ := mustBindDOM(t, domProbeHTML)
	steps := []struct{ src, want string }{
		{`(function(){ const kid = document.createElement("em"); kid.textContent = "E";
			const r = document.getElementById("c").appendChild(kid);
			return (r === kid) + "," + document.getElementById("c").innerHTML; })()`,
			`true,tail<em>E</em>`},
		{`(function(){ const a = document.getElementById("a"); const b = document.getElementById("b");
			a.removeChild(b); document.getElementById("c").appendChild(b);
			return document.getElementById("b").textContent + "," + a.innerHTML; })()`, `hi,`},
		{`(function(){ const e = document.getElementById("c"); e.textContent = "plain";
			return e.innerHTML + "," + e.textContent; })()`, `plain,plain`},
		{`(function(){ const e = document.getElementById("c"); const t = e.textContent;
			e.textContent = null; return t + "," + e.innerHTML; })()`, `plain,`},
	}
	for _, st := range steps {
		if got := evalStr(t, rt, st.src); got != st.want {
			t.Errorf("Eval = %q, want %q\nsrc: %s", got, st.want, st.src)
		}
	}
	errCases := []struct{ src, prefix string }{
		{`(function(){ const a = document.getElementById("a");
			a.removeChild(document.getElementById("c")); })()`,
			"browser-lite: js error: NotFoundError:"},
		{`(function(){ const a = document.getElementById("a"); a.appendChild(a); })()`,
			"browser-lite: js error: HierarchyRequestError:"},
		{`document.getElementById("c").appendChild({})`,
			"browser-lite: js error: TypeError:"},
	}
	for _, ec := range errCases {
		_, err := rt.Eval(ec.src, "test.js")
		if err == nil || !strings.HasPrefix(err.Error(), ec.prefix) {
			t.Errorf("Eval err = %v, want prefix %q", err, ec.prefix)
		}
	}
}

// matchesClosest: element.matches via the shared selector engine, closest
// over the ancestor chain (self first); bad selectors throw SyntaxError.
func TestDOMMatchesClosest(t *testing.T) {
	rt, _ := mustBindDOM(t, domProbeHTML)
	for src, want := range map[string]string{
		`document.getElementById("b").matches(".y")`:            "true",
		`document.getElementById("b").matches("div")`:           "false",
		`document.getElementById("b").matches("#a .y")`:         "true",
		`document.getElementById("b").closest("div").id`:        "a",
		`document.getElementById("b").closest("span").id`:       "b",
		`document.getElementById("b").closest("table")`:         "null",
		`document.getElementById("b").closest("body") !== null`: "true",
	} {
		if got := evalStr(t, rt, src); got != want {
			t.Errorf("Eval(%s) = %q, want %q", src, got, want)
		}
	}
	for _, bad := range []string{
		`document.getElementById("b").matches("")`,
		`document.getElementById("b").closest(">")`,
		`document.querySelector("")`,
	} {
		_, err := rt.Eval(bad, "test.js")
		if err == nil || !strings.HasPrefix(err.Error(),
			"browser-lite: js error: SyntaxError:") {
			t.Errorf("Eval(%s) err = %v, want SyntaxError", bad, err)
		}
	}
}

// bannerPin: the codeberg ToU pattern — `#tou-announcement` starts hidden
// behind `tw-hidden` and the toggle script shows it via querySelector +
// classList.remove (S3 preview at unit level).
func TestDOMBannerPin(t *testing.T) {
	rt, _ := mustBindDOM(t, `<!doctype html><html><head><title>t</title></head><body>`+
		`<div id="tou-announcement" class="tw-hidden"><span>ToU</span></div>`+
		`<script>document.querySelector("#tou-announcement").classList.remove("tw-hidden");</script></body></html>`)
	if err := rt.EvalPage(`document.querySelector("#tou-announcement").classList.remove("tw-hidden");`,
		"inline-0.js"); err != nil {
		t.Fatalf("toggle script: %v", err)
	}
	if got := evalStr(t, rt, `document.querySelector("#tou-announcement").classList.contains("tw-hidden")`); got != "false" {
		t.Errorf("banner still hidden: %q", got)
	}
	if got := evalStr(t, rt, `document.querySelector("#tou-announcement").textContent`); got != "ToU" {
		t.Errorf("banner text = %q, want ToU", got)
	}
}

// oracleSurfacePreserved: the 7 oracle members keep byte-exact behavior with
// the DOM surface installed; T2-owned members stay missing.
func TestDOMOracleSurfacePreserved(t *testing.T) {
	rt, doc := mustBindDOM(t, domProbeHTML)
	if _, err := rt.Eval(`document.title = "dom touched";`, "test.js"); err != nil {
		t.Fatalf("set title: %v", err)
	}
	if got := rt.Title(); got != "dom touched" {
		t.Fatalf("Title() = %q, want dom touched", got)
	}
	if got := evalStr(t, rt, `window === globalThis`); got != "true" {
		t.Errorf("window alias broke: %q", got)
	}
	if got := evalStr(t, rt, `document.body !== null && document.body.tagName`); got != "BODY" {
		t.Errorf("document.body = %q, want BODY", got)
	}
	if got := evalStr(t, rt, `document.documentElement.tagName`); got != "HTML" {
		t.Errorf("documentElement = %q, want HTML", got)
	}
	// T2 owns these: timers still throw, cookies still absent.
	if got := evalStr(t, rt, `String(typeof __bl_dom)`); got != "undefined" {
		t.Errorf("__bl_dom visible to page scripts: %q", got)
	}
	if _, err := rt.Eval(`setTimeout(function () {}, 1)`, "test.js"); err == nil ||
		err.Error() != "browser-lite: js error: ReferenceError: setTimeout is not defined" {
		t.Errorf("setTimeout err = %v, want oracle ReferenceError", err)
	}
	if got := evalStr(t, rt, `String(document.cookie === undefined)`); got != "true" {
		t.Errorf("document.cookie present too early: %q", got)
	}
	if got := evalStr(t, rt, `String(document.activeElement === undefined)`); got != "true" {
		t.Errorf("document.activeElement present too early: %q", got)
	}
	// Mutations flag the pipeline for a style re-render.
	if rt.DomDirty() {
		t.Errorf("DomDirty before any mutation, want false")
	}
	if _, err := rt.Eval(`document.getElementById("a").classList.add("z");`, "test.js"); err != nil {
		t.Fatalf("mutate: %v", err)
	}
	if !rt.DomDirty() {
		t.Errorf("DomDirty after classList.add, want true")
	}
	_ = doc
}
