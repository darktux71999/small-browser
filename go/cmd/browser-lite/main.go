// Command browser-lite is the Go engine's CLI, mirroring src/main.rs:
// --dump prints the DOM tree then the layout boxes plus the title line,
// --png renders the full document and reports the wrote line, and anything
// else errors exactly like the oracle does.
package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"browser-lite/css"
	"browser-lite/dom"
	"browser-lite/js"
	"browser-lite/layout"
	blnet "browser-lite/net"
	"browser-lite/paint"
	"browser-lite/shell"
	"strconv"
)

// viewportWidth is the layout width, also the viewport that @media resolves
// against: the document is laid out once for the window that shows it.
const viewportWidth = 1000.0

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI and returns the process exit code, writing the
// oracle's user-visible texts byte for byte.
func run(args []string, stdout, stderr io.Writer) int {
	var pngPath *string
	var fileArg *string
	var frames *uint32
	var wptURL *string
	wptTimeoutMs := 15000
	dump := false

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--png":
			i++
			if i < len(args) {
				v := args[i]
				pngPath = &v
			}
		case "--dump":
			dump = true
		case "--frames":
			i++
			if i < len(args) {
				if n, err := strconv.ParseUint(args[i], 10, 32); err == nil {
					v := uint32(n)
					frames = &v
				}
			}
		case "--wpt":
			i++
			if i < len(args) {
				v := args[i]
				wptURL = &v
			}
		case "--wpt-timeout-ms":
			i++
			if i < len(args) {
				n, err := strconv.Atoi(args[i])
				if err != nil || n <= 0 {
					fmt.Fprintf(stderr, "browser-lite: invalid --wpt-timeout-ms %q\n", args[i])
					return 2
				}
				wptTimeoutMs = n
			}
		case "--help", "-h":
		default:
			v := args[i]
			fileArg = &v
		}
		i++
	}

	if wptURL != nil {
		return runWPT(*wptURL, wptTimeoutMs, stdout, stderr)
	}

	if fileArg == nil {
		fmt.Fprintln(stderr, "usage: browser-lite [--dump] [--png out.png] [--wpt url] <file.html|http(s)://url>")
		return 2
	}

	source := *fileArg
	var html, base, pageURL string
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		text, finalURL, err := blnet.FetchTextResult(source)
		if err != nil {
			fmt.Fprintf(stderr, "browser-lite: %s: %s\n", source, err)
			return 1
		}
		html, base, pageURL = text, finalURL, finalURL
	} else {
		raw, err := os.ReadFile(source)
		if err != nil {
			fmt.Fprintf(stderr, "browser-lite: %s: %s\n", source, osError(err))
			return 1
		}
		if !utf8.Valid(raw) {
			fmt.Fprintf(stderr, "browser-lite: %s: stream did not contain valid UTF-8\n", source)
			return 1
		}
		html = string(raw)
		abs, err := filepath.Abs(source)
		if err != nil {
			abs = source
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		base = filepath.Dir(abs)
		pageURL = "file://" + abs
	}

	doc := dom.Parse(html)
	if dump {
		dumpDOM(stdout, doc)
	}

	loader := newLoader(stderr)
	sheets := loadSheets(doc, base, loader)
	inputs := make([]css.Sheet, len(sheets))
	for i, sheet := range sheets {
		inputs[i] = css.Sheet{URL: sheet.url, CSS: sheet.stripped}
	}
	// Inline style="" declarations resolve their url() against the document
	// base, like the stylesheets do (leading-fidelity inline-style fix).
	styles, _, err := css.ComputeDocument(domIndex{doc}, inputs, viewportWidth, base)
	if err != nil {
		fmt.Fprintf(stderr, "browser-lite: %s\n", err)
		return 1
	}
	loader.registerTopFaces(sheets)

	title := ""
	if id, ok := doc.QuerySelector("title"); ok {
		title = doc.TextContent(id)
	}

	// Page scripts run after style resolution and before layout, like the
	// oracle (src/main.rs:82-109): each failure is reported and execution
	// continues. Title is re-read after scripts (last-good fallback lives
	// in js.Title), so --dump's `title:` line and the shell see mutations.
	rt, err := js.New()
	if err != nil {
		var jserr *js.JSError
		msg := err.Error()
		if errors.As(err, &jserr) {
			msg = jserr.Msg
		}
		fmt.Fprintf(stderr, "browser-lite: %s\n", msg)
		return 1
	}
	defer rt.Close()
	if err := rt.Bind(title, pageURL); err != nil {
		fmt.Fprintf(stderr, "browser-lite: js location: %s\n", err)
	}
	// T1 (dom-apis-track S1): the DOM surface over the live tree. A BindDOM
	// failure degrades to the oracle surface (scripts throw the oracle
	// TypeErrors); it never stops the remaining scripts.
	if err := rt.BindDOM(doc); err != nil {
		fmt.Fprintf(stderr, "browser-lite: js dom: %s\n", err)
	}
	runPageScripts(doc, rt, stderr)
	title = rt.Title()
	// T1 re-render: scripts run after style resolution in oracle order
	// (src/main.rs:82-109), which is safe for the oracle because its
	// scripts cannot touch the DOM. Ours now can (classes, text, tree),
	// so recompute styles on the mutated tree before layout — but only
	// when a script actually mutated something (DomDirty).
	if rt.DomDirty() {
		var recomputeErr error
		styles, _, recomputeErr = css.ComputeDocument(domIndex{doc}, inputs, viewportWidth, base)
		if recomputeErr != nil {
			fmt.Fprintf(stderr, "browser-lite: %s\n", recomputeErr)
			return 1
		}
	}

	images := layout.LoadImages(doc, styles, base)
	l := layout.RenderWithImages(doc, styles, viewportWidth, images)

	switch {
	case pngPath != nil:
		if err := paint.RenderPNG(l, *pngPath); err != nil {
			fmt.Fprintf(stderr, "browser-lite: paint failed: %s\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote %s (%sx%s)\n",
			*pngPath, minimalFloat(l.Width), minimalFloat(math.Ceil(l.Height)))
	case dump:
		dumpLayout(stdout, l)
	default:
		// Interactive path: the shell opens the fixed 1000x760 viewport on
		// the full-document layout and re-paints it on scroll.
		// --png/--dump above keep rendering the whole document.
		if err := shell.Run(l, title, frames); err != nil {
			if runErr, ok := err.(*shell.RunError); ok {
				fmt.Fprintln(stderr, runErr.Diagnostic())
			} else {
				fmt.Fprintf(stderr, "browser-lite: window failed: %s\n", err)
			}
			return 1
		}
	}

	if dump {
		fmt.Fprintf(stdout, "title: %s\n", title)
	}
	return 0
}
