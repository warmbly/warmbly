//! Anti-abuse layer for the tracking endpoints.
//!
//! Two independent controls, applied before an event reaches Kafka:
//! - per-source rate limiting (fixed 60s window, bounded cache)
//! - prefetch / scanner filtering (the response is still served so real
//!   clients never break; only the analytics event is suppressed)
//!
//! Open-redirect protection lives in the ticket design itself (`links.rs`):
//! destinations never travel inside the URL, so there is nothing to forge.

use axum::http::HeaderMap;
use moka::future::Cache;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;
use std::time::Duration;

/// Fixed-window per-source request counter. Window resets via entry TTL, the
/// cache is hard-capped so a botnet rotating sources cannot exhaust memory.
pub struct RateLimiter {
    buckets: Cache<String, Arc<AtomicU32>>,
    limit: u32,
}

impl RateLimiter {
    pub fn new(limit_per_min: u32) -> Self {
        Self {
            buckets: Cache::builder()
                .max_capacity(50_000)
                .time_to_live(Duration::from_secs(60))
                .build(),
            limit: limit_per_min,
        }
    }

    /// Returns true while the source is within its per-minute budget.
    pub async fn allow(&self, source: &str) -> bool {
        let counter = self
            .buckets
            .get_with(source.to_string(), async { Arc::new(AtomicU32::new(0)) })
            .await;
        counter.fetch_add(1, Ordering::Relaxed) < self.limit
    }
}

/// Browser/link-warming prefetches and previews: the fetch is speculative,
/// not a human open/click, so it must not count.
pub fn is_prefetch(headers: &HeaderMap) -> bool {
    for name in ["sec-purpose", "purpose", "x-purpose", "x-moz"] {
        if let Some(value) = headers.get(name).and_then(|h| h.to_str().ok()) {
            let value = value.to_ascii_lowercase();
            if value.contains("prefetch")
                || value.contains("preview")
                || value.contains("prerender")
            {
                return true;
            }
        }
    }
    false
}

/// UA markers for crawlers, CLI clients, link-expanding chat apps, uptime
/// monitors, and email security gateways that follow every link in a message.
/// Gmail's image proxy is deliberately NOT listed: it is the only open signal
/// Gmail exposes, and filtering it would zero out opens for Gmail recipients.
const SCANNER_UA_MARKERS: &[&str] = &[
    "bot",
    "spider",
    "crawl",
    "curl/",
    "wget/",
    "python-requests",
    "python/",
    "go-http-client",
    "okhttp",
    "java/",
    "headless",
    "phantomjs",
    "validator",
    "pingdom",
    "uptime",
    "statuscake",
    "site24x7",
    "bingpreview",
    "skypeuripreview",
    "whatsapp",
    "telegram",
    // email security gateways / link rewriters
    "urldefense",
    "safelinks",
    "barracuda",
    "mimecast",
    "proofpoint",
    "forcepoint",
    "symantec",
    "trendmicro",
    "sophos",
    "zscaler",
];

pub fn is_scanner(user_agent: Option<&str>) -> bool {
    let Some(ua) = user_agent else {
        // No UA at all is never a real mail client or browser.
        return true;
    };
    let ua = ua.to_ascii_lowercase();
    SCANNER_UA_MARKERS.iter().any(|marker| ua.contains(marker))
}
