//! Minimal HTTP(S) client over the system libcurl.
//! Returns the response body; headers and streaming come later.

use std::time::Duration;

use curl::easy::Easy;

const USER_AGENT: &str = "browser-lite/0.1";

pub fn fetch(url: &str) -> Result<Vec<u8>, String> {
    let mut easy = Easy::new();
    easy.url(url).map_err(|e| format!("invalid URL {url}: {e}"))?;
    easy.follow_location(true)
        .map_err(|e| format!("redirects: {e}"))?;
    easy.max_redirections(5)
        .map_err(|e| format!("redirects: {e}"))?;
    easy.useragent(USER_AGENT)
        .map_err(|e| format!("user agent: {e}"))?;
    easy.connect_timeout(Duration::from_secs(10))
        .map_err(|e| format!("connect timeout: {e}"))?;
    easy.timeout(Duration::from_secs(20))
        .map_err(|e| format!("timeout: {e}"))?;
    // Let libcurl negotiate and decode compressed responses.
    easy.accept_encoding("gzip, deflate")
        .map_err(|e| format!("encoding: {e}"))?;

    let mut body = Vec::new();
    {
        let mut transfer = easy.transfer();
        transfer
            .write_function(|data| {
                body.extend_from_slice(data);
                Ok(data.len())
            })
            .map_err(|e| format!("transfer: {e}"))?;
        transfer.perform().map_err(|e| format!("request failed: {e}"))?;
    }
    Ok(body)
}
