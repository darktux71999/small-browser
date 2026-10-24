// Font registration for @font-face: fetched font files are cached on disk
// and handed to fontconfig, mirroring the oracle's T10 loader
// (src/css_libcss.rs cache_font_file + layout::fontconfig::add_font_file).
package paint

/*
#cgo pkg-config: fontconfig
#include <stdlib.h>
#include <fontconfig/fontconfig.h>

static int paint_font_add_file(const char *path) {
	return FcConfigAppFontAddFile(NULL, (const FcChar8 *)path) != 0;
}
*/
import "C"

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"
)

// RegisterFontFile hands a font file to fontconfig for the rest of the
// process, reporting whether fontconfig accepted it. It mirrors
// layout::fontconfig::add_font_file (FcConfigAppFontAddFile on the current
// config).
func RegisterFontFile(path string) bool {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	return C.paint_font_add_file(cpath) != 0
}

// CacheFontFile stores downloaded font bytes in the app font cache
// (~/.cache/browser-lite/fonts) under a stable name: the FNV-1a hash of the
// URL plus the ttf/otf extension. The write goes through a temporary file
// plus a rename so another process registering the same URL never sees it
// half-written. It reports false when HOME is unset or the write fails,
// like the oracle's cache_font_file returning None.
func CacheFontFile(rawURL string, data []byte) (string, bool) {
	home := os.Getenv("HOME")
	if home == "" {
		return "", false
	}
	dir := filepath.Join(home, ".cache", "browser-lite", "fonts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false
	}
	ext := "ttf"
	if strings.HasSuffix(strings.ToLower(rawURL), ".otf") {
		ext = "otf"
	}
	name := fnv1aHex(rawURL) + "." + ext
	path := filepath.Join(dir, name)
	tmp := filepath.Join(dir, "."+name+"."+strconv.Itoa(os.Getpid())+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", false
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", false
	}
	return path, true
}

// fnv1aHex is FNV-1a over the URL, rendered as 16 lowercase hex digits: the
// stable cache file name, like the oracle's fnv1a.
func fnv1aHex(text string) string {
	hash := uint64(0xcbf29ce484222325)
	for i := 0; i < len(text); i++ {
		hash ^= uint64(text[i])
		hash *= 0x100000001b3
	}
	const digits = "0123456789abcdef"
	var out [16]byte
	for i := 15; i >= 0; i-- {
		out[i] = digits[hash&0xf]
		hash >>= 4
	}
	return string(out[:])
}
