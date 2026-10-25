package css

/*
#cgo pkg-config: libcss libwapcaplet
#include "glue.h"
*/
import "C"

import "unsafe"

type parsedSheet struct {
	sheet *C.css_stylesheet
}

// createEmptySheet builds a finished, empty stylesheet under `url`; it is
// what an unfetchable @import registers as (libcss requires an import to
// exist before the parent is used).
func createEmptySheet(sheetURL string) (*C.css_stylesheet, error) {
	cURL, free := cString(sheetURL)
	defer free()
	var params C.css_stylesheet_params
	C.cssgo_params_init(&params, cURL, 0)
	var sheet *C.css_stylesheet
	if err := check(C.css_stylesheet_create(&params, &sheet), "css_stylesheet_create (import)"); err != nil {
		return nil, err
	}
	if err := check(C.css_stylesheet_data_done(sheet), "css_stylesheet_data_done (import)"); err != nil {
		return nil, err
	}
	return sheet, nil
}

// finishImports registers the sheets libcss is waiting for on this parent's
// pending-import list. The no-loader path (the only one in this unit)
// registers an empty sheet per pending URL, as libcss requires; imported
// rules then keep their place before the importing sheet's rules. A non-OK
// answer or a NULL url both end the walk, mirroring the oracle (which
// returns on either condition).
func finishImports(parent *C.css_stylesheet) error {
	for {
		var pending *C.lwc_string
		if err := C.css_stylesheet_next_pending_import(parent, &pending); int32(err) != cssOK || pending == nil {
			return nil
		}
		urlText := lwcBytes(pending)
		child, err := createEmptySheet(urlText)
		if err != nil {
			lwcUnref(pending)
			return err
		}
		if err := check(C.css_stylesheet_register_import(parent, child), "css_stylesheet_register_import"); err != nil {
			lwcUnref(pending)
			return err
		}
		lwcUnref(pending)
	}
}

// parseSheet parses one stylesheet body: the rewriter runs over the raw
// text first (the carriers; inline style declares a declaration list, the
// others rule lists), extracting the custom-property/var() side table
// libcss 0.9.2 would silently discard (see CaptureSheet), then libcss
// gets the remaining body in one append call. The capture is a pure
// function of the text: NewCustomProperties re-captures the same input
// for the resolver, so both sides always see the same side table.
func parseSheet(cssText, sheetURL string, inline bool) (*C.css_stylesheet, error) {
	var rewritten string
	if inline {
		rewritten, _ = CaptureInline(cssText)
	} else {
		rewritten, _ = CaptureSheet(cssText)
	}
	cURL, free := cString(sheetURL)
	defer free()
	var params C.css_stylesheet_params
	C.cssgo_params_init(&params, cURL, C.int(boolToInt(inline)))
	var sheet *C.css_stylesheet
	if err := check(C.css_stylesheet_create(&params, &sheet), "css_stylesheet_create"); err != nil {
		return nil, err
	}

	// An empty rewritten body is valid CSS: it happens when every
	// declaration of an inline style="" list was captured (the var() and
	// gradient declarations the manual passes replay), and libcss answers
	// css_stylesheet_append_data with CSS_BADPARM for a NULL data pointer
	// even at length zero — so the append is skipped and the sheet is
	// finished empty.
	if len(rewritten) > 0 {
		data := (*C.uint8_t)(unsafe.Pointer(unsafe.StringData(rewritten)))
		if err := C.css_stylesheet_append_data(sheet, data, C.size_t(len(rewritten))); err != cssOK && err != cssNeedData {
			return nil, errLibcss{what: "css_stylesheet_append_data", code: int32(err), text: cErrorString(err)}
		}
	}
	// css_stylesheet_data_done answers CSS_IMPORTS_PENDING when the sheet
	// still waits for its imports (registered empty without a loader).
	err := C.css_stylesheet_data_done(sheet)
	if int32(err) == cssImportsPending {
		if err := finishImports(sheet); err != nil {
			return nil, err
		}
	} else if int32(err) != cssOK {
		return nil, errLibcss{what: "css_stylesheet_data_done", code: int32(err), text: cErrorString(err)}
	}
	return sheet, nil
}

func cString(text string) (*C.char, func()) {
	raw := C.cssgo_strdup((*C.char)(unsafe.Pointer(unsafe.StringData(text))), C.size_t(len(text)))
	return raw, func() { C.free(unsafe.Pointer(raw)) }
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
