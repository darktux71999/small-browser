//! S3 — image subresources: fetch, decode to RGBA and cache, ported from the
//! the Rust oracle (src/img.rs). Fixtures are the shared read-only corpus in
//! test/data; HTTP exchanges use httptest so the tests stay hermetic.

package img

import (
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"browser-lite/dom"
)

// The shared fixture corpus, read-only, relative to this package's directory.
const fixtureDir = "../../test/data"

func fixture(name string) string {
	return filepath.Join(fixtureDir, name)
}

// baseFor hands the engine the page's directory, exactly like main does for a
// local page.
func baseFor() string {
	return fixtureDir + "/"
}

// firstImg returns the id of the document's first <img> element.
func firstImg(t *testing.T, doc *dom.Document) dom.NodeID {
	t.Helper()
	id, ok := doc.QuerySelector("img")
	if !ok {
		t.Fatal("the document has no <img> element")
	}
	return id
}

// ---------------------------------------------------------------------------
// Decoding
// ---------------------------------------------------------------------------

func TestDecodeFixturesToRGBA(t *testing.T) {
	t.Run("img-red", func(t *testing.T) {
		body, err := os.ReadFile(fixture("img-red.png"))
		if err != nil {
			t.Fatal(err)
		}
		image, err := Decode(body)
		if err != nil {
			t.Fatalf("decode must succeed: %v", err)
		}
		if image.Width != 4 || image.Height != 3 {
			t.Fatalf("size = %dx%d, want 4x3", image.Width, image.Height)
		}
		if len(image.RGBA) != 4*3*4 {
			t.Fatalf("RGBA buffer is %d bytes, want width*height*4 = %d", len(image.RGBA), 4*3*4)
		}
		// The fixture is solid opaque red, straight (non-premultiplied) alpha.
		for i := 0; i < 4*3; i++ {
			px := image.RGBA[i*4 : i*4+4]
			if px[0] != 255 || px[1] != 0 || px[2] != 0 || px[3] != 255 {
				t.Fatalf("pixel %d = %v, want [255 0 0 255]", i, px)
			}
		}
	})
	t.Run("img-alpha", func(t *testing.T) {
		body, err := os.ReadFile(fixture("img-alpha.png"))
		if err != nil {
			t.Fatal(err)
		}
		image, err := Decode(body)
		if err != nil {
			t.Fatalf("decode must succeed: %v", err)
		}
		if image.Width != 2 || image.Height != 2 {
			t.Fatalf("size = %dx%d, want 2x2", image.Width, image.Height)
		}
		// 50% red: straight alpha 128 survives the decode untouched.
		for i := 0; i < 2*2; i++ {
			px := image.RGBA[i*4 : i*4+4]
			if px[0] != 255 || px[1] != 0 || px[2] != 0 || px[3] != 128 {
				t.Fatalf("pixel %d = %v, want [255 0 0 128]", i, px)
			}
		}
	})
	t.Run("background-tile", func(t *testing.T) {
		body, err := os.ReadFile(fixture("background-tile.png"))
		if err != nil {
			t.Fatal(err)
		}
		image, err := Decode(body)
		if err != nil {
			t.Fatalf("decode must succeed: %v", err)
		}
		if image.Width != 4 || image.Height != 4 {
			t.Fatalf("size = %dx%d, want 4x4", image.Width, image.Height)
		}
		// Red and blue quadrants: (0,0) red, (2,0) blue, (0,2) blue, (2,2) red.
		want := [][4]byte{
			{255, 0, 0, 255}, {0, 0, 255, 255},
			{0, 0, 255, 255}, {255, 0, 0, 255},
		}
		for i, w := range want {
			x, y := (i%2)*2, (i/2)*2
			off := (y*4 + x) * 4
			px := image.RGBA[off : off+4]
			if [4]byte{px[0], px[1], px[2], px[3]} != w {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, px, w)
			}
		}
	})
	t.Run("img-wide", func(t *testing.T) {
		body, err := os.ReadFile(fixture("img-wide.png"))
		if err != nil {
			t.Fatal(err)
		}
		image, err := Decode(body)
		if err != nil {
			t.Fatalf("decode must succeed: %v", err)
		}
		if image.Width != 1200 || image.Height != 10 {
			t.Fatalf("size = %dx%d, want 1200x10", image.Width, image.Height)
		}
		if len(image.RGBA) != 1200*10*4 {
			t.Fatalf("RGBA buffer is %d bytes, want %d", len(image.RGBA), 1200*10*4)
		}
	})
}

func TestDecodeJPEGToRGBA(t *testing.T) {
	body := hex(t, JPEG)

	image, err := Decode(body)
	if err != nil {
		t.Fatalf("decode must succeed: %v", err)
	}
	if image.Width != 2 || image.Height != 2 {
		t.Fatalf("size = %dx%d, want 2x2", image.Width, image.Height)
	}
	if len(image.RGBA) != 2*2*4 {
		t.Fatalf("RGBA buffer is %d bytes, want %d", len(image.RGBA), 2*2*4)
	}
	// Lossy: near-red is the contract, not an exact triple.
	px := image.RGBA[0:4]
	if !(px[0] > 200 && px[1] < 80 && px[2] < 80) {
		t.Fatalf("first pixel = %v, want near-red", px)
	}
}

func TestDecodeGIFToRGBA(t *testing.T) {
	body := hex(t, GIF)

	image, err := Decode(body)
	if err != nil {
		t.Fatalf("decode must succeed: %v", err)
	}
	if image.Width != 2 || image.Height != 2 {
		t.Fatalf("size = %dx%d, want 2x2", image.Width, image.Height)
	}
	for i := 0; i < 2*2; i++ {
		px := image.RGBA[i*4 : i*4+4]
		if px[0] != 255 || px[1] != 0 || px[2] != 0 || px[3] != 255 {
			t.Fatalf("pixel %d = %v, want [255 0 0 255]", i, px)
		}
	}
}

// TestDecodeUnknownFormatMatchesOracle pins the user-visible string for a
// body no decoder claims: the oracle surfaces gdk-pixbuf's message
// "Unrecognized image file format" (observed oracle stderr on the
// img-broken fixture), not a Go-side paraphrase.
func TestDecodeUnknownFormatMatchesOracle(t *testing.T) {
	for _, body := range [][]byte{[]byte("this is not an image"), mustFixture(t, "img-notimage.png")} {
		_, err := Decode(body)
		if err == nil {
			t.Fatal("decode must fail")
		}
		if want := "Unrecognized image file format"; err.Error() != want {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
	}
}

func mustFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(fixture(name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestDecodeRejectsBadBodies(t *testing.T) {
	notImage, err := os.ReadFile(fixture("img-notimage.png"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		body []byte
		want string
	}{
		// The empty body reports a reason, never decodes.
		{"empty", nil, "empty body"},
		// SVG is detected before decoding; a document that looks like SVG
		// but is not a well-formed <svg> root fails with the svg package's
		// message instead of a generic format error.
		{"svg truncated markup", []byte(`<svg><g></svg>`), "invalid SVG markup: XML syntax error on line 1: element <g> closed by </svg>"},
		{"svg uppercase root", []byte(`<?xml version="1.0"?><SVG></SVG>`), "not an SVG document: root element is <SVG>"},
		// A body that sniffs as no known format reports gdk-pixbuf's message,
		// the user-visible text of the oracle (observed oracle stderr:
		// "skipping image ... img-notimage.png: Unrecognized image file
		// format"; src/img.rs surfaces the GError message).
		{"garbage", []byte("this is not an image"), "Unrecognized image file format"},
		{"notimage fixture", notImage, "Unrecognized image file format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(tt.body)
			if err == nil {
				t.Fatal("decode must fail")
			}
			if err.Error() != tt.want {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

// TestDecodeCorruptBodySurfacesDecoderMessage pins the corrupt-body class:
// a body a decoder claims but cannot finish surfaces that decoder's
// message, the same rule as the oracle's GError passthrough (src/img.rs
// pixbuf::decode take_error). The text itself is engine-specific —
// gdk-pixbuf says "Premature end-of-file encountered" for a truncated PNG
// while Go's stdlib says "unexpected EOF" — so this pins the rule, not the
// byte text.
func TestDecodeCorruptBodySurfacesDecoderMessage(t *testing.T) {
	full, err := os.ReadFile(fixture("img-red.png"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decode(full[:40])
	if err == nil {
		t.Fatal("a truncated PNG must fail")
	}
	if want := "unexpected EOF"; err.Error() != want {
		t.Fatalf("error = %q, want %q (Go decoder passthrough)", err.Error(), want)
	}
}

func TestDecodeWebPIsReported(t *testing.T) {
	// The oracle decodes WebP through libwebp via cgo; this port has no cgo,
	// so a WebP body is reported with libwebp's own message.
	_, err := Decode(hex(t, WEBP))
	if err == nil {
		t.Fatal("decode must fail without a WebP decoder")
	}
	if want := "unsupported or corrupt WebP data"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestImageFromChecksThePixelCount(t *testing.T) {
	// The decompression-bomb guard: decode succeeded, the declared pixel
	// count decides (src/img.rs decode).
	if _, err := imageFrom(stubImage{image.Rect(0, 0, 10, 10)}); err != nil {
		t.Fatalf("a small image must decode: %v", err)
	}
	_, err := imageFrom(stubImage{image.Rect(0, 0, 0, 5)})
	if err == nil || err.Error() != "image has no pixels" {
		t.Fatalf("zero pixels error = %v, want %q", err, "image has no pixels")
	}
	_, err = imageFrom(stubImage{image.Rect(0, 0, 40001, 1000)})
	want := "40001000 pixels exceeds the 40000000 pixel limit"
	if err == nil || err.Error() != want {
		t.Fatalf("oversized error = %v, want %q", err, want)
	}
}

func TestMaxImagePixelsMatchesTheOracle(t *testing.T) {
	if MaxImagePixels != 40_000_000 {
		t.Fatalf("MaxImagePixels = %d, want 40000000 (src/img.rs)", MaxImagePixels)
	}
	if DefaultLimits().MaxBytes != 10*1024*1024 {
		t.Fatalf("default cap = %d, want 10MiB (src/img.rs MAX_IMAGE_BYTES)", DefaultLimits().MaxBytes)
	}
	if DefaultLimits().Timeout != 20*time.Second {
		t.Fatalf("default timeout = %v, want 20s (src/img.rs IMAGE_TIMEOUT)", DefaultLimits().Timeout)
	}
}

// ---------------------------------------------------------------------------
// Loading <img> subresources
// ---------------------------------------------------------------------------

func TestLoadDecodesFixtureImages(t *testing.T) {
	doc := dom.Parse(`<body><img src="img-red.png"></body>`)
	node := firstImg(t, doc)

	images := Load(doc, baseFor())

	if len(images.Warnings()) != 0 {
		t.Fatalf("warnings = %v, want none", images.Warnings())
	}
	image := images.Image(node)
	if image == nil {
		t.Fatalf("img-red.png must load: %v", images.Warnings())
	}
	if image.Width != 4 || image.Height != 3 {
		t.Fatalf("size = %dx%d, want 4x3", image.Width, image.Height)
	}
	if images.Fetched() != 1 || images.Loaded() != 1 || images.Failed() != 0 {
		t.Fatalf("counters = %d fetched / %d loaded / %d failed, want 1/1/0",
			images.Fetched(), images.Loaded(), images.Failed())
	}
	url, ok := images.URL(node)
	if !ok || !strings.HasSuffix(url, "img-red.png") {
		t.Fatalf("URL = (%q, %v), want the resolved fixture path", url, ok)
	}
}

func TestLoadWarningsForBrokenResources(t *testing.T) {
	tests := []struct {
		name string
		html string
		base string
		want string
		// failed is the Failed() count after the warning: a resource that
		// was fetched and rejected counts, an unresolvable URL never enters
		// the cache.
		failed int
	}{
		{
			// The OS error keeps the oracle's io::Error rendering.
			name:   "missing local file",
			html:   `<img src="img-missing.png">`,
			base:   baseFor(),
			want:   "skipping image ../../test/data/img-missing.png: No such file or directory (os error 2)",
			failed: 1,
		},
		{
			name:   "undecodable body",
			html:   `<img src="img-notimage.png">`,
			base:   baseFor(),
			want:   "skipping image ../../test/data/img-notimage.png: Unrecognized image file format",
			failed: 1,
		},
		{
			name: "unresolvable src",
			html: `<img src="x.png">`,
			base: "",
			want: `skipping image "x.png": cannot resolve it against ""`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := dom.Parse(tt.html)
			node := firstImg(t, doc)

			images := Load(doc, tt.base)

			warnings := images.Warnings()
			if len(warnings) != 1 {
				t.Fatalf("warnings = %v, want exactly one", warnings)
			}
			if warnings[0] != tt.want {
				t.Fatalf("warning = %q, want %q", warnings[0], tt.want)
			}
			if images.Image(node) != nil {
				t.Fatal("a failed resource has no image")
			}
			if images.Failed() != tt.failed {
				t.Fatalf("failed = %d, want %d", images.Failed(), tt.failed)
			}
		})
	}
}

func TestLoadSkipsImagesWithoutASource(t *testing.T) {
	doc := dom.Parse(`<img><img src=""><img src="   "><p>no image</p>`)

	images := Load(doc, baseFor())

	if len(images.Warnings()) != 0 {
		t.Fatalf("warnings = %v, a sourceless image is not a failure", images.Warnings())
	}
	if images.Fetched() != 0 {
		t.Fatalf("fetched = %d, want 0", images.Fetched())
	}
}

func TestEachURLIsFetchedOncePerRun(t *testing.T) {
	doc := dom.Parse(`<img src="img-red.png"><img src="./img-red.png"><img src="img-missing.png"><img src="img-missing.png">`)

	images := Load(doc, baseFor())

	// "./img-red.png" normalises onto the same cache key; failures cache too.
	if images.Fetched() != 2 {
		t.Fatalf("fetched = %d, want 2 (one per unique URL)", images.Fetched())
	}
	if images.Loaded() != 1 || images.Failed() != 1 {
		t.Fatalf("loaded/failed = %d/%d, want 1/1", images.Loaded(), images.Failed())
	}
	if len(images.Warnings()) != 1 {
		t.Fatalf("warnings = %v, want one per resource", images.Warnings())
	}
	// Every element pointing at the shared URL reuses the one decode.
	id0, _ := doc.QuerySelector("img")
	all := doc.QuerySelectorAll("img")
	if images.Image(id0) == nil || images.Image(all[1]) == nil {
		t.Fatal("both elements must share the cached image")
	}
}

func TestLoadWithLimitsRejectsOversizedBodies(t *testing.T) {
	doc := dom.Parse(`<img src="img-red.png">`)
	limits := Limits{MaxBytes: 16, Timeout: 10 * time.Second}

	images := LoadWithLimits(doc, baseFor(), limits)

	warnings := images.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	// read_capped reports "N bytes exceeds the M byte limit" (src/img.rs).
	want := "skipping image ../../test/data/img-red.png: 75 bytes exceeds the 16 byte limit"
	if warnings[0] != want {
		t.Fatalf("warning = %q, want %q", warnings[0], want)
	}
	if images.Failed() != 1 {
		t.Fatalf("failed = %d, want 1", images.Failed())
	}
}

// ---------------------------------------------------------------------------
// Remote images
// ---------------------------------------------------------------------------

func TestRemoteRelativeSrcResolvesAgainstTheDocumentURL(t *testing.T) {
	body, err := os.ReadFile(fixture("img-red.png"))
	if err != nil {
		t.Fatal(err)
	}
	var requested atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested.Store(r.URL.Path)
		w.Header().Set("Content-Type", "image/png")
		w.Write(body)
	}))
	defer server.Close()

	doc := dom.Parse(`<body><img src="img-red.png"></body>`)
	images := Load(doc, server.URL+"/sub/")

	image := images.Image(firstImg(t, doc))
	if image == nil {
		t.Fatalf("remote image not loaded: %v", images.Warnings())
	}
	if image.Width != 4 || image.Height != 3 {
		t.Fatalf("size = %dx%d, want 4x3", image.Width, image.Height)
	}
	if path, _ := requested.Load().(string); path != "/sub/img-red.png" {
		t.Fatalf("requested %q, want the URL resolved against the document directory", path)
	}
}

func TestRemoteFailureWarningsCarryTheStatusLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	url := server.URL + "/img-missing.png"

	doc := dom.Parse(`<img src="img-missing.png">`)
	images := Load(doc, server.URL+"/")

	warnings := images.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	// "skipping image {url}: {err}" where err is "{url}: {status line}".
	want := "skipping image " + url + ": " + url + ": HTTP/1.1 404 Not Found"
	if warnings[0] != want {
		t.Fatalf("warning = %q, want %q", warnings[0], want)
	}
}

func TestRemoteOversizedBodyIsRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 100))
	}))
	defer server.Close()
	url := server.URL + "/big.bin"
	limits := Limits{MaxBytes: 10, Timeout: 10 * time.Second}

	doc := dom.Parse(`<img src="big.bin">`)
	images := LoadWithLimits(doc, server.URL+"/", limits)

	warnings := images.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	want := "skipping image " + url + ": " + url + ": response larger than 10 bytes"
	if warnings[0] != want {
		t.Fatalf("warning = %q, want %q", warnings[0], want)
	}
}

// ---------------------------------------------------------------------------
// background-image loading (the loader map T10 added to the oracle)
// ---------------------------------------------------------------------------

// fakeStyle is a computed style carrying a background-image URL.
type fakeStyle struct {
	url string
	has bool
}

func (f fakeStyle) BackgroundImage() (string, bool) { return f.url, f.has }

func TestBackgroundImagesShareTheResolvedURLCache(t *testing.T) {
	doc := dom.Parse(`<img src="img-alpha.png">`)
	imgNode := firstImg(t, doc)
	styles := []BackgroundStyle{fakeStyle{"img-alpha.png", true}, fakeStyle{}, fakeStyle{"img-alpha.png", true}}

	images := Load(doc, baseFor()).LoadBackgroundImages(styles, baseFor())

	// One URL, one fetch: the <img> and the background share the decode.
	if images.Fetched() != 1 || images.Loaded() != 1 {
		t.Fatalf("fetched/loaded = %d/%d, want 1/1", images.Fetched(), images.Loaded())
	}
	bg := images.Background("img-alpha.png")
	if bg == nil {
		t.Fatal("the background URL must resolve through the shared cache")
	}
	if images.Image(imgNode) == nil {
		t.Fatal("the <img> keeps its own lookup")
	}
	if bg != images.Image(imgNode) {
		t.Fatal("both must be the same cached decode")
	}
	if len(images.Warnings()) != 0 {
		t.Fatalf("warnings = %v, want none", images.Warnings())
	}
}

func TestBackgroundFailureIsCachedPerRawURL(t *testing.T) {
	doc := dom.Parse(`<p>no images</p>`)
	styles := []BackgroundStyle{
		fakeStyle{"img-missing.png", true},
		fakeStyle{"img-missing.png", true},
	}

	images := Load(doc, baseFor()).LoadBackgroundImages(styles, baseFor())

	if images.Fetched() != 1 {
		t.Fatalf("fetched = %d, the raw URL is fetched once", images.Fetched())
	}
	warnings := images.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one per resource", warnings)
	}
	want := "skipping image ../../test/data/img-missing.png: No such file or directory (os error 2)"
	if warnings[0] != want {
		t.Fatalf("warning = %q, want %q", warnings[0], want)
	}
	if images.Background("img-missing.png") != nil {
		t.Fatal("a failed background caches nil, not an image")
	}
}

func TestBackgroundUnresolvableURLWarns(t *testing.T) {
	doc := dom.Parse(`<p>no images</p>`)
	styles := []BackgroundStyle{fakeStyle{"bg.png", true}}

	images := Load(doc, "").LoadBackgroundImages(styles, "")

	warnings := images.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	want := `skipping background image "bg.png": cannot resolve it against ""`
	if warnings[0] != want {
		t.Fatalf("warning = %q, want %q", warnings[0], want)
	}
	if images.Background("bg.png") != nil {
		t.Fatal("an unresolvable background caches nil")
	}
}

// ---------------------------------------------------------------------------
// PRESERVE: the empty set
// ---------------------------------------------------------------------------

func TestEmptyHasNoImagesAndNoWarnings(t *testing.T) {
	images := Empty()

	if images.Image(0) != nil {
		t.Fatal("the empty set has no images")
	}
	if _, ok := images.URL(0); ok {
		t.Fatal("the empty set has no URLs")
	}
	if images.Fetched() != 0 || images.Loaded() != 0 || images.Failed() != 0 {
		t.Fatal("the empty set has empty counters")
	}
	if len(images.Warnings()) != 0 {
		t.Fatal("the empty set has no warnings")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stubImage reports fixed bounds without holding pixels, to exercise the
// decompression-bomb guard without allocating huge buffers.
type stubImage struct{ r image.Rectangle }

func (s stubImage) ColorModel() color.Model { return color.RGBAModel }
func (s stubImage) Bounds() image.Rectangle { return s.r }
func (s stubImage) At(x, y int) color.Color { return color.RGBA{} }

// hex turns a whitespace-padded hex dump into bytes.
func hex(t *testing.T, text string) []byte {
	t.Helper()
	var out []byte
	nibble := func(c byte) byte {
		switch {
		case '0' <= c && c <= '9':
			return c - '0'
		case 'a' <= c && c <= 'f':
			return c - 'a' + 10
		case 'A' <= c && c <= 'F':
			return c - 'A' + 10
		default:
			t.Fatalf("bad hex digit %q", c)
			return 0
		}
	}
	pending := byte(0)
	have := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\n' || c == '\t' || c == '\r' {
			continue
		}
		if !have {
			pending, have = nibble(c)<<4, true
		} else {
			out = append(out, pending|nibble(c))
			have = false
		}
	}
	if have {
		t.Fatal("odd number of hex digits")
	}
	return out
}

// 2x2 solid red JPEG (the oracle's tests/images.rs fixture body).
const JPEG = `
    ffd8ffe000104a46494600010100000100010000ffdb0043000201010101010201010102020202020403020202020504
    040304060506060605060606070908060709070606080b08090a0a0a0a0a06080b0c0b0a0c090a0a0affdb0043010202
    02020202050303050a0706070a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a
    0a0a0a0a0a0a0a0a0a0a0a0a0a0affc00011080002000203012200021101031101ffc4001f0000010501010101010100
    000000000000000102030405060708090a0bffc400b5100002010303020403050504040000017d010203000411051221
    31410613516107227114328191a1082342b1c11552d1f02433627282090a161718191a25262728292a3435363738393a
    434445464748494a535455565758595a636465666768696a737475767778797a838485868788898a9293949596979899
    9aa2a3a4a5a6a7a8a9aab2b3b4b5b6b7b8b9bac2c3c4c5c6c7c8c9cad2d3d4d5d6d7d8d9dae1e2e3e4e5e6e7e8e9eaf1
    f2f3f4f5f6f7f8f9faffc4001f0100030101010101010101010000000000000102030405060708090a0bffc400b51100
    020102040403040705040400010277000102031104052131061241510761711322328108144291a1b1c109233352f015
    6272d10a162434e125f11718191a262728292a35363738393a434445464748494a535455565758595a63646566676869
    6a737475767778797a82838485868788898a92939495969798999aa2a3a4a5a6a7a8a9aab2b3b4b5b6b7b8b9bac2c3c4
    c5c6c7c8c9cad2d3d4d5d6d7d8d9dae2e3e4e5e6e7e8e9eaf2f3f4f5f6f7f8f9faffda000c03010002110311003f00f8
    be8a28afe533fdfc3fffd9
`

// 2x2 solid red GIF (the oracle's tests/images.rs fixture body).
const GIF = `
    47494638376102000200810000ff00000000000000000000002c0000000002000200000806000108041010003b
`

// 2x2 lossless red WebP (the oracle's tests/images.rs fixture body).
const WEBP = `
    524946461c000000574542505650384c0f0000002f014000000710fd8ffe0722a2ff0100
`

// ---------------------------------------------------------------------------
// SVG decoding
// ---------------------------------------------------------------------------

// The probe fixture is a 240x120 viewBox with a blue disc at user (60,60)
// (#2255aa), an orange rectangle over user x 120..220 (#aa5522) and a pale
// background (#eef4ff). The raster is 512 px wide, so user x maps to 2.133x.
func TestDecodeSVGFixtureRendersInk(t *testing.T) {
	body, err := os.ReadFile(fixture("probe-svg-image.svg"))
	if err != nil {
		t.Fatal(err)
	}
	image, err := Decode(body)
	if err != nil {
		t.Fatal(err)
	}
	if image.Width != 240 || image.Height != 120 {
		t.Fatalf("intrinsic = %dx%d, want 240x120", image.Width, image.Height)
	}
	rw, rh := image.RasterSize()
	if rw != 512 || rh != 256 || len(image.RGBA) != int(rw*rh*4) {
		t.Fatalf("raster = %dx%d with %d bytes, want 512x256", rw, rh, len(image.RGBA))
	}
	if !image.KeepAspect {
		t.Fatal("an SVG keeps its aspect ratio inside the box")
	}
	at := func(x, y int) [4]byte {
		i := (y*int(rw) + x) * 4
		return [4]byte{image.RGBA[i], image.RGBA[i+1], image.RGBA[i+2], image.RGBA[i+3]}
	}
	tests := []struct {
		name string
		x, y int
		want [3]byte
	}{
		{"disc centre", 128, 128, [3]byte{0x22, 0x55, 0xaa}},
		{"orange rect", 363, 128, [3]byte{0xaa, 0x55, 0x22}},
		{"pale background", 5, 5, [3]byte{0xee, 0xf4, 0xff}},
	}
	for _, tt := range tests {
		got := at(tt.x, tt.y)
		if got[3] != 255 || got[0] != tt.want[0] || got[1] != tt.want[1] || got[2] != tt.want[2] {
			t.Fatalf("%s pixel = %v, want opaque %v", tt.name, got, tt.want)
		}
	}
}

func TestDecodeSVGIntrinsicSizeIsTheCSSSize(t *testing.T) {
	// Layout reads Width/Height as the natural size of an unhinted <img>:
	// it must be the CSS size, not the raster size.
	image, err := Decode([]byte(`<svg width="16" height="16" viewBox="0 0 800 300"><rect width="800" height="300"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if image.Width != 16 || image.Height != 16 {
		t.Fatalf("intrinsic = %dx%d, want 16x16", image.Width, image.Height)
	}
	if rw, rh := image.RasterSize(); rw != 512 || rh != 192 {
		t.Fatalf("raster = %dx%d, want 512x192 (the viewBox aspect)", rw, rh)
	}
}

func TestDecodeSVGWithoutSizeUsesTheDefault(t *testing.T) {
	image, err := Decode([]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if image.Width != 300 || image.Height != 150 {
		t.Fatalf("intrinsic = %dx%d, want the 300x150 default", image.Width, image.Height)
	}
}

func TestDecodeSVGChecksTheIntrinsicPixelCount(t *testing.T) {
	_, err := Decode([]byte(`<svg width="100000" height="100000"></svg>`))
	if err == nil || !strings.Contains(err.Error(), "pixel limit") {
		t.Fatalf("error = %v, want the pixel limit", err)
	}
}

func TestRasterImagesKeepTheirPixelSize(t *testing.T) {
	doc := dom.Parse(`<img src="img-red.png">`)
	images := Load(doc, baseFor())
	image := images.Image(firstImg(t, doc))
	if image == nil {
		t.Fatal("img-red.png must load")
	}
	if rw, rh := image.RasterSize(); rw != 4 || rh != 3 || image.KeepAspect {
		t.Fatalf("raster %dx%d keepAspect=%v, want 4x3 stretched", rw, rh, image.KeepAspect)
	}
}

func TestLoadDecodesSVGImage(t *testing.T) {
	doc := dom.Parse(`<body><img src="probe-svg-image.svg"></body>`)
	node := firstImg(t, doc)

	images := Load(doc, baseFor())

	if len(images.Warnings()) != 0 {
		t.Fatalf("warnings = %v, want none", images.Warnings())
	}
	if image := images.Image(node); image == nil || image.Width != 240 {
		t.Fatalf("SVG img must decode: %v", images.Warnings())
	}
}
