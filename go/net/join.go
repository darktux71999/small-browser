package net

import (
	"net/url"
	"os"
	"strings"
)

// JoinURL resolves rel against base, mirroring the oracle's join_url (the
// private helper both src/img.rs and src/css_libcss.rs share).
//
// base is an absolute URL (http(s), file, ...) or a filesystem path. A path
// is a directory when it ends with a separator or names an existing
// directory; otherwise its parent directory is the base. Relative paths are
// normalised so the cache key is stable (a/../b.png == b.png). Reports
// false when there is nothing to resolve, like the oracle's None.
func JoinURL(base, rel string) (string, bool) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", false
	}
	if isAbsoluteURL(rel) {
		return rel, true
	}
	if baseURL, err := url.Parse(base); err == nil && baseURL.Scheme != "" {
		ref, err := url.Parse(rel)
		if err != nil {
			return "", false
		}
		if resolved := baseURL.ResolveReference(ref); resolved != nil {
			return resolved.String(), true
		}
		return "", false
	}
	if base == "" {
		return "", false
	}
	var dir string
	switch {
	case strings.HasSuffix(base, "/"):
		dir = base
	default:
		if info, err := os.Stat(base); err == nil && info.IsDir() {
			dir = base
		} else {
			dir = parentOf(base)
		}
	}
	var joined string
	switch {
	case strings.HasPrefix(rel, "/"):
		joined = rel
	case dir == "":
		joined = rel
	default:
		joined = dir + "/" + rel
	}
	return normalizePath(joined), true
}

// isAbsoluteURL reports whether s is a URL with its own scheme, following
// the oracle's Url::parse acceptance: the "special" schemes need the "//"
// form, everything else (data:, about:, ...) stands alone.
func isAbsoluteURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch u.Scheme {
	case "http", "https", "ws", "wss", "ftp", "file":
		return strings.HasPrefix(s, u.Scheme+"://")
	}
	return true
}

// parentOf is the directory holding base's last component: "a/b" -> "a",
// "/b" -> "/", a bare name -> "" (the empty path, which joins without a
// separator), and "/" itself stays.
func parentOf(base string) string {
	if base == "/" {
		return base
	}
	i := strings.LastIndex(base, "/")
	if i < 0 {
		return ""
	}
	if i == 0 {
		return "/"
	}
	return base[:i]
}

// normalizePath removes "." and ".." components lexically, without touching
// the filesystem. ".." climbing past the root or the start of a relative
// path is kept, like the oracle's component walk.
func normalizePath(path string) string {
	absolute := strings.HasPrefix(path, "/")
	var out []string
	for _, component := range strings.Split(path, "/") {
		switch component {
		case "", ".":
			continue
		case "..":
			if n := len(out); n > 0 && out[n-1] != ".." {
				out = out[:n-1]
			} else {
				out = append(out, "..")
			}
		default:
			out = append(out, component)
		}
	}
	if absolute {
		return "/" + strings.Join(out, "/")
	}
	return strings.Join(out, "/")
}
