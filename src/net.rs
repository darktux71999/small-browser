//! Minimal HTTP(S) client over the system libcurl.
//!
//! `fetch` keeps the old byte-only shape. `fetch_result` also reports the URL
//! the body finally came from, `fetch_text` decodes the body with the charset
//! declared in `Content-Type`, and the `*_async` variants run the blocking
//! transfer on a worker thread so the UI stays responsive.

use std::sync::mpsc::{self, Receiver};
use std::thread;
use std::time::Duration;

use curl::easy::Easy;

const USER_AGENT: &str = "browser-lite/0.1";
const CONNECT_TIMEOUT: Duration = Duration::from_secs(10);
const TIMEOUT: Duration = Duration::from_secs(20);
const MAX_REDIRECTS: u32 = 10;

/// A finished response: the body plus the URL it finally came from.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FetchResult {
    /// The URL after redirects.
    pub final_url: String,
    /// The raw body.
    pub bytes: Vec<u8>,
}

/// Fetch a URL and return the raw body.
pub fn fetch(url: &str) -> Result<Vec<u8>, String> {
    Ok(request(url)?.bytes)
}

/// Fetch a URL and return the body together with the URL it came from.
pub fn fetch_result(url: &str) -> Result<FetchResult, String> {
    let raw = request(url)?;
    Ok(FetchResult {
        final_url: raw.final_url,
        bytes: raw.bytes,
    })
}

/// Fetch a URL and return the body decoded with the declared charset.
pub fn fetch_text(url: &str) -> Result<String, String> {
    let raw = request(url)?;
    decode_body(&raw.bytes, raw.content_type.as_deref())
}

/// Fetch a URL on a worker thread. The result arrives over the channel.
pub fn fetch_async(url: &str) -> Receiver<Result<Vec<u8>, String>> {
    spawn_fetch(url, fetch)
}

/// Like [`fetch_async`], but the result also carries the final URL.
pub fn fetch_result_async(url: &str) -> Receiver<Result<FetchResult, String>> {
    spawn_fetch(url, fetch_result)
}

/// Like [`fetch_async`], but the result is the decoded body.
pub fn fetch_text_async(url: &str) -> Receiver<Result<String, String>> {
    spawn_fetch(url, fetch_text)
}

fn spawn_fetch<T, F>(url: &str, fetch: F) -> Receiver<Result<T, String>>
where
    F: FnOnce(&str) -> Result<T, String> + Send + 'static,
    T: Send + 'static,
{
    let (sender, receiver) = mpsc::channel();
    let url = url.to_string();
    thread::spawn(move || {
        let _ = sender.send(fetch(&url));
    });
    receiver
}

/// What a transfer produced, before any decoding.
struct Raw {
    final_url: String,
    bytes: Vec<u8>,
    content_type: Option<String>,
}

fn request(url: &str) -> Result<Raw, String> {
    let mut easy = Easy::new();
    easy.url(url).map_err(|e| format!("invalid URL {url}: {e}"))?;
    easy.follow_location(true)
        .map_err(|e| format!("redirects: {e}"))?;
    easy.max_redirections(MAX_REDIRECTS)
        .map_err(|e| format!("redirects: {e}"))?;
    easy.useragent(USER_AGENT)
        .map_err(|e| format!("user agent: {e}"))?;
    easy.connect_timeout(CONNECT_TIMEOUT)
        .map_err(|e| format!("connect timeout: {e}"))?;
    easy.timeout(TIMEOUT)
        .map_err(|e| format!("timeout: {e}"))?;
    // Let libcurl negotiate and decode compressed responses.
    easy.accept_encoding("gzip, deflate")
        .map_err(|e| format!("encoding: {e}"))?;

    let mut body = Vec::new();
    let mut status_line = String::new();
    {
        let mut transfer = easy.transfer();
        transfer
            .write_function(|data| {
                body.extend_from_slice(data);
                Ok(data.len())
            })
            .map_err(|e| format!("transfer: {e}"))?;
        // The status line of the last response is the one that counts: a
        // redirect chain reports several, one per hop.
        transfer
            .header_function(|data| {
                let line = String::from_utf8_lossy(data);
                let line = line.trim_end_matches(['\r', '\n']);
                if line.starts_with("HTTP/") {
                    status_line = line.to_string();
                }
                true
            })
            .map_err(|e| format!("headers: {e}"))?;
        transfer
            .perform()
            .map_err(|e| format!("request failed for {url}: {e}"))?;
    }

    let code = easy.response_code().map_err(|e| format!("status: {e}"))?;
    if code >= 400 {
        let line = if status_line.is_empty() {
            format!("HTTP {code}")
        } else {
            status_line
        };
        return Err(format!("{url}: {line}"));
    }

    let final_url = easy
        .effective_url()
        .ok()
        .flatten()
        .map(str::to_string)
        .unwrap_or_else(|| url.to_string());
    let content_type = easy.content_type().ok().flatten().map(str::to_string);

    Ok(Raw {
        final_url,
        bytes: body,
        content_type,
    })
}

/// Decode `bytes` with the charset declared by `content_type`.
///
/// The charset is read from the `Content-Type` header; UTF-8 is assumed when
/// none is declared. An unknown charset falls back to UTF-8 so the page still
/// renders, and the fallback is reported on stderr.
pub fn decode_body(bytes: &[u8], content_type: Option<&str>) -> Result<String, String> {
    let charset = charset_of(content_type).unwrap_or_else(|| "utf-8".to_string());
    match charset.as_str() {
        "utf-8" | "utf8" => Ok(String::from_utf8_lossy(strip_utf8_bom(bytes)).into_owned()),
        "iso-8859-1" | "iso8859-1" | "latin1" | "latin-1" | "us-ascii" | "ascii"
        | "windows-1252" | "cp1252" => Ok(decode_windows_1252(bytes)),
        "utf-16" | "utf-16le" | "utf-16be" => decode_utf16(bytes, &charset),
        other => {
            eprintln!("browser-lite: unknown charset {other:?}, decoding as utf-8");
            Ok(String::from_utf8_lossy(bytes).into_owned())
        }
    }
}

/// The charset of a `Content-Type` header value, if it declares one.
fn charset_of(content_type: Option<&str>) -> Option<String> {
    let content_type = content_type?;
    for parameter in content_type.split(';').skip(1) {
        let Some((key, value)) = parameter.split_once('=') else {
            continue;
        };
        if key.trim().eq_ignore_ascii_case("charset") {
            let value = value.trim().trim_matches('"').trim();
            if !value.is_empty() {
                return Some(value.to_ascii_lowercase());
            }
        }
    }
    None
}

fn strip_utf8_bom(bytes: &[u8]) -> &[u8] {
    bytes.strip_prefix(&[0xEF, 0xBB, 0xBF]).unwrap_or(bytes)
}

/// Decode a single-byte encoding.
///
/// ISO-8859-1, US-ASCII and windows-1252 share the same bytes for everything
/// except 0x80-0x9F, where windows-1252 has printable characters. Browsers
/// decode that whole family as windows-1252, so this does too.
fn decode_windows_1252(bytes: &[u8]) -> String {
    bytes
        .iter()
        .map(|&byte| match byte {
            0x80 => '\u{20AC}',
            0x82 => '\u{201A}',
            0x83 => '\u{0192}',
            0x84 => '\u{201E}',
            0x85 => '\u{2026}',
            0x86 => '\u{2020}',
            0x87 => '\u{2021}',
            0x88 => '\u{02C6}',
            0x89 => '\u{2030}',
            0x8A => '\u{0160}',
            0x8B => '\u{2039}',
            0x8C => '\u{0152}',
            0x8E => '\u{017D}',
            0x91 => '\u{2018}',
            0x92 => '\u{2019}',
            0x93 => '\u{201C}',
            0x94 => '\u{201D}',
            0x95 => '\u{2022}',
            0x96 => '\u{2013}',
            0x97 => '\u{2014}',
            0x98 => '\u{02DC}',
            0x99 => '\u{2122}',
            0x9A => '\u{0161}',
            0x9B => '\u{203A}',
            0x9C => '\u{0153}',
            0x9E => '\u{017E}',
            0x9F => '\u{0178}',
            other => other as char,
        })
        .collect()
}

/// Decode UTF-16, honouring a byte order mark when there is one.
fn decode_utf16(bytes: &[u8], charset: &str) -> Result<String, String> {
    let mut body = bytes;
    let little_endian = match charset {
        "utf-16le" => true,
        "utf-16be" => false,
        _ => {
            if let Some(rest) = body.strip_prefix(&[0xFF, 0xFE]) {
                body = rest;
                true
            } else if let Some(rest) = body.strip_prefix(&[0xFE, 0xFF]) {
                body = rest;
                false
            } else {
                // No mark: UTF-16 defaults to big endian.
                false
            }
        }
    };

    if body.len() % 2 != 0 {
        return Err(format!("invalid utf-16 body: {} bytes", bytes.len()));
    }
    let units: Vec<u16> = body
        .chunks_exact(2)
        .map(|pair| {
            if little_endian {
                u16::from_le_bytes([pair[0], pair[1]])
            } else {
                u16::from_be_bytes([pair[0], pair[1]])
            }
        })
        .collect();
    Ok(String::from_utf16_lossy(&units))
}
