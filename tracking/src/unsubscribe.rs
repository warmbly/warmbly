//! Recipient opt-out served on the workspace's own tracking domain.
//!
//! The confirm page, the RFC 8058 one-click POST and the suppression itself
//! all live in the backend. This is the thin pass-through that lets
//! `https://t.customer.com/unsubscribe/<token>` answer them, so the opt-out
//! address in a campaign email sits on the sender's domain like every other
//! link in the message instead of naming the platform.
//!
//! Nothing is decided here. The token is opaque, the backend verifies it, and
//! the response goes back byte for byte. The path is fixed and the token is
//! shape-checked before any request leaves, so this is a proxy for exactly one
//! backend route and never a general one.

// reqwest and axum are on different `http` majors, so header values cross the
// proxy boundary as strings rather than as the two incompatible HeaderValue
// types.
use axum::{
    body::Bytes,
    http::{header, HeaderMap, HeaderValue, StatusCode},
    response::{IntoResponse, Response},
};
use std::time::Duration;
use tracing::warn;

/// Longest token accepted before the backend is asked. The self-contained
/// signed token is 96 base64url characters; the ceiling only keeps a megabyte
/// of junk in a path from becoming a backend request.
const MAX_TOKEN_LEN: usize = 512;
/// Shortest token accepted. A stored ticket is 22 characters (16 random bytes
/// in base64url, 128 bits), which is what campaign mail carries now: the
/// opt-out address is the one URL a recipient reads in full, so its length is
/// the point. Guessing one is not what this bound defends against; entropy is.
/// It keeps a path that cannot be either token shape away from the backend.
const MIN_TOKEN_LEN: usize = 22;
/// Cap on the form body of a confirm or one-click POST, which is a few bytes.
pub const MAX_BODY_BYTES: usize = 16 * 1024;

/// What a recipient sees when the backend cannot be reached. Neutral, like the
/// backend's own pages: the email came from the customer's mailbox, so no
/// brand is named, and replying is a route to the same outcome because reply
/// opt-outs are detected and suppressed too.
const UNAVAILABLE_HTML: &str = r#"<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">
<title>Try again shortly</title>
<style>body{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;max-width:32rem;margin:4rem auto;padding:0 1.25rem;color:#0f172a;line-height:1.5}
h1{font-size:1.25rem;margin:0 0 .5rem}p{color:#475569;margin:0 0 1.25rem}</style></head>
<body><h1>Try again shortly</h1><p>We could not reach the sender's server just now. Open this link again in a few minutes, or reply to the email and the sender will stop.</p></body></html>"#;

pub struct UnsubscribeProxy {
    http: reqwest::Client,
    backend_url: String,
}

impl UnsubscribeProxy {
    pub fn new(backend_url: String) -> Self {
        Self {
            http: reqwest::Client::builder()
                .timeout(Duration::from_secs(5))
                // A redirect would take the recipient off this host; the
                // backend's own pages are same-path forms, so there is
                // nothing legitimate to follow.
                .redirect(reqwest::redirect::Policy::none())
                .build()
                .expect("reqwest client"),
            backend_url: backend_url.trim_end_matches('/').to_string(),
        }
    }

    /// Proxies one opt-out request. `suffix` is the fixed path after the token
    /// ("" or "/resubscribe"); `body` is None for GET.
    async fn forward(&self, token: &str, suffix: &str, body: Option<(Bytes, String)>) -> Response {
        let url = format!("{}/unsubscribe/{}{}", self.backend_url, token, suffix);
        let request = match body {
            Some((bytes, content_type)) => self
                .http
                .post(&url)
                .header(reqwest::header::CONTENT_TYPE, content_type)
                .body(bytes),
            None => self.http.get(&url),
        };

        let response = match request.send().await {
            Ok(response) => response,
            Err(e) => {
                warn!("unsubscribe proxy: backend unreachable: {}", e);
                return unavailable();
            }
        };

        let status =
            StatusCode::from_u16(response.status().as_u16()).unwrap_or(StatusCode::BAD_GATEWAY);
        let content_type = response
            .headers()
            .get(reqwest::header::CONTENT_TYPE)
            .and_then(|v| v.to_str().ok())
            .and_then(|v| HeaderValue::from_str(v).ok())
            .unwrap_or_else(|| HeaderValue::from_static("text/html; charset=utf-8"));
        let body = match response.bytes().await {
            Ok(body) => body,
            Err(e) => {
                warn!("unsubscribe proxy: truncated backend response: {}", e);
                return unavailable();
            }
        };

        // Only the headers a recipient-facing page needs travel back: a
        // Set-Cookie or auth header from the backend has no business on the
        // customer's domain.
        (
            status,
            [
                (header::CONTENT_TYPE, content_type),
                (header::CACHE_CONTROL, HeaderValue::from_static("no-store")),
                (
                    header::HeaderName::from_static("x-robots-tag"),
                    HeaderValue::from_static("noindex"),
                ),
            ],
            Bytes::from(body.to_vec()),
        )
            .into_response()
    }
}

fn unavailable() -> Response {
    (
        StatusCode::SERVICE_UNAVAILABLE,
        [
            (
                header::CONTENT_TYPE,
                HeaderValue::from_static("text/html; charset=utf-8"),
            ),
            (header::CACHE_CONTROL, HeaderValue::from_static("no-store")),
        ],
        UNAVAILABLE_HTML,
    )
        .into_response()
}

/// Tokens are base64url without padding. Rejecting anything else here keeps a
/// path-traversal attempt or a spray of junk away from the backend entirely.
pub fn valid_token(token: &str) -> bool {
    (MIN_TOKEN_LEN..=MAX_TOKEN_LEN).contains(&token.len())
        && token
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}

/// The form content-type a browser or a provider sends, or the default when
/// the request carried none.
pub fn body_content_type(headers: &HeaderMap) -> String {
    headers
        .get(header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("application/x-www-form-urlencoded")
        .to_string()
}

/// What an unknown-shaped token gets: the backend's own wording for an invalid
/// link, so a probe cannot tell the two apart.
pub fn invalid_token() -> Response {
    (
        StatusCode::BAD_REQUEST,
        [
            (
                header::CONTENT_TYPE,
                HeaderValue::from_static("text/html; charset=utf-8"),
            ),
            (header::CACHE_CONTROL, HeaderValue::from_static("no-store")),
        ],
        r#"<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">
<title>This unsubscribe link is invalid</title>
<style>body{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;max-width:32rem;margin:4rem auto;padding:0 1.25rem;color:#0f172a;line-height:1.5}
h1{font-size:1.25rem;margin:0 0 .5rem}p{color:#475569;margin:0 0 1.25rem}</style></head>
<body><h1>This unsubscribe link is invalid</h1><p>Reply to the email instead and the sender will stop.</p></body></html>"#,
    )
        .into_response()
}

impl UnsubscribeProxy {
    pub async fn get(&self, token: &str) -> Response {
        self.forward(token, "", None).await
    }

    pub async fn post(&self, token: &str, body: Bytes, content_type: String) -> Response {
        self.forward(token, "", Some((body, content_type))).await
    }

    pub async fn resubscribe(&self, token: &str, body: Bytes, content_type: String) -> Response {
        self.forward(token, "/resubscribe", Some((body, content_type)))
            .await
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn token_shape_is_checked_before_the_backend_is_asked() {
        assert!(valid_token(&"a".repeat(96)));
        assert!(valid_token("abcABC012-_abcABC012-_abcABC012-_"));
        // A stored ticket: 22 base64url characters.
        assert!(valid_token("Xk3mP9qR2tLwAb7dEfGhIj"));
        // One character short of a ticket is not a token either shape mints.
        assert!(!valid_token("Xk3mP9qR2tLwAb7dEfGhI"));
        assert!(!valid_token("short"));
        assert!(!valid_token(&"a".repeat(MAX_TOKEN_LEN + 1)));
        // Traversal and separators never reach the proxied path.
        assert!(!valid_token(&format!("{}/../admin", "a".repeat(40))));
        assert!(!valid_token(&format!("{}?x=1", "a".repeat(40))));
        assert!(!valid_token(&format!("{}%2f", "a".repeat(40))));
    }
}
