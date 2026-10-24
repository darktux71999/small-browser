// Package net is the engine's HTTP client, ported from the Rust oracle
// (src/net.rs): GET with a total timeout and a response-size cap, redirect
// following with the same user-visible error texts, body decoding with the
// charset declared in Content-Type, and asynchronous wrappers so a transfer
// never blocks the UI.
//
// The oracle drives libcurl; this port drives net/http. Where the oracle
// surfaces libcurl's own phrasing ("request failed for {url}: {err}") the
// shape is kept and only the transport's inner message differs.
package net

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// The transfer profile of the engine, from src/net.rs.
const (
	userAgent      = "browser-lite/0.1"
	connectTimeout = 10 * time.Second
	timeout        = 20 * time.Second
	maxRedirects   = 10
)

// Response is a finished fetch: the body plus the URL it finally came from
// (after redirects).
type Response struct {
	// FinalURL is the URL after redirects.
	FinalURL string
	// Bytes is the raw body.
	Bytes []byte
}

// Result carries the outcome of an asynchronous fetch over its channel.
type Result[T any] struct {
	// Value is the fetched data, valid when Err is nil.
	Value T
	// Err is the failure, mirroring the oracle's Result<T, String>.
	Err error
}

// Fetch fetches a URL and returns the raw body.
func Fetch(url string) ([]byte, error) {
	raw, err := request(url, defaultLimits())
	if err != nil {
		return nil, err
	}
	return raw.bytes, nil
}

// FetchResult fetches a URL and returns the body together with the URL it
// came from.
func FetchResult(url string) (Response, error) {
	raw, err := request(url, defaultLimits())
	if err != nil {
		return Response{}, err
	}
	return Response{FinalURL: raw.finalURL, Bytes: raw.bytes}, nil
}

// FetchText fetches a URL and returns the body decoded with the declared
// charset.
func FetchText(url string) (string, error) {
	raw, err := request(url, defaultLimits())
	if err != nil {
		return "", err
	}
	return DecodeBody(raw.bytes, raw.contentType)
}

// FetchTextResult fetches a URL and returns the decoded body plus the URL it
// finally came from (after redirects), so callers can resolve relative
// subresource URLs.
func FetchTextResult(url string) (text string, finalURL string, err error) {
	raw, err := request(url, defaultLimits())
	if err != nil {
		return "", "", err
	}
	text, err = DecodeBody(raw.bytes, raw.contentType)
	if err != nil {
		return "", "", err
	}
	return text, raw.finalURL, nil
}

// FetchBytesLimited fetches a URL with a size cap and a total timeout.
//
// A body larger than maxBytes is rejected, not truncated, so a caller can
// drop the whole resource instead of using half of it. Returns the body and
// the declared Content-Type, when the response carries one.
func FetchBytesLimited(url string, maxBytes int, timeout time.Duration) (body []byte, contentType string, err error) {
	limits := limits{
		// A connect phase can never outlive the whole request.
		connectTimeout: minDuration(connectTimeout, timeout),
		timeout:        timeout,
		maxBytes:       maxBytes,
	}
	raw, err := request(url, limits)
	if err != nil {
		return nil, "", err
	}
	return raw.bytes, raw.contentType, nil
}

// FetchAsync fetches a URL on a worker goroutine. The result arrives over the
// channel.
func FetchAsync(url string) <-chan Result[[]byte] {
	return spawnFetch(url, Fetch)
}

// FetchResultAsync is like FetchAsync, but the result also carries the final
// URL.
func FetchResultAsync(url string) <-chan Result[Response] {
	return spawnFetch(url, FetchResult)
}

// FetchTextAsync is like FetchAsync, but the result is the decoded body.
func FetchTextAsync(url string) <-chan Result[string] {
	return spawnFetch(url, FetchText)
}

func spawnFetch[T any](url string, fetch func(string) (T, error)) <-chan Result[T] {
	ch := make(chan Result[T], 1)
	go func() {
		value, err := fetch(url)
		ch <- Result[T]{Value: value, Err: err}
	}()
	return ch
}

// raw is what a transfer produced, before any decoding.
type raw struct {
	finalURL    string
	bytes       []byte
	contentType string
}

// limits is the per-request transfer profile: the page-fetch profile is
// defaultLimits. A maxBytes of zero or less mirrors the oracle's absent cap.
type limits struct {
	connectTimeout time.Duration
	timeout        time.Duration
	maxBytes       int
}

func defaultLimits() limits {
	return limits{
		connectTimeout: connectTimeout,
		timeout:        timeout,
		maxBytes:       0, // no cap, like the oracle's DEFAULT_LIMITS
	}
}

func request(url string, lim limits) (raw, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return raw{}, fmt.Errorf("invalid URL %s: %v", url, err)
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: lim.connectTimeout}).DialContext,
		},
		Timeout: lim.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// curl's max_redirections(10) follows 10 hops and fails on the
			// 11th; via already holds the requests made so far.
			if len(via) > maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return raw{}, fmt.Errorf("request failed for %s: %v", url, err)
	}
	defer resp.Body.Close()

	// The response arrived; the size cap now rejects (never truncates) a body
	// larger than the limit.
	var body []byte
	if lim.maxBytes > 0 {
		body, err = io.ReadAll(io.LimitReader(resp.Body, int64(lim.maxBytes)+1))
		if err != nil {
			return raw{}, fmt.Errorf("request failed for %s: %v", url, err)
		}
		if len(body) > lim.maxBytes {
			return raw{}, fmt.Errorf("%s: response larger than %d bytes", url, lim.maxBytes)
		}
	} else {
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return raw{}, fmt.Errorf("request failed for %s: %v", url, err)
		}
	}

	// The status of the last response is the one that counts: a redirect
	// chain reports several, one per hop.
	if resp.StatusCode >= 400 {
		return raw{}, fmt.Errorf("%s: %s %s", url, resp.Proto, resp.Status)
	}

	finalURL := url
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return raw{
		finalURL:    finalURL,
		bytes:       body,
		contentType: resp.Header.Get("Content-Type"),
	}, nil
}

func minDuration(a, b time.Duration) time.Duration {
	if b < a {
		return b
	}
	return a
}

// DecodeBody decodes body with the charset declared by contentType.
//
// The charset is read from the Content-Type header; UTF-8 is assumed when
// none is declared. An unknown charset falls back to UTF-8 so the page still
// renders, and the fallback is reported on stderr.
func DecodeBody(body []byte, contentType string) (string, error) {
	charset, ok := charsetOf(contentType)
	if !ok {
		charset = "utf-8"
	}
	switch charset {
	case "utf-8", "utf8":
		return decodeLossyUTF8(bytes.TrimPrefix(body, utf8BOM)), nil
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1", "us-ascii", "ascii",
		"windows-1252", "cp1252":
		return decodeWindows1252(body), nil
	case "utf-16", "utf-16le", "utf-16be":
		return decodeUTF16(body, charset)
	default:
		fmt.Fprintf(os.Stderr, "browser-lite: unknown charset %q, decoding as utf-8\n", charset)
		return decodeLossyUTF8(body), nil
	}
}

// utf8BOM marks UTF-8 text; it never belongs to the decoded content.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// charsetOf is the charset of a Content-Type header value, if it declares
// one.
func charsetOf(contentType string) (string, bool) {
	if contentType == "" {
		return "", false
	}
	// The first segment is the media type; parameters follow.
	for _, parameter := range strings.Split(contentType, ";")[1:] {
		key, value, found := strings.Cut(parameter, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "charset") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		value = strings.TrimSpace(value)
		if value != "" {
			return strings.ToLower(value), true
		}
	}
	return "", false
}

// decodeLossyUTF8 renders body as UTF-8, replacing every invalid byte with
// U+FFFD like the oracle's from_utf8_lossy (which never fails).
func decodeLossyUTF8(body []byte) string {
	if utf8.Valid(body) {
		return string(body)
	}
	var out strings.Builder
	out.Grow(len(body))
	for i := 0; i < len(body); {
		r, size := utf8.DecodeRune(body[i:])
		if r == utf8.RuneError && size <= 1 {
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		out.Write(body[i : i+size])
		i += size
	}
	return out.String()
}

// decodeWindows1252 decodes a single-byte encoding.
//
// ISO-8859-1, US-ASCII and windows-1252 share the same bytes for everything
// except 0x80-0x9F, where windows-1252 has printable characters. Browsers
// decode that whole family as windows-1252, so this does too.
func decodeWindows1252(body []byte) string {
	out := make([]rune, len(body))
	for i, b := range body {
		out[i] = windows1252[b]
	}
	return string(out)
}

// windows1252 maps every byte to its rune: identity outside 0x80-0x9F, the
// windows-1252 picture characters inside (src/net.rs decode_windows_1252).
var windows1252 = func() [256]rune {
	var table [256]rune
	for i := range table {
		table[i] = rune(i)
	}
	for b, r := range map[byte]rune{
		0x80: 0x20AC, // €
		0x82: 0x201A, // ‚
		0x83: 0x0192, // ƒ
		0x84: 0x201E, // „
		0x85: 0x2026, // …
		0x86: 0x2020, // †
		0x87: 0x2021, // ‡
		0x88: 0x02C6, // ˆ
		0x89: 0x2030, // ‰
		0x8A: 0x0160, // Š
		0x8B: 0x2039, // ‹
		0x8C: 0x0152, // Œ
		0x8E: 0x017D, // Ž
		0x91: 0x2018, // '
		0x92: 0x2019, // '
		0x93: 0x201C, // "
		0x94: 0x201D, // "
		0x95: 0x2022, // •
		0x96: 0x2013, // –
		0x97: 0x2014, // —
		0x98: 0x02DC, // ˜
		0x99: 0x2122, // ™
		0x9A: 0x0161, // š
		0x9B: 0x203A, // ›
		0x9C: 0x0153, // œ
		0x9E: 0x017E, // ž
		0x9F: 0x0178, // Ÿ
	} {
		table[b] = r
	}
	return table
}()

// decodeUTF16 decodes UTF-16, honouring a byte order mark when there is one.
func decodeUTF16(body []byte, charset string) (string, error) {
	rest := body
	littleEndian := false
	switch charset {
	case "utf-16le":
		littleEndian = true
	default:
		switch {
		case bytes.HasPrefix(rest, []byte{0xFF, 0xFE}):
			rest = rest[2:]
			littleEndian = true
		case bytes.HasPrefix(rest, []byte{0xFE, 0xFF}):
			rest = rest[2:]
			littleEndian = false
		default:
			// No mark: UTF-16 defaults to big endian.
			littleEndian = false
		}
	}

	if len(rest)%2 != 0 {
		return "", fmt.Errorf("invalid utf-16 body: %d bytes", len(body))
	}
	units := make([]uint16, len(rest)/2)
	for i := range units {
		if littleEndian {
			units[i] = binary.LittleEndian.Uint16(rest[i*2:])
		} else {
			units[i] = binary.BigEndian.Uint16(rest[i*2:])
		}
	}
	return string(utf16.Decode(units)), nil
}
