//! URL helpers. Resolution is delegated to the `url` crate so relative
//! links follow the WHATWG algorithm.

pub fn is_http(url: &str) -> bool {
    let lower = url.trim_start().to_ascii_lowercase();
    lower.starts_with("http://") || lower.starts_with("https://")
}

/// Resolve `href` against `base`. Returns `None` when either is invalid or the
/// result is not http(s).
pub fn resolve(base: &str, href: &str) -> Option<String> {
    let base = url::Url::parse(base).ok()?;
    let joined = base.join(href).ok()?;
    matches!(joined.scheme(), "http" | "https").then(|| joined.to_string())
}

/// Convert a local path into a file:// URL, for resolving relative links.
pub fn file_url(path: &str) -> Option<String> {
    let abs = std::fs::canonicalize(path).ok()?;
    url::Url::from_file_path(abs).ok().map(|u| u.to_string())
}
