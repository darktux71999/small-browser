package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"browser-lite/css"
	"browser-lite/dom"
	blnet "browser-lite/net"
	"browser-lite/paint"
)

// The transfer profile for stylesheets and fonts, from src/css_libcss.rs.
const (
	maxSheetBytes = 2 * 1024 * 1024
	sheetTimeout  = 8 * time.Second
	maxFontBytes  = 10 * 1024 * 1024
	fontTimeout   = 20 * time.Second
)

// loader fetches each subresource URL once per run and warns like the
// oracle's Loader: one diagnostic per broken resource, printed immediately.
type loader struct {
	stderr io.Writer
	sheets map[string]sheetResult
	fonts  map[string]fontResult
	warned map[string]bool
}

type sheetResult struct {
	css string
	ok  bool
}

type fontResult struct {
	data []byte
	ok   bool
}

func newLoader(stderr io.Writer) *loader {
	return &loader{
		stderr: stderr,
		sheets: map[string]sheetResult{},
		fonts:  map[string]fontResult{},
		warned: map[string]bool{},
	}
}

func (l *loader) warn(format string, args ...any) {
	fmt.Fprintf(l.stderr, "browser-lite: "+format+"\n", args...)
}

// loadedSheet is one author stylesheet ready for the cascade: the URL its
// relative references resolve against, the expanded CSS (imports spliced in,
// @font-face intact) for font registration, and the stripped CSS (faces
// removed, like the rewriter hands libcss) for style computation.
type loadedSheet struct {
	url      string
	expanded string
	stripped string
}

// loadSheets collects the document's author stylesheets in document order:
// inline <style> blocks plus fetched <link rel="stylesheet"> sheets, with
// @import chains expanded depth-first. Warnings print in oracle order:
// link failures, then per-sheet import failures, then font registration.
func loadSheets(doc *dom.Document, base string, l *loader) []loadedSheet {
	var out []loadedSheet
	for id := range doc.Nodes {
		node := &doc.Nodes[id]
		if node.IsText {
			continue
		}
		switch {
		case strings.EqualFold(node.Tag, "style"):
			text := doc.TextContent(dom.NodeID(id))
			if strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, expandSheet(base, text, l))
		case strings.EqualFold(node.Tag, "link"):
			rel, _ := node.Attr("rel")
			sheet := false
			for _, token := range strings.Fields(rel) {
				if strings.EqualFold(token, "stylesheet") {
					sheet = true
					break
				}
			}
			if !sheet {
				continue
			}
			href, ok := node.Attr("href")
			if !ok {
				continue
			}
			if media, ok := node.Attr("media"); ok && !mediaAppliesToScreen(media) {
				continue
			}
			url, ok := blnet.JoinURL(base, href)
			if !ok {
				l.warn("skipping stylesheet %s: cannot resolve it against %s", rustDebug(href), rustDebug(base))
				continue
			}
			if cssText, ok := l.loadSheet(url); ok {
				out = append(out, expandSheet(url, cssText, l))
			}
		}
	}
	return out
}

// expandSheet splices a sheet's @import chain into the text and strips the
// @font-face blocks libcss must never see, registering the imported faces
// on the way (the oracle registers them while parsing).
func expandSheet(url, cssText string, l *loader) loadedSheet {
	active := map[string]bool{url: true}
	expanded := expandImports(cssText, url, l, active)
	return loadedSheet{url: url, expanded: expanded, stripped: stripFontFaces(expanded)}
}

// registerTopFaces registers one sheet's own @font-face blocks after style
// computation, like the oracle's post-selection loop.
func (l *loader) registerTopFaces(sheets []loadedSheet) {
	for _, sheet := range sheets {
		registerFaces(sheet.expanded, sheet.url, l)
	}
}

// loadSheet fetches a stylesheet URL once per run, warning on failure.
func (l *loader) loadSheet(url string) (string, bool) {
	if cached, ok := l.sheets[url]; ok {
		return cached.css, cached.ok
	}
	cssText, err := fetchSheet(url)
	if err != nil {
		l.warn("skipping stylesheet %s: %s", url, err)
		l.sheets[url] = sheetResult{ok: false}
		return "", false
	}
	l.sheets[url] = sheetResult{css: cssText, ok: true}
	return cssText, true
}

// loadFont fetches a font URL once per run, warning on failure.
func (l *loader) loadFont(url string) ([]byte, bool) {
	if cached, ok := l.fonts[url]; ok {
		return cached.data, cached.ok
	}
	data, err := fetchFontBytes(url)
	if err != nil {
		l.warn("skipping font %s: %s", url, err)
		l.fonts[url] = fontResult{ok: false}
		return nil, false
	}
	l.fonts[url] = fontResult{data: data, ok: true}
	return data, true
}

// fetchSheet fetches one stylesheet body: http(s) through the shared capped
// client with charset decoding, local files with the reject-not-truncate
// rule.
func fetchSheet(url string) (string, error) {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		body, contentType, err := blnet.FetchBytesLimited(url, maxSheetBytes, sheetTimeout)
		if err != nil {
			return "", err
		}
		return blnet.DecodeBody(body, contentType)
	}
	path, ok := sheetLocalPath(url)
	if !ok {
		return "", errString(fmt.Sprintf("unsupported stylesheet URL %s", rustDebug(url)))
	}
	body, err := readCappedBytes(path, maxSheetBytes)
	if err != nil {
		return "", err
	}
	if utf8.Valid(body) {
		return string(body), nil
	}
	return strings.ToValidUTF8(string(body), "\uFFFD"), nil
}

// fetchFontBytes fetches one @font-face body as raw bytes, honouring the
// same caps: http(s) through the shared capped client, local files with the
// reject-not-truncate rule of the stylesheet reader.
func fetchFontBytes(url string) ([]byte, error) {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		body, _, err := blnet.FetchBytesLimited(url, maxFontBytes, fontTimeout)
		if err != nil {
			return nil, err
		}
		return body, nil
	}
	path, ok := sheetLocalPath(url)
	if !ok {
		return nil, errString(fmt.Sprintf("unsupported font URL %s", rustDebug(url)))
	}
	return readCappedBytes(path, maxFontBytes)
}

// sheetLocalPath is the filesystem path behind a stylesheet or font URL, if
// there is one: file: URLs unwrap to their path, any other scheme has no
// local form, and a non-URL is already a plain path.
func sheetLocalPath(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err == nil && u.Scheme != "" {
		if u.Scheme != "file" {
			return "", false
		}
		if u.Host != "" && u.Host != "localhost" {
			return "", false
		}
		if unescaped, err := url.PathUnescape(u.Path); err == nil {
			return unescaped, true
		}
		return u.Path, true
	}
	return rawURL, true
}

// readCappedBytes reads a file, rejecting (not truncating) anything over
// maxBytes, with the oracle's messages.
func readCappedBytes(path string, maxBytes int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, osError(err)
	}
	if info.IsDir() {
		return nil, errString("is a directory")
	}
	if info.Size() > int64(maxBytes) {
		return nil, errString(fmt.Sprintf("%d bytes exceeds the %d byte limit", info.Size(), maxBytes))
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, osError(err)
	}
	if len(body) > maxBytes {
		return nil, errString(fmt.Sprintf("body exceeds the %d byte limit", maxBytes))
	}
	return body, nil
}

// registerFaces fetches and registers the @font-face blocks of one sheet,
// warning exactly like the oracle's register_font_faces.
func registerFaces(cssText, sheetURL string, l *loader) {
	for _, face := range css.FontFaceDeclarations(cssText) {
		src := css.SupportedFontSrc(face.Src)
		if src == "" {
			if !l.warned[face.Family] {
				l.warned[face.Family] = true
				l.warn("skipping @font-face for family %s%s: no supported src (woff/woff2 and local() are not supported here)",
					rustDebug(face.Family), css.FaceDetail(face))
			}
			continue
		}
		url, ok := blnet.JoinURL(sheetURL, src)
		if !ok {
			l.warn("skipping @font-face for family %s: cannot resolve %s against %s",
				rustDebug(face.Family), rustDebug(src), rustDebug(sheetURL))
			continue
		}
		data, ok := l.loadFont(url)
		if !ok {
			continue
		}
		path, ok := paint.CacheFontFile(url, data)
		if !ok {
			l.warn("skipping font %s: cannot write the font cache", url)
			continue
		}
		if !paint.RegisterFontFile(path) {
			l.warn("skipping font %s: fontconfig rejected %s", url, path)
		}
	}
}

// mediaAppliesToScreen reports whether a <link> media attribute (or an
// @import media list) targets a screen render: a list made up exclusively
// of other media types contributes nothing.
func mediaAppliesToScreen(media string) bool {
	if strings.TrimSpace(media) == "" {
		return true
	}
	for _, query := range strings.Split(media, ",") {
		words := strings.Fields(query)
		if len(words) == 0 {
			return true
		}
		negated := false
		kind := words[0]
		if strings.EqualFold(words[0], "not") {
			negated = true
			kind = ""
			if len(words) > 1 {
				kind = words[1]
			}
		} else if strings.EqualFold(words[0], "only") {
			kind = ""
			if len(words) > 1 {
				kind = words[1]
			}
		}
		matches := kind == "" ||
			strings.HasPrefix(kind, "(") ||
			strings.EqualFold(kind, "all") ||
			strings.EqualFold(kind, "screen")
		if negated != matches {
			return true
		}
	}
	return false
}

// errString is an error with fixed text, for the oracle's literal messages.
type errString string

func (e errString) Error() string { return string(e) }

// osError renders an OS failure the way the Rust oracle's io::Error
// displays ("No such file or directory (os error 2)"), because the text
// reaches the user through the browser-lite: warnings on stderr.
func osError(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errString(fmt.Sprintf("%s (os error %d)", upperASCII(errno.Error()), uint(errno)))
	}
	return err
}

// upperASCII upper-cases the first ASCII letter, like strerror's wording.
func upperASCII(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if 'a' <= b[0] && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}
