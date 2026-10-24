package css

/*
#cgo pkg-config: libcss libwapcaplet
#include "glue.h"
*/
import "C"

import "unsafe"

func internC(text string) *C.lwc_string {
	// Go strings are read-only byte slices with NULs allowed: libwapcaplet
	// takes an explicit length, so the pointer is borrowed only for the
	// duration of the C call.
	var data unsafe.Pointer
	if len(text) > 0 {
		data = unsafe.Pointer(unsafe.StringData(text))
	}
	return C.cssgo_intern((*C.char)(data), C.size_t(len(text)))
}

// lwcBytes mirrors lwc_bytes: the text lives just behind the header
// (lwc_string_data).
func lwcBytes(s *C.lwc_string) string {
	if s == nil {
		return ""
	}
	return C.GoStringN(C.cssgo_lwc_data(s), C.int(C.cssgo_lwc_len(s)))
}

// lwcRef hands the reference back with a bumped refcount.
func lwcRef(s *C.lwc_string) *C.lwc_string {
	if s == nil {
		return nil
	}
	return C.cssgo_lwc_ref(s)
}

// lwcUnref releases a reference (the unref macro's semantics run C-side).
func lwcUnref(s *C.lwc_string) {
	if s == nil {
		return
	}
	C.cssgo_lwc_unref(s)
}
