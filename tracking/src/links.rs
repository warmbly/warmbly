//! Click-ticket resolver.
//!
//! Emails carry only an opaque link id (`/c/<uuid>`); this module resolves it
//! to the stored destination via the backend internal API. The layered
//! defenses keep a ticket-spray attack away from the backend:
//!
//! 1. positive cache: resolved tickets are served from memory
//! 2. negative cache: recently-confirmed-unknown ids are 404d from memory
//! 3. per-source miss budget: real clickers essentially never miss, so a
//!    source accumulating misses is probing and gets cut off without lookups
//! 4. circuit breaker: when the backend errors/times out, lookups stop for a
//!    cooldown and misses fail closed instead of piling on

use moka::future::Cache;
use serde::Deserialize;
use std::sync::atomic::{AtomicU32, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tracing::warn;

#[derive(Clone, Debug)]
pub struct ResolvedLink {
    pub destination: String,
    pub task_id: String,
}

#[derive(Deserialize)]
struct LinkResponse {
    destination: String,
    task_id: String,
}

pub enum Resolution {
    /// Ticket is known; redirect + count.
    Found(ResolvedLink),
    /// Ticket is confirmed unknown (or this source exhausted its miss budget).
    NotFound,
    /// Backend unavailable / breaker open; fail closed without counting a miss.
    Unavailable,
}

/// Consecutive backend failures before the breaker opens.
const BREAKER_TRIP: u32 = 5;
/// How long the breaker stays open once tripped.
const BREAKER_COOLDOWN: Duration = Duration::from_secs(15);
/// Unknown-ticket lookups allowed per source per minute. Legitimate clicks
/// resolve, so anything past a handful of misses is a probe.
const MISS_BUDGET_PER_MIN: u32 = 12;

pub struct LinkResolver {
    http: reqwest::Client,
    backend_url: String,
    internal_token: String,
    found: Cache<String, ResolvedLink>,
    not_found: Cache<String, ()>,
    miss_budget: Cache<String, Arc<AtomicU32>>,
    breaker_failures: AtomicU32,
    breaker_open_until_ms: AtomicU64,
    started: Instant,
}

impl LinkResolver {
    pub fn new(backend_url: String, internal_token: String) -> Self {
        Self {
            http: reqwest::Client::builder()
                .timeout(Duration::from_secs(3))
                .build()
                .expect("reqwest client"),
            backend_url: backend_url.trim_end_matches('/').to_string(),
            internal_token,
            // Tickets are immutable once minted; a long TTL is safe and keeps
            // repeat clicks (forwarded emails, retries) off the backend.
            found: Cache::builder()
                .max_capacity(200_000)
                .time_to_live(Duration::from_secs(24 * 3600))
                .build(),
            // Short negative TTL: protects against repeat probes of one id
            // without permanently 404ing a ticket minted milliseconds later.
            not_found: Cache::builder()
                .max_capacity(100_000)
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

    pub async fn resolve(&self, link_id: &str, source: &str) -> Resolution {
        if let Some(link) = self.found.get(link_id).await {
            return Resolution::Found(link);
        }

        if self.not_found.contains_key(link_id) {
            self.count_miss(source).await;
            return Resolution::NotFound;
        }

        // Probing sources get cut off before any backend traffic.
        if !self.miss_allowed(source).await {
            return Resolution::NotFound;
        }

        if self.breaker_is_open() {
            return Resolution::Unavailable;
        }

        let url = format!(
            "{}/api/v1/internal/tracked-links/{}",
            self.backend_url, link_id
        );
        let response = self
            .http
            .get(&url)
            .bearer_auth(&self.internal_token)
            .send()
            .await;

        match response {
            Ok(resp) if resp.status().is_success() => match resp.json::<LinkResponse>().await {
                Ok(body) => {
                    self.breaker_failures.store(0, Ordering::Relaxed);
                    let link = ResolvedLink {
                        destination: body.destination,
                        task_id: body.task_id,
                    };
                    self.found.insert(link_id.to_string(), link.clone()).await;
                    Resolution::Found(link)
                }
                Err(e) => {
                    warn!("tracked-link decode failed: {}", e);
                    self.record_failure();
                    Resolution::Unavailable
                }
            },
            Ok(resp) if resp.status() == reqwest::StatusCode::NOT_FOUND => {
                self.breaker_failures.store(0, Ordering::Relaxed);
                self.not_found.insert(link_id.to_string(), ()).await;
                self.count_miss(source).await;
                Resolution::NotFound
            }
            Ok(resp) => {
                warn!("tracked-link lookup unexpected status: {}", resp.status());
                self.record_failure();
                Resolution::Unavailable
            }
            Err(e) => {
                warn!("tracked-link lookup failed: {}", e);
                self.record_failure();
                Resolution::Unavailable
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
            warn!("tracked-link breaker open for {:?}", BREAKER_COOLDOWN);
        }
    }
}
