package js

import (
	"fmt"
	"net/url"
	"strconv"
)

// Web bindings: the oracle's exact JS surface (src/js.rs is the spec — the
// frozen Rust runtime exposes nothing beyond this, and neither do we).
//
//   - `console` (log/error/warn `[js] ...` lines) comes from New (T1).
//   - `document` holds only a mutable `title`.
//   - `navigator` holds only `userAgent`.
//   - `window` is the global object itself (`window === globalThis`).
//   - `location` is a read-only view (href/protocol/host/hostname/pathname,
//     plus toString) over state seeded by SetURL (`about:blank` by default).
//   - `addEventListener`/`removeEventListener` register `DOMContentLoaded`
//     and `load` handlers; EvalPage fires them after every script (each
//     handler runs once per event) exactly like the oracle's eval.
//
// Members the fixtures name but the oracle never implemented
// (getElementById, querySelector(All), createElement, innerHTML, cookie,
// timers, per-node events, document.body/activeElement) stay missing from
// THIS surface too, so the same engine-native exceptions keep the
// differential identical for runtimes without BindDOM. The S1 DOM track
// (dom-apis-track, go/js/dom.go) adds getElementById, querySelector(All),
// classList, className, innerHTML/textContent, attributes, createElement,
// appendChild/removeChild, closest/matches and document.body on top via
// BindDOM — a deliberate divergence recorded in KNOWN_DIFFS.md.
//
// A Runtime carries web bindings only after Bind, which mirrors
// JsRuntime::new: install document/navigator, run the bootstrap, seed the URL.

// UserAgent is the script-visible identity, kept in sync with net's
// User-Agent (src/js.rs: "Kept in sync with net::USER_AGENT ...").
const UserAgent = "browser-lite/0.1"

// bootstrap is the oracle's BOOTSTRAP (src/js.rs) verbatim: the location
// state, the read-only location view, the listener registry with
// __bl_dispatch, and the window alias.
const bootstrap = `(function () {
  "use strict";
  const g = globalThis;

  // Mutable state behind the read-only ` + "`location`" + ` view; Rust writes it.
  const state = { href: "", protocol: "", host: "", hostname: "", pathname: "" };
  Object.defineProperty(g, "__bl_location", {
    value: state, writable: false, enumerable: false, configurable: false,
  });

  const location = {};
  for (const key of ["href", "protocol", "host", "hostname", "pathname"]) {
    Object.defineProperty(location, key, {
      get: function () { return g.__bl_location[key]; },
      enumerable: true,
      configurable: false,
    });
  }
  Object.defineProperty(location, "toString", {
    value: function () { return g.__bl_location.href; },
    enumerable: false,
  });
  Object.defineProperty(g, "location", {
    value: location, writable: false, enumerable: true, configurable: false,
  });

  const listeners = new Map();
  function register(type, fn) {
    if (typeof fn !== "function" && !(fn && typeof fn.handleEvent === "function")) return;
    const key = String(type);
    let list = listeners.get(key);
    if (!list) {
      list = [];
      listeners.set(key, list);
    }
    if (list.indexOf(fn) === -1) list.push(fn);
  }
  function unregister(type, fn) {
    const list = listeners.get(String(type));
    if (!list) return;
    const at = list.indexOf(fn);
    if (at !== -1) list.splice(at, 1);
  }
  Object.defineProperty(g, "addEventListener", {
    value: function (type, fn) { register(type, fn); },
    writable: true, enumerable: true, configurable: true,
  });
  Object.defineProperty(g, "removeEventListener", {
    value: function (type, fn) { unregister(type, fn); },
    writable: true, enumerable: true, configurable: true,
  });

  function report(err) {
    try {
      console.error("uncaught error in event handler: " + (err && err.message ? err.message : String(err)));
    } catch (_) {}
  }

  // Fires and consumes the handlers registered for one lifecycle event.
  Object.defineProperty(g, "__bl_dispatch", {
    value: function (type) {
      const key = String(type);
      const list = listeners.get(key);
      if (!list || list.length === 0) return 0;
      const handlers = list.slice();
      list.length = 0;
      const event = { type: key, target: g, currentTarget: g, bubbles: false };
      for (const fn of handlers) {
        try {
          if (typeof fn === "function") fn.call(g, event);
          else fn.handleEvent(event);
        } catch (err) {
          report(err);
        }
      }
      return handlers.length;
    },
    writable: false, enumerable: false, configurable: false,
  });

  // ` + "`window`" + ` is the global alias: ` + "`window.foo`" + ` and bare ` + "`foo`" + ` are one binding.
  Object.defineProperty(g, "window", {
    value: g, writable: false, enumerable: true, configurable: false,
  });
})();`

// jsQuote renders s as a double-quoted JS string literal (JSON-compatible
// for the sane range: Go escapes \" \\ \n \xHH, all of which JS parses).
func jsQuote(s string) string {
	return strconv.Quote(s)
}

// Bind installs the oracle's web surface on a fresh runtime and seeds the
// document title and URL. Call once per runtime, before any page script.
func (r *Runtime) Bind(title, rawURL string) error {
	head := `globalThis.document = {title: ` + jsQuote(title) +
		`}; globalThis.navigator = {userAgent: ` + jsQuote(UserAgent) + `};`
	if _, err := r.Eval(head, "browser-lite:document"); err != nil {
		return err
	}
	if _, err := r.Eval(bootstrap, "browser-lite:bootstrap"); err != nil {
		return err
	}
	r.mu.Lock()
	r.webTitle = title
	r.mu.Unlock()
	return r.SetURL(rawURL)
}

// SetURL exposes rawURL through `location` (absolute URLs only, parsed like
// the oracle's `url` crate; on error the previous location stays untouched).
func (r *Runtime) SetURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || !u.IsAbs() {
		return fmt.Errorf("js: invalid URL %q", rawURL)
	}
	hostname := u.Hostname()
	host := hostname
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	pathname := u.Path
	if u.Opaque != "" {
		pathname = u.Opaque // e.g. about:blank -> "blank" (url crate path())
	} else if pathname == "" {
		pathname = "/"
	}
	set := func(name, value string) error {
		_, err := r.Eval(`globalThis.__bl_location.`+name+` = `+jsQuote(value)+`;`, "browser-lite:location")
		return err
	}
	for _, member := range [][2]string{
		{"href", u.String()},
		{"protocol", u.Scheme + ":"},
		{"host", host},
		{"hostname", hostname},
		{"pathname", pathname},
	} {
		if err := set(member[0], member[1]); err != nil {
			return err
		}
	}
	return nil
}

// Title reports the current document.title, mirroring JsRuntime::title: the
// property is re-read (and coerced) on every call, falling back to the last
// good value when the page broke document.
func (r *Runtime) Title() string {
	v, err := r.Eval(`String(document.title)`, "browser-lite:title")
	if err != nil || v.Kind != KindString {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.webTitle
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.webTitle = v.S
	return v.S
}

// EvalPage runs a page script and then fires the lifecycle events for the
// handlers registered so far, mirroring JsRuntime::eval: page events never
// mask the script's own error.
func (r *Runtime) EvalPage(src, filename string) error {
	_, rerr := r.Eval(src, filename)
	derr := r.DispatchPageEvents()
	if rerr != nil {
		return rerr
	}
	return derr
}

// DispatchPageEvents fires DOMContentLoaded then load for the handlers
// registered so far (each runs once — __bl_dispatch consumes the lists).
func (r *Runtime) DispatchPageEvents() error {
	if _, err := r.Eval(`__bl_dispatch('DOMContentLoaded');`, "browser-lite:events"); err != nil {
		return err
	}
	_, err := r.Eval(`__bl_dispatch('load');`, "browser-lite:events")
	return err
}
