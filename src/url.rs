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
