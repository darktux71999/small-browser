package main

// WPT headless mode (--wpt): loads one test page through the engine,
// runs its scripts with the WPT hook injected first, and reports the
// per-subtest results in a stable machine-readable line set:
//
//	WPT HARNESS <harness>[ | <message>]
//	WPT <STATUS> <name>[ | <message>]
//	WPT SUMMARY total=N pass=N fail=N timeout=N notrun=N precondition_failed=N
//	WPT JSON <compact result object>
//
// Exit codes: 0 when the harness completed OK (or PRECONDITION_FAILED)
// and every subtest passed; 1 when a subtest failed, timed out, did not
// run, or the harness errored; 2 when the page never produced results
// (hook failure, finish failure, unreadable JSON).
//
// JSON schema (contract for tools/wpt-score.py; also documented at the
// top of test/wpt-hook.js, the canonical hook source):
//
//	{"harness": "OK"|"ERROR"|"TIMEOUT"|"PRECONDITION_FAILED",
//	 "message": string|null,
//	 "subtests": [{"name": string,
//	               "status": "PASS"|"FAIL"|"TIMEOUT"|"NOTRUN"|
//	                         "PRECONDITION_FAILED",
//	               "message": string|null}]}
//
// Scope (mirrors the reference semantics, degraded honestly): only
// testharness.js-style tests. No reftests, no workers, no wdspec. Async
// tests that need timers fail or time out because this runtime has no
// setTimeout/setInterval (see the hook header); they are reported, never
// faked.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"browser-lite/dom"
	"browser-lite/js"
	blnet "browser-lite/net"
)

// wptHook is the page-visible hook, installed before any page script.
// Canonical source: test/wpt-hook.js (TestWPTHookMatchesCanonicalFile
// pins this const to that file byte for byte).
const wptHook = `// WPT hook for browser-lite --wpt mode (canonical source; the Go
// constant wptHook in go/cmd/browser-lite/wpt.go embeds this file
// byte for byte, pinned by TestWPTHookMatchesCanonicalFile).
//
// What it does: installed into the fresh JS runtime BEFORE any page
// script runs, it traps the testharness.js completion API
// (add_completion_callback) the way a real WPT runner does, collects
// one result per subtest, and exposes __bl_wpt_finish(), which the Go
// driver calls after the page scripts (and their microtasks) run.
//
// Two worlds, one hook:
//
//  1. Pages shipping testharness.js: assigning the global
//     add_completion_callback fires the setter trap below, which hands
//     the harness our completion callback. Statuses arrive numeric and
//     are mapped by name (testharness.js codes: harness
//     OK=0/ERROR=1/TIMEOUT=2/PRECONDITION_FAILED=3; subtests
//     PASS=0/FAIL=1/TIMEOUT=2/NOTRUN=3/PRECONDITION_FAILED=4).
//  2. Pages without testharness.js (our test/wpt-local fixtures): the
//     hook itself provides a minimal compatible surface -- test,
//     async_test, promise_test, setup, assert_true/false,
//     assert_equals/not_equals, assert_in_array, assert_throws_js,
//     assert_unreached -- and completion is computed at finish time.
//
// JSON schema (contract for tools/wpt-score.py; message fields are raw,
// the Go driver squashes control runs only for the text lines):
//
//   {"harness": "OK"|"ERROR"|"TIMEOUT"|"PRECONDITION_FAILED",
//    "message": string|null,
//    "subtests": [{"name": string,
//                  "status": "PASS"|"FAIL"|"TIMEOUT"|"NOTRUN"|"PRECONDITION_FAILED",
//                  "message": string|null}]}
//
// Honest gaps (NOT-RUN degradation, never faked passes): this runtime
// has no setTimeout/setInterval, no fetch/XHR, no workers. Async tests
// that need timers throw ReferenceError inside their step (recorded as
// FAIL with that message); async_test handles never done() by finish
// time are reported TIMEOUT; promise_tests never settled by finish time
// are reported TIMEOUT. A page that records no subtest and completes no
// real harness finishes with harness ERROR ("no subtests recorded").
// The hook itself needs nothing beyond ES5 + JSON + Promise.
(function (global) {
  "use strict";
  if (global.__bl_wpt_installed) return;
  global.__bl_wpt_installed = true;
  global.__bl_wpt_done = false;
  global.__bl_wpt_json = "";

  var PASS = "PASS", FAIL = "FAIL", TIMEOUT = "TIMEOUT";
  var HARNESS_NAMES = ["OK", "ERROR", "TIMEOUT", "PRECONDITION_FAILED"];
  var SUBTEST_NAMES = ["PASS", "FAIL", "TIMEOUT", "NOTRUN", "PRECONDITION_FAILED"];

  var subtests = [];
  var callbacks = [];
  var finalizers = [];
  var real = null;
  var realSeen = false;

  function record(name, status, message) {
    subtests.push({
      name: String(name),
      status: status,
      message: message === undefined || message === null ? null : String(message)
    });
  }

  function errText(e) {
    if (e === undefined || e === null) return "empty exception";
    if (e.message !== undefined) return String(e.message);
    return String(e);
  }

  function fmt(v) {
    if (typeof v === "string") return JSON.stringify(v);
    try { return String(v); } catch (e) { return "?"; }
  }

  function shimTest(fn, name) {
    var t = name === undefined ? "" : String(name);
    try {
      fn();
      record(t, PASS, null);
    } catch (e) {
      record(t, FAIL, errText(e));
    }
  }

  function shimAsyncTest(name) {
    var t = name === undefined ? "" : String(name);
    var failed = null, finished = false;
    function fin() {
      if (finished) return null;
      finished = true;
      return failed === null ? { status: PASS, message: null } : { status: FAIL, message: failed };
    }
    finalizers.push(function () {
      var r = fin();
      if (r !== null) record(t, TIMEOUT, "async_test did not call done()");
    });
    return {
      step: function (fn) {
        if (finished) return;
        try {
          fn();
        } catch (e) {
          if (failed === null) failed = errText(e);
        }
      },
      done: function () {
        var r = fin();
        if (r !== null) record(t, r.status, r.message);
      }
    };
  }

  function shimPromiseTest(fn, name) {
    var t = name === undefined ? "" : String(name);
    var settled = false;
    function pass() {
      if (!settled) { settled = true; record(t, PASS, null); }
    }
    function fail(e) {
      if (!settled) { settled = true; record(t, FAIL, errText(e)); }
    }
    finalizers.push(function () {
      if (!settled) { settled = true; record(t, TIMEOUT, "promise_test never settled (no timers in this runtime)"); }
    });
    try {
      var r = fn();
      if (r && typeof r.then === "function") r.then(pass, fail);
      else pass();
    } catch (e) {
      fail(e);
    }
  }

  function shimSetup() {}

  function check(cond, desc) {
    if (!cond) throw new Error(desc === undefined ? "assert failed" : String(desc));
  }

  var asserts = {
    assert_true: function (x, desc) { check(!!x, desc === undefined ? "expected true, got " + fmt(x) : desc); },
    assert_false: function (x, desc) { check(!x, desc === undefined ? "expected false, got " + fmt(x) : desc); },
    assert_equals: function (a, e, desc) {
      var same = a === e || (a !== a && e !== e);
      if (!same) throw new Error((desc === undefined ? "" : String(desc) + ": ") + "expected " + fmt(e) + " but got " + fmt(a));
    },
    assert_not_equals: function (a, e, desc) {
      var same = a === e || (a !== a && e !== e);
      if (same) throw new Error((desc === undefined ? "" : String(desc) + ": ") + "values must differ: " + fmt(a));
    },
    assert_in_array: function (x, arr, desc) {
      var found = false;
      for (var i = 0; i < arr.length; i++) {
        if (arr[i] === x || (arr[i] !== arr[i] && x !== x)) { found = true; break; }
      }
      if (!found) throw new Error((desc === undefined ? "" : String(desc) + ": ") + fmt(x) + " not in " + fmt(arr));
    },
    assert_throws_js: function (ctor, fn, desc) {
      var threw = null;
      try { fn(); } catch (e) { threw = e; }
      if (threw === null) throw new Error(desc === undefined ? "expected throw, nothing thrown" : String(desc));
      var prefix = desc === undefined ? "" : String(desc) + ": ";
      if (typeof ctor === "string") {
        var gotName = threw && threw.name !== undefined ? String(threw.name) : "?";
        if (gotName !== ctor) throw new Error(prefix + "expected " + ctor + ", got " + gotName);
        return;
      }
      if (ctor && typeof ctor === "object" && ctor.name !== undefined) {
        var wantName = String(ctor.name);
        var gotName2 = threw && threw.name !== undefined ? String(threw.name) : "?";
        if (gotName2 !== wantName) throw new Error(prefix + "expected " + wantName + ", got " + gotName2);
        return;
      }
      if (ctor && !(threw instanceof ctor)) {
        var want = ctor.name || "?";
        var got = threw && threw.constructor ? threw.constructor.name || "?" : "?";
        throw new Error(prefix + "expected " + want + ", got " + got);
      }
    },
    assert_unreached: function (desc) { throw new Error(desc === undefined ? "reached unreachable code" : String(desc)); }
  };

  if (typeof global.test !== "function") global.test = shimTest;
  if (typeof global.async_test !== "function") global.async_test = shimAsyncTest;
  if (typeof global.promise_test !== "function") global.promise_test = shimPromiseTest;
  if (typeof global.setup !== "function") global.setup = shimSetup;
  for (var k in asserts) {
    if (typeof global[k] !== "function") global[k] = asserts[k];
  }

  function realComplete(tests, status) {
    var list = [];
    for (var i = 0; i < tests.length; i++) {
      var t = tests[i];
      var st = SUBTEST_NAMES[t.status] || ("UNKNOWN(" + t.status + ")");
      list.push({
        name: String(t.name),
        status: st,
        message: t.message === undefined || t.message === null ? null : String(t.message)
      });
    }
    var h = HARNESS_NAMES[status.status] || ("UNKNOWN(" + status.status + ")");
    real = {
      harness: h,
      message: status.message === undefined || status.message === null ? null : String(status.message),
      subtests: list
    };
  }

  function shimAddCallback(cb) {
    if (typeof cb === "function") callbacks.push(cb);
  }

  Object.defineProperty(global, "add_completion_callback", {
    configurable: true,
    get: function () { return shimAddCallback; },
    set: function (fn) {
      delete global.add_completion_callback;
      global.add_completion_callback = fn;
      if (!realSeen && typeof fn === "function") {
        realSeen = true;
        try { fn(realComplete); } catch (e) {}
      }
    }
  });

  Object.defineProperty(global, "__bl_wpt_finish", {
    value: function () {
      if (global.__bl_wpt_done) return global.__bl_wpt_json;
      try {
        var desc = Object.getOwnPropertyDescriptor(global, "add_completion_callback");
        var fn = (desc && desc.value) || global.add_completion_callback;
        if (!realSeen && typeof fn === "function" && fn !== shimAddCallback) {
          realSeen = true;
          try { fn(realComplete); } catch (e) {}
        }
      } catch (e) {}
      for (var i = 0; i < finalizers.length; i++) {
        try { finalizers[i](); } catch (e) {}
      }
      var out;
      if (real !== null) {
        out = real;
      } else {
        out = {
          harness: subtests.length > 0 ? "OK" : "ERROR",
          message: subtests.length > 0 ? null : "no subtests recorded (testharness.js missing or no test() calls)",
          subtests: subtests
        };
      }
      var numeric = { OK: 0, ERROR: 1, TIMEOUT: 2, PRECONDITION_FAILED: 3 };
      var statusArg = { status: numeric[out.harness] === undefined ? 1 : numeric[out.harness], message: out.message };
      for (var j = 0; j < callbacks.length; j++) {
        try { callbacks[j](out.subtests, statusArg); } catch (e) {}
      }
      global.__bl_wpt_json = JSON.stringify(out);
      global.__bl_wpt_done = true;
      return global.__bl_wpt_json;
    },
    writable: false, enumerable: false, configurable: false
  });
})(typeof globalThis !== "undefined" ? globalThis : this);
`

// wptSubtest is one collected subtest result.
type wptSubtest struct {
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	Message *string `json:"message"`
}

// wptResult is the finish payload: the scorer contract.
type wptResult struct {
	Harness  string       `json:"harness"`
	Message  *string      `json:"message"`
	Subtests []wptSubtest `json:"subtests"`
}

// wptSquash folds a result name or message to one text line: control
// characters become spaces and whitespace runs collapse.
func wptSquash(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// loadWPTPage fetches a test page: http(s) URLs go through the net
// layer like the main pipeline, anything else is a local file.
func loadWPTPage(source string) (html, pageURL string, err error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		text, finalURL, ferr := blnet.FetchTextResult(source)
		if ferr != nil {
			return "", "", ferr
		}
		return text, finalURL, nil
	}
	raw, rerr := os.ReadFile(source)
	if rerr != nil {
		return "", "", rerr
	}
	if !utf8.Valid(raw) {
		return "", "", fmt.Errorf("stream did not contain valid UTF-8")
	}
	abs, aerr := filepath.Abs(source)
	if aerr != nil {
		abs = source
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		abs = resolved
	}
	return string(raw), "file://" + abs, nil
}

// runWPT executes one test page in WPT mode, printing the result lines
// to stdout and returning the process exit code.
func runWPT(source string, timeoutMs int, stdout, stderr io.Writer) int {
	html, pageURL, lerr := loadWPTPage(source)
	if lerr != nil {
		if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
			fmt.Fprintf(stderr, "browser-lite: %s: %s\n", source, lerr)
		} else {
			fmt.Fprintf(stderr, "browser-lite: %s: %s\n", source, osError(lerr))
		}
		return 1
	}

	doc := dom.Parse(html)
	rt, err := js.New()
	if err != nil {
		fmt.Fprintf(stderr, "browser-lite: %s\n", err)
		return 1
	}
	defer rt.Close()
	rt.SetTimeLimit(time.Duration(timeoutMs) * time.Millisecond)
	if err := rt.Bind("", pageURL); err != nil {
		fmt.Fprintf(stderr, "browser-lite: js location: %s\n", err)
	}
	if err := rt.BindDOM(doc); err != nil {
		fmt.Fprintf(stderr, "browser-lite: js dom: %s\n", err)
	}
	if _, err := rt.Eval(wptHook, "browser-lite:wpt-hook"); err != nil {
		fmt.Fprintf(stderr, "browser-lite: wpt: harness produced no results (hook failed: %s)\n", err)
		return 2
	}
	runPageScripts(doc, rt, stderr)

	v, err := rt.Eval(`globalThis.__bl_wpt_finish()`, "browser-lite:wpt-finish")
	if err != nil || v.Kind != js.KindString || v.S == "" {
		fmt.Fprintf(stderr, "browser-lite: wpt: harness produced no results (finish failed)\n")
		return 2
	}
	var res wptResult
	if jerr := json.Unmarshal([]byte(v.S), &res); jerr != nil {
		fmt.Fprintf(stderr, "browser-lite: wpt: harness produced no results (bad JSON: %s)\n", jerr)
		return 2
	}
	if res.Subtests == nil {
		res.Subtests = []wptSubtest{}
	}

	var harnessMsg string
	if res.Message != nil {
		harnessMsg = *res.Message
	}
	if harnessMsg != "" {
		fmt.Fprintf(stdout, "WPT HARNESS %s | %s\n", wptSquash(res.Harness), wptSquash(harnessMsg))
	} else {
		fmt.Fprintf(stdout, "WPT HARNESS %s\n", wptSquash(res.Harness))
	}
	counts := map[string]int{}
	for _, s := range res.Subtests {
		counts[s.Status]++
		msg := ""
		if s.Message != nil {
			msg = *s.Message
		}
		if msg != "" {
			fmt.Fprintf(stdout, "WPT %s %s | %s\n", s.Status, wptSquash(s.Name), wptSquash(msg))
		} else {
			fmt.Fprintf(stdout, "WPT %s %s\n", s.Status, wptSquash(s.Name))
		}
	}
	fmt.Fprintf(stdout, "WPT SUMMARY total=%d pass=%d fail=%d timeout=%d notrun=%d precondition_failed=%d\n",
		len(res.Subtests), counts["PASS"], counts["FAIL"], counts["TIMEOUT"], counts["NOTRUN"], counts["PRECONDITION_FAILED"])
	fmt.Fprintf(stdout, "WPT JSON %s\n", v.S)

	if (res.Harness == "OK" || res.Harness == "PRECONDITION_FAILED") &&
		counts["PASS"] == len(res.Subtests) {
		return 0
	}
	return 1
}
