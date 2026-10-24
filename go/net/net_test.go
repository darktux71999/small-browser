//! S3 — networking: capped GET with redirects, charset decoding and URL
//! joining, ported from the Rust oracle (src/net.rs and the private join_url
//! of src/img.rs / src/css_libcss.rs). The HTTP exchanges use httptest so the
//! tests stay hermetic and deterministic.

package net

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fetching
// ---------------------------------------------------------------------------

func TestFetchReturnsTheBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<p>hi</p>")
	}))
	defer server.Close()

	body, err := Fetch(server.URL + "/page.html")
	if err != nil {
		t.Fatalf("fetch must succeed: %v", err)
	}
	if string(body) != "<p>hi</p>" {
		t.Fatalf("body = %q, want %q", body, "<p>hi</p>")
	}
}

func TestFetchSendsTheEngineUserAgent(t *testing.T) {
	agent := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent <- r.UserAgent()
	}))
	defer server.Close()

	if _, err := Fetch(server.URL); err != nil {
		t.Fatalf("fetch must succeed: %v", err)
	}
	if got := <-agent; got != "browser-lite/0.1" {
		t.Fatalf("User-Agent = %q, want %q", got, "browser-lite/0.1")
	}
}

func TestStatus400OrMoreIsAnErrorCarryingTheStatusLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	url := server.URL + "/missing.html"

	_, err := FetchResult(url)
	if err == nil {
		t.Fatal("404 must fail")
	}
	// The oracle formats "{url}: {status_line}" (src/net.rs).
	want := url + ": HTTP/1.1 404 Not Found"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestFetchResultFollowsRedirectsToTheFinalURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start.html":
			http.Redirect(w, r, "/final.html", http.StatusFound)
		case "/final.html":
			io.WriteString(w, "<p>final</p>")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	url := server.URL + "/start.html"

	result, err := FetchResult(url)
	if err != nil {
		t.Fatalf("fetch must succeed: %v", err)
	}
	if result.FinalURL != server.URL+"/final.html" {
		t.Fatalf("final URL = %q, want %q", result.FinalURL, server.URL+"/final.html")
	}
	if string(result.Bytes) != "<p>final</p>" {
		t.Fatalf("body = %q, want %q", result.Bytes, "<p>final</p>")
	}
}

func TestRedirectsAreFollowedUpToTheOracleCap(t *testing.T) {
	// curl's max_redirections(10) follows 10 hops; a page must survive a
	// chain of exactly 10 redirects and fail on the 11th.
	chain := func(redirects int) (*httptest.Server, string) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var n int
			fmt.Sscanf(r.URL.Path, "/hop/%d", &n)
			if n >= redirects {
				io.WriteString(w, "end")
				return
			}
			http.Redirect(w, r, fmt.Sprintf("/hop/%d", n+1), http.StatusFound)
		}))
		return server, server.URL + "/hop/0"
	}

	server, url := chain(10)
	result, err := FetchResult(url)
	server.Close()
	if err != nil {
		t.Fatalf("a chain of 10 redirects must succeed: %v", err)
	}
	if string(result.Bytes) != "end" {
		t.Fatalf("body = %q, want %q", result.Bytes, "end")
	}

	server, url = chain(11)
	_, err = FetchResult(url)
	server.Close()
	if err == nil {
		t.Fatal("an 11th redirect must fail")
	}
	if !strings.Contains(err.Error(), "request failed for "+url) {
		t.Fatalf("error = %q, it must name the request", err.Error())
	}
}

func TestRedirectLoopFailsAfterTheCap(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/loop.html", http.StatusFound)
	}))
	defer server.Close()
	url := server.URL + "/loop.html"

	_, err := FetchResult(url)
	if err == nil {
		t.Fatal("a redirect loop must fail")
	}
	if !strings.Contains(err.Error(), "request failed for "+url) {
		t.Fatalf("error = %q, it must name the request", err.Error())
	}
}

func TestFetchBytesLimitedRejectsAnOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", 100))
	}))
	defer server.Close()
	url := server.URL + "/big.bin"

	_, _, err := FetchBytesLimited(url, 10, time.Second)
	if err == nil {
		t.Fatal("a body larger than the cap must fail, not truncate")
	}
	// The oracle formats "{url}: response larger than {max} bytes" (src/net.rs).
	want := url + ": response larger than 10 bytes"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestFetchBytesLimitedReportsTheContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
		io.WriteString(w, "caf\xe9")
	}))
	defer server.Close()

	body, contentType, err := FetchBytesLimited(server.URL+"/x.html", 1024, time.Second)
	if err != nil {
		t.Fatalf("fetch must succeed: %v", err)
	}
	if string(body) != "caf\xe9" {
		t.Fatalf("body = %q, want %q", body, "caf\xe9")
	}
	if contentType != "text/html; charset=iso-8859-1" {
		t.Fatalf("content type = %q, want the declared header", contentType)
	}
}

func TestFetchBytesLimitedTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		io.WriteString(w, "too late")
	}))
	defer server.Close()
	url := server.URL + "/slow.html"

	start := time.Now()
	_, _, err := FetchBytesLimited(url, 1024, 50*time.Millisecond)
	if err == nil {
		t.Fatal("a transfer outliving the timeout must fail")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the timeout did not bound the transfer: %v", elapsed)
	}
	if !strings.Contains(err.Error(), "request failed for "+url) {
		t.Fatalf("error = %q, it must name the request", err.Error())
	}
}

func TestFetchTextHonoursTheDeclaredCharset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
		w.Write([]byte("caf\xe9"))
	}))
	defer server.Close()

	text, err := FetchText(server.URL + "/latin1.html")
	if err != nil {
		t.Fatalf("fetch must succeed: %v", err)
	}
	if text != "café" {
		t.Fatalf("text = %q, want %q", text, "café")
	}
}

func TestFetchTextResultCarriesTheFinalURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start.html":
			http.Redirect(w, r, "/final.html", http.StatusFound)
		default:
			io.WriteString(w, "<p>final</p>")
		}
	}))
	defer server.Close()

	text, finalURL, err := FetchTextResult(server.URL + "/start.html")
	if err != nil {
		t.Fatalf("fetch must succeed: %v", err)
	}
	if text != "<p>final</p>" {
		t.Fatalf("text = %q, want %q", text, "<p>final</p>")
	}
	if finalURL != server.URL+"/final.html" {
		t.Fatalf("final URL = %q, want %q", finalURL, server.URL+"/final.html")
	}
}

// ---------------------------------------------------------------------------
// Asynchronous fetch
// ---------------------------------------------------------------------------

func TestAsyncFetchDeliversTheResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<p>async</p>")
	}))
	defer server.Close()

	result := <-FetchAsync(server.URL + "/async.html")
	if result.Err != nil {
		t.Fatalf("fetch must succeed: %v", result.Err)
	}
	if string(result.Value) != "<p>async</p>" {
		t.Fatalf("body = %q, want %q", result.Value, "<p>async</p>")
	}
}

func TestAsyncFetchReportsHTTPErrorsOverTheChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	result := <-FetchResultAsync(server.URL + "/boom.html")
	if result.Err == nil {
		t.Fatal("500 must fail")
	}
	if !strings.Contains(result.Err.Error(), "500") {
		t.Fatalf("error = %q, it must carry the status", result.Err.Error())
	}
}

func TestAsyncFetchTextDecodesTheBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<p>text</p>")
	}))
	defer server.Close()

	result := <-FetchTextAsync(server.URL + "/t.html")
	if result.Err != nil {
		t.Fatalf("fetch must succeed: %v", result.Err)
	}
	if result.Value != "<p>text</p>" {
		t.Fatalf("text = %q, want %q", result.Value, "<p>text</p>")
	}
}

// ---------------------------------------------------------------------------
// Charset decoding
// ---------------------------------------------------------------------------

func TestDecodeBodyCharsets(t *testing.T) {
	tests := []struct {
		name        string
		body        []byte
		contentType string
		want        string
	}{
		// UTF-8 is the default when none is declared.
		{"utf8 default", []byte("café"), "text/html", "café"},
		{"no content type", []byte("café"), "", "café"},
		// Quoted charset, uppercase, and extra parameters before it.
		{"quoted uppercase", []byte("caf\xe9"), `text/html; foo=bar; charset="ISO-8859-1"`, "café"},
		{"iso-8859-1", []byte("caf\xe9"), "text/html; charset=iso-8859-1", "café"},
		{"latin1 alias", []byte("caf\xe9"), "text/html; charset=latin-1", "café"},
		{"us-ascii", []byte("caf"), "text/html; charset=US-ASCII", "caf"},
		// 0x80 is the euro sign in windows-1252, but a control character in
		// ISO-8859-1; browsers decode the whole family as windows-1252.
		{"windows-1252", []byte{0x80}, "text/html; charset=windows-1252", "€"},
		{"windows-1252 quote", []byte{0x93, 0x94}, "text/html; charset=cp1252", "“”"},
		// UTF-16 with a little-endian byte order mark.
		{"utf-16le bom", []byte{0xFF, 0xFE, 0x63, 0x00, 0x61, 0x00, 0x66, 0x00}, "text/html; charset=utf-16", "caf"},
		// UTF-16 without a mark defaults to big endian.
		{"utf-16be default", []byte{0x00, 0x63, 0x00, 0x61, 0x00, 0x66}, "text/html; charset=utf-16be", "caf"},
		// An unknown charset falls back to UTF-8 so the page still renders.
		{"unknown falls back", []byte("café"), "text/html; charset=x-made-up", "café"},
		// Invalid UTF-8 never fails: the bad bytes become U+FFFD.
		{"lossy utf-8", []byte{'a', 0xFF, 'b'}, "text/html; charset=utf-8", "a\uFFFDb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeBody(tt.body, tt.contentType)
			if err != nil {
				t.Fatalf("decode must succeed: %v", err)
			}
			if got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodeBodyStripsTheUTF8BOM(t *testing.T) {
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte("<p>bom</p>")...)

	got, err := DecodeBody(body, "text/html; charset=utf-8")
	if err != nil {
		t.Fatalf("decode must succeed: %v", err)
	}
	if got != "<p>bom</p>" {
		t.Fatalf("text = %q, want %q", got, "<p>bom</p>")
	}
}

func TestDecodeBodyRejectsAnOddUTF16Body(t *testing.T) {
	_, err := DecodeBody([]byte{0xFF, 0xFE, 0x63, 0x00, 0x61}, "text/html; charset=utf-16")
	if err == nil {
		t.Fatal("an odd UTF-16 body must fail")
	}
	// The oracle formats "invalid utf-16 body: {n} bytes" with the original
	// length, before the mark is stripped (src/net.rs).
	if want := "invalid utf-16 body: 5 bytes"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestDecodeBodyUnknownCharsetWarnsAndFallsBackToUTF8(t *testing.T) {
	var text string
	var err error
	stderr := captureStderr(t, func() {
		text, err = DecodeBody([]byte("café"), "text/html; charset=x-made-up")
	})
	if err != nil {
		t.Fatalf("decode must succeed: %v", err)
	}
	if text != "café" {
		t.Fatalf("text = %q, want %q", text, "café")
	}
	want := "browser-lite: unknown charset \"x-made-up\", decoding as utf-8\n"
	if stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

// ---------------------------------------------------------------------------
// URL joining (the shared rule of src/img.rs and src/css_libcss.rs)
// ---------------------------------------------------------------------------

func TestJoinURL(t *testing.T) {
	existingDir := t.TempDir() // an absolute path that names a directory
	tests := []struct {
		name string
		base string
		rel  string
		want string
		ok   bool
	}{
		// Absolute URLs are returned as they are written.
		{"absolute rel", "http://x/sub/", "https://other/a.png", "https://other/a.png", true},
		{"data url rel", "http://x/sub/", "data:text/plain,x", "data:text/plain,x", true},
		// An http(s) base resolves like RFC 3986.
		{"relative", "http://x/sub/page.html", "img.png", "http://x/sub/img.png", true},
		{"relative dir base", "http://x/sub/", "img.png", "http://x/sub/img.png", true},
		{"root relative", "http://x/sub/page.html", "/img.png", "http://x/img.png", true},
		{"protocol relative", "http://x/a/", "//y/b.png", "http://y/b.png", true},
		// A base path is a directory when it ends with a separator or names
		// an existing directory; otherwise its parent is the base.
		{"dir slash base", "dir/", "img.png", "dir/img.png", true},
		{"existing dir base", existingDir, "img.png", existingDir + "/img.png", true},
		{"file base parent", "dir/page.html", "img.png", "dir/img.png", true},
		{"bare file base", "page.html", "img.png", "img.png", true},
		{"absolute file base", "/abs/page.html", "img.png", "/abs/img.png", true},
		{"absolute rel path", "dir/", "/root.png", "/root.png", true},
		// The rel URL is trimmed before resolving.
		{"padded rel", "dir/", "  img.png  ", "dir/img.png", true},
		// Failures the oracle reports as None.
		{"empty rel", "dir/", "   ", "", false},
		{"empty base", "", "img.png", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := JoinURL(tt.base, tt.rel)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("JoinURL(%q, %q) = (%q, %v), want (%q, %v)", tt.base, tt.rel, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestJoinURLNormalizesDotSegments(t *testing.T) {
	tests := []struct {
		base string
		rel  string
		want string
	}{
		// Relative paths normalise so the cache key is stable.
		{"dir/sub/", "a/../b.png", "dir/sub/b.png"},
		{"dir/", "./b.png", "dir/b.png"},
		{"dir/", "../b.png", "b.png"},
		// ".." climbing past the root is kept, like the oracle's lexical walk.
		{"/a/", "../../b.png", "/../b.png"},
	}
	for _, tt := range tests {
		got, ok := JoinURL(tt.base, tt.rel)
		if !ok || got != tt.want {
			t.Fatalf("JoinURL(%q, %q) = (%q, %v), want (%q, true)", tt.base, tt.rel, got, ok, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// captureStderr runs fn with os.Stderr redirected and returns what it wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	read := make(chan string)
	go func() {
		var b strings.Builder
		io.Copy(&b, r)
		read <- b.String()
	}()
	defer func() {
		os.Stderr = old
	}()
	fn()
	w.Close()
	out := <-read
	r.Close()
	return out
}
