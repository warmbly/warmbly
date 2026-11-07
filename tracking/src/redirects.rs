//! Sending-domain redirects.
//!
//! A workspace can point the bare domain it sends from (and its www) at this
//! service; once the backend has verified the DNS, a visit is answered with a
//! redirect to the company's main website. The host is the only input, so the
//! same layered defenses as the click resolver keep a Host-header spray away
//! from the backend: positive and negative caches, a per-source miss budget
//! and a circuit breaker.

use moka::future::Cache;
use serde::Deserialize;
use std::sync::atomic::{AtomicU32, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tracing::warn;

#[derive(Deserialize)]
struct RedirectResponse {
    target_url: String,
}

pub enum Lookup {
    Found(String),
    NotFound,
    Unavailable,
}

const BREAKER_TRIP: u32 = 5;
const BREAKER_COOLDOWN: Duration = Duration::from_secs(15);
/// Unknown-host lookups allowed per source per minute.
const MISS_BUDGET_PER_MIN: u32 = 12;

pub struct RedirectResolver {
    http: reqwest::Client,
    backend_url: String,
    internal_token: String,
    found: Cache<String, String>,
    not_found: Cache<String, ()>,
    miss_budget: Cache<String, Arc<AtomicU32>>,
    breaker_failures: AtomicU32,
    breaker_open_until_ms: AtomicU64,
    started: Instant,
}

/// normalize_host lower-cases a Host header and drops its port and trailing dot.
pub fn normalize_host(raw: &str) -> Option<String> {
    let host = raw.trim().to_ascii_lowercase();
    let host = match host.rsplit_once(':') {
        Some((h, port)) if port.chars().all(|c| c.is_ascii_digit()) && !h.contains(':') => {
            h.to_string()
        }
        _ => host,
    };
    let host = host.trim_end_matches('.').to_string();
    let valid = !host.is_empty()
        && host.len() <= 253
        && host.contains('.')
        && host
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '.');
    if valid {
        Some(host)
    } else {
        None
    }
}

impl RedirectResolver {
    pub fn new(backend_url: String, internal_token: String) -> Self {
        Self {
            http: crate::serve_tls::backend_client()
                .timeout(Duration::from_secs(3))
                .redirect(reqwest::redirect::Policy::none())
                .build()
                .expect("reqwest client"),
            backend_url: backend_url.trim_end_matches('/').to_string(),
            internal_token,
            // Short enough that a changed target or a removed redirect takes effect within minutes.
            found: Cache::builder()
                .max_capacity(50_000)
                .time_to_live(Duration::from_secs(300))
                .build(),
            not_found: Cache::builder()
                .max_capacity(50_000)
                .time_to_live(Duration::from_secs(60))
                .build(),
            miss_budget: Cache::builder()
                .max_capacity(50_000)
                .time_to_live(Duration::from_secs(60))
                .build(),
            breaker_failures: AtomicU32::new(0),
            breaker_open_until_ms: AtomicU64::new(0),
            started: Instant::now(),
        }
    }

    /// lookup resolves a host; count_miss spends the source's miss budget on an
    /// unknown host, which ordinary traffic on a tracking host must not.
    pub async fn lookup(&self, host: &str, source: &str, count_miss: bool) -> Lookup {
        if let Some(target) = self.found.get(host).await {
            return Lookup::Found(target);
        }
        if self.not_found.contains_key(host) {
            return Lookup::NotFound;
        }
        if count_miss && !self.miss_allowed(source).await {
            return Lookup::NotFound;
        }
        if self.breaker_is_open() {
            return Lookup::Unavailable;
        }
        let url = format!(
            "{}/api/v1/internal/domain-redirects/{}",
            self.backend_url, host
        );
        match self
            .http
            .get(&url)
            .bearer_auth(&self.internal_token)
            .send()
            .await
        {
            Ok(resp) if resp.status().is_success() => match resp.json::<RedirectResponse>().await {
                Ok(body) if safe_target(&body.target_url) => {
                    self.breaker_failures.store(0, Ordering::Relaxed);
                    self.found
                        .insert(host.to_string(), body.target_url.clone())
                        .await;
                    Lookup::Found(body.target_url)
                }
                Ok(_) => {
                    self.not_found.insert(host.to_string(), ()).await;
                    Lookup::NotFound
                }
                Err(e) => {
                    warn!("domain-redirect decode failed: {}", e);
                    self.record_failure();
                    Lookup::Unavailable
                }
            },
            Ok(resp) if resp.status() == reqwest::StatusCode::NOT_FOUND => {
                self.breaker_failures.store(0, Ordering::Relaxed);
                self.not_found.insert(host.to_string(), ()).await;
                if count_miss {
                    self.count_miss(source).await;
                }
                Lookup::NotFound
            }
            Ok(resp) => {
                warn!(
                    "domain-redirect lookup unexpected status: {}",
                    resp.status()
                );
                self.record_failure();
                Lookup::Unavailable
            }
            Err(e) => {
                warn!("domain-redirect lookup failed: {}", e);
                self.record_failure();
                Lookup::Unavailable
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
            warn!("domain-redirect breaker open for {:?}", BREAKER_COOLDOWN);
        }
    }
}

/// safe_target accepts only an absolute http(s) address, so nothing the backend
/// returns can become a script or a relative path in the Location header.
pub fn safe_target(target: &str) -> bool {
    let lower = target.to_ascii_lowercase();
    (lower.starts_with("https://") || lower.starts_with("http://"))
        && target.len() <= 2048
        && !target.chars().any(|c| c.is_control() || c == ' ')
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalize_host_strips_port_and_dot() {
        assert_eq!(normalize_host("Acme.IO:443").as_deref(), Some("acme.io"));
        assert_eq!(
            normalize_host("www.acme.io.").as_deref(),
            Some("www.acme.io")
        );
        assert_eq!(normalize_host("localhost"), None);
        assert_eq!(normalize_host("evil.com/../x"), None);
        assert_eq!(normalize_host(""), None);
    }

    #[test]
    fn safe_target_refuses_scripts_and_relative_paths() {
        assert!(safe_target("https://acme.com/"));
        assert!(!safe_target("javascript:alert(1)"));
        assert!(!safe_target("//evil.com"));
        assert!(!safe_target("https://acme.com/\r\nSet-Cookie: x"));
    }
}
