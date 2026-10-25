// Package img loads the page's image subresources: fetch, decode to RGBA and
// cache them for one run. It mirrors the Rust oracle (src/img.rs).
//
// An <img src> resolves like a stylesheet: an http(s) page resolves against
// its final document URL, a local page against the directory of the file.
// Every URL is fetched and decoded once per run and the result — including a
// failure — is cached, so a page that repeats a logo costs one request.
// background-image URLs ride the same fetch/decode/cache machinery through
// ImageSet.LoadBackgroundImages.
//
// Decoding is headless and uses the Go standard library: PNG, JPEG and GIF.
// SVG is rasterized by package svg (the subset the codeberg icon set needs,
// see that package's comment); its CSS size is the intrinsic size layout
// reads, and its raster is kept at a fixed resolution so it stays sharp
// when painted into a larger box. WebP would need libwebp via cgo; its body
// is reported instead.
//
// A resource that fails (unreachable, over the size cap, unknown or
// unsupported format) never aborts the render: it warns once through
// `browser-lite:` on stderr, exactly like a broken stylesheet.
package img

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"math"
	// The standard decoders register themselves with image.Decode; PNG, JPEG
	// and GIF cover the formats the oracle decodes through gdk-pixbuf.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"browser-lite/dom"
	blnet "browser-lite/net"
	"browser-lite/svg"
)

// Hard cap on one image body. A body larger than this is rejected, not
// truncated, so a runaway download cannot be mistaken for a broken image.
const MaxImageBytes = 10 * 1024 * 1024

// Per-image transfer timeout, matching the stylesheet loader's order of
// magnitude: an image must not stall the render forever.
const ImageTimeout = 20 * time.Second

// Decompression-bomb guard: a 100x100 PNG can declare 40000x40000 pixels.
const MaxImagePixels uint64 = 40_000_000

// Image is one decoded image: non-premultiplied RGBA8, top-down. Cairo wants
// premultiplied ARGB32, so paint converts at draw time.
//
// Width and Height are the image's intrinsic CSS size: layout sizes an
// unhinted <img> from them. For raster formats they are also the pixel size
// of RGBA (Width*Height*4 bytes). An SVG is rasterized at its own resolution,
// so RGBA is RasterWidth x RasterHeight instead; RasterSize returns the
// pixel size of RGBA either way.
type Image struct {
	Width  uint32
	Height uint32
	RGBA   []byte
	// RasterWidth and RasterHeight are the pixel size of RGBA when it
	// differs from Width and Height. Zero means RGBA is Width x Height.
	RasterWidth  uint32
	RasterHeight uint32
	// KeepAspect fits the content into the layout box, keeping its aspect
	// ratio and centring it (SVG preserveAspectRatio xMidYMid meet). Raster
	// formats stretch to the box instead.
	KeepAspect bool
}

// RasterSize is the pixel size of RGBA.
func (i *Image) RasterSize() (uint32, uint32) {
	if i.RasterWidth == 0 || i.RasterHeight == 0 {
		return i.Width, i.Height
	}
	return i.RasterWidth, i.RasterHeight
}

// Limits is the per-run image profile, the counterpart of the oracle's
// ImageLimits.
type Limits struct {
	// Bodies larger than this are rejected instead of decoded.
	MaxBytes int
	Timeout  time.Duration
}

// DefaultLimits is the profile a normal run uses.
func DefaultLimits() Limits {
	return Limits{MaxBytes: MaxImageBytes, Timeout: ImageTimeout}
}

// BackgroundStyle is the part of a computed style the background loader
// reads: the background-image URL as the style carries it (sheet-resolved by
// the cascade, as authored). The css package's Style satisfies it; a style
// declaring no background reports false.
type BackgroundStyle interface {
	BackgroundImage() (string, bool)
}

// ImageSet holds the images of one document, decoded and cached for the
// whole run.
//
// Keyed by resolved URL for the cache and by node for the layout lookup: two
// <img> elements pointing at the same file share one decode.
type ImageSet struct {
	// nodes that referenced a resource, mapped to its resolved URL.
	urls map[dom.NodeID]string
	// background-image URL (as the style carries it) to its image; a nil
	// value caches a failure. Keyed by that URL because every element that
	// writes the same URL shares one fetch through the resolved-URL cache
	// below.
	backgrounds map[string]*Image
	// resolved URL to its image; a nil entry caches a failure so it is not
	// retried.
	cache map[string]*Image
	// unique URLs that were attempted (successful or not).
	fetched int
	// one message per failed resource, without the browser-lite: prefix.
	warnings []string
	// inline <svg> elements, rasterized from their own markup (no URL, no
	// fetch). A markup that fails to decode leaves a hole, silently, like
	// an unreachable image without a warning line of its own.
	inline map[dom.NodeID]*Image
}

// Empty returns the set of a page without <img>: what layout's plain entry
// point lays out against.
func Empty() *ImageSet {
	return &ImageSet{
		urls:        map[dom.NodeID]string{},
		backgrounds: map[string]*Image{},
		cache:       map[string]*Image{},
		inline:      map[dom.NodeID]*Image{},
	}
}

// Load fetches and decodes every <img src> of doc, resolving relative URLs
// against base (the document URL or the page's directory).
func Load(doc *dom.Document, base string) *ImageSet {
	return LoadWithLimits(doc, base, DefaultLimits())
}

// LoadWithLimits is Load with explicit limits, so tests can drive the caps.
func LoadWithLimits(doc *dom.Document, base string, limits Limits) *ImageSet {
	set := Empty()
	for id := range doc.Nodes {
		node := &doc.Nodes[id]
		if !node.IsText && asciiEqualFold(node.Tag, "svg") {
			set.loadInlineSVG(doc, dom.NodeID(id))
			continue
		}
		if node.IsText || !asciiEqualFold(node.Tag, "img") {
			continue
		}
		src, ok := node.Attr("src")
		if !ok {
			continue
		}
		src = strings.TrimSpace(src)
		if src == "" {
			// No source is not a failed resource: nothing to report.
			continue
		}
		url, ok := blnet.JoinURL(base, src)
		if !ok {
			set.warn(fmt.Sprintf("skipping image %q: cannot resolve it against %q", src, base))
			continue
		}
		set.urls[dom.NodeID(id)] = url
		set.loadURL(url, &limits)
	}
	return set
}

// Image returns the decoded image for node, or nil when it has none (no
// src, unreachable, undecodable, or unsupported).
func (s *ImageSet) Image(node dom.NodeID) *Image {
	if image, ok := s.inline[node]; ok {
		return image
	}
	url, ok := s.urls[node]
	if !ok {
		return nil
	}
	return s.cache[url]
}

// loadInlineSVG rasterizes one inline <svg> element through the same SVG
// decoder that serves <img src=*.svg>: the element's subtree is serialized
// back to XML and handed to decodeSVG, so inline and file-based icons share
// one rasterizer (go/svg). Its intrinsic size comes from its own width,
// height and viewBox, exactly like a file's root <svg>.
func (s *ImageSet) loadInlineSVG(doc *dom.Document, id dom.NodeID) {
	var markup strings.Builder
	writeSVGMarkup(&markup, doc, id)
	image, err := decodeSVG([]byte(markup.String()))
	if err != nil {
		return
	}
	s.inline[id] = image
}

// svgCamelTags restores the SVG element names the HTML parser lowercases
// (x/net/html keeps camelCase only for attributes): the go/svg paint server
// lookups match the spec spelling.
var svgCamelTags = map[string]string{
	"lineargradient": "linearGradient",
	"radialgradient": "radialGradient",
	"clippath":       "clipPath",
}

// writeSVGMarkup serializes the subtree at id as well-formed XML: the tag and
// attribute names as parsed (attributes are already SVG-cased), text and
// attribute values escaped, empty elements self-closed, attributes in sorted
// order so the output is deterministic.
func writeSVGMarkup(b *strings.Builder, doc *dom.Document, id dom.NodeID) {
	node := &doc.Nodes[id]
	if node.IsText {
		_ = xml.EscapeText(b, []byte(node.Text))
		return
	}
	tag := node.Tag
	if camel, ok := svgCamelTags[tag]; ok {
		tag = camel
	}
	b.WriteByte('<')
	b.WriteString(tag)
	keys := make([]string, 0, len(node.Attrs))
	for k := range node.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !xmlName(k) {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(k)
		b.WriteString(`="`)
		_ = xml.EscapeText(b, []byte(node.Attrs[k]))
		b.WriteByte('"')
	}
	if len(node.Children) == 0 {
		b.WriteString("/>")
		return
	}
	b.WriteByte('>')
	for _, child := range node.Children {
		writeSVGMarkup(b, doc, child)
	}
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteByte('>')
}

// xmlName reports whether name is a plain XML name the serializer can emit
// unescaped: letters, digits and the - _ . : punctuation.
func xmlName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == ':':
		default:
			return false
		}
	}
	return true
}

// LoadBackgroundImages fetches and decodes the background-image URLs of
// styles on top of the <img> images, resolving them against base exactly
// like <img src> and sharing the same per-URL cache: a page that references
// the same file as an <img> and as a background decodes it once.
func (s *ImageSet) LoadBackgroundImages(styles []BackgroundStyle, base string) *ImageSet {
	return s.LoadBackgroundImagesWithLimits(styles, base, DefaultLimits())
}

// LoadBackgroundImagesWithLimits is LoadBackgroundImages with explicit
// limits, so tests can drive the caps.
func (s *ImageSet) LoadBackgroundImagesWithLimits(styles []BackgroundStyle, base string, limits Limits) *ImageSet {
	for _, style := range styles {
		raw, ok := style.BackgroundImage()
		if !ok {
			continue
		}
		if _, seen := s.backgrounds[raw]; seen {
			continue
		}
		var loaded *Image
		if url, ok := blnet.JoinURL(base, raw); ok {
			loaded = s.loadURL(url, &limits)
		} else {
			s.warn(fmt.Sprintf("skipping background image %q: cannot resolve it against %q", raw, base))
		}
		s.backgrounds[raw] = loaded
	}
	return s
}

// Background returns the decoded image behind a background-image URL as the
// style carries it, or nil when it was never requested or failed to load.
func (s *ImageSet) Background(rawURL string) *Image {
	return s.backgrounds[rawURL]
}

// URL returns the resolved URL node points at, when it had a usable src.
func (s *ImageSet) URL(node dom.NodeID) (string, bool) {
	url, ok := s.urls[node]
	return url, ok
}

// Warnings are the non-fatal problems of this run, without the
// browser-lite: prefix (also printed on stderr).
func (s *ImageSet) Warnings() []string {
	return s.warnings
}

// Fetched counts the unique URLs attempted in this run.
func (s *ImageSet) Fetched() int {
	return s.fetched
}

// Loaded counts the unique URLs that decoded.
func (s *ImageSet) Loaded() int {
	count := 0
	for _, image := range s.cache {
		if image != nil {
			count++
		}
	}
	return count
}

// Failed counts the unique URLs that failed; the failures are cached too.
func (s *ImageSet) Failed() int {
	count := 0
	for _, image := range s.cache {
		if image == nil {
			count++
		}
	}
	return count
}

func (s *ImageSet) warn(message string) {
	fmt.Fprintf(os.Stderr, "browser-lite: %s\n", message)
	s.warnings = append(s.warnings, message)
}

// loadURL fetches and decodes url once. A nil result is cached as well, so a
// broken URL is reported once even when the page repeats it.
func (s *ImageSet) loadURL(url string, limits *Limits) *Image {
	if cached, ok := s.cache[url]; ok {
		return cached
	}
	s.fetched++
	var result *Image
	body, err := fetch(url, limits)
	if err == nil {
		var decoded *Image
		decoded, err = Decode(body)
		result = decoded
	}
	if err != nil {
		s.warn(fmt.Sprintf("skipping image %s: %v", url, err))
		result = nil
	}
	s.cache[url] = result
	return result
}

// svgRasterSide is the longer side of an SVG's raster. It is fixed, not
// derived from the layout box (decoding happens before layout), and large
// enough that a box of a few hundred CSS pixels is still downscaled.
const svgRasterSide = 512

// Decode decodes one image body to RGBA, sniffing the format from its bytes.
//
// The Content-Type and the file extension are ignored on purpose: a
// mislabelled body still renders, and a .png that is not a PNG fails with
// the same message as any other unknown format.
func Decode(body []byte) (*Image, error) {
	if len(body) == 0 {
		return nil, errors.New("empty body")
	}
	if looksLikeSVG(body) {
		return decodeSVG(body)
	}
	if isWebP(body) {
		// The oracle decodes WebP through libwebp (cgo); this port cannot,
		// so the body is reported with libwebp's own message.
		return nil, errors.New("unsupported or corrupt WebP data")
	}
	src, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			// The user-visible text of the oracle's format sniffing: when
			// no loader claims the body, gdk-pixbuf fails the write with
			// "Unrecognized image file format" (observed oracle stderr on
			// img-notimage.png; src/img.rs surfaces the GError message).
			return nil, errors.New("Unrecognized image file format")
		}
		// A body a decoder claims but cannot finish: surface the decoder's
		// message, the same rule as the oracle's GError passthrough
		// (src/img.rs pixbuf::decode take_error). The text itself is
		// engine-specific (gdk-pixbuf vs Go stdlib, e.g. truncated PNG:
		// "Premature end-of-file encountered" vs "unexpected EOF").
		return nil, err
	}
	return imageFrom(src)
}

// decodeSVG rasterizes an SVG body. The intrinsic size is the document's CSS
// size (width/height, or the viewBox), and the pixel guard applies to that
// size like it does to rasters.
func decodeSVG(body []byte) (*Image, error) {
	doc, err := svg.Parse(body)
	if err != nil {
		return nil, err
	}
	cssW, cssH := doc.IntrinsicSize()
	if !(cssW >= 1 && cssH >= 1) || cssW > math.MaxUint32 || cssH > math.MaxUint32 {
		return nil, fmt.Errorf("SVG size %vx%v is out of range", cssW, cssH)
	}
	width, height := uint32(math.Round(cssW)), uint32(math.Round(cssH))
	if pixels := uint64(width) * uint64(height); pixels > MaxImagePixels {
		return nil, fmt.Errorf("%d pixels exceeds the %d pixel limit", pixels, MaxImagePixels)
	}
	rw, rh, rgba := doc.Render(svgRasterSide)
	return &Image{
		Width:        width,
		Height:       height,
		RGBA:         rgba,
		RasterWidth:  uint32(rw),
		RasterHeight: uint32(rh),
		KeepAspect:   true,
	}, nil
}

// imageFrom converts a decoded image to straight RGBA8 and enforces the
// pixel-count guards, like the oracle's decode tail.
func imageFrom(src image.Image) (*Image, error) {
	bounds := src.Bounds()
	width, height := uint32(bounds.Dx()), uint32(bounds.Dy())
	pixels := uint64(width) * uint64(height)
	if pixels == 0 {
		return nil, errors.New("image has no pixels")
	}
	if pixels > MaxImagePixels {
		return nil, fmt.Errorf("%d pixels exceeds the %d pixel limit", pixels, MaxImagePixels)
	}
	return &Image{
		Width:  width,
		Height: height,
		RGBA:   toRGBA(src, width, height),
	}, nil
}

// toRGBA copies src into a packed, top-down, non-premultiplied RGBA8 buffer,
// the layout gdk-pixbuf hands the oracle.
func toRGBA(src image.Image, width, height uint32) []byte {
	out := make([]byte, width*height*4)
	switch m := src.(type) {
	case *image.NRGBA:
		// PNG keeps straight samples: copy them row by row.
		for y := uint32(0); y < height; y++ {
			row := m.Pix[y*uint32(m.Stride) : y*uint32(m.Stride)+width*4]
			copy(out[y*width*4:(y+1)*width*4], row)
		}
	case *image.NRGBA64:
		for y := uint32(0); y < height; y++ {
			for x := uint32(0); x < width; x++ {
				i := (y*uint32(m.Stride) + x*8)
				o := (y*width + x) * 4
				out[o+0] = m.Pix[i+0]
				out[o+1] = m.Pix[i+2]
				out[o+2] = m.Pix[i+4]
				out[o+3] = m.Pix[i+6]
			}
		}
	case *image.Paletted:
		// Palette entries are straight colors (PLTE + tRNS).
		for y := uint32(0); y < height; y++ {
			for x := uint32(0); x < width; x++ {
				c := m.Palette[m.Pix[y*uint32(m.Stride)+x]]
				r, g, b, a := c.RGBA()
				o := (y*width + x) * 4
				out[o+0] = unpremultiply8(r, a)
				out[o+1] = unpremultiply8(g, a)
				out[o+2] = unpremultiply8(b, a)
				out[o+3] = unpremultiply8(a, 0xffff)
			}
		}
	case *image.Gray:
		for i := uint32(0); i < width*height; i++ {
			v := m.Pix[i]
			out[i*4+0], out[i*4+1], out[i*4+2], out[i*4+3] = v, v, v, 255
		}
	case *image.Gray16:
		for i := uint32(0); i < width*height; i++ {
			v := m.Pix[i*2] // high byte first, like a 16-bit sample >> 8
			out[i*4+0], out[i*4+1], out[i*4+2], out[i*4+3] = v, v, v, 255
		}
	default:
		// Everything else (YCbCr and CMYK JPEGs, premultiplied RGBA/RGBA64)
		// goes through At, which reports alpha-premultiplied samples; undo
		// the multiplication so the buffer stays straight like gdk-pixbuf's.
		for y := uint32(0); y < height; y++ {
			for x := uint32(0); x < width; x++ {
				r, g, b, a := src.At(boundsMinX(src)+int(x), boundsMinY(src)+int(y)).RGBA()
				o := (y*width + x) * 4
				out[o+0] = unpremultiply8(r, a)
				out[o+1] = unpremultiply8(g, a)
				out[o+2] = unpremultiply8(b, a)
				out[o+3] = unpremultiply8(a, 0xffff)
			}
		}
	}
	return out
}

func boundsMinX(src image.Image) int { return src.Bounds().Min.X }
func boundsMinY(src image.Image) int { return src.Bounds().Min.Y }

// unpremultiply8 turns one 16-bit alpha-premultiplied channel back into the
// straight 8-bit sample.
func unpremultiply8(value, alpha uint32) uint8 {
	if alpha == 0 {
		return 0
	}
	if alpha < 0xffff {
		value = (value*0xffff + alpha/2) / alpha
		if value > 0xffff {
			value = 0xffff
		}
	}
	return uint8(value >> 8)
}

// looksLikeSVG reports whether the body is SVG: XML starting with <svg>
// after the prolog. Detected before decoding because the standard decoders
// have no SVG loader and would report a generic unknown format instead of
// naming what is missing.
func looksLikeSVG(body []byte) bool {
	body = bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
	start := 0
	for start < len(body) && isASCIIWhitespace(body[start]) {
		start++
	}
	text := body[start:]
	if bytes.HasPrefix(text, []byte("<svg")) {
		return true
	}
	xmlProlog := bytes.HasPrefix(text, []byte("<?xml")) ||
		bytes.HasPrefix(text, []byte("<!--")) ||
		bytes.HasPrefix(text, []byte("<!DOCTYPE"))
	if !xmlProlog {
		return false
	}
	head := text
	if len(head) > 4096 {
		head = head[:4096]
	}
	return bytes.Contains(bytes.ToLower(head), []byte("<svg"))
}

func isASCIIWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', 0x0B, 0x0C:
		return true
	}
	return false
}

func isWebP(body []byte) bool {
	return len(body) >= 12 && bytes.Equal(body[0:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP"))
}

// ---------------------------------------------------------------------------
// Fetching
// ---------------------------------------------------------------------------

// fetch fetches one image body, honouring the size cap and timeout.
func fetch(url string, limits *Limits) ([]byte, error) {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		bytes, _, err := blnet.FetchBytesLimited(url, limits.MaxBytes, limits.Timeout)
		return bytes, err
	}
	path, ok := localPath(url)
	if !ok {
		return nil, fmt.Errorf("unsupported image URL %q", url)
	}
	return readCapped(path, limits.MaxBytes)
}

// localPath is the filesystem path behind an image URL, if there is one.
func localPath(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err == nil && u.Scheme != "" {
		// Another scheme (data:, about:, ...): not a resource we can read.
		if u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
			return "", false
		}
		return u.Path, true
	}
	// Not a URL: a plain filesystem path.
	return rawURL, true
}

// readCapped reads a file, rejecting (not truncating) anything over
// maxBytes, with the oracle's messages.
func readCapped(path string, maxBytes int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, ioError(err)
	}
	if info.IsDir() {
		return nil, errors.New("is a directory")
	}
	if info.Size() > int64(maxBytes) {
		return nil, fmt.Errorf("%d bytes exceeds the %d byte limit", info.Size(), maxBytes)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, ioError(err)
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("body exceeds the %d byte limit", maxBytes)
	}
	return body, nil
}

// ioError renders an OS failure the way the Rust oracle's io::Error
// displays ("No such file or directory (os error 2)"), because the text
// reaches the user through the browser-lite: warnings on stderr.
func ioError(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return fmt.Errorf("%s (os error %d)", upperASCII(errno.Error()), uint(errno))
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

// asciiEqualFold reports ASCII case-insensitive equality, like the oracle's
// eq_ignore_ascii_case (Unicode folds do not apply).
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
