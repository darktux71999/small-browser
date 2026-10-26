package main

// Script pipeline (S3): collect and run page scripts with the oracle's exact
// observable behavior (src/main.rs:99-107 is the spec).
//
// The oracle queries all <script> elements in document order, reads each
// one's text content, skips it when blank, and evals the rest with the
// filename "inline-{n}.js" where n counts every script element (skipped
// ones included). It never looks at `src`: external scripts are not fetched
// and emit no error line. Each failure prints
// "browser-lite: js error: <msg>" to stderr and execution continues with
// the next script. Lifecycle events fire inside every eval (see
// js.EvalPage), so nothing extra is needed here.

import (
	"fmt"
	"io"
	"strings"

	"browser-lite/dom"
	"browser-lite/js"
)

// runPageScripts evals the page's scripts in document order through rt,
// reporting each failure to stderr in the oracle's exact format. Scripts
// that fail do not stop the scripts after them.
func runPageScripts(doc *dom.Document, rt *js.Runtime, stderr io.Writer) {
	for n, id := range doc.QuerySelectorAll("script") {
		source := doc.TextContent(id)
		if strings.TrimSpace(source) == "" {
			continue
		}
		if err := rt.EvalPage(source, fmt.Sprintf("inline-%d.js", n)); err != nil {
			fmt.Fprintf(stderr, "%s\n", err)
		}
	}
}
