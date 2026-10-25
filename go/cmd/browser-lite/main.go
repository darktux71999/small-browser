// Command browser-lite is the Go engine's CLI, mirroring src/main.rs:
// --dump prints the DOM tree then the layout boxes plus the title line,
// --png renders the full document and reports the wrote line, and anything
// else errors exactly like the oracle does.
package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"browser-lite/css"
	"browser-lite/dom"
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
		case "--help", "-h":
		default:
			v := args[i]
			fileArg = &v
		}
		i++
	}

	if fileArg == nil {
		fmt.Fprintln(stderr, "usage: browser-lite [--dump] [--png out.png] <file.html|http(s)://url>")
		return 2
	}

	source := *fileArg
	var html, base string
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		text, finalURL, err := blnet.FetchTextResult(source)
		if err != nil {
			fmt.Fprintf(stderr, "browser-lite: %s: %s\n", source, err)
			return 1
		}
		html, base = text, finalURL
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
