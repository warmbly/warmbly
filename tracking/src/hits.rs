//! Website page-view ingest: validate the snippet's payload, keep anything a
//! browser could lie about out of it, and forward to the backend, which turns
//! the user agent and IP into device and location and stores the row. Same
//! layered defenses as the click resolver: negative cache for unknown keys,
//! per-source miss budget, circuit breaker. Nothing in a request can name a
//! contact; only the click ticket does, and the backend verifies it.

use moka::future::Cache;
use serde::{Deserialize, Serialize};
use std::sync::atomic::{AtomicU32, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tracing::warn;

/// Largest request body accepted, in bytes. A page view is a few hundred.
pub const MAX_BODY_BYTES: usize = 8 * 1024;

const MAX_URL: usize = 2048;
const MAX_TITLE: usize = 512;
const MAX_REFERRER: usize = 2048;
const MAX_LANGUAGE: usize = 32;
const MAX_TIMEZONE: usize = 64;
const MAX_KEY: usize = 64;
const MIN_KEY: usize = 16;
const MAX_SCREEN: u32 = 20_000;

/// Consecutive backend failures before the breaker opens.
const BREAKER_TRIP: u32 = 5;
/// How long the breaker stays open once tripped.
const BREAKER_COOLDOWN: Duration = Duration::from_secs(15);
/// Unknown-site-key hits allowed per source per minute.
const MISS_BUDGET_PER_MIN: u32 = 12;
/// Repeat views of one URL by one browser inside this window are not
/// counted: double-fires, reloads, and back/forward cache restores.
const DEDUPE_WINDOW: Duration = Duration::from_secs(30);

/// The snippet's wire format. Short keys keep the beacon small.
#[derive(Deserialize)]
pub struct HitPayload {
    /// site key
    pub k: String,
    /// visitor id
    pub v: String,
    /// session id
    pub s: String,
    /// consent: "granted" | "implicit"
    #[serde(default)]
    pub c: String,
    /// identification ticket from the landing URL, if any
    #[serde(default)]
    pub t: String,
    pub u: String,
    #[serde(default)]
    pub ti: String,
    #[serde(default)]
    pub r: String,
    #[serde(default)]
    pub l: String,
    #[serde(default)]
    pub tz: String,
    #[serde(default)]
    pub sw: u32,
    #[serde(default)]
    pub sh: u32,
    #[serde(default)]
    pub ld: bool,
}

fn valid_key(k: &str) -> bool {
    (MIN_KEY..=MAX_KEY).contains(&k.len())
        && k.bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}

impl HitPayload {
    /// Structural validation only; policy (consent, allowed hosts) is the
    /// backend's call because it owns the settings.
    pub fn validate(&self) -> bool {
        valid_key(&self.k)
            && valid_key(&self.v)
            && valid_key(&self.s)
            && !self.u.is_empty()
            && self.u.len() <= MAX_URL
            && (self.u.starts_with("http://") || self.u.starts_with("https://"))
            && self.ti.len() <= MAX_TITLE
            && self.r.len() <= MAX_REFERRER
            && self.l.len() <= MAX_LANGUAGE
            && self.tz.len() <= MAX_TIMEZONE
            && self.sw <= MAX_SCREEN
            && self.sh <= MAX_SCREEN
            && self.t.len() <= MAX_KEY
            && self.c.len() <= 16
    }
}

/// What the backend receives: the payload plus the request facts only this
/// edge saw. Field names match the Go models.WebsiteHitRequest.
#[derive(Serialize)]
pub struct ForwardedHit {
    pub site_key: String,
    pub visitor_key: String,
    pub session_key: String,
    pub consent: String,
    pub identify_token: String,
    pub url: String,
    pub title: String,
    pub referrer: String,
    pub language: String,
    pub timezone: String,
    pub screen_width: u32,
    pub screen_height: u32,
    pub landing: bool,
    pub user_agent: String,
    pub ip: String,
    pub origin_host: String,
}

#[derive(Deserialize, Default)]
pub struct HitResponse {
    #[serde(default)]
    pub new_visitor_key: String,
}

pub enum Outcome {
    /// Stored (or quietly declined by policy); optionally the browser must
    /// adopt a new visitor id.
    Accepted(Option<String>),
    /// Site key confirmed unknown, or this source exhausted its miss budget.
    UnknownSite,
    /// Payload the backend refused as malformed.
    Malformed,
    /// Backend unavailable / breaker open. Nothing was stored.
    Unavailable,
}

pub struct HitForwarder {
    http: reqwest::Client,
    backend_url: String,
    internal_token: String,
    unknown_sites: Cache<String, ()>,
    miss_budget: Cache<String, Arc<AtomicU32>>,
    /// (visitor, URL) -> the id of the request that owns the entry.
    dedupe: Cache<String, u64>,
    next_request: AtomicU64,
    breaker_failures: AtomicU32,
    breaker_open_until_ms: AtomicU64,
    started: Instant,
}

impl HitForwarder {
    pub fn new(backend_url: String, internal_token: String) -> Self {
        Self {
            http: crate::serve_tls::backend_client()
                .timeout(Duration::from_secs(3))
                .build()
                .expect("reqwest client"),
            backend_url: backend_url.trim_end_matches('/').to_string(),
            internal_token,
            // Short negative TTL: a key rotated or enabled seconds ago must
            // start working without a restart.
            unknown_sites: Cache::builder()
                .max_capacity(100_000)
                .time_to_live(Duration::from_secs(60))
                .build(),
            miss_budget: Cache::builder()
                .max_capacity(50_000)
                .time_to_live(Duration::from_secs(60))
                .build(),
            dedupe: Cache::builder()
                .max_capacity(200_000)
                .time_to_live(DEDUPE_WINDOW)
                .build(),
            next_request: AtomicU64::new(1),
            breaker_failures: AtomicU32::new(0),
            breaker_open_until_ms: AtomicU64::new(0),
            started: Instant::now(),
        }
    }

    /// Claims the (visitor, URL) pair for this request. None when another
    /// request inside the window already owns it; otherwise the claim token
    /// to hand back to `forget` if the forward fails. The insert is atomic, so
    /// two concurrent requests cannot both pass.
    pub async fn claim(&self, visitor: &str, url: &str) -> Option<u64> {
        let token = self.next_request.fetch_add(1, Ordering::Relaxed);
        let entry = self
            .dedupe
            .entry(format!("{}:{}", visitor, url))
            .or_insert(token)
            .await;
        if entry.is_fresh() {
            Some(token)
        } else {
            None
        }
    }

    /// Releases a claim after a failed forward so the retry is counted. Only
    /// the owning request can release it: a concurrent request that succeeded
    /// keeps its entry.
    pub async fn forget(&self, visitor: &str, url: &str, token: u64) {
        let key = format!("{}:{}", visitor, url);
        if self.dedupe.get(&key).await == Some(token) {
            self.dedupe.invalidate(&key).await;
        }
    }

    pub async fn forward(&self, hit: ForwardedHit, source: &str) -> Outcome {
        if self.unknown_sites.contains_key(&hit.site_key) {
            self.count_miss(source).await;
            return Outcome::UnknownSite;
        }
        if !self.miss_allowed(source).await {
            return Outcome::UnknownSite;
        }
        if self.breaker_is_open() {
            return Outcome::Unavailable;
        }

        let url = format!("{}/api/v1/internal/page-hits", self.backend_url);
        let response = self
            .http
            .post(&url)
            .bearer_auth(&self.internal_token)
            .json(&hit)
            .send()
            .await;

        match response {
            Ok(resp) if resp.status() == reqwest::StatusCode::NO_CONTENT => {
                self.breaker_failures.store(0, Ordering::Relaxed);
                Outcome::Accepted(None)
            }
            Ok(resp) if resp.status().is_success() => {
                self.breaker_failures.store(0, Ordering::Relaxed);
                let body = resp.json::<HitResponse>().await.unwrap_or_default();
                let rotate = if body.new_visitor_key.is_empty() {
                    None
                } else {
                    Some(body.new_visitor_key)
                };
                Outcome::Accepted(rotate)
            }
            Ok(resp) if resp.status() == reqwest::StatusCode::NOT_FOUND => {
                self.breaker_failures.store(0, Ordering::Relaxed);
                self.unknown_sites.insert(hit.site_key.clone(), ()).await;
                self.count_miss(source).await;
                Outcome::UnknownSite
            }
            Ok(resp) if resp.status() == reqwest::StatusCode::BAD_REQUEST => {
                self.breaker_failures.store(0, Ordering::Relaxed);
                Outcome::Malformed
            }
            Ok(resp) => {
                warn!("page-hit forward unexpected status: {}", resp.status());
                self.record_failure();
                Outcome::Unavailable
            }
            Err(e) => {
                warn!("page-hit forward failed: {}", e);
                self.record_failure();
                Outcome::Unavailable
            }
        }
    }

    async fn miss_allowed(&self, source: &str) -> bool {
        let counter = self
            .miss_budget
            .get_with(source.to_string(), async { Arc::new(AtomicU32::new(0)) })
            .await;
        counter.load(Ordering::Relaxed) < MISS_BUDGET_PER_MIN
    }

    async fn count_miss(&self, source: &str) {
        let counter = self
            .miss_budget
            .get_with(source.to_string(), async { Arc::new(AtomicU32::new(0)) })
            .await;
        counter.fetch_add(1, Ordering::Relaxed);
    }

    fn now_ms(&self) -> u64 {
        self.started.elapsed().as_millis() as u64
    }

    fn breaker_is_open(&self) -> bool {
        self.now_ms() < self.breaker_open_until_ms.load(Ordering::Relaxed)
    }

    fn record_failure(&self) {
        let failures = self.breaker_failures.fetch_add(1, Ordering::Relaxed) + 1;
        if failures >= BREAKER_TRIP {
            self.breaker_open_until_ms.store(
                self.now_ms() + BREAKER_COOLDOWN.as_millis() as u64,
                Ordering::Relaxed,
            );
            self.breaker_failures.store(0, Ordering::Relaxed);
            warn!("page-hit breaker open for {:?}", BREAKER_COOLDOWN);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn payload(url: &str) -> HitPayload {
        HitPayload {
            k: "0123456789abcdef0123456789abcdef".into(),
            v: "0123456789abcdef0123456789abcdef".into(),
            s: "0123456789abcdef".into(),
            c: "granted".into(),
            t: String::new(),
            u: url.into(),
            ti: String::new(),
            r: String::new(),
            l: String::new(),
            tz: String::new(),
            sw: 0,
            sh: 0,
            ld: true,
        }
    }

    #[tokio::test]
    async fn claim_is_exclusive_and_forget_only_releases_the_owner() {
        let f = HitForwarder::new("http://127.0.0.1:9".into(), "t".into());
        let a = f.claim("v", "u").await.expect("first claim wins");
        assert!(
            f.claim("v", "u").await.is_none(),
            "second claim inside the window is a duplicate"
        );
        // A stale token (a concurrent loser) cannot release the winner's entry.
        f.forget("v", "u", a + 1).await;
        assert!(f.claim("v", "u").await.is_none());
        // The owner can.
        f.forget("v", "u", a).await;
        assert!(f.claim("v", "u").await.is_some());
    }

    #[test]
    fn accepts_a_plain_page_view() {
        assert!(payload("https://example.com/pricing").validate());
    }

    #[test]
    fn refuses_non_http_urls_and_short_keys() {
        assert!(!payload("javascript:alert(1)").validate());
        let mut p = payload("https://example.com/");
        p.v = "short".into();
        assert!(!p.validate());
        let mut p = payload("https://example.com/");
        p.k = "has spaces in it!!".into();
        assert!(!p.validate());
    }

    #[test]
    fn refuses_oversized_fields() {
        let mut p = payload("https://example.com/");
        p.ti = "x".repeat(MAX_TITLE + 1);
        assert!(!p.validate());
        let mut p = payload("https://example.com/");
        p.sw = MAX_SCREEN + 1;
        assert!(!p.validate());
    }
}
