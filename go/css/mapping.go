package css

/*
#cgo pkg-config: libcss libwapcaplet
#include "glue.h"
*/
import "C"

import "unsafe"

type lengthGetter func(style *C.css_computed_style, length *C.css_fixed, unit *C.css_unit) C.uint8_t

type colorGetter func(style *C.css_computed_style, color *C.css_color) C.uint8_t

type styleGetter func(style *C.css_computed_style) C.uint8_t

func getWidth(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_width(s, l, u)
}

func getHeight(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_height(s, l, u)
}

func getMarginTop(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_margin_top(s, l, u)
}

func getMarginRight(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_margin_right(s, l, u)
}

func getMarginBottom(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_margin_bottom(s, l, u)
}

func getMarginLeft(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_margin_left(s, l, u)
}

func getPaddingTop(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_padding_top(s, l, u)
}

func getPaddingRight(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_padding_right(s, l, u)
}

func getPaddingBottom(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_padding_bottom(s, l, u)
}

func getPaddingLeft(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_padding_left(s, l, u)
}

func getTop(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_top(s, l, u)
}

func getRight(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_right(s, l, u)
}

func getBottom(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_bottom(s, l, u)
}

func getLeft(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_left(s, l, u)
}

func getMinWidth(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_min_width(s, l, u)
}

func getMaxWidth(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_max_width(s, l, u)
}

func getColumnGap(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_column_gap(s, l, u)
}

func getFontSize(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_font_size(s, l, u)
}

func getLetterSpacing(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_letter_spacing(s, l, u)
}

func getBorderTopWidth(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_border_top_width(s, l, u)
}

func getBorderRightWidth(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_border_right_width(s, l, u)
}

func getBorderBottomWidth(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_border_bottom_width(s, l, u)
}

func getBorderLeftWidth(s *C.css_computed_style, l *C.css_fixed, u *C.css_unit) C.uint8_t {
	return C.css_computed_border_left_width(s, l, u)
}

func getBorderTopColor(s *C.css_computed_style, c *C.css_color) C.uint8_t {
	return C.css_computed_border_top_color(s, c)
}

func getBorderRightColor(s *C.css_computed_style, c *C.css_color) C.uint8_t {
	return C.css_computed_border_right_color(s, c)
}

func getBorderBottomColor(s *C.css_computed_style, c *C.css_color) C.uint8_t {
	return C.css_computed_border_bottom_color(s, c)
}

func getBorderLeftColor(s *C.css_computed_style, c *C.css_color) C.uint8_t {
	return C.css_computed_border_left_color(s, c)
}

func getBorderTopStyle(s *C.css_computed_style) C.uint8_t {
	return C.css_computed_border_top_style(s)
}

func getBorderRightStyle(s *C.css_computed_style) C.uint8_t {
	return C.css_computed_border_right_style(s)
}

func getBorderBottomStyle(s *C.css_computed_style) C.uint8_t {
	return C.css_computed_border_bottom_style(s)
}

func getBorderLeftStyle(s *C.css_computed_style) C.uint8_t {
	return C.css_computed_border_left_style(s)
}

// lengthPx returns an absolute length in CSS pixels; (0, false) for auto,
// inherit and percentages (no containing block here to resolve those
// against, same as css.rs).
func lengthPx(style *C.css_computed_style, unitCtx *C.css_unit_ctx, get lengthGetter, setStatus uint8) (float64, bool) {
	var length C.css_fixed
	var unit C.css_unit
	if status := get(style, &length, &unit); status != C.uint8_t(setStatus) || uint32(unit) == cssUnitPct {
		return 0, false
	}
	return fixedToF64(C.css_unit_len2css_px(style, unitCtx, length, C.css_unit(unit))), true
}

// positionInset keeps percentage insets as percentages and converts em/rem
// and other absolute units through libcss' shared CSS-pixel helper.
func positionInset(style *C.css_computed_style, unitCtx *C.css_unit_ctx, get lengthGetter) *PositionInset {
	var length C.css_fixed
	var unit C.css_unit
	if get(style, &length, &unit) != C.uint8_t(cssInsetSet) {
		return nil
	}
	if uint32(unit) == cssUnitPct {
		return &PositionInset{Percent: true, Value: fixedToF64(length)}
	}
	return &PositionInset{Value: fixedToF64(C.css_unit_len2css_px(style, unitCtx, length, unit))}
}

// backgroundPosition maps one computed background-position axis (CSS 2.1
// §8.14.1): a percentage unit keeps the percentage number — the form the
// left/center/right and top/center/bottom keywords compute to — and an
// absolute length converts to CSS pixels.
func backgroundPosition(length C.css_fixed, unit C.css_unit,
	style *C.css_computed_style, unitCtx *C.css_unit_ctx) BackgroundPosition {
	if uint32(unit) == cssUnitPct {
		return BackgroundPosition{Percent: true, Value: fixedToF64(length)}
	}
	return BackgroundPosition{
		Value: fixedToF64(C.css_unit_len2css_px(style, unitCtx, length, unit)),
	}
}

// borderWidthPx returns one border-*-width in CSS pixels: computed lengths
// convert like any other length; the thin/medium/thick keywords map to the
// 1/3/5px the CSS spec suggests.
func borderWidthPx(style *C.css_computed_style, unitCtx *C.css_unit_ctx, get lengthGetter) float64 {
	var length C.css_fixed
	var unit C.css_unit
	switch get(style, &length, &unit) {
	case C.uint8_t(cssBorderWidthWidth):
		if uint32(unit) == cssUnitPct {
			return 0.0
		}
		px := fixedToF64(C.css_unit_len2css_px(style, unitCtx, length, C.css_unit(unit)))
		if px < 0 {
			return 0
		}
		return px
	case C.uint8_t(cssBorderWidthThin):
		return 1.0
	case C.uint8_t(cssBorderWidthMedium):
		return 3.0
	case C.uint8_t(cssBorderWidthThick):
		return 5.0
	default:
		return 0.0
	}
}

// The per-side border getters, shared by mapStyle's full read and the
// var() border pass's masked read (bordervar.go): one place where libcss'
// computed borders become Style fields.
var (
	borderWidthGetters = [4]lengthGetter{getBorderTopWidth, getBorderRightWidth, getBorderBottomWidth, getBorderLeftWidth}
	borderColorGetters = [4]colorGetter{getBorderTopColor, getBorderRightColor, getBorderBottomColor, getBorderLeftColor}
	borderStyleGetters = [4]styleGetter{getBorderTopStyle, getBorderRightStyle, getBorderBottomStyle, getBorderLeftStyle}
)

// allBorderSides masks every side of every border slot.
var allBorderSides = [4]bool{true, true, true, true}

// readComputedBorders copies the border values of one libcss computed
// style into dst, overwriting only the slots the three masks select.
// textColor answers the currentColor status exactly like mapStyle's
// out.Color did at that point (the inherited value — css_computed_color
// runs below it). This is the single reader of the libcss border
// expansion: the var() pass replays resolved border declarations through
// libcss' own select and lands here, so the shorthand expansion, the
// per-side mapping and the keyword widths never live twice.
func readComputedBorders(style *C.css_computed_style, unitCtx *C.css_unit_ctx, textColor Color,
	widths, styles, colors [4]bool, dst *Style) {
	for i, get := range borderWidthGetters {
		if widths[i] {
			dst.BorderWidth[i] = borderWidthPx(style, unitCtx, get)
		}
	}
	for i, get := range borderColorGetters {
		if !colors[i] {
			continue
		}
		var color C.css_color
		switch uint8(get(style, &color)) {
		case cssBorderColorColor:
			dst.BorderColor[i] = rgba(color)
		case cssBorderColorCurrentColor:
			// The current-oracle quirk: in mapStyle, textColor is still
			// the inherited value at this point (css_computed_color runs
			// below).
			dst.BorderColor[i] = textColor
		default:
			// Anything else unresolved: the text colour.
			dst.BorderColor[i] = textColor
		}
	}
	for i, get := range borderStyleGetters {
		if !styles[i] {
			continue
		}
		switch uint8(get(style)) {
		case cssBorderStyleNone, cssBorderStyleHidden:
			dst.BorderStyle[i] = BorderNone
		default:
			// dashed/dotted/double/... degrade to solid (see BorderKind).
			dst.BorderStyle[i] = BorderSolid
		}
	}
}

// rgba converts a libcss css_color (AARRGGBB) into the engine's Color.
func rgba(c C.css_color) Color {
	return Color{
		A: float64((uint32(c)>>24)&0xff) / 255.0,
		R: float64((uint32(c)>>16)&0xff) / 255.0,
		G: float64((uint32(c)>>8)&0xff) / 255.0,
		B: float64(uint32(c)&0xff) / 255.0,
	}
}

// flexStyle reads the flex container properties of a computed style. Row-gap
// travels in the min-width carrier; column-gap is computed natively by libcss.
func flexStyle(style *C.css_computed_style, unitCtx *C.css_unit_ctx) *FlexStyle {
	flex := &FlexStyle{}
	switch uint8(C.css_computed_flex_direction(style)) {
	case cssFlexDirectionColumn, cssFlexDirectionColumnReverse:
		flex.Direction = FlexDirectionColumn
	default:
		flex.Direction = FlexDirectionRow
	}
	switch uint8(C.css_computed_flex_wrap(style)) {
	case cssFlexWrapWrap, cssFlexWrapWrapReverse:
		flex.Wrap = FlexWrapWrap
	default:
		flex.Wrap = FlexWrapNoWrap
	}
	switch uint8(C.css_computed_justify_content(style)) {
	case cssJustifyContentCenter:
		flex.Justify = JustifyCenter
	case cssJustifyContentFlexEnd:
		flex.Justify = JustifyFlexEnd
	case cssJustifyContentSpaceBetween:
		flex.Justify = JustifySpaceBetween
	case cssJustifyContentSpaceAround:
		flex.Justify = JustifySpaceAround
	case cssJustifyContentSpaceEvenly:
		flex.Justify = JustifySpaceEvenly
	default:
		flex.Justify = JustifyFlexStart
	}
	switch uint8(C.css_computed_align_items(style)) {
	case cssAlignItemsFlexStart:
		flex.Align = AlignItemsFlexStart
	case cssAlignItemsCenter:
		flex.Align = AlignItemsCenter
	case cssAlignItemsFlexEnd:
		flex.Align = AlignItemsFlexEnd
	// baseline is not part of the minimal flexbox.
	case cssAlignItemsBaseline:
		flex.Align = AlignItemsFlexStart
	default:
		flex.Align = AlignItemsStretch
	}
	flex.RowGap, _ = lengthPx(style, unitCtx, getMinWidth, cssMinWidthSet)
	flex.ColumnGap, _ = lengthPx(style, unitCtx, getColumnGap, cssColumnGapSet)
	return flex
}

// flexItem reads the element's own flex item properties: flex-grow,
// flex-shrink and flex-basis.
func flexItem(style *C.css_computed_style, unitCtx *C.css_unit_ctx) FlexItemProps {
	var number C.css_fixed
	grow := 0.0
	if C.css_computed_flex_grow(style, &number) == C.uint8_t(cssFlexGrowSet) {
		grow = fixedToF64(number)
	}
	shrink := 1.0
	if C.css_computed_flex_shrink(style, &number) == C.uint8_t(cssFlexShrinkSet) {
		shrink = fixedToF64(number)
	}

	var length C.css_fixed
	var unit C.css_unit
	basis := FlexBasis{Kind: BasisAuto}
	switch uint8(C.css_computed_flex_basis(style, &length, &unit)) {
	case cssFlexBasisSet:
		if uint32(unit) == cssUnitPct {
			basis = FlexBasis{Kind: BasisPercent, Value: fixedToF64(length)}
		} else {
			basis = FlexBasis{
				Kind:  BasisPx,
				Value: fixedToF64(C.css_unit_len2css_px(style, unitCtx, length, C.css_unit(unit))),
			}
		}
	case cssFlexBasisContent:
		basis = FlexBasis{Kind: BasisContent}
	default:
		basis = FlexBasis{Kind: BasisAuto}
	}

	return FlexItemProps{Grow: grow, Shrink: shrink, Basis: basis}
}

// maxFamilies caps the font-family entries read from one computed style; a
// sane ceiling against a malformed array (a real stack is under a dozen).
const maxFamilies = 64

// familyNames copies the named families out of libcss' NULL-terminated
// array, in author order. The strings are copied out: the computed style is
// destroyed again by the time the Style values reach layout.
func familyNames(families **C.lwc_string) []string {
	var out []string
	if families == nil {
		return out
	}
	slice := (*[maxFamilies]*C.lwc_string)(unsafe.Pointer(families))
	for i := 0; i < maxFamilies; i++ {
		entry := slice[i]
		if entry == nil {
			break
		}
		out = append(out, lwcBytes(entry))
	}
	return out
}

// familyStack builds the full family stack: the named families plus the
// generic keyword the declaration ends with (libcss stores it separately
// from the names), or the sans default when there is none. A generic
// already inside the names is not repeated.
func familyStack(names []string, familyType uint8) []string {
	keyword := ""
	switch familyType {
	case cssFontFamilySerif:
		keyword = "serif"
	case cssFontFamilySansSerif:
		keyword = "sans-serif"
	case cssFontFamilyCursive:
		keyword = "cursive"
	case cssFontFamilyFantasy:
		keyword = "fantasy"
	case cssFontFamilyMonospace:
		keyword = "monospace"
	}
	stack := names
	if keyword != "" {
		duplicate := false
		for _, name := range stack {
			if asciiEqualFold(name, keyword) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			stack = append(stack, keyword)
		}
	}
	if len(stack) == 0 {
		stack = []string{"sans-serif"}
	}
	return stack
}

// mapStyle copies one libcss computed style into the engine's Style,
// mirroring map_style of css_libcss.rs.
func mapStyle(style *C.css_computed_style, unitCtx *C.css_unit_ctx, isRoot bool) Style {
	out := New()

	display := uint8(C.css_computed_display(style, C.bool(isRoot)))
	switch display {
	case cssDisplayNone:
		out.Display = DisplayNone
	case cssDisplayBlock, cssDisplayListItem, cssDisplayTable,
		cssDisplayGrid, cssDisplayInlineGrid:
		out.Display = DisplayBlock
	case cssDisplayFlex, cssDisplayInlineFlex:
		// A flex container is a block-level box: the engine has no
		// DisplayFlex variant, so the container keeps DisplayBlock and
		// carries its flex properties in Style.Flex.
		out.Display = DisplayBlock
		out.Flex = flexStyle(style, unitCtx)
	default:
		// Inline-level displays and the resolved CSS_DISPLAY_INHERIT stay
		// inline runs.
		out.Display = DisplayInline
	}
	if display == cssDisplayGrid || display == cssDisplayInlineGrid {
		// Like flex, a grid container keeps DisplayBlock and is marked
		// here; the track list arrives in applyGridStyles, which reads the
		// properties libcss does not know.
		out.Display = DisplayBlock
		out.Grid = &GridStyle{}
	}
	position := uint8(C.css_computed_position(style))
	out.PositionRelative = position == cssPositionRelative
	out.PositionAbsolute = position == cssPositionAbsolute
	out.DirectionRTL = uint8(C.css_computed_direction(style)) == cssDirectionRTL
	if out.PositionRelative {
		if value, ok := lengthPx(style, unitCtx, getTop, cssInsetSet); ok {
			out.RelativeTop = &value
		}
		if value, ok := lengthPx(style, unitCtx, getRight, cssInsetSet); ok {
			out.RelativeRight = &value
		}
		if value, ok := lengthPx(style, unitCtx, getBottom, cssInsetSet); ok {
			out.RelativeBottom = &value
		}
		if value, ok := lengthPx(style, unitCtx, getLeft, cssInsetSet); ok {
			out.RelativeLeft = &value
		}
	}
	if out.PositionAbsolute {
		out.AbsoluteTop = positionInset(style, unitCtx, getTop)
		out.AbsoluteRight = positionInset(style, unitCtx, getRight)
		out.AbsoluteBottom = positionInset(style, unitCtx, getBottom)
		out.AbsoluteLeft = positionInset(style, unitCtx, getLeft)
	}
	out.GridItem = GridItemProps{ColSpan: 1, RowSpan: 1}
	// Row gap travels in the min-width carrier for every element, not just
	// flex containers: grid reuses the same value instead of re-parsing gap.
	out.RowGap, _ = lengthPx(style, unitCtx, getMinWidth, cssMinWidthSet)
	out.ColumnGap, _ = lengthPx(style, unitCtx, getColumnGap, cssColumnGapSet)
	out.FlexItem = flexItem(style, unitCtx)

	if width, ok := lengthPx(style, unitCtx, getWidth, cssWidthSet); ok {
		out.Width = &width
	}
	if height, ok := lengthPx(style, unitCtx, getHeight, cssHeightSet); ok {
		out.Height = &height
	}
	out.BorderBox = uint8(C.css_computed_box_sizing(style)) == cssBoxSizingBorder
	if maxWidth, ok := lengthPx(style, unitCtx, getMaxWidth, cssMaxWidthSet); ok {
		out.MaxWidth = &maxWidth
	}

	margins := [4]lengthGetter{getMarginTop, getMarginRight, getMarginBottom, getMarginLeft}
	for i, get := range margins {
		var length C.css_fixed
		var unit C.css_unit
		out.MarginAuto[i] = get(style, &length, &unit) == C.uint8_t(cssMarginAuto)
		value, ok := lengthPx(style, unitCtx, get, cssMarginSet)
		if !ok {
			value = 0
		}
		out.Margin[i] = value
	}
	paddings := [4]lengthGetter{getPaddingTop, getPaddingRight, getPaddingBottom, getPaddingLeft}
	for i, get := range paddings {
		value, ok := lengthPx(style, unitCtx, get, cssPaddingSet)
		if !ok {
			value = 0
		}
		out.Padding[i] = value
	}

	// Borders (S10): libcss computes the cascade for width/style/colour, so
	// shorthands, longhands, currentColor and CSS-wide keywords all follow
	// its computed values -- nothing is re-parsed here. border-radius has
	// no computed getter in libcss 0.9.2; it is read from the author sheets
	// by applyBorderRadiusStyles after the cascade. The same reader serves
	// the var() border replay (bordervar.go): the expansion lives once.
	//
	// The currentColor branch reads out.Color before css_computed_color
	// below has run: it still carries the inherited value, exactly like the
	// oracle (readComputedBorders keeps that quirk).
	readComputedBorders(style, unitCtx, out.Color, allBorderSides, allBorderSides, allBorderSides, &out)
	// A used width of zero where no border paints: like browsers, `none`
	// takes no space even when libcss reports its medium initial width.
	for i := 0; i < 4; i++ {
		if out.BorderStyle[i] == BorderNone {
			out.BorderWidth[i] = 0.0
		}
	}

	var color C.css_color
	if C.css_computed_color(style, &color) == C.uint8_t(cssColorColor) {
		out.Color = rgba(color)
	}

	var background C.css_color
	switch uint8(C.css_computed_background_color(style, &background)) {
	case cssBackgroundColorColor:
		c := rgba(background)
		if c.A > 0 {
			out.Background = &c
		}
	case cssBackgroundColorCurrentColor:
		inherited := out.Color
		out.Background = &inherited
	default:
		out.Background = nil
	}

	// libcss resolved the URL against the sheet's own URL at parse time
	// (the resolve callback), so for a local or http page it is already
	// absolute. NONE and IMAGE share the return value, so the NULL
	// out-parameter is what says "no image".
	var backgroundURL *C.lwc_string
	if uint8(C.css_computed_background_image(style, &backgroundURL)) != cssBackgroundImageInherit &&
		backgroundURL != nil {
		out.BackgroundImage = lwcBytes(backgroundURL)
		out.HasBackgroundImage = true
	}

	// The tiling and placement libcss computes natively (CSS 2.1
	// §8.14.1): background-repeat as its computed keyword, and
	// background-position as two lengths whose unit says whether the
	// value is a percentage — where the left/center/right and
	// top/center/bottom keywords land (0/50/100%, libcss'
	// select/properties/background_position.c) — or an absolute length.
	// The S10 painter reads both through Style to place the image inside
	// the box instead of tiling it from the top-left corner.
	switch uint8(C.css_computed_background_repeat(style)) {
	case cssBackgroundRepeatRepeatX:
		out.BackgroundRepeat = BackgroundRepeatRepeatX
	case cssBackgroundRepeatRepeatY:
		out.BackgroundRepeat = BackgroundRepeatRepeatY
	case cssBackgroundRepeatNoRepeat:
		out.BackgroundRepeat = BackgroundRepeatNoRepeat
	default:
		out.BackgroundRepeat = BackgroundRepeatRepeat
	}
	var hlen, vlen C.css_fixed
	var hun, vun C.css_unit
	if uint8(C.css_computed_background_position(style, &hlen, &hun, &vlen, &vun)) == cssBackgroundPositionSet {
		out.BackgroundPositionX = backgroundPosition(hlen, hun, style, unitCtx)
		out.BackgroundPositionY = backgroundPosition(vlen, vun, style, unitCtx)
	}

	// The full computed family list (T10): libcss hands back a NULL
	// terminated array of named families plus a generic status byte.
	var families **C.lwc_string
	familyType := uint8(C.css_computed_font_family(style, &families))
	names := familyNames(families)
	out.FontFamily = "Sans"
	switch {
	case len(names) > 0:
		out.FontFamily = names[0]
	default:
		switch familyType {
		case cssFontFamilySerif:
			out.FontFamily = "Serif"
		case cssFontFamilyCursive:
			out.FontFamily = "Cursive"
		case cssFontFamilyFantasy:
			out.FontFamily = "Fantasy"
		case cssFontFamilyMonospace:
			out.FontFamily = "Monospace"
		default:
			out.FontFamily = "Sans"
		}
	}
	out.FontFamilies = familyStack(names, familyType)

	if px, ok := lengthPx(style, unitCtx, getFontSize, cssFontSizeDimension); ok {
		out.FontSize = px
	}

	switch weight := uint8(C.css_computed_font_weight(style)); {
	case weight == cssFontWeightNormal || weight == cssFontWeightInherit:
		out.FontWeight = 400
	case weight == cssFontWeightBold || weight == cssFontWeightBolder:
		out.FontWeight = 700
	case weight == cssFontWeightLighter:
		out.FontWeight = 300
	case cssFontWeight100 <= weight && weight <= cssFontWeight900:
		out.FontWeight = uint32(weight-4) * 100
	default:
		out.FontWeight = 400
	}

	styleItalic := uint8(C.css_computed_font_style(style))
	out.Italic = styleItalic == cssFontStyleItalic || styleItalic == cssFontStyleOblique

	var lineHeightLength C.css_fixed
	var lineHeightUnit C.css_unit
	out.LineHeight = 1.2
	switch uint8(C.css_computed_line_height(style, &lineHeightLength, &lineHeightUnit)) {
	case cssLineHeightNumber:
		out.LineHeight = fixedToF64(lineHeightLength)
	case cssLineHeightDimension:
		if uint32(lineHeightUnit) == cssUnitPct {
			out.LineHeight = 1.2
		} else {
			px := fixedToF64(C.css_unit_len2css_px(style, unitCtx, lineHeightLength, C.css_unit(lineHeightUnit)))
			if out.FontSize > 0 {
				out.LineHeight = px / out.FontSize
			} else {
				out.LineHeight = 1.2
			}
		}
	default:
		// inherit / normal
		out.LineHeight = 1.2
	}

	out.TextAlign = TextAlignLeft
	switch align := uint8(C.css_computed_text_align(style)); align {
	case cssTextAlignCenter, cssTextAlignLibcssCenter:
		out.TextAlign = TextAlignCenter
	case cssTextAlignRight, cssTextAlignLibcssRight:
		out.TextAlign = TextAlignRight
	case cssTextAlignJustify:
		out.TextAlign = TextAlignJustify
	case cssTextAlignLeft, cssTextAlignLibcssLeft:
		out.TextAlign = TextAlignLeft
	default:
		// inherit / default already resolved by compose
		out.TextAlign = TextAlignLeft
	}

	if spacing, ok := lengthPx(style, unitCtx, getLetterSpacing, cssLetterSpacingSet); ok {
		out.LetterSpacing = spacing
	}

	// Keep layout/paint's coarse flags in sync with the typography fields.
	out.Bold = out.FontWeight >= 600
	out.InlineAlign = out.TextAlign == TextAlignCenter
	out.WhiteSpaceNowrap = uint8(C.css_computed_white_space(style)) == cssWhiteSpaceNowrap

	return out
}
