package css

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// joinURL resolves `rel` against `base`, mirroring join_url of
// css_libcss.rs.
//
// `base` is an absolute URL (http(s), file, or any other scheme) or a
// filesystem path. A path is a directory when it ends with a separator or
// names an existing directory; otherwise its parent directory is the base,
// which is what a stylesheet URL needs. Relative paths are normalised so
// the cache key is stable (a/../b.css and b.css are one URL).
func joinURL(base, rel string) (string, bool) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", false
	}
	// An absolute URL is handed back verbatim, exactly like Rust's
	// Url::parse(rel).is_ok() shortcut.
	if _, err := url.Parse(rel); err == nil && isAbsoluteURL(rel) {
		return rel, true
	}
	if baseURL, err := url.Parse(base); err == nil && isAbsoluteURL(base) {
		// RFC 3986 merge, like Rust's Url::join: the last base segment
		// is replaced (page.css + tile.png -> dir/tile.png), not
		// appended to (JoinPath would yield dir/page.css/tile.png).
		if refURL, err := url.Parse(rel); err == nil {
			return baseURL.ResolveReference(refURL).String(), true
		}
		return "", false
	}
	if base == "" {
		return "", false
	}
	dir := base
	// A filesystem path is the base directory: via the separator or an
	// existing directory; otherwise the parent directory is what a
	// stylesheet URL resolves against.
	if !strings.HasSuffix(base, "/") && !dirExists(base) {
		dir = dirOf(base)
	}
	var joined string
	if filepath.IsAbs(rel) {
		joined = rel
	} else {
		joined = filepath.Join(dir, rel)
	}
	return normalizePathLexical(joined), true
}

// isAbsoluteURL mirrors the scheme-detecting part of a successful
// Url::parse: a scheme prefix (alpha followed by alphanumeric, +, - or .)
// and a ':'. The standard library accepts plenty of relative material as
// "parse errors only", which is what the OK check above cannot do.
func isAbsoluteURL(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.':
			if i == 0 {
				return false
			}
		case c == ':' && i > 0:
			return true
		default:
			return false
		}
	}
	return false
}

// dirOf is the parent directory of a filesystem path (Rust's
// Path::parent().unwrap_or(base_path)).
func dirOf(path string) string {
	parent := filepath.Dir(path)
	if parent == "." || parent == string(filepath.Separator) {
		return path
	}
	return parent
}

// dirExists stats whether the path names an existing directory
// (Rust's Path::is_dir).
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// normalizePathLexical removes `.`/`..` lexically, without touching the
// filesystem (mirroring normalize_path).
func normalizePathLexical(path string) string {
	if isAbsoluteURL(path) || strings.Contains(path, "://") {
		// URLs keep their scheme; only the path part is normalised.
		return path
	}
	return filepath.Clean(path)
}
