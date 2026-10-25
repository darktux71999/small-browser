package css

// cgo bindings to libcss 0.9.2 + libwapcaplet, mirroring the #[link] extern
// declarations of src/css_libcss.rs. The C side of the bridge (the handler
// table built from the //export callbacks, the stylesheet parameter
// template and the inline helpers that replace libwapcaplet's macros) lives
// in the preamble below; every error path returns a Go error, never a
// crash.

/*
#cgo pkg-config: libcss libwapcaplet
#include "glue.h"
*/
import "C"

// Error codes mirroring libcss' css_error (libcss/errors.h).
const (
	cssOK             = 0
	cssInvalid        = 3
	cssNeedData       = 5
	cssImportsPending = 8
)

// Style-level constants mirrored from css_libcss.rs (the enum values of
// libcss 0.9.2).
const (
	cssPseudoElementCount = 5

	cssSelectHandlerVersion1 = 1

	cssOriginUA     = 0
	cssOriginAuthor = 2

	cssMediaScreen               = uint32(1 << 6)
	cssMediaOrientationLandscape = uint32(1)

	cssDisplayInline           = uint8(0x01)
	cssDisplayBlock            = uint8(0x02)
	cssDisplayListItem         = uint8(0x03)
	cssDisplayRunIn            = uint8(0x04)
	cssDisplayInlineBlock      = uint8(0x05)
	cssDisplayTable            = uint8(0x06)
	cssDisplayInlineTable      = uint8(0x07)
	cssDisplayTableRowGroup    = uint8(0x08)
	cssDisplayTableHeaderGroup = uint8(0x09)
	cssDisplayTableFooterGroup = uint8(0x0a)
	cssDisplayTableRow         = uint8(0x0b)
	cssDisplayTableColumnGroup = uint8(0x0c)
	cssDisplayTableColumn      = uint8(0x0d)
	cssDisplayTableCell        = uint8(0x0e)
	cssDisplayTableCaption     = uint8(0x0f)
	cssDisplayNone             = uint8(0x10)
	cssDisplayFlex             = uint8(0x11)
	cssDisplayInlineFlex       = uint8(0x12)
	cssDisplayGrid             = uint8(0x13)
	cssDisplayInlineGrid       = uint8(0x14)

	cssWidthSet   = uint8(0x01)
	cssHeightSet  = uint8(0x01)
	cssMarginSet  = uint8(0x01)
	cssMarginAuto = uint8(0x02)

	cssPositionRelative = uint8(0x02)
	cssPositionAbsolute = uint8(0x03)
	cssDirectionRTL     = uint8(0x02)
	cssBoxSizingBorder  = uint8(0x02)
	cssPaddingSet       = uint8(0x01)

	cssColorColor = uint8(0x01)

	cssBackgroundColorColor        = uint8(0x01)
	cssBackgroundColorCurrentColor = uint8(0x02)

	cssBackgroundImageInherit = uint8(0x0)

	cssBackgroundPositionSet = uint8(0x1)

	cssBackgroundRepeatRepeatX  = uint8(0x1)
	cssBackgroundRepeatRepeatY  = uint8(0x2)
	cssBackgroundRepeatRepeat   = uint8(0x3)
	cssBackgroundRepeatNoRepeat = uint8(0x4)

	cssFontFamilySerif     = uint8(0x1)
	cssFontFamilySansSerif = uint8(0x2)
	cssFontFamilyCursive   = uint8(0x3)
	cssFontFamilyFantasy   = uint8(0x4)
	cssFontFamilyMonospace = uint8(0x5)

	cssFontSizeDimension = uint8(0x0a)

	cssFontStyleItalic  = uint8(0x2)
	cssFontStyleOblique = uint8(0x3)

	cssFontWeightInherit = uint8(0x0)
	cssFontWeightNormal  = uint8(0x1)
	cssFontWeightBold    = uint8(0x2)
	cssFontWeightBolder  = uint8(0x3)
	cssFontWeightLighter = uint8(0x4)
	cssFontWeight100     = uint8(0x5)
	cssFontWeight900     = uint8(0xd)

	cssLineHeightInherit   = uint8(0x0)
	cssLineHeightNumber    = uint8(0x1)
	cssLineHeightDimension = uint8(0x2)

	cssTextAlignLeft         = uint8(0x2)
	cssTextAlignRight        = uint8(0x3)
	cssTextAlignCenter       = uint8(0x4)
	cssTextAlignJustify      = uint8(0x5)
	cssTextAlignLibcssLeft   = uint8(0x7)
	cssTextAlignLibcssCenter = uint8(0x8)
	cssTextAlignLibcssRight  = uint8(0x9)

	cssLetterSpacingSet = uint8(0x1)

	cssWhiteSpaceNowrap = uint8(0x3)

	cssColumnGapSet = uint8(0x1)

	cssMinWidthSet = uint8(0x1)
	cssMaxWidthSet = uint8(0x1)
	cssInsetSet    = uint8(0x1)

	cssBorderWidthThin   = uint8(0x1)
	cssBorderWidthMedium = uint8(0x2)
	cssBorderWidthThick  = uint8(0x3)
	cssBorderWidthWidth  = uint8(0x4)

	cssBorderColorColor        = uint8(0x01)
	cssBorderColorCurrentColor = uint8(0x02)

	cssBorderStyleNone   = uint8(0x1)
	cssBorderStyleHidden = uint8(0x2)

	cssAlignItemsStretch   = uint8(0x1)
	cssAlignItemsFlexStart = uint8(0x2)
	cssAlignItemsFlexEnd   = uint8(0x3)
	cssAlignItemsCenter    = uint8(0x4)
	cssAlignItemsBaseline  = uint8(0x5)

	cssFlexBasisSet     = uint8(0x1)
	cssFlexBasisAuto    = uint8(0x2)
	cssFlexBasisContent = uint8(0x3)

	cssFlexDirectionRow           = uint8(0x1)
	cssFlexDirectionRowReverse    = uint8(0x2)
	cssFlexDirectionColumn        = uint8(0x3)
	cssFlexDirectionColumnReverse = uint8(0x4)

	cssFlexGrowSet   = uint8(0x1)
	cssFlexShrinkSet = uint8(0x1)

	cssFlexWrapNoWrap      = uint8(0x1)
	cssFlexWrapWrap        = uint8(0x2)
	cssFlexWrapWrapReverse = uint8(0x3)

	cssJustifyContentFlexStart    = uint8(0x1)
	cssJustifyContentFlexEnd      = uint8(0x2)
	cssJustifyContentCenter       = uint8(0x3)
	cssJustifyContentSpaceBetween = uint8(0x4)
	cssJustifyContentSpaceAround  = uint8(0x5)
	cssJustifyContentSpaceEvenly  = uint8(0x6)

	cssUnitPct = uint32(0x15)

	cssPropColor       = uint32(0x018)
	cssPropFontFamily  = uint32(0x024)
	cssPropQuotes      = uint32(0x04a)
	cssPropVoiceFamily = uint32(0x05c)

	cssQuotesNone = uint8(0x1)

	cssNodeDeleted = uint32(0)

	cssFixedScale = 1024.0

	defaultFontSize       = 16.0
	defaultViewportHeight = 600.0
)

// cssFixed is libcss' css_fixed: a signed 32 bit value at a 2^10 scale
// (libcss/fpmath.h). This is C.css_fixed declared in Go terms only for the
// helpers below; all conversions run on the C side.
type cssFixed = C.css_fixed

// check converts a libcss error into a Go error, mirroring the `check`
// helper of css_libcss.rs (which panics); cgo error paths return errors
// instead of crashing.
func check(err C.css_error, what string) error {
	if err != cssOK {
		return errLibcss{what: what, code: int32(err), text: cErrorString(err)}
	}
	return nil
}

type errLibcss struct {
	what string
	code int32
	text string
}

func (e errLibcss) Error() string {
	return "libcss: " + e.what + "failed (" + itoa32(e.code) + ": " + e.text + ")"
}

func cErrorString(err C.css_error) string {
	p := C.css_error_to_string(err)
	if p == nil {
		return "?"
	}
	return C.GoString(p)
}

func itoa32(v int32) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	var out []byte
	u := uint32(v)
	if negative {
		u = uint32(-v)
	}
	for u > 0 {
		out = append([]byte{byte('0' + u%10)}, out...)
		u /= 10
	}
	if negative {
		return "-" + string(out)
	}
	return string(out)
}

// toFixedValue mirrors to_fixed: the fixed-scaled int32 value of a CSS
// pixel number.
func toFixedValue(v float64) C.css_fixed {
	return C.css_fixed(v * cssFixedScale)
}

// fixedToF64 mirrors fixed_to_f64.
func fixedToF64(v C.css_fixed) float64 {
	return float64(v) / cssFixedScale
}
