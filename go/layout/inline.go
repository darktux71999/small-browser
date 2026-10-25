package layout

// Inline formatting of atomic boxes (CSS 2.1 §9.2.2 inline-level boxes, §10.8
// inline replaced elements, §10.3.9 inline-block widths).
//
// A block container's inline content is one run of text pieces and atomic
// inline boxes in document order. An atomic box is an inline replaced element
// (an <img>, or an inline <svg>) or an inline-block box. Each atom sits on the
// line's baseline: its bottom margin edge is the baseline, so a 16px icon in
// "Hi <svg/> there" occupies the 16px above the text baseline, and its
// painted box shares the line with the text around it. Runs without atoms
// keep the single-text-run path (flushText) unchanged.

import (
	"strings"
	"unicode"

	"browser-lite/css"
	"browser-lite/dom"
)

// atomMarker stands in for one atomic box in inlineRun.String, so a run that
// holds only an atom is non-blank to the margin-collapse rules.
const atomMarker = "\uFFFC"

// inlineRun is the inline content collected between a block container's
// block-level children: text pieces and atomic boxes in document order.
type inlineRun struct {
	items []inlineItem
}

// inlineItem is one text piece or one atomic box of an inlineRun.
type inlineItem struct {
	text   string
	atom   dom.NodeID
	isAtom bool
}

// String is the run's content with each atom as a non-space marker. It
// serves the blank checks of the margin-collapse rules.
func (r *inlineRun) String() string {
	var b strings.Builder
	for _, it := range r.items {
		if it.isAtom {
			b.WriteString(atomMarker)
		} else {
			b.WriteString(it.text)
		}
	}
	return b.String()
}

// hasAtoms reports whether the run holds any atomic box.
func (r *inlineRun) hasAtoms() bool {
	for _, it := range r.items {
		if it.isAtom {
			return true
		}
	}
	return false
}

// plainText concatenates the text pieces of the run (atoms contribute
// nothing), the input of the single-text-run path.
func (r *inlineRun) plainText() string {
	var b strings.Builder
	for _, it := range r.items {
		if !it.isAtom {
			b.WriteString(it.text)
		}
	}
	return b.String()
}

// collectInline is collectInlineText for block flow: it gathers the same text
// and also records every atomic box (inline <svg>, <img> nested in inline
// content, inline-block) in document order. An atomic box is never flattened,
// so its own subtree is laid out by its own box, not by the text run.
func collectInline(doc *dom.Document, styles []css.Style, id dom.NodeID, run *inlineRun) {
	node := &doc.Nodes[id]
	if node.IsText {
		run.items = append(run.items, inlineItem{text: node.Text})
		return
	}
	if isNoscript(doc, id) {
		return
	}
	switch node.Tag {
	case "script", "style", "head", "title":
		return
	}
	if isInlineAtom(doc, styles, id) {
		run.items = append(run.items, inlineItem{atom: id, isAtom: true})
		return
	}
	for _, child := range node.Children {
		collectInline(doc, styles, child, run)
	}
}

// isInlineAtom reports whether the element lays out as an atomic inline box:
// a replaced element (<img>, <svg>) or an inline-block, unless display:none.
func isInlineAtom(doc *dom.Document, styles []css.Style, id dom.NodeID) bool {
	style := &styles[id]
	if style.Display == css.DisplayNone {
		return false
	}
	return isImage(doc, id) || isSVG(doc, id) || style.InlineBlock
}

// isSVG reports an inline <svg> element (a replaced element in this engine).
func isSVG(doc *dom.Document, id dom.NodeID) bool {
	node := &doc.Nodes[id]
	return !node.IsText && strings.EqualFold(node.Tag, "svg")
}

// lineAscent is the baseline offset inside a text line box of height lineH:
// the half-leading plus the ascent. The text package exposes no font metrics,
// so the ascent is taken as 0.8em and the descent as 0.2em, which centres the
// em box in the line. Text and atomic boxes on one line share this one value.
func lineAscent(lineH, fontSize float64) float64 {
	return lineH/2 + 0.3*fontSize
}

// inlineAtom is one atomic inline box, measured before line breaking. Its
// margin box is ml+w+mr wide and mt+h+mb tall; the border box sits between
// the margins, its bottom margin edge on the baseline (CSS 2.1 §10.8.1).
type inlineAtom struct {
	id dom.NodeID
	// w and h are the border-box size; ml, mr, mt, mb the margins.
	w, h           float64
	ml, mr, mt, mb float64
	// block marks an inline-block, whose boxes were laid out at the origin
	// and occupy boxes[boxStart:boxEnd] and runs[runStart:runEnd]; placement
	// shifts that range. A replaced atom is emitted at placement instead.
	block            bool
	boxStart, boxEnd int
	runStart, runEnd int
}

// inlineTokKind classifies the tokens of an inline run.
type inlineTokKind uint8

const (
	tokWord inlineTokKind = iota
	tokSpace
	tokAtom
)

// inlineTok is one word, collapsed inter-word space, or atom of a run.
type inlineTok struct {
	kind inlineTokKind
	text string
	// w is the advance the token takes on its line (margin box for atoms).
	w    float64
	atom *inlineAtom
}

// inlinePiece is one placed piece of a line: a text run or an atom.
type inlinePiece struct {
	text string
	atom *inlineAtom
	w, h float64
}

// flushInline lays out a block container's inline run at (x, y) inside width
// and returns the height it takes. A run without atoms is exactly the old
// single text run (flushText); with atoms the run is broken into lines of
// words and atoms, every line sharing one baseline per atom.
func (st *state) flushInline(doc *dom.Document, styles []css.Style, style *css.Style, x, y, width float64, run *inlineRun) float64 {
	items := run.items
	run.items = nil
	if !hasAtomItem(items) {
		var text strings.Builder
		for _, it := range items {
			text.WriteString(it.text)
		}
		return st.flushText(style, x, y, width, &text)
	}
	toks := st.tokenizeInline(doc, styles, style, items, width)
	space := st.spaceWidth(style)
	wrapW := width
	if style.WhiteSpaceNowrap {
		wrapW = nowrapWidth
	}
	var total float64
	for _, line := range wrapInline(toks, space, wrapW) {
		total += st.placeLine(doc, styles, style, x, y+total, width, line)
	}
	return total
}

// hasAtomItem reports whether any item of the run is an atomic box.
func hasAtomItem(items []inlineItem) bool {
	for _, it := range items {
		if it.isAtom {
			return true
		}
	}
	return false
}

// tokenizeInline splits the run into words, collapsed spaces and atoms. Text
// that touches with no space between (across inline elements) stays one word.
// Leading spaces are dropped; the atoms are measured (and inline-blocks laid
// out) here, so wrapping sees their final sizes.
func (st *state) tokenizeInline(doc *dom.Document, styles []css.Style, style *css.Style, items []inlineItem, width float64) []inlineTok {
	var toks []inlineTok
	var word strings.Builder
	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		w, _ := st.measureInline(style, word.String())
		toks = append(toks, inlineTok{kind: tokWord, text: word.String(), w: w})
		word.Reset()
	}
	for _, it := range items {
		if it.isAtom {
			flushWord()
			a := st.prepareAtom(doc, styles, it.atom, width)
			toks = append(toks, inlineTok{kind: tokAtom, w: a.ml + a.w + a.mr, atom: a})
			continue
		}
		for _, r := range it.text {
			if !unicode.IsSpace(r) {
				word.WriteRune(r)
				continue
			}
			flushWord()
			if len(toks) > 0 && toks[len(toks)-1].kind != tokSpace {
				toks = append(toks, inlineTok{kind: tokSpace})
			}
		}
	}
	flushWord()
	return toks
}

// spaceWidth is the advance of one inter-word space in the style's font: the
// difference between "a a" and "aa", which isolates the space glyph.
func (st *state) spaceWidth(style *css.Style) float64 {
	spaced, _ := st.measureInline(style, "a a")
	joined, _ := st.measureInline(style, "aa")
	if spaced <= joined {
		return 0
	}
	return spaced - joined
}

// wrapInline breaks the tokens greedily into lines no wider than width (an
// over-wide token stands alone on its line). A space token is kept only
// between two items of one line, so no line starts or ends with a space.
func wrapInline(toks []inlineTok, space, width float64) [][]inlineTok {
	var lines [][]inlineTok
	var cur []inlineTok
	var curW float64
	pending := false
	for _, t := range toks {
		if t.kind == tokSpace {
			pending = true
			continue
		}
		gap := 0.0
		if pending && len(cur) > 0 {
			gap = space
		}
		if len(cur) > 0 && curW+gap+t.w > width {
			lines = append(lines, cur)
			cur, curW, gap = nil, 0, 0
		}
		if gap > 0 {
			cur = append(cur, inlineTok{kind: tokSpace})
		}
		cur = append(cur, t)
		curW += gap + t.w
		pending = false
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// placeLine measures and emits one line of tokens whose top is y, and returns
// the line height. Text pieces are measured exactly; the line's baseline sits
// at asc below its top, where asc is the largest ascent among its text (the
// line's baseline offset) and its atoms (their margin-box heights).
func (st *state) placeLine(doc *dom.Document, styles []css.Style, style *css.Style, x, y, width float64, line []inlineTok) float64 {
	var pieces []inlinePiece
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		w, h := st.measureInline(style, text.String())
		pieces = append(pieces, inlinePiece{text: text.String(), w: w, h: h})
		text.Reset()
	}
	for _, t := range line {
		switch t.kind {
		case tokSpace:
			text.WriteByte(' ')
		case tokWord:
			text.WriteString(t.text)
		case tokAtom:
			flush()
			pieces = append(pieces, inlinePiece{atom: t.atom, w: t.w})
		}
	}
	flush()

	var lineW, textH float64
	hasText := false
	for _, p := range pieces {
		lineW += p.w
		if p.atom == nil {
			hasText = true
			textH = max(textH, p.h)
		}
	}
	var asc, desc float64
	textAsc := 0.0
	if hasText {
		textAsc = lineAscent(textH, style.FontSize)
		asc, desc = textAsc, textH-textAsc
	}
	for _, p := range pieces {
		if p.atom != nil {
			asc = max(asc, p.atom.mt+p.atom.h+p.atom.mb)
		}
	}
	baseline := y + asc

	cursor := x + alignOffset(style, width, lineW)
	for _, p := range pieces {
		if p.atom == nil {
			st.out.Boxes = append(st.out.Boxes, LayoutBox{
				Node:        NoNode,
				Tag:         "#text",
				X:           cursor,
				Y:           baseline - textAsc,
				W:           p.w,
				H:           textH,
				Text:        p.text,
				Color:       style.Color,
				FontSize:    style.FontSize,
				Bold:        style.Bold,
				BorderColor: [4]css.Color{style.Color, style.Color, style.Color, style.Color},
			})
			// The +1px keeps float dust from re-wrapping the piece when the
			// painter re-lays it out, as the nowrap runs do.
			st.out.Runs = append(st.out.Runs, TextRun{X: cursor, Y: baseline - textAsc, Width: p.w + 1, Text: p.text, Style: *style})
			cursor += p.w
			continue
		}
		a := p.atom
		cursor += a.ml
		st.placeAtom(doc, styles, a, cursor, baseline-a.mb-a.h)
		cursor += a.w + a.mr
	}
	return asc + desc
}

// alignOffset is the horizontal start of a line of width lineW inside width,
// from text-align (center and right; left and justify start at the edge).
func alignOffset(style *css.Style, width, lineW float64) float64 {
	free := max(width-lineW, 0)
	switch style.TextAlign {
	case css.TextAlignCenter:
		return free / 2
	case css.TextAlignRight:
		return free
	}
	return 0
}

// measureInline measures text on one unwrapped line: its advance and the
// line height Pango reports for the style.
func (st *state) measureInline(style *css.Style, text string) (w, h float64) {
	m := st.shaper.Measure(text, adaptStyle(style), nowrapWidth)
	for _, line := range m.Lines {
		w = max(w, line.W)
	}
	return w, m.Height
}

// prepareAtom measures one atomic box. A replaced element takes its decoded
// size (imageSize, clamped to width); an inline-block is laid out now at the
// origin, its shrink-to-fit width taken from its own content (CSS 2.1 §10.3.9:
// the content's natural width, capped by the available width), and its boxes
// kept in place for placeAtom to move.
func (st *state) prepareAtom(doc *dom.Document, styles []css.Style, id dom.NodeID, width float64) *inlineAtom {
	style := &styles[id]
	a := &inlineAtom{id: id, ml: style.Margin[3], mr: style.Margin[1], mt: style.Margin[0], mb: style.Margin[2]}
	if style.InlineBlock && !isImage(doc, id) && !isSVG(doc, id) {
		a.block = true
		a.boxStart, a.runStart = len(st.out.Boxes), len(st.out.Runs)
		a.w, a.h = st.layoutInlineBlock(doc, styles, id, width)
		a.boxEnd, a.runEnd = len(st.out.Boxes), len(st.out.Runs)
		return a
	}
	a.w, a.h = imageSize(&doc.Nodes[id], style, st.images.Image(id), width, nil)
	return a
}

// layoutInlineBlock lays out an inline-block's box at the origin and returns
// its border-box size. With an authored width the box keeps it; otherwise the
// box is first laid out unwrapped to measure its natural content width, the
// measuring boxes are dropped, and the box is laid out again at
// min(natural, available). Only text and replaced content count as natural
// width: block children fill the probe width and would hide it.
func (st *state) layoutInlineBlock(doc *dom.Document, styles []css.Style, id dom.NodeID, avail float64) (w, h float64) {
	style := &styles[id]
	boxStart, runStart := len(st.out.Boxes), len(st.out.Runs)
	if style.Width == nil {
		st.layoutBlock(doc, styles, id, 0, 0, nowrapWidth, forced{}, false)
		root := st.out.Boxes[boxStart]
		var natural float64
		for _, b := range st.out.Boxes[boxStart+1:] {
			if b.Tag == "#text" || b.Image != nil {
				natural = max(natural, b.X+b.W-root.X)
			}
		}
		st.out.Boxes = st.out.Boxes[:boxStart]
		st.out.Runs = st.out.Runs[:runStart]
		fitted := min(natural+style.Padding[1]+style.BorderWidth[1], max(avail, 0))
		st.layoutBlock(doc, styles, id, 0, 0, avail, forced{width: &fitted}, false)
	} else {
		st.layoutBlock(doc, styles, id, 0, 0, avail, forced{}, false)
	}
	root := st.out.Boxes[boxStart]
	return root.W, root.H
}

// placeAtom puts an atom's border box at (x, y): a replaced element is emitted
// there, an inline-block's laid-out range is moved so its box lands there.
func (st *state) placeAtom(doc *dom.Document, styles []css.Style, a *inlineAtom, x, y float64) {
	if a.block {
		root := st.out.Boxes[a.boxStart]
		st.shiftRange(a.boxStart, a.boxEnd, a.runStart, a.runEnd, x-root.X, y-root.Y)
		return
	}
	st.emitImage(doc, styles, a.id, x, y, a.w, a.h)
}
