package layout

import (
	"math"
	"strconv"
	"strings"

	"browser-lite/css"
	"browser-lite/dom"
	"browser-lite/img"
	"browser-lite/text"
)

// NoNode marks a box with no DOM node: the anonymous #text line boxes
// flush_text emits. It mirrors the oracle's usize::MAX sentinel
// (src/layout.rs:1610): the maximum int can never be a node id.
const NoNode = dom.NodeID(int(^uint(0) >> 1))

// LayoutBox is one painted box: a block element, a replaced <img> or one
// visual text line. It mirrors the oracle's LayoutBox field for field
// (src/layout.rs:28-61): the reported x/y/w/h keep their meaning with the
// border ring INSIDE, the content box shrinks by padding and borders.
type LayoutBox struct {
	// Node is the DOM node id, or NoNode for anonymous #text lines.
	Node dom.NodeID
	// Tag is the element tag, or "#text" for a visual line.
	Tag string
	// X, Y, W, H is the border box in CSS pixels.
	X, Y, W, H float64
	// Text is the line's text for #text boxes, empty otherwise.
	Text string
	// Color is the text colour.
	Color css.Color
	// Background is the element's background colour, nil when none.
	Background *css.Color
	// BackgroundImage holds the decoded background-image pixels, nil when
	// the element has no usable background image.
	BackgroundImage *img.Image
	// FontSize in CSS pixels; Bold mirrors FontWeight >= 600; Centered
	// mirrors text-align center.
	FontSize float64
	Bold     bool
	Centered bool
	// Image holds the decoded pixels for a replaced <img>, nil for
	// text/block boxes and for images that failed to load.
	Image *img.Image
	// BorderWidth/Style/Color per side (top, right, bottom, left).
	BorderWidth [4]float64
	BorderStyle [4]css.BorderKind
	BorderColor [4]css.Color
	// BorderRadius per corner (top-left, top-right, bottom-right,
	// bottom-left), as specified.
	BorderRadius [4]css.Radius
}

// TextRun is a text run as Pango sees it: the block's text plus the
// typography and the wrap width. Paint rebuilds the layout from this
// instead of re-wrapping (src/layout.rs:63-73).
type TextRun struct {
	X, Y  float64
	Width float64
	Text  string
	Style css.Style
}

// Layout is the laid-out page: boxes and runs plus the viewport width and
// the content height (floored at 1px like the oracle).
type Layout struct {
	Boxes  []LayoutBox
	Runs   []TextRun
	Width  float64
	Height float64
}

// forced is the size the layout dictates for a box, overriding what the
// cascade gives it (src/layout.rs:82-94). The flex algorithm fills width
// and height for its items (T4b); blockify lays a box out as a block even
// when its display is inline — a flex item is blockified (src/layout.rs:91-93).
type forced struct {
	// width overrides the border-box width.
	width *float64
	// height is a minimum border-box height; content may overflow it.
	height *float64
	// blockify lays the box out as a block even when its display is inline.
	blockify bool
}

// state threads the shaper, the images and the output through the flow.
type state struct {
	shaper *text.Shaper
	images *img.ImageSet
	out    *Layout
}

// Render lays out doc against an empty image set: pages without <img> or
// background images. It mirrors layout() (src/layout.rs:96-98).
func Render(doc *dom.Document, styles []css.Style, viewportWidth float64) *Layout {
	return RenderWithImages(doc, styles, viewportWidth, img.Empty())
}

// RenderWithImages lays out doc against an already loaded ImageSet, so the
// caller keeps the decoded images (and their warnings) for the rest of the
// run. It mirrors layout_with_images (src/layout.rs:118-154).
func RenderWithImages(doc *dom.Document, styles []css.Style, viewportWidth float64, images *img.ImageSet) *Layout {
	shaper, err := text.New()
	if err != nil {
		// The oracle unwraps ("cannot create a text measuring context"):
		// without Pango there is no text to lay out, so degrade to an
		// empty page rather than aborting the render.
		return &Layout{Width: viewportWidth, Height: 1}
	}
	defer shaper.Close()
	st := &state{shaper: shaper, images: images, out: &Layout{Width: viewportWidth}}
	var cursor float64
	for _, child := range doc.Nodes[doc.Root].Children {
		cursor += st.layoutNode(doc, styles, child, 0, cursor, viewportWidth, forced{})
	}
	st.out.Height = max(cursor, 1)
	return st.out
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// layoutNode lays out one node and returns the space it takes in the flow
// (border height plus vertical margins). It mirrors the block path of
// layout_node (src/layout.rs:156-303): display:none takes nothing, replaced
// <img> elements take their own box, inline boxes exist only inside the
// flush of their block container (unless blockified as a flex item), flex
// containers open the flex formatting context, and grid containers fall
// through to the plain block flow until T4c owns them.
func (st *state) layoutNode(doc *dom.Document, styles []css.Style, id dom.NodeID, originX, originY, availableWidth float64, f forced) float64 {
	style := &styles[id]
	if style.Display == css.DisplayNone {
		return 0
	}
	if isImage(doc, id) {
		return st.layoutImage(doc, styles, id, originX, originY, availableWidth, f)
	}
	if style.Display == css.DisplayInline && !f.blockify {
		return 0 // inline runs are flushed by their block container
	}
	if style.Flex != nil {
		return st.layoutFlexContainer(doc, styles, id, originX, originY, availableWidth, f)
	}
	// T4c: a grid container lays out as plain block for now.

	content := computeContentBox(style, originX, originY, availableWidth, f)
	borderX, borderW := content.borderX, content.borderW
	contentX, contentW := content.x, content.w

	borderY := originY + style.Margin[0]
	cursorY := content.y

	// Placeholder so children find a stable index; height filled after flow.
	boxIndex := len(st.out.Boxes)
	st.out.Boxes = append(st.out.Boxes, LayoutBox{
		Node:            id,
		Tag:             doc.Nodes[id].Tag,
		X:               borderX,
		Y:               borderY,
		W:               borderW,
		FontSize:        style.FontSize,
		Bold:            style.Bold,
		Centered:        style.InlineAlign,
		Color:           style.Color,
		Background:      style.Background,
		BackgroundImage: backgroundFor(style, st.images),
		BorderWidth:     style.BorderWidth,
		BorderStyle:     style.BorderStyle,
		BorderColor:     style.BorderColor,
		BorderRadius:    style.BorderRadius,
	})

	var run strings.Builder
	for _, child := range doc.Nodes[id].Children {
		childStyle := &styles[child]
		switch {
		case childStyle.Display == css.DisplayNone:
		case isImage(doc, child):
			// A replaced element takes its own line in this block flow,
			// so the text collected so far is flushed above it.
			cursorY += st.flushText(style, contentX, cursorY, contentW, &run)
			cursorY += st.layoutNode(doc, styles, child, contentX, cursorY, contentW, forced{})
		case childStyle.Display == css.DisplayBlock:
			cursorY += st.flushText(style, contentX, cursorY, contentW, &run)
			cursorY += st.layoutNode(doc, styles, child, contentX, cursorY, contentW, forced{})
		default:
			collectInlineText(doc, child, &run)
		}
	}
	cursorY += st.flushText(style, contentX, cursorY, contentW, &run)

	borderH := cursorY - borderY + style.Padding[2] + style.BorderWidth[2]
	if style.Height != nil {
		borderH = max(borderH, *style.Height)
	}
	if f.height != nil {
		borderH = max(borderH, *f.height)
	}
	st.out.Boxes[boxIndex].H = max(borderH, 0)

	return borderH + style.Margin[0] + style.Margin[2]
}

// contentBox is the box model of a block-level element: the rectangle its
// children (and a replaced image) live in. Exact port of content_box
// (src/layout.rs:320-346): margins offset the border box, the cascade width
// grows by padding only (borders live INSIDE w per the T11 decision), and
// the content width shrinks by padding and borders, floored at 1px.
type contentBox struct {
	// Left edge of the border box.
	borderX float64
	// Width of the border box.
	borderW float64
	// Top edge of the content box.
	y float64
	// Left edge of the content box.
	x float64
	// Width of the content box.
	w float64
}

func computeContentBox(style *css.Style, originX, originY, availableWidth float64, f forced) contentBox {
	borderX := originX + style.Margin[3]
	var borderW float64
	if f.width != nil {
		borderW = *f.width
	} else if style.Width != nil {
		borderW = *style.Width + style.Padding[1] + style.Padding[3]
	} else {
		borderW = max(availableWidth-style.Margin[1]-style.Margin[3], 1)
	}
	return contentBox{
		borderX: borderX,
		borderW: borderW,
		y:       originY + style.Margin[0] + style.BorderWidth[0] + style.Padding[0],
		x:       borderX + style.BorderWidth[3] + style.Padding[3],
		w:       max(borderW-style.Padding[1]-style.Padding[3]-style.BorderWidth[1]-style.BorderWidth[3], 1),
	}
}

func isImage(doc *dom.Document, id dom.NodeID) bool {
	node := &doc.Nodes[id]
	return !node.IsText && strings.EqualFold(node.Tag, "img")
}

// layoutImage lays out a replaced <img>: one box holding the decoded
// pixels, sized from its intrinsic dimensions (or the author's
// width/height) and never wider than the content box. Exact port of
// layout_image (src/layout.rs:1448-1496). Returns the space it takes in the
// flow (border height plus vertical margins — an image has no border or
// padding of its own here, so h plus margins).
func (st *state) layoutImage(doc *dom.Document, styles []css.Style, id dom.NodeID, originX, originY, availableWidth float64, f forced) float64 {
	style := &styles[id]
	content := computeContentBox(style, originX, originY, availableWidth, f)
	image := st.images.Image(id)
	w, h := imageSize(&doc.Nodes[id], style, image, content.w, forcedContentWidth(style, f))
	y := originY + style.Margin[0] + style.BorderWidth[0] + style.Padding[0]

	st.out.Boxes = append(st.out.Boxes, LayoutBox{
		Node:            id,
		Tag:             doc.Nodes[id].Tag,
		X:               content.x,
		Y:               y,
		W:               w,
		H:               h,
		FontSize:        style.FontSize,
		Bold:            style.Bold,
		Centered:        style.InlineAlign,
		Color:           style.Color,
		Background:      style.Background,
		BackgroundImage: backgroundFor(style, st.images),
		Image:           image,
		BorderWidth:     style.BorderWidth,
		BorderStyle:     style.BorderStyle,
		BorderColor:     style.BorderColor,
		BorderRadius:    style.BorderRadius,
	})
	return h + style.Margin[0] + style.Margin[2]
}

// forcedContentWidth is the content width the flex algorithm decided for an
// item (src/layout.rs:1531-1538, via image_size's forced_width): the border
// width minus padding. Without flex it is always nil.
func forcedContentWidth(style *css.Style, f forced) *float64 {
	if f.width == nil {
		return nil
	}
	w := max(*f.width-style.Padding[1]-style.Padding[3], 0)
	return &w
}

// imageSize is the painted size of an image box (src/layout.rs:1498-1546):
// CSS width/height win over the HTML width/height attributes, and both win
// over the intrinsic pixel size. A single dimension keeps the intrinsic
// aspect ratio; without pixels (failed or missing image) it stays 0. The
// result never exceeds the content box.
func imageSize(node *dom.Node, style *css.Style, image *img.Image, maxWidth float64, forcedWidth *float64) (float64, float64) {
	var intrinsicW, intrinsicH float64
	if image != nil {
		intrinsicW, intrinsicH = float64(image.Width), float64(image.Height)
	}
	widthHint := style.Width
	if widthHint == nil {
		if v, ok := node.Attr("width"); ok {
			widthHint = lengthPx(v)
		}
	}
	heightHint := style.Height
	if heightHint == nil {
		if v, ok := node.Attr("height"); ok {
			heightHint = lengthPx(v)
		}
	}
	var w, h float64
	switch {
	case widthHint != nil && heightHint != nil:
		w, h = *widthHint, *heightHint
	case widthHint != nil:
		w, h = *widthHint, scaleBy(intrinsicW, intrinsicH, *widthHint)
	case heightHint != nil:
		w, h = scaleBy(intrinsicH, intrinsicW, *heightHint), *heightHint
	default:
		w, h = intrinsicW, intrinsicH
	}
	if forcedWidth != nil {
		w = *forcedWidth
		if heightHint != nil {
			h = *heightHint
		} else {
			h = scaleBy(intrinsicW, intrinsicH, *forcedWidth)
		}
		return max(w, 0), max(h, 0)
	}
	if w > maxWidth {
		scale := maxWidth / w
		w = maxWidth
		h *= scale
	}
	return max(w, 0), max(h, 0)
}

// scaleBy is the missing side of an aspect ratio (src/layout.rs:1550-1556):
// known * wanted / basis. A zero basis (no pixels decoded) has no ratio.
func scaleBy(basis, other, wanted float64) float64 {
	if basis > 0 {
		return other * wanted / basis
	}
	return 0
}

// lengthPx parses an HTML length attribute (width="30", height="30px") in
// CSS pixels (src/layout.rs:1558-1569). Percentages and unparsable values
// are ignored: they do not size the box.
func lengthPx(value string) *float64 {
	v := strings.TrimSpace(value)
	if rest, ok := cutSuffix(v, "px"); ok {
		v = strings.TrimSpace(rest)
	} else if rest, ok := cutSuffix(v, "PX"); ok {
		v = strings.TrimSpace(rest)
	}
	px, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(px) || math.IsInf(px, 0) || px < 0 {
		return nil
	}
	out := px
	return &out
}

func cutSuffix(s, suffix string) (string, bool) {
	if strings.HasSuffix(s, suffix) {
		return s[:len(s)-len(suffix)], true
	}
	return s, false
}

// collectInlineText concatenates the inline descendants of a block into a
// single text run (src/layout.rs:1571-1584). Script, style, head and title
// subtrees never paint.
func collectInlineText(doc *dom.Document, id dom.NodeID, out *strings.Builder) {
	node := &doc.Nodes[id]
	if node.IsText {
		out.WriteString(node.Text)
		return
	}
	switch node.Tag {
	case "script", "style", "head", "title":
		return
	}
	for _, child := range node.Children {
		collectInlineText(doc, child, out)
	}
}

// flushText lays a run out with Pango inside width and pushes one #text box
// per visual line, plus the run itself for the painter. It mirrors
// flush_text (src/layout.rs:1588-1639): whitespace-normalised, empty runs
// emit nothing, line boxes carry the run's colour and typography with zero
// borders, and the returned run height is the measured Pango height.
//
// strings.Fields splits on Unicode White Space, matching Rust's
// split_whitespace on the White_Space property for every character the
// engine's fixtures use.
func (st *state) flushText(style *css.Style, x, y, width float64, run *strings.Builder) float64 {
	text := strings.Join(strings.Fields(run.String()), " ")
	run.Reset()
	if text == "" {
		return 0
	}
	measured := st.shaper.Measure(text, adaptStyle(style), width)
	for _, line := range measured.Lines {
		if line.Text == "" {
			continue
		}
		st.out.Boxes = append(st.out.Boxes, LayoutBox{
			Node:        NoNode,
			Tag:         "#text",
			X:           x + line.X,
			Y:           y + line.Y,
			W:           line.W,
			H:           line.H,
			Text:        line.Text,
			Color:       style.Color,
			FontSize:    style.FontSize,
			Bold:        style.Bold,
			BorderColor: [4]css.Color{style.Color, style.Color, style.Color, style.Color},
		})
	}
	st.out.Runs = append(st.out.Runs, TextRun{X: x, Y: y, Width: width, Text: text, Style: *style})
	return measured.Height
}

// adaptStyle projects the computed style onto the typography the shaper
// consumes. Field for field it follows what pango::create_layout reads
// (src/layout.rs:1844-1899).
func adaptStyle(style *css.Style) text.Style {
	align := text.AlignLeft
	switch style.TextAlign {
	case css.TextAlignCenter:
		align = text.AlignCenter
	case css.TextAlignRight:
		align = text.AlignRight
	case css.TextAlignJustify:
		align = text.AlignJustify
	}
	return text.Style{
		FontFamilies:  style.FontFamilies,
		FontFamily:    style.FontFamily,
		FontSize:      style.FontSize,
		FontWeight:    style.FontWeight,
		Italic:        style.Italic,
		LineHeight:    style.LineHeight,
		TextAlign:     align,
		LetterSpacing: style.LetterSpacing,
	}
}

// backgroundFor resolves the element's background-image against the loaded
// set, like the oracle's images.background(url) lookup in layout_node
// (src/layout.rs:221-224).
func backgroundFor(style *css.Style, images *img.ImageSet) *img.Image {
	if !style.HasBackgroundImage {
		return nil
	}
	return images.Background(style.BackgroundImage)
}

// bgStyle adapts one computed style to the background loader's interface.
type bgStyle struct{ css.Style }

// BackgroundImage reports the style's background-image URL.
func (s bgStyle) BackgroundImage() (string, bool) {
	if !s.Style.HasBackgroundImage {
		return "", false
	}
	return s.Style.BackgroundImage, true
}

// LoadImages fetches and decodes the page's <img> sources and
// background-image URLs against base (the final document URL for http(s)
// pages, the containing directory for local files), mirroring the image
// half of layout_with_subresources (src/layout.rs:100-114). Failures
// degrade to empty boxes and warnings; they never abort the render.
func LoadImages(doc *dom.Document, styles []css.Style, base string) *img.ImageSet {
	set := img.Load(doc, base)
	bg := make([]img.BackgroundStyle, 0, len(styles))
	for _, s := range styles {
		bg = append(bg, bgStyle{s})
	}
	return set.LoadBackgroundImages(bg, base)
}
