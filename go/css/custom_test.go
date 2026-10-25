package css

import (
	"reflect"
	"testing"
)

// Phase 1 (S1 capture, S2 resolution) of var()/custom-property support.
// libcss 0.9.2 discards every declaration whose value contains var() and
// knows no --* properties at all, so the rewriter extracts both into a
// side table before libcss sees the text, and the resolver answers the
// effective --* map of an element (inherited, overridden by the element's
// own declarations) plus var() substitution for the manual cascade (T2).

// --- S1: capture ---------------------------------------------------------

func TestCaptureSheetExtractsSideTable(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		out   string
		decls []CustomDecl
	}{
		{
			"custom property is removed, the other declarations stay verbatim",
			"a { --c: red; color: blue }",
			"a {; color: blue }",
			[]CustomDecl{{Property: "--c", Value: "red", Selector: "a", Order: 0}},
		},
		{
			"var() value is removed, its neighbours stay byte for byte",
			"a { color: red; border: 1px solid var(--c); margin: 0 }",
			"a { color: red;; margin: 0 }",
			[]CustomDecl{{
				Property: "border", Value: "1px solid var(--c)",
				Selector: "a", Order: 0,
			}},
		},
		{
			"a rule that only carries captured declarations empties its body",
			"a { --c: red }",
			"a {}",
			[]CustomDecl{{Property: "--c", Value: "red", Selector: "a", Order: 0}},
		},
		{
			"var( inside a string is not a var() call: nothing captured",
			`a { content: "var(--x)" }`,
			`a { content: "var(--x)" }`,
			nil,
		},
		{
			"var( inside a comment is not a var() call: nothing captured",
			"a { color: red /* var(--x) */ }",
			"a { color: red /* var(--x) */ }",
			nil,
		},
		{
			"the gap rewrite keeps working beside a captured declaration",
			"#x { display: flex; gap: 20px; border: var(--c) }",
			"#x { display: flex;min-width:20px;max-width:20px;}",
			[]CustomDecl{{
				Property: "border", Value: "var(--c)",
				Selector: "#x", Order: 0,
			}},
		},
		{
			"a gap shorthand with a var() value is captured, not carrier-rewritten",
			"#x { gap: var(--g) }",
			"#x {}",
			[]CustomDecl{{Property: "gap", Value: "var(--g)", Selector: "#x", Order: 0}},
		},
		{
			"the enclosing media condition is recorded, not evaluated",
			"@media (min-width: 500px) { a { border: 1px solid var(--c) } }",
			"@media (min-width: 500px) { a {} }",
			[]CustomDecl{{
				Property: "border", Value: "1px solid var(--c)",
				Selector: "a", Media: "(min-width: 500px)", Order: 0,
			}},
		},
		{
			"!important is split off the captured value",
			"a { border: 1px solid var(--c) !important }",
			"a {}",
			[]CustomDecl{{
				Property: "border", Value: "1px solid var(--c)",
				Selector: "a", Important: true, Order: 0,
			}},
		},
		{
			"a custom property whose value nests var() travels whole",
			"a { --a: var(--b, 1px) }",
			"a {}",
			[]CustomDecl{{Property: "--a", Value: "var(--b, 1px)", Selector: "a", Order: 0}},
		},
		{
			"VAR( is case-insensitive like every CSS function",
			"a { border: 1px solid VAR(--c) }",
			"a {}",
			[]CustomDecl{{
				Property: "border", Value: "1px solid VAR(--c)",
				Selector: "a", Order: 0,
			}},
		},
		{
			"custom property names keep their authored case",
			"a { --Color: red }",
			"a {}",
			[]CustomDecl{{Property: "--Color", Value: "red", Selector: "a", Order: 0}},
		},
		{
			"a custom property named like a gap property is still captured",
			"a { --gap: 1 }",
			"a {}",
			[]CustomDecl{{Property: "--gap", Value: "1", Selector: "a", Order: 0}},
		},
		{
			"two captured declarations in one rule number in document order",
			"a { --a: 1; margin: var(--m) }",
			"a {;}",
			[]CustomDecl{
				{Property: "--a", Value: "1", Selector: "a", Order: 0},
				{Property: "margin", Value: "var(--m)", Selector: "a", Order: 1},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, decls := CaptureSheet(tc.in)
			if got != tc.out {
				t.Errorf("rewritten text:\n  in:  %q\n  got: %q\n  exp: %q", tc.in, got, tc.out)
			}
			if !reflect.DeepEqual(decls, tc.decls) {
				t.Errorf("side table:\n  got: %+v\n  exp: %+v", decls, tc.decls)
			}
		})
	}
}

func TestCaptureInlineExtractsSideTable(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		out   string
		decls []CustomDecl
	}{
		{
			"leading custom property drops, the rest stays",
			"--c: red; color: blue",
			"; color: blue",
			[]CustomDecl{{Property: "--c", Value: "red", Order: 0}},
		},
		{
			"a trailing var() declaration drops with its text",
			"color: red; border: 1px solid var(--c)",
			"color: red;",
			[]CustomDecl{{
				Property: "border", Value: "1px solid var(--c)", Order: 0,
			}},
		},
		{
			"an all-captured inline list becomes empty",
			"--c: red",
			"",
			[]CustomDecl{{Property: "--c", Value: "red", Order: 0}},
		},
		{
			"plain declarations are copied byte for byte",
			"color: red; letter-spacing: 1.5px",
			"color: red; letter-spacing: 1.5px",
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, decls := CaptureInline(tc.in)
			if got != tc.out {
				t.Errorf("rewritten text:\n  in:  %q\n  got: %q\n  exp: %q", tc.in, got, tc.out)
			}
			if !reflect.DeepEqual(decls, tc.decls) {
				t.Errorf("side table:\n  got: %+v\n  exp: %+v", decls, tc.decls)
			}
		})
	}
}

// --- S2: resolution ------------------------------------------------------

// customFixture builds a three-level DOM (a > b > c under html/body) and
// the resolver over the given author sheets.
func customFixture(t *testing.T, sheets ...string) (*fakeDoc, *CustomProperties, int, int, int) {
	t.Helper()
	doc := parseFakeHTML(`<div id="a"><div id="b"><div id="c"></div></div></div>`)
	cp := NewCustomProperties(doc, sheets)
	a := int(mustSelect(t, doc, "#a"))
	b := int(mustSelect(t, doc, "#b"))
	c := int(mustSelect(t, doc, "#c"))
	return doc, cp, a, b, c
}

func TestCustomPropertiesInheritDownThreeLevels(t *testing.T) {
	_, cp, a, b, c := customFixture(t,
		`html { --c: red; --e: var(--c) } #b { --c: blue }`)

	checks := []struct {
		element int
		name    string
		want    string
	}{
		{a, "--c", "red"},  // a inherits html
		{a, "--e", "red"},  // a inherits html's resolved --e
		{b, "--c", "blue"}, // b's own declaration overrides the inherited one
		{b, "--e", "red"},  // the other inherited property is untouched
		{c, "--c", "blue"}, // the grandchild inherits b's override
		{c, "--e", "red"},  // the grandchild keeps the chain from the root
	}
	for _, want := range checks {
		if got := cp.EffectiveMap(want.element)[want.name]; got != want.want {
			t.Errorf("element %d: %s = %q, want %q", want.element, want.name, got, want.want)
		}
	}
}

func TestVarFallbackUsedWhenPropertyUndefined(t *testing.T) {
	_, cp, _, b, _ := customFixture(t, `html { --c: red }`)

	got, ok := cp.Resolve(b, "1px solid var(--missing, black)")
	if !ok || got != "1px solid black" {
		t.Errorf("Resolve = %q, %v; want %q, true", got, ok, "1px solid black")
	}
}

func TestVarFallbackIgnoredWhenPropertyDefined(t *testing.T) {
	_, cp, _, b, _ := customFixture(t, `html { --c: red }`)

	got, ok := cp.Resolve(b, "var(--c, black)")
	if !ok || got != "red" {
		t.Errorf("Resolve = %q, %v; want %q, true", got, ok, "red")
	}
}

func TestNestedVarSubstitution(t *testing.T) {
	_, cp, _, b, _ := customFixture(t,
		`html { --c: red; --x: 1px solid var(--c) }`)

	cases := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"a custom property value substitutes its own nested var()",
			"var(--x)", "1px solid red", true},
		{"a nested fallback substitutes when the outer property is undefined",
			"var(--missing, var(--c, green))", "red", true},
		{"the innermost fallback wins when both properties are undefined",
			"var(--missing, var(--also-missing, green))", "green", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := cp.Resolve(b, tc.in)
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Errorf("Resolve(%q) = %q, %v; want %q, %v",
					tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestVarCycleIsUnresolvable(t *testing.T) {
	_, cp, _, b, _ := customFixture(t,
		`html { --a: var(--b); --b: var(--a) }`)

	for _, name := range []string{"--a", "--b"} {
		if _, ok := cp.EffectiveMap(b)[name]; ok {
			t.Errorf("EffectiveMap[%s] survived the cycle: %q",
				name, cp.EffectiveMap(b)[name])
		}
	}
	if got, ok := cp.Resolve(b, "var(--a)"); ok {
		t.Errorf("Resolve(var(--a)) = %q, true; want unresolvable", got)
	}
	// Invalid at computed-value time: the cyclic property behaves like an
	// undefined one, so a fallback at the reference site still applies.
	if got, ok := cp.Resolve(b, "var(--a, black)"); !ok || got != "black" {
		t.Errorf("Resolve(var(--a, black)) = %q, %v; want %q, true",
			got, ok, "black")
	}
}

func TestVarUndefinedWithoutFallbackIsUnresolvable(t *testing.T) {
	_, cp, _, b, _ := customFixture(t, `html { --c: red }`)

	got, ok := cp.Resolve(b, "var(--nope)")
	if ok || got != "" {
		t.Errorf("Resolve = %q, %v; want \"\", false", got, ok)
	}
}

func TestBorderVarSmokeResolve(t *testing.T) {
	// The exact string phase 2 feeds the border cascade, through the
	// real handoff: the declaration is captured out of the sheet and
	// resolved on the element whose root declares --c: red.
	_, cp, _, b, _ := customFixture(t,
		`html { --c: red } #b { border: 1px solid var(--c) }`)

	var captured []CustomDecl
	for _, decl := range cp.Declarations() {
		if decl.Property == "border" {
			captured = append(captured, decl)
		}
	}
	if len(captured) != 1 {
		t.Fatalf("captured border declarations: %+v, want exactly one", captured)
	}
	got, ok := cp.Resolve(b, captured[0].Value)
	if !ok || got != "1px solid red" {
		t.Errorf("Resolve = %q, %v; want %q, true", got, ok, "1px solid red")
	}
}

func TestInlineCustomPropertyOverridesRuleDeclarations(t *testing.T) {
	doc := parseFakeHTML(`<div id="a" style="--c: green"><div id="b"></div></div>`)
	cp := NewCustomProperties(doc, []string{`#a { --c: red }`})
	a := int(mustSelect(t, doc, "#a"))
	b := int(mustSelect(t, doc, "#b"))

	if got := cp.EffectiveMap(a)["--c"]; got != "green" {
		t.Errorf("inline declaration lost: --c = %q, want green", got)
	}
	if got := cp.EffectiveMap(b)["--c"]; got != "green" {
		t.Errorf("child of an inline override: --c = %q, want green", got)
	}
}

func TestDeclarationsExposeTheCapturedSideTable(t *testing.T) {
	// T2 applies the captured declarations through the manual cascade: the
	// side table arrives complete, sheets in document order, with one
	// document-wide sequence number.
	doc := parseFakeHTML(`<div id="a"></div>`)
	cp := NewCustomProperties(doc, []string{
		`#a { --c: red }`,
		`#a { border: 1px solid var(--c) }`,
	})

	decls := cp.Declarations()
	want := []CustomDecl{
		{Property: "--c", Value: "red", Selector: "#a", Order: 0},
		{Property: "border", Value: "1px solid var(--c)", Selector: "#a", Order: 1},
	}
	if !reflect.DeepEqual(decls, want) {
		t.Errorf("Declarations():\n  got: %+v\n  exp: %+v", decls, want)
	}
}
