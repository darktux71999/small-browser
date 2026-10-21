//! A small cookie jar.
//!
//! Cookies are parsed from `Set-Cookie` headers, matched against a request
//! URL and persisted through the SQLite `Store`. The matching is deliberately
//! simple: host-only and domain cookies, path prefixes and absolute expiry.
//! `Secure` and `HttpOnly` are parsed and kept, but not enforced yet.

use std::time::{SystemTime, UNIX_EPOCH};

use url::Url;

use crate::storage::{Store, StoredCookie};

/// One cookie.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Cookie {
    pub name: String,
    pub value: String,
    /// The domain the cookie belongs to.
    pub domain: String,
    /// Whether the cookie only matches its exact host.
    pub host_only: bool,
    /// The path prefix the cookie is limited to.
    pub path: String,
    pub secure: bool,
    pub http_only: bool,
    /// Absolute expiry, in seconds since the Unix epoch. `None` means a
    /// session cookie.
    pub expires: Option<i64>,
}

impl Cookie {
    /// Whether the cookie is past its expiry.
    pub fn is_expired(&self, now: i64) -> bool {
        matches!(self.expires, Some(at) if at <= now)
    }

    /// Whether the cookie may be sent to `url`.
    pub fn matches(&self, url: &Url) -> bool {
        let Some(host) = url.host_str() else {
            return false;
        };
        let host = host.to_ascii_lowercase();
        let host_ok = if self.host_only {
            host == self.domain
        } else {
            domain_matches(&host, &self.domain)
        };
        host_ok && path_matches(&self.path, url.path())
    }
}

/// The cookies the browser holds.
#[derive(Debug, Clone, Default)]
pub struct CookieJar {
    cookies: Vec<Cookie>,
}

impl CookieJar {
    pub fn new() -> CookieJar {
        CookieJar::default()
    }

    /// Store a cookie from a `Set-Cookie` header value received from `url`.
    ///
    /// A cookie with the same domain, path and name replaces the old one.
    pub fn set_from_header(&mut self, url: &str, set_cookie_value: &str) -> Result<(), String> {
        let url = Url::parse(url).map_err(|e| format!("invalid url {url}: {e}"))?;
        let cookie = parse_set_cookie(&url, set_cookie_value)?;
        self.cookies.retain(|old| {
            !(old.domain == cookie.domain && old.path == cookie.path && old.name == cookie.name)
        });
        self.cookies.push(cookie);
        Ok(())
    }

    /// The `Cookie` header value for `url`, if any cookie matches it.
    pub fn header_for(&self, url: &str) -> Option<String> {
        let url = Url::parse(url).ok()?;
        let now = now_seconds();
        let mut parts = Vec::new();
        for cookie in &self.cookies {
            if cookie.is_expired(now) || !cookie.matches(&url) {
                continue;
            }
            parts.push(format!("{}={}", cookie.name, cookie.value));
        }
        if parts.is_empty() {
            None
        } else {
            Some(parts.join("; "))
        }
    }

    /// The cookies in the jar.
    pub fn cookies(&self) -> &[Cookie] {
        &self.cookies
    }

    /// Drop the cookies that are past their expiry.
    pub fn remove_expired(&mut self) {
        let now = now_seconds();
        self.cookies.retain(|cookie| !cookie.is_expired(now));
    }

    /// Persist the jar through the store.
    pub fn store(&self, store: &Store) -> Result<(), String> {
        for cookie in &self.cookies {
            store.store_cookie(&StoredCookie::from(cookie))?;
        }
        Ok(())
    }

    /// Read a jar back from the store.
    pub fn load(store: &Store) -> Result<CookieJar, String> {
        let stored = store.load_cookies()?;
        Ok(CookieJar {
            cookies: stored.iter().map(Cookie::from).collect(),
        })
    }
}

impl From<&Cookie> for StoredCookie {
    fn from(cookie: &Cookie) -> StoredCookie {
        StoredCookie {
            domain: cookie.domain.clone(),
            name: cookie.name.clone(),
            value: cookie.value.clone(),
            path: cookie.path.clone(),
            host_only: cookie.host_only,
            secure: cookie.secure,
            http_only: cookie.http_only,
            expires: cookie.expires,
        }
    }
}

impl From<&StoredCookie> for Cookie {
    fn from(stored: &StoredCookie) -> Cookie {
        Cookie {
            name: stored.name.clone(),
            value: stored.value.clone(),
            domain: stored.domain.clone(),
            host_only: stored.host_only,
            path: stored.path.clone(),
            secure: stored.secure,
            http_only: stored.http_only,
            expires: stored.expires,
        }
    }
}

/// Parse one `Set-Cookie` header value received from `url`.
fn parse_set_cookie(url: &Url, value: &str) -> Result<Cookie, String> {
    let mut parts = value.split(';');
    let first = parts.next().unwrap_or("").trim();
    let (name, cookie_value) = first
        .split_once('=')
        .ok_or_else(|| format!("Set-Cookie without a name and value: {first}"))?;
    let name = name.trim();
    if name.is_empty() {
        return Err(format!("Set-Cookie without a name: {first}"));
    }

    let host = url.host_str().unwrap_or_default().to_ascii_lowercase();
    let mut cookie = Cookie {
        name: name.to_string(),
        value: cookie_value.trim().to_string(),
        domain: host.clone(),
        host_only: true,
        path: default_path(url.path()),
        secure: false,
        http_only: false,
        expires: None,
    };

    let mut max_age: Option<i64> = None;
    for attribute in parts {
        let attribute = attribute.trim();
        let (key, attribute_value) = match attribute.split_once('=') {
            Some((key, attribute_value)) => (key.trim(), attribute_value.trim()),
            None => (attribute, ""),
        };
        match key.to_ascii_lowercase().as_str() {
            "domain" => {
                let domain = attribute_value.trim_start_matches('.').to_ascii_lowercase();
                if domain.is_empty() {
                    continue;
                }
                if !domain_matches(&host, &domain) {
                    return Err(format!(
                        "Set-Cookie domain {domain} does not belong to {host}"
                    ));
                }
                cookie.domain = domain;
                cookie.host_only = false;
            }
            "path" => {
                if attribute_value.starts_with('/') {
                    cookie.path = attribute_value.to_string();
                }
            }
            "secure" => cookie.secure = true,
            "httponly" => cookie.http_only = true,
            "max-age" => max_age = attribute_value.parse::<i64>().ok(),
            "expires" => cookie.expires = parse_expires(attribute_value),
            // `samesite`, `partitioned` and anything else is ignored.
            _ => {}
        }
    }

    // `Max-Age` wins over `Expires`.
    if let Some(max_age) = max_age {
        cookie.expires = Some(now_seconds() + max_age);
    }
    Ok(cookie)
}

/// Whether `host` is `domain` or a subdomain of it.
fn domain_matches(host: &str, domain: &str) -> bool {
    if host == domain {
        return true;
    }
    host.len() > domain.len()
        && host.ends_with(domain)
        && host.as_bytes()[host.len() - domain.len() - 1] == b'.'
}

/// Whether a request path falls under a cookie path.
fn path_matches(cookie_path: &str, request_path: &str) -> bool {
    if request_path == cookie_path {
        return true;
    }
    if !request_path.starts_with(cookie_path) {
        return false;
    }
    if cookie_path.ends_with('/') {
        return true;
    }
    request_path[cookie_path.len()..].starts_with('/')
}

/// The path a cookie without a `Path` attribute gets.
fn default_path(request_path: &str) -> String {
    if !request_path.starts_with('/') {
        return "/".to_string();
    }
    match request_path.rfind('/') {
        Some(0) | None => "/".to_string(),
        Some(index) => request_path[..index].to_string(),
    }
}

/// Parse an `Expires` date into seconds since the Unix epoch.
///
/// Accepts the RFC 1123 spelling (`Wed, 09 Jun 2021 10:18:14 GMT`) and the
/// RFC 850 one (`Wednesday, 09-Jun-21 10:18:14 GMT`).
fn parse_expires(value: &str) -> Option<i64> {
    let rest = match value.split_once(',') {
        Some((_weekday, rest)) => rest.trim(),
        None => value.trim(),
    };
    let mut fields = rest.split_whitespace();
    let day_field = fields.next()?;
    let (day, month_field, year_field) = match day_field.split_once('-') {
        // RFC 850: "09-Jun-21".
        Some((day, month)) => {
            let year = fields.next()?;
            (day, month, year)
        }
        None => (day_field, fields.next()?, fields.next()?),
    };
    let day: i64 = day.parse().ok()?;
    let month = month_index(month_field)?;
    let mut year: i64 = year_field.parse().ok()?;
    if year < 100 {
        year += if year < 70 { 2000 } else { 1900 };
    }
    let time = fields.next()?;
    let (hour, minute, second) = parse_time(time)?;
    Some(days_from_civil(year, month, day) * 86_400 + hour * 3_600 + minute * 60 + second)
}

fn parse_time(value: &str) -> Option<(i64, i64, i64)> {
    let mut parts = value.split(':');
    let hour: i64 = parts.next()?.parse().ok()?;
    let minute: i64 = parts.next()?.parse().ok()?;
    let second: i64 = parts.next()?.parse().ok()?;
    Some((hour, minute, second))
}

fn month_index(name: &str) -> Option<i64> {
    const MONTHS: [&str; 12] = [
        "jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec",
    ];
    let lower = name.to_ascii_lowercase();
    MONTHS
        .iter()
        .position(|month| *month == lower)
        .map(|index| index as i64 + 1)
}

/// Days between the Unix epoch and a civil date (Howard Hinnant's algorithm).
fn days_from_civil(year: i64, month: i64, day: i64) -> i64 {
    let year = if month <= 2 { year - 1 } else { year };
    let era = if year >= 0 { year } else { year - 399 } / 400;
    let year_of_era = year - era * 400;
    let month_position = (month + 9) % 12;
    let day_of_year = (153 * month_position + 2) / 5 + day - 1;
    let day_of_era =
        year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    era * 146_097 + day_of_era - 719_468
}

/// Seconds since the Unix epoch.
fn now_seconds() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64)
        .unwrap_or(0)
}
