// Package css is the Go port of the engine's CSS layer (src/css.rs +
// src/css_libcss.rs): libcss 0.9.2 through cgo, the stylesheet rewriter
// (gap carriers, font-family normalisation, @font-face extraction), the
// computed-style mapping into Style, and the grid/border-radius passes that
// read the properties libcss does not compute.
//
// This package owns no network access (T2 net) and no DOM (T1 dom): the DOM
// is reached through the NodeIndex interface so the layer stays swappable
// and testable.
//
// Test-first port of T3 (S3); the Rust engine is the behavioural oracle.
package css

// Display describes the computed display of a box in the minimal engine
// (block, inline-run or none). Flex/grid containers keep DisplayBlock and
// carry their properties in Style.Flex / Style.Grid instead.
type Display uint8

// Display values. Only Block/Inline/None exist here, exactly like
// css.rs::Display.
const (
	DisplayInline Display = iota
	DisplayBlock
	DisplayNone
)

// TextAlign is the horizontal alignment of text inside a block.
type TextAlign uint8

// TextAlign values.
const (
	TextAlignLeft TextAlign = iota
	TextAlignCenter
	TextAlignRight
	TextAlignJustify
)

// FlexDirection is the main axis of a flex container. The reverse variants
// are not part of the minimal flexbox: they lay out like their plain twin.
type FlexDirection uint8

// FlexDirection values.
const (
	FlexDirectionRow FlexDirection = iota
	FlexDirectionColumn
)

// FlexWrap tells whether a flex container breaks its items into lines.
type FlexWrap uint8

// FlexWrap values.
const (
	FlexWrapNoWrap FlexWrap = iota
	FlexWrapWrap
)

// JustifyContent is how a flex container distributes items along the main
// axis.
type JustifyContent uint8

// JustifyContent values.
const (
	JustifyFlexStart JustifyContent = iota
	JustifyCenter
	JustifyFlexEnd
	JustifySpaceBetween
	JustifySpaceAround
	JustifySpaceEvenly
)

// AlignItems is how a flex container aligns items along the cross axis.
// Baseline is not part of the minimal flexbox: it falls back to
// AlignItemsStretch's flex-start twin (AlignItemsFlexStart).
type AlignItems uint8

// AlignItems values.
const (
	AlignItemsStretch AlignItems = iota
	AlignItemsFlexStart
	AlignItemsCenter
	AlignItemsFlexEnd
)

// FlexBasisKind discriminates a flex item's base size.
type FlexBasisKind uint8

// FlexBasis values: auto (the item's content size), content, a definite
// length in CSS pixels, or a percentage of the container.
const (
	BasisAuto FlexBasisKind = iota
	BasisContent
	BasisPx
	BasisPercent
)

// FlexBasis is the flex base size of a flex item.
type FlexBasis struct {
	// Kind selects Auto, Content, or the Px/Percent payload in Value.
	Kind FlexBasisKind
	// Value carries the CSS-pixel length (Kind == BasisPx) or the
	// percentage number (Kind == BasisPercent).
	Value float64
}

// FlexStyle holds the flex container properties of an element; the CSS
// layer only produces it when the element is a flex container
// (display: flex / inline-flex). A flex container keeps DisplayBlock and
// is marked here instead.
type FlexStyle struct {
	Direction FlexDirection
	Wrap      FlexWrap
	Justify   JustifyContent
	Align     AlignItems
	// RowGap is the gap between the lines of the container, in CSS pixels,
	// taken from the min-width carrier.
	RowGap float64
	// ColumnGap is the gap between items of a line, in CSS pixels, taken
	// from libcss' native computed column-gap.
	ColumnGap float64
}

// FlexItemProps is the element's own flex item properties. The initial
// value mirrors the CSS initial value of `flex`: 0 1 auto.
type FlexItemProps struct {
	Grow   float64
	Shrink float64
	Basis  FlexBasis
}

// TrackKind discriminates one grid track sizing function.
type TrackKind uint8

// Track values: a definite length, a flexible fraction (fr), sized to
// content (auto), or clamped between bounds (minmax).
const (
	TrackAuto TrackKind = iota
	TrackPx
	TrackFr
	TrackMinMax
)

// GridTrack is one entry of grid-template-columns in the minimal grid.
type GridTrack struct {
	// Kind selects Px, Fr, Auto or MinMax. The Value holds the Px/Fr
	// number.
	Kind TrackKind
	// Value is the length in CSS pixels (Kind == TrackPx) or the fr
	// fraction (Kind == TrackFr).
	Value float64
	// Min and Max are the lower and upper bounds in CSS pixels (Kind ==
	// TrackMinMax). MinAuto/MaxAuto mark an auto bound: the track then
	// floors at zero content or grows unbounded, respectively.
	Min     float64
	MinAuto bool
	Max     float64
	MaxAuto bool
}

// GridStyle is a grid container's properties. Tracks is the explicit
// column list, in order; empty means the implicit single column.
type GridStyle struct {
	Tracks []GridTrack
}

// GridSelfAlign is one axis of a grid item's self alignment (CSS Box
// Alignment L3 §§5-7: align-self, justify-self, and the place-self
// shorthand). Auto behaves as the parent's justify-items/align-items; the
// engine models no *-items properties, so auto means stretch — the CSS
// initial value of both. Left maps to Start and right to End: the engine
// lays out left-to-right only.
type GridSelfAlign uint8

// Grid self-alignment values.
const (
	GridSelfAuto GridSelfAlign = iota
	GridSelfStretch
	GridSelfStart
	GridSelfCenter
	GridSelfEnd
)

// GridItemProps is the element's own grid item properties: how many tracks
// it spans and its self alignment on each axis.
type GridItemProps struct {
	ColSpan uint32
	RowSpan uint32
	// Justify is the inline-axis self alignment (justify-self).
	Justify GridSelfAlign
	// Align is the block-axis self alignment (align-self).
	Align GridSelfAlign
}

// Color is an RGBA colour with components in [0,1] (r/g/b/a), mirroring
// css.rs::Color.
type Color struct {
	R, G, B, A float64
}

// BackgroundRepeat is the computed tiling mode of background-image
// (CSS 2.1 §8.14.1). The zero value is the initial repeat: boxes built
// without a cascade (the painter's fixtures, anonymous boxes) tile exactly
// like they did before the placement fields existed.
type BackgroundRepeat uint8

// The four modes libcss computes.
const (
	BackgroundRepeatRepeat BackgroundRepeat = iota
	BackgroundRepeatRepeatX
	BackgroundRepeatRepeatY
	BackgroundRepeatNoRepeat
)

// BackgroundPosition is one axis of the computed background-position:
// a percentage of the free space (Percent true — where the keyword
// left/center/right and top/center/bottom land as 0/50/100), or a CSS
// pixel offset from the box's top-left corner (CSS 2.1 §8.14.1).
type BackgroundPosition struct {
	Percent bool
	Value   float64
}

// LinearGradient is a two-stop vertical gradient: From paints at the box's
// top edge, To at its bottom edge, alpha included (the CSS Images 3 §3.4
// subset the engine paints; other shapes keep the engine's behavior for
// backgrounds without an image).
type LinearGradient struct {
	From Color
	To   Color
}

// BorderKind is the computed per-side border marker: today the engine
// paints only the solid case; every other CSS style degrades to solid.
// (The Style field keeps the Rust field name border_style, so the enum
// type carries the "kind" suffix.)
type BorderKind uint8

// Border kinds. none (and hidden) paint nothing.
const (
	BorderNone BorderKind = iota
	BorderSolid
)

// Radius is one corner of border-radius, as specified: an absolute length
// in CSS pixels, or a percentage resolved at paint time against the box
// (mirroring css.rs::BorderRadius; elliptical radii keep only the
// horizontal radius). The type carries the short name Radius so the Style
// field can keep the Rust field name BorderRadius.
type Radius struct {
	// Percent selects "value is a percentage of the box" instead of CSS
	// pixels.
	Percent bool
	// Value is the CSS-pixel length or the percentage number.
	Value float64
}

// RadiusPx builds an absolute-pixel corner radius.
func RadiusPx(px float64) Radius { return Radius{Value: px} }

// RadiusPercent builds a percentage corner radius.
func RadiusPercent(pct float64) Radius { return Radius{Percent: true, Value: pct} }

// PositionInset is a computed absolute-position inset (CSS 2.1 §10.3.7 and
// §10.6.4): a percentage remains relative to the containing block's axis.
type PositionInset struct {
	Percent bool
	Value   float64
}

// Style mirrors css.rs::Style: the per-node computed style of the minimal
// engine. The CSS layer populates everything libcss computes plus the
// manual cascades for grid tracks and border radii; fields that later
// waves only read downstream stay as computed.
type Style struct {
	// Display: Block/Inline/None (flex and grid containers keep Block).
	Display Display
	// Width/Height/MaxWidth in CSS pixels; nil when not set (auto,
	// percentage, or max-width:none).
	Width    *float64
	Height   *float64
	MaxWidth *float64
	// BorderBox means CSS width/max-width include horizontal padding and
	// borders (computed box-sizing:border-box).
	BorderBox bool
	// Margin and padding, top/right/bottom/left, in CSS pixels.
	Margin  [4]float64
	Padding [4]float64
	// MarginAuto preserves which margin sides were specified as auto so
	// block layout can distribute positive leftover inline space.
	MarginAuto [4]bool
	// Relative positioning (CSS 2.1 §9.4.3). Insets are CSS pixels; nil is
	// auto/unresolved. Layout applies these after normal flow placement.
	PositionRelative bool
	// PositionAbsolute removes the box from normal flow (CSS 2.1 §9.3.1).
	PositionAbsolute bool
	DirectionRTL     bool
	RelativeTop      *float64
	RelativeRight    *float64
	RelativeBottom   *float64
	RelativeLeft     *float64
	// AbsoluteInsets retain percentages until layout knows the containing
	// block; nil means auto/unresolved.
	AbsoluteTop    *PositionInset
	AbsoluteRight  *PositionInset
	AbsoluteBottom *PositionInset
	AbsoluteLeft   *PositionInset
	// Borders (T11): per-side widths in CSS pixels; styles (only solid
	// paints today); colours; corner radii as specified. A used width of
	// zero is reported where the style is none: no border paints even
	// when libcss reports the medium initial width.
	BorderWidth  [4]float64
	BorderStyle  [4]BorderKind
	BorderColor  [4]Color
	BorderRadius [4]Radius
	// Color: the text colour.
	Color Color
	// Background: the element's background colour, nil when none.
	Background *Color
	// BackgroundImage: the background-image URL, resolved against the
	// sheet's own URL by libcss at parse time; "" when the element
	// declares none (or the declaration is absent).
	HasBackgroundImage bool
	BackgroundImage    string
	// BackgroundRepeat: the computed tiling mode of the background image
	// (CSS 2.1 §8.14.1; the zero value is the initial repeat).
	BackgroundRepeat BackgroundRepeat
	// BackgroundPositionX/Y: the computed background-position per axis
	// (CSS Backgrounds 3 §4.4). Percent positions the value's point of
	// the image against the same point of the box's free space (the
	// keyword center/top/bottom/left/right land as 50/0/100/0/100%);
	// a plain length offsets the image from the box's top-left corner
	// (the CSS 2.1 §8.14.1 semantics).
	BackgroundPositionX BackgroundPosition
	BackgroundPositionY BackgroundPosition
	// BackgroundGradient: the winning background image when it is a
	// two-stop vertical linear-gradient, read from the author sheets by
	// the manual cascade (libcss 0.9.2 knows no image functions at all,
	// so these never reach a computed style). nil when none.
	BackgroundGradient *LinearGradient
	// FontSize in CSS pixels (16 by default).
	FontSize float64
	// FontFamily is the first family of FontFamilies, or the display name
	// of the generic keyword when the declaration names no family
	// (Serif/Sans/Monospace/Cursive/Fantasy), kept as the pre-T10
	// single-family view.
	FontFamily string
	// FontFamilies is the full computed family list in author order:
	// every named family libcss computed (unquoted, multi-word names keep
	// their spaces) plus the generic keyword tail (serif, sans-serif, ...)
	// when the declaration carries one, or the sans default. Layout
	// resolves the list left to right and falls back to the sans default.
	FontFamilies []string
	// FontWeight: 100-900.
	FontWeight uint32
	// Italic is font-style italic (or oblique).
	Italic bool
	// LineHeight: a number (unitless ratio, px/font-size, or the 1.2
	// default when normal/inherit).
	LineHeight float64
	// TextAlign, mapped from the computed value.
	TextAlign TextAlign
	// LetterSpacing in CSS pixels.
	LetterSpacing float64
	// Bold mirrors FontWeight >= 600 for layout/paint.
	Bold bool
	// InlineAlign mirrors TextAlign == Center for layout/paint.
	InlineAlign bool
	// WhiteSpaceNowrap mirrors white-space: nowrap (CSS Text L3 §4.1.2:
	// collapsing preserved, wrapping suppressed). Only nowrap is mapped;
	// pre/pre-wrap/pre-line keep normal wrapping, the engine's standing
	// approximation.
	WhiteSpaceNowrap bool
	// Flex holds the flex container properties, when the element is one.
	Flex *FlexStyle
	// FlexItem is the element's own flex item properties.
	FlexItem FlexItemProps
	// Grid holds the grid container properties (tracks filled from the
	// author sheets by the manual cascade), when the element is one.
	Grid *GridStyle
	// GridItem is the element's own grid item properties.
	GridItem GridItemProps
	// RowGap is the row gap between grid rows (and flex lines of a column
	// container) in CSS pixels; the CSS layer fills it from the min-width
	// carrier the same way the flex gaps travel.
	RowGap float64
	// ColumnGap is the column gap between grid tracks (and flex items of a
	// row container) in CSS pixels, read from libcss' native computed value.
	ColumnGap float64
}

var black = Color{R: 0, G: 0, B: 0, A: 1}

// New returns the default Style, mirroring css.rs::Style::default.
func New() Style {
	return Style{
		Display:      DisplayInline,
		Color:        black,
		BorderRadius: [4]Radius{RadiusPx(0), RadiusPx(0), RadiusPx(0), RadiusPx(0)},
		FontSize:     16,
		FontFamily:   "Sans",
		FontFamilies: []string{"sans-serif"},
		FontWeight:   400,
		Italic:       false,
		LineHeight:   1.2,
		TextAlign:    TextAlignLeft,
		Bold:         false,
		InlineAlign:  false,
		FlexItem: FlexItemProps{
			Grow:   0,
			Shrink: 1,
			Basis:  FlexBasis{Kind: BasisAuto},
		},
		GridItem: GridItemProps{ColSpan: 1, RowSpan: 1},
	}
}
