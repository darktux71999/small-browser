package js

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

// MustNew builds a runtime or fails the test.
func MustNew(t *testing.T) *Runtime {
	t.Helper()
	rt, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rt
}

// evalPrimitives: Eval of a simple expression returns the right primitive.
func TestEvalPrimitives(t *testing.T) {
	rt := MustNew(t)
	defer rt.Close()
	cases := []struct {
		name string
		src  string
		want Value
	}{
		{"int arithmetic", "1+2", Number(3)},
		{"float", "0.5", Number(0.5)},
		{"string", `"hello"`, String("hello")},
		{"bool true", "true", Bool(true)},
		{"bool false", "!true", Bool(false)},
		{"null", "null", Null()},
		{"undefined", "undefined", Undefined()},
		{"void", "void 0", Undefined()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rt.Eval(tc.src, "test.js")
			if err != nil {
				t.Fatalf("Eval(%q) error: %v", tc.src, err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("Eval(%q) = %+v, want %+v", tc.src, got, tc.want)
			}
		})
	}
}

// oracleErrors: a syntax error or a thrown exception yields the oracle's
// byte-exact message. Each golden below was captured from the oracle
// binary itself:
//
//	./target/debug/browser-lite --dump <fixture> 2>stderr
//
// for the same script source, on quickjs-ng 0.17.0.
func TestOracleErrors(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		golden string
	}{
		{"syntax", "foo(", "browser-lite: js error: SyntaxError: unexpected token in expression: ''"},
		{"reference", "noSuchVar;", "browser-lite: js error: ReferenceError: noSuchVar is not defined"},
		{"thrown error", `throw new Error("boom");`, "browser-lite: js error: Error: boom"},
		{"thrown string", `throw "plain";`, "browser-lite: js error: plain"},
		{"null property", "null.foo", "browser-lite: js error: TypeError: cannot read property 'foo' of null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := MustNew(t)
			defer rt.Close()
			_, err := rt.Eval(tc.src, "test.js")
			if err == nil {
				t.Fatalf("Eval(%q) succeeded, want error %q", tc.src, tc.golden)
			}
			if got := err.Error(); got != tc.golden {
				t.Fatalf("Eval(%q) error = %q, want golden %q", tc.src, got, tc.golden)
			}
			var jserr *JSError
			if !errors.As(err, &jserr) {
				t.Fatalf("Eval(%q) error type = %T, want *JSError", tc.src, err)
			}
		})
	}
}

// objectHandles: an object created in JS round-trips through the handle
// registry — properties read back, then the handle frees deterministically.
func TestObjectHandles(t *testing.T) {
	rt := MustNew(t)
	defer rt.Close()
	v, err := rt.Eval(`({a: 42, name: "x"})`, "test.js")
	if err != nil {
		t.Fatalf("Eval object: %v", err)
	}
	if v.Kind != KindObject || !v.H.Valid() {
		t.Fatalf("Eval object = %+v, want object handle", v)
	}
	a, err := rt.Get(v.H, "a")
	if err != nil {
		t.Fatalf("Get(a): %v", err)
	}
	if !a.Equal(Number(42)) {
		t.Fatalf("Get(a) = %+v, want 42", a)
	}
	missing, err := rt.Get(v.H, "nope")
	if err != nil {
		t.Fatalf("Get(missing): %v", err)
	}
	if !missing.Equal(Undefined()) {
		t.Fatalf("Get(missing) = %+v, want undefined", missing)
	}
	rt.Free(v.H)
	if _, err := rt.Get(v.H, "a"); err == nil {
		t.Fatal("Get after Free succeeded, want error")
	}
}

// functionCall: Eval yields a function handle and call-through works.
func TestFunctionCall(t *testing.T) {
	rt := MustNew(t)
	defer rt.Close()
	fn, err := rt.Eval(`(function(a, b) { return a + b; })`, "test.js")
	if err != nil {
		t.Fatalf("Eval function: %v", err)
	}
	if fn.Kind != KindFunction || !fn.H.Valid() {
		t.Fatalf("Eval function = %+v, want function handle", fn)
	}
	sum, err := rt.Call(fn.H, Number(2), Number(3))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !sum.Equal(Number(5)) {
		t.Fatalf("Call = %+v, want 5", sum)
	}
}

// globalFunc: a Go function attached to the global object is callable
// from JS (the T2 registration point); a Go-side error becomes a thrown
// JS error in oracle format.
func TestGlobalFunc(t *testing.T) {
	rt := MustNew(t)
	defer rt.Close()
	if err := rt.SetGlobalFunc("add", func(args []Value) (Value, error) {
		return Number(args[0].N + args[1].N), nil
	}); err != nil {
		t.Fatalf("SetGlobalFunc: %v", err)
	}
	v, err := rt.Eval(`add(20, 22)`, "test.js")
	if err != nil {
		t.Fatalf("Eval add: %v", err)
	}
	if !v.Equal(Number(42)) {
		t.Fatalf("add(20, 22) = %+v, want 42", v)
	}
	if err := rt.SetGlobalFunc("boom", func(args []Value) (Value, error) {
		return Undefined(), errBoom
	}); err != nil {
		t.Fatalf("SetGlobalFunc boom: %v", err)
	}
	_, err = rt.Eval(`boom()`, "test.js")
	if err == nil || !strings.HasPrefix(err.Error(), "browser-lite: js error: ") {
		t.Fatalf("boom() error = %v, want oracle-format error", err)
	}
}

// timeLimit: the evaluation limit trips on an infinite loop (the S5
// mitigation), while ordinary scripts still run under the default bound.
func TestTimeLimit(t *testing.T) {
	rt := MustNew(t)
	defer rt.Close()
	if _, err := rt.Eval(`1+1`, "test.js"); err != nil {
		t.Fatalf("Eval under default limit: %v", err)
	}
	rt.SetTimeLimit(50 * time.Millisecond)
	_, err := rt.Eval(`while (true) {}`, "test.js")
	if err == nil {
		t.Fatal("infinite loop Eval succeeded, want interrupt error")
	}
	if !strings.HasPrefix(err.Error(), "browser-lite: js error: ") {
		t.Fatalf("interrupt error = %q, want oracle-format prefix", err.Error())
	}
	t.Logf("interrupt message: %q", err.Error())
	rt.SetTimeLimit(0)
	if _, err := rt.Eval(`1+1`, "test.js"); err != nil {
		t.Fatalf("Eval with limit disabled: %v", err)
	}
}
