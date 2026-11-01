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

use crate::unsubscribe_i18n::{PageCopy, COPIES};

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

pub struct UnsubscribeProxy {
    http: reqwest::Client,
    backend_url: String,
}

impl UnsubscribeProxy {
    pub fn new(backend_url: String) -> Self {
        Self {
            http: crate::serve_tls::backend_client()
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
    /// ("" or "/resubscribe"); `body` is None for GET. The browser's
    /// Accept-Language travels on, so the backend's page is in the
    /// recipient's language.
    async fn forward(
        &self,
        token: &str,
        suffix: &str,
        body: Option<(Bytes, String)>,
        accept_language: Option<String>,
    ) -> Response {
        let url = format!("{}/unsubscribe/{}{}", self.backend_url, token, suffix);
        let request = match body {
            Some((bytes, content_type)) => self
                .http
                .post(&url)
                .header(reqwest::header::CONTENT_TYPE, content_type)
                .body(bytes),
            None => self.http.get(&url),
        };
        let request = match &accept_language {
            Some(lang) => request.header(reqwest::header::ACCEPT_LANGUAGE, lang),
            None => request,
        };

        let response = match request.send().await {
            Ok(response) => response,
            Err(e) => {
                warn!("unsubscribe proxy: backend unreachable: {}", e);
                return unavailable(accept_language.as_deref());
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
                return unavailable(accept_language.as_deref());
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
                (header::VARY, HeaderValue::from_static("Accept-Language")),
            ],
            Bytes::from(body.to_vec()),
        )
            .into_response()
    }
}

/// What a recipient sees when the backend cannot be reached. Neutral, like the
/// backend's own pages: the email came from the customer's mailbox, so no
/// brand is named, and replying is a route to the same outcome because reply
/// opt-outs are detected and suppressed too.
fn unavailable(accept_language: Option<&str>) -> Response {
    let (lang, copy) = page_copy(accept_language);
    own_page(
        StatusCode::SERVICE_UNAVAILABLE,
        lang,
        copy,
        copy.retry_title,
        copy.unavailable_body,
    )
}

/// One of the two pages the proxy answers itself, as plain as the backend's.
fn own_page(status: StatusCode, lang: &str, copy: &PageCopy, title: &str, body: &str) -> Response {
    let dir = if copy.rtl { r#" dir="rtl""# } else { "" };
    let html = format!(
        r#"<!doctype html><html lang="{lang}"{dir}><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">
<title>{title}</title>
<style>body{{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;max-width:32rem;margin:4rem auto;padding:0 1.25rem;color:#0f172a;line-height:1.5}}
h1{{font-size:1.25rem;margin:0 0 .5rem}}p{{color:#475569;margin:0 0 1.25rem}}</style></head>
<body><h1>{title}</h1><p>{body}</p></body></html>"#,
        title = escape(title),
        body = escape(body),
    );
    (
        status,
        [
            (
                header::CONTENT_TYPE,
                HeaderValue::from_static("text/html; charset=utf-8"),
            ),
            (header::CACHE_CONTROL, HeaderValue::from_static("no-store")),
            (header::VARY, HeaderValue::from_static("Accept-Language")),
        ],
        html,
    )
        .into_response()
}

fn escape(s: &str) -> String {
    s.replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
}

/// The browser's Accept-Language, when it sent one.
pub fn accept_language(headers: &HeaderMap) -> Option<String> {
    headers
        .get(header::ACCEPT_LANGUAGE)
        .and_then(|v| v.to_str().ok())
        .map(str::to_string)
}

/// The most preferred language the pages are written in, English when none
/// is. Mirrors unsubLanguage in the backend, so a recipient reads one
/// language whichever side answers.
pub fn page_copy(accept_language: Option<&str>) -> (&'static str, &'static PageCopy) {
    let mut best: Option<(&'static str, &'static PageCopy, f64)> = None;
    for part in accept_language.unwrap_or("").split(',') {
        let mut fields = part.trim().splitn(2, ';');
        let tag = fields.next().unwrap_or("");
        let q = match fields.next().map(str::trim) {
            Some(params) => match params.strip_prefix("q=") {
                Some(v) => match v.parse::<f64>() {
                    Ok(q) => q,
                    Err(_) => continue,
                },
                None => 1.0,
            },
            None => 1.0,
        };
        // A qvalue is 0 to 1, and NaN is refused like the backend refuses it.
        if !(q > 0.0 && q <= 1.0) || best.is_some_and(|(_, _, b)| q <= b) {
            continue;
        }
        if let Some((code, copy)) = lookup(tag) {
            best = Some((code, copy, q));
        }
    }
    match best {
        Some((code, copy, _)) => (code, copy),
        None => (COPIES[0].0, &COPIES[0].1),
    }
}

/// The copy a language tag asks for. Norwegian and Tagalog tags name the
/// language written as nb and fil, and Taiwan, Hong Kong and Macau read
/// Traditional Chinese.
fn lookup(tag: &str) -> Option<(&'static str, &'static PageCopy)> {
    let tag = tag.to_ascii_lowercase().replace('_', "-");
    let mut parts = tag.split('-');
    let primary = parts.next().unwrap_or("");
    let code = match primary {
        "no" | "nn" => "nb",
        "tl" => "fil",
        "zh" if parts.any(|p| matches!(p, "hant" | "tw" | "hk" | "mo")) => "zh-Hant",
        other => other,
    };
    COPIES
        .iter()
        .find(|(c, _)| *c == code)
        .map(|(c, copy)| (*c, copy))
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
/// link, in the recipient's language, so a probe cannot tell the two apart.
pub fn invalid_token(headers: &HeaderMap) -> Response {
    let accept = accept_language(headers);
    let (lang, copy) = page_copy(accept.as_deref());
    own_page(
        StatusCode::BAD_REQUEST,
        lang,
        copy,
        copy.invalid_title,
        copy.reply_body,
    )
}

impl UnsubscribeProxy {
    pub async fn get(&self, token: &str, accept_language: Option<String>) -> Response {
        self.forward(token, "", None, accept_language).await
    }

    pub async fn post(
        &self,
        token: &str,
        body: Bytes,
        content_type: String,
        accept_language: Option<String>,
    ) -> Response {
        self.forward(token, "", Some((body, content_type)), accept_language)
            .await
    }

    pub async fn resubscribe(
        &self,
        token: &str,
        body: Bytes,
        content_type: String,
        accept_language: Option<String>,
    ) -> Response {
        self.forward(
            token,
            "/resubscribe",
            Some((body, content_type)),
            accept_language,
        )
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

    #[test]
    fn page_language_follows_the_browser() {
        let lang = |h: &str| page_copy(Some(h)).0;
        assert_eq!(page_copy(None).0, "en");
        assert_eq!(lang("de-DE,de;q=0.9,en;q=0.8"), "de");
        assert_eq!(lang("en-US,en;q=0.9,de;q=0.8"), "en");
        assert_eq!(lang("fr;q=0.5, de;q=0.9"), "de");
        assert_eq!(lang("xx-YY, pt-BR;q=0.7"), "pt");
        assert_eq!(lang("de;q=0"), "en");
        assert_eq!(lang("de;q=abc, it"), "it");
        assert_eq!(lang("zh-TW"), "zh-Hant");
        assert_eq!(lang("zh-CN"), "zh");
        assert_eq!(lang("nn-NO"), "nb");
        assert_eq!(lang("tl-PH"), "fil");
        assert_eq!(lang("ja-JP;q=0.8, ko-KR;q=0.8"), "ja");
        assert_eq!(lang("de;q=0.5,fr;q=NaN"), "de");
        assert_eq!(lang("de;q=0.5,fr;q=2"), "de");
        assert_eq!(lang("de;q=0.5,fr;q=inf"), "de");
    }

    #[tokio::test]
    async fn own_pages_are_in_the_recipients_language() {
        let mut headers = HeaderMap::new();
        headers.insert(header::ACCEPT_LANGUAGE, HeaderValue::from_static("ar"));
        let response = invalid_token(&headers);
        assert_eq!(response.status(), StatusCode::BAD_REQUEST);
        let body = axum::body::to_bytes(response.into_body(), usize::MAX)
            .await
            .unwrap();
        let html = String::from_utf8(body.to_vec()).unwrap();
        let (_, ar) = page_copy(Some("ar"));
        assert!(html.contains(r#"<html lang="ar" dir="rtl">"#));
        assert!(html.contains(ar.invalid_title));
        assert!(html.contains(ar.reply_body));
    }
}
