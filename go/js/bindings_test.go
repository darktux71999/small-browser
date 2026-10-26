package js

import (
	"strings"
	"testing"

	"browser-lite/dom"
)

// mustBind builds a runtime with the oracle's web surface installed.
func mustBind(t *testing.T, title, rawURL string) *Runtime {
	t.Helper()
	rt := MustNew(t)
	t.Cleanup(rt.Close)
	if err := rt.Bind(title, rawURL); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return rt
}

// titles: document.title seeds from the page and round-trips through JS.
func TestBindTitle(t *testing.T) {
	cases := []struct {
		name  string
		title string
	}{
		{"plain", "cookie-echo"},
		{"with parens", "demo lite"},
		{"utf8", "café — ñ €"},
		{"empty", ""},
		{"quotes", `say "hi" 'yo'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := mustBind(t, tc.title, "about:blank")
			if got := rt.Title(); got != tc.title {
				t.Fatalf("Title() = %q, want %q", got, tc.title)
			}
			if _, err := rt.Eval(`document.title = "demo lite (js)"`, "test.js"); err != nil {
				t.Fatalf("set title: %v", err)
			}
			if got := rt.Title(); got != "demo lite (js)" {
				t.Fatalf("Title() after set = %q, want %q", got, "demo lite (js)")
			}
		})
	}
}

// windowSurface: window is the global alias; navigator carries the UA.
func TestBindWindowSurface(t *testing.T) {
	rt := mustBind(t, "window surface", "about:blank")
	for src, want := range map[string]Value{
		`window === globalThis`:                    Bool(true),
		`window.__not_a_real_member === undefined`: Bool(true),
		`typeof window.navigator.userAgent`:        String("string"),
		`window.navigator.userAgent`:               String("browser-lite/0.1"),
		`typeof window.addEventListener`:           String("function"),
		`typeof addEventListener`:                  String("function"),
		`typeof removeEventListener`:               String("function"),
	} {
		got, err := rt.Eval(src, "test.js")
		if err != nil {
			t.Fatalf("Eval(%s): %v", src, err)
		}
		if !got.Equal(want) {
			t.Fatalf("Eval(%s) = %+v, want %+v", src, got, want)
		}
	}
}

// locations: the read-only view seeded from the page URL (oracle set_url).
func TestBindLocation(t *testing.T) {
	rt := mustBind(t, "t", "about:blank")
	for src, want := range map[string]string{
		`location.href`:       "about:blank",
		`location.protocol`:   "about:",
		`location.host`:       "",
		`location.hostname`:   "",
		`location.pathname`:   "blank",
		`location.toString()`: "about:blank",
	} {
		got, err := rt.Eval(src, "test.js")
		if err != nil {
			t.Fatalf("Eval(%s): %v", src, err)
		}
		if !got.Equal(String(want)) {
			t.Fatalf("Eval(%s) = %+v, want %q", src, got, want)
		}
	}

	if err := rt.SetURL("http://example.com:8080/a/b?x=1#frag"); err != nil {
		t.Fatalf("SetURL: %v", err)
	}
	for src, want := range map[string]string{
		`location.href`:     "http://example.com:8080/a/b?x=1#frag",
		`location.protocol`: "http:",
		`location.host`:     "example.com:8080",
		`location.hostname`: "example.com",
		`location.pathname`: "/a/b",
	} {
		got, err := rt.Eval(src, "test.js")
		if err != nil {
			t.Fatalf("Eval(%s): %v", src, err)
		}
		if !got.Equal(String(want)) {
			t.Fatalf("Eval(%s) = %+v, want %q", src, got, want)
		}
	}

	// Invalid or relative URLs are rejected and keep the previous location.
	for _, bad := range []string{"http://[::1", "relative/path", ""} {
		if err := rt.SetURL(bad); err == nil {
			t.Fatalf("SetURL(%q) succeeded, want error", bad)
		}
	}
	got, err := rt.Eval(`location.href`, "test.js")
	if err != nil {
		t.Fatalf("Eval href: %v", err)
	}
	if !got.Equal(String("http://example.com:8080/a/b?x=1#frag")) {
		t.Fatalf("href after bad SetURL = %+v, want previous untouched", got)
	}
}

// locationReadOnly: assigning to the view does not navigate (no setters).
func TestBindLocationReadOnly(t *testing.T) {
	rt := mustBind(t, "t", "http://example.com/a")
	if _, err := rt.Eval(`location.href = "http://evil.example/";`, "test.js"); err != nil {
		t.Fatalf("assign href: %v", err)
	}
	got, err := rt.Eval(`location.href`, "test.js")
	if err != nil {
		t.Fatalf("Eval href: %v", err)
	}
	if !got.Equal(String("http://example.com/a")) {
		t.Fatalf("href = %+v, want navigation ignored", got)
	}
}

// pageEvents: global listeners fire once per EvalPage and are then consumed.
func TestBindPageEvents(t *testing.T) {
	rt := mustBind(t, "t", "about:blank")
	if _, err := rt.Eval(`
globalThis.__bl_calls = [];
addEventListener("DOMContentLoaded", function (ev) { globalThis.__bl_calls.push("dom:" + ev.type); });
addEventListener("load", function (ev) { globalThis.__bl_calls.push("load:" + ev.type); });
`, "test.js"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := rt.EvalPage(`1+1`, "inline-0.js"); err != nil {
		t.Fatalf("EvalPage: %v", err)
	}
	got, err := rt.Eval(`globalThis.__bl_calls.join(",")`, "test.js")
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if !got.Equal(String("dom:DOMContentLoaded,load:load")) {
		t.Fatalf("calls = %+v, want both lifecycle events once", got)
	}
	// Consumed: a second page script sees no handlers.
	if err := rt.EvalPage(`1+1`, "inline-1.js"); err != nil {
		t.Fatalf("EvalPage again: %v", err)
	}
	got, err = rt.Eval(`globalThis.__bl_calls.join(",")`, "test.js")
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if !got.Equal(String("dom:DOMContentLoaded,load:load")) {
		t.Fatalf("calls after second EvalPage = %+v, want handlers consumed", got)
	}
}

// eventRemoval: removeEventListener, duplicates, and handleEvent objects.
func TestBindEventRemoval(t *testing.T) {
	rt := mustBind(t, "t", "about:blank")
	if _, err := rt.Eval(`
globalThis.__bl_n = 0;
function inc() { globalThis.__bl_n++; }
addEventListener("DOMContentLoaded", inc);
addEventListener("DOMContentLoaded", inc); // duplicate registers once
addEventListener("DOMContentLoaded", { handleEvent: function () { globalThis.__bl_n += 10; } });
addEventListener("load", function () { globalThis.__bl_n += 100; });
removeEventListener("load", function () {}); // removing a stranger is a no-op
removeEventListener("DOMContentLoaded", inc);
`, "test.js"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := rt.EvalPage(`1+1`, "inline-0.js"); err != nil {
		t.Fatalf("EvalPage: %v", err)
	}
	got, err := rt.Eval(`globalThis.__bl_n`, "test.js")
	if err != nil {
		t.Fatalf("read n: %v", err)
	}
	// inc removed, duplicate never double-counted: only handleEvent(+10) and load(+100).
	if !got.Equal(Number(110)) {
		t.Fatalf("n = %+v, want 110", got)
	}
}

// eventHandlerErrors: a throwing handler reports through console and does
// not stop the remaining handlers or fail the page script.
func TestBindEventHandlerErrors(t *testing.T) {
	rt := mustBind(t, "t", "about:blank")
	if _, err := rt.Eval(`
globalThis.__bl_after = false;
addEventListener("DOMContentLoaded", function () { throw new Error("handler boom"); });
addEventListener("DOMContentLoaded", function () { globalThis.__bl_after = true; });
`, "test.js"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := rt.EvalPage(`1+1`, "inline-0.js"); err != nil {
		t.Fatalf("EvalPage with throwing handler: %v", err)
	}
	got, err := rt.Eval(`globalThis.__bl_after`, "test.js")
	if err != nil {
		t.Fatalf("read flag: %v", err)
	}
	if !got.Equal(Bool(true)) {
		t.Fatalf("flag = %+v, want second handler still ran", got)
	}
}

// evalPageErrors: the script's own error wins, but lifecycle handlers still run.
func TestBindEvalPageErrors(t *testing.T) {
	rt := mustBind(t, "t", "about:blank")
	if _, err := rt.Eval(`
globalThis.__bl_dom = false;
addEventListener("DOMContentLoaded", function () { globalThis.__bl_dom = true; });
`, "test.js"); err != nil {
		t.Fatalf("register: %v", err)
	}
	err := rt.EvalPage(`window.__missing_function_that_pages_might_guard();`, "inline-0.js")
	if err == nil {
		t.Fatal("EvalPage succeeded, want the script TypeError")
	}
	if got := err.Error(); got != "browser-lite: js error: TypeError: not a function" {
		t.Fatalf("error = %q, want oracle js-window-error bytes", got)
	}
	got, err := rt.Eval(`globalThis.__bl_dom`, "test.js")
	if err != nil {
		t.Fatalf("read flag: %v", err)
	}
	if !got.Equal(Bool(true)) {
		t.Fatalf("flag = %+v, want lifecycle still dispatched", got)
	}
}

// fixtureWindow: the exact js-window script yields the oracle's title.
func TestBindFixtureWindow(t *testing.T) {
	rt := mustBind(t, "window surface", "about:blank")
	src := `
    const unknown = window.__not_a_real_member === undefined;
    window.addEventListener("DOMContentLoaded", function (event) {
      const alias = window === globalThis;
      const nav = typeof window.navigator.userAgent === "string";
      const lifecycle = event.type === "DOMContentLoaded";
      document.title = "window:" + alias + ":" + unknown + ":" + nav + ":" + lifecycle;
    });
`
	if err := rt.EvalPage(src, "inline-0.js"); err != nil {
		t.Fatalf("EvalPage: %v", err)
	}
	if got := rt.Title(); got != "window:true:true:true:true" {
		t.Fatalf("Title() = %q, want the oracle --dump title", got)
	}
}

// fixtureWindowError: the exact js-window-error script keeps before-error.
func TestBindFixtureWindowError(t *testing.T) {
	rt := mustBind(t, "window error", "about:blank")
	src := `
    document.title = "before-error";
    window.__missing_function_that_pages_might_guard();
    document.title = "unreachable";
`
	err := rt.EvalPage(src, "inline-0.js")
	if err == nil {
		t.Fatal("EvalPage succeeded, want TypeError")
	}
	if got := err.Error(); got != "browser-lite: js error: TypeError: not a function" {
		t.Fatalf("error = %q, want oracle stderr bytes", got)
	}
	if got := rt.Title(); got != "before-error" {
		t.Fatalf("Title() = %q, want before-error", got)
	}
}

// fixtureDemoErrors: console passes, the trailing script fails like the oracle.
func TestBindFixtureDemoErrors(t *testing.T) {
	rt := mustBind(t, "demo lite", "about:blank")
	if _, err := rt.Eval(`console.log("js ok", 1 + 1); document.title = "demo lite (js)";`, "inline-0.js"); err != nil {
		t.Fatalf("first script: %v", err)
	}
	if got := rt.Title(); got != "demo lite (js)" {
		t.Fatalf("Title() = %q, want demo lite (js)", got)
	}
	err := rt.EvalPage(`syntax error here`, "inline-1.js")
	if err == nil {
		t.Fatal("EvalPage succeeded, want SyntaxError")
	}
	if got := err.Error(); got != "browser-lite: js error: SyntaxError: expecting ';'" {
		t.Fatalf("error = %q, want oracle demo stderr bytes", got)
	}
}

// missingAPIsMatchOracle: fixture members the oracle never implemented stay
// missing here too — the same exceptions keep the differential identical.
func TestBindMissingAPIsMatchOracle(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		golden string
	}{
		{"getElementById", `document.getElementById("out")`, "browser-lite: js error: TypeError: not a function"},
		{"createElement", `document.createElement("h1")`, "browser-lite: js error: TypeError: not a function"},
		{"setTimeout", `setTimeout(function () {}, 300)`, "browser-lite: js error: ReferenceError: setTimeout is not defined"},
		{"querySelector", `document.querySelector("p")`, "browser-lite: js error: TypeError: not a function"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := mustBind(t, "t", "about:blank")
			_, err := rt.Eval(tc.src, "inline-0.js")
			if err == nil {
				t.Fatalf("Eval(%s) succeeded, want %q", tc.src, tc.golden)
			}
			if got := err.Error(); got != tc.golden {
				t.Fatalf("Eval(%s) error = %q, want %q", tc.src, got, tc.golden)
			}
		})
	}
	// Absent data members read as undefined (cookie/body/activeElement).
	rt := mustBind(t, "t", "about:blank")
	for src := range map[string]struct{}{
		`document.cookie`:        {},
		`document.body`:          {},
		`document.activeElement`: {},
	} {
		got, err := rt.Eval(`String(`+src+` === undefined)`, "test.js")
		if err != nil {
			t.Fatalf("Eval(%s): %v", src, err)
		}
		if !got.Equal(String("true")) {
			t.Fatalf("Eval(%s) undefined-check = %+v, want true", src, got)
		}
	}
	// The cookie-set order holds too: assigning document.cookie on the plain
	// object succeeds (like the oracle), and only getElementById throws.
	if _, err := rt.Eval(`document.cookie = "demo=1; Path=/";`, "inline-0.js"); err != nil {
		t.Fatalf("cookie assign: %v", err)
	}
	if _, err := rt.Eval(`document.getElementById("out")`, "inline-0.js"); err == nil ||
		err.Error() != "browser-lite: js error: TypeError: not a function" {
		t.Fatalf("cookie-set tail err = %v, want oracle TypeError", err)
	}
}

// smokeParsedFixture: the timers script fails on the oracle's first missing
// member and leaves the parsed go/dom tree untouched.
func TestBindSmokeParsedFixture(t *testing.T) {
	doc := dom.Parse(`<html><head><title>Timers</title></head><body><p id="out">waiting</p></body></html>`)
	rt := mustBind(t, "Timers", "about:blank")
	out, ok := doc.QuerySelector("#out")
	if !ok {
		t.Fatal("fixture has no #out")
	}
	err := rt.EvalPage(`setTimeout(() => document.getElementById("out").textContent = "later", 300);`, "inline-0.js")
	if err == nil || !strings.HasPrefix(err.Error(), "browser-lite: js error: ") {
		t.Fatalf("smoke EvalPage err = %v, want oracle-format error", err)
	}
	if got := err.Error(); got != "browser-lite: js error: ReferenceError: setTimeout is not defined" {
		t.Fatalf("smoke error = %q", got)
	}
	if got := doc.TextContent(out); got != "waiting" {
		t.Fatalf("#out text = %q, want untouched waiting", got)
	}
}
