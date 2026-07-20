use axum::{
    extract::{Path, State},
    http::{header, HeaderMap, StatusCode},
    response::{IntoResponse, Redirect, Response},
};
use chrono::Utc;
use moka::future::Cache;
use sha2::{Digest, Sha256};
use std::sync::Arc;
use std::time::Duration;

use crate::abuse::{is_prefetch, is_scanner, RateLimiter};
use crate::config::Config;
use crate::events::TrackingEvent;
use crate::links::{LinkResolver, Resolution};
use crate::producer::Producer;

// 1x1 transparent GIF (43 bytes)
const TRANSPARENT_GIF: &[u8] = &[
    0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0xFF, 0xFF, 0xFF,
    0x00, 0x00, 0x00, 0x21, 0xF9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2C, 0x00, 0x00, 0x00, 0x00,
    0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3B,
];

/// Cache key format: {event_type}:{task_id}:{ip_hash}
/// Prevents duplicate events from the same IP within a time window
type DedupeCache = Cache<String, ()>;

#[derive(Clone)]
pub struct AppState {
    pub producer: Producer,
    /// Cache to deduplicate tracking events
    /// Each event type + task + IP is cached for 1 hour
    pub dedupe_cache: Arc<DedupeCache>,
    /// Per-source request budget (anti-flood)
    pub rate_limiter: Arc<RateLimiter>,
    /// Click-ticket resolver (backend internal API + layered caches)
    pub links: Arc<LinkResolver>,
}

impl AppState {
    pub fn new(producer: Producer, config: &Config) -> Self {
        // Create cache with:
        // - Max 100k entries
        // - TTL of 1 hour per entry
        // - TTI (time to idle) of 30 minutes
        let dedupe_cache = Cache::builder()
            .max_capacity(100_000)
            .time_to_live(Duration::from_secs(3600)) // 1 hour
            .time_to_idle(Duration::from_secs(1800)) // 30 min idle
            .build();

        Self {
            producer,
            dedupe_cache: Arc::new(dedupe_cache),
            rate_limiter: Arc::new(RateLimiter::new(config.rate_limit_per_min)),
            links: Arc::new(LinkResolver::new(
                config.backend_internal_url.clone(),
                config.internal_api_token.clone(),
            )),
        }
    }

    /// Check if this event was already processed (returns true if duplicate)
    async fn is_duplicate(
        &self,
        event_type: &str,
        task_id: &str,
        ip_hash: &Option<String>,
    ) -> bool {
        let key = format!(
            "{}:{}:{}",
            event_type,
            task_id,
            ip_hash.as_deref().unwrap_or("unknown")
        );

        // Check if exists, if not insert
        if self.dedupe_cache.contains_key(&key) {
            return true;
        }

        // Insert into cache
        self.dedupe_cache.insert(key, ()).await;
        false
    }
}

/// Health check endpoint
pub async fn health() -> impl IntoResponse {
    (StatusCode::OK, "OK")
}

/// Open tracking pixel handler
/// GET /t/o/{task_id}.png
pub async fn track_open(
    State(state): State<AppState>,
    Path(task_id): Path<String>,
    headers: HeaderMap,
) -> Response {
    // Remove .png suffix if present
    let task_id = task_id.trim_end_matches(".png").to_string();

    // Validate task_id is a valid UUID format
    if uuid::Uuid::parse_str(&task_id).is_err() {
        return pixel_response();
    }

    // Extract IP hash for deduplication + rate limiting
    let ip_hash = extract_ip_hash(&headers);

    // Anti-flood: over-budget sources still get the pixel (real mail clients
    // must never see a broken image), but nothing is published.
    let source = ip_hash.clone().unwrap_or_else(|| "unknown".to_string());
    if !state.rate_limiter.allow(&source).await {
        return pixel_response();
    }

    // Extract metadata from request
    let user_agent = headers
        .get(header::USER_AGENT)
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string());

    // Speculative fetches and scanners are served but never counted.
    if is_prefetch(&headers) || is_scanner(user_agent.as_deref()) {
        return pixel_response();
    }

    // Check for duplicate (same task + IP within 1 hour)
    if state.is_duplicate("OPEN", &task_id, &ip_hash).await {
        // Still return pixel but don't publish event
        return pixel_response();
    }

    // Publish event asynchronously (fire and forget)
    let producer = state.producer.clone();
    tokio::spawn(async move {
        producer
            .publish(TrackingEvent {
                event_type: "EMAIL_OPENED".to_string(),
                task_id,
                original_url: None,
                timestamp: Utc::now().to_rfc3339(),
                user_agent,
                ip_hash,
            })
            .await;
    });

    pixel_response()
}

/// Click tracking redirect handler
/// GET /c/{link_id}
///
/// The email carries only this opaque ticket; the destination lives
/// server-side, so there is nothing to forge and no open-redirect surface.
/// Unknown tickets 404.
pub async fn track_click(
    State(state): State<AppState>,
    Path(link_id): Path<String>,
    headers: HeaderMap,
) -> Response {
    // Garbage dies before any lookup or counter work
    if uuid::Uuid::parse_str(&link_id).is_err() {
        return (StatusCode::NOT_FOUND, "Unknown link").into_response();
    }

    // Anti-flood: cap total request rate per source
    let ip_hash = extract_ip_hash(&headers);
    let source = ip_hash.clone().unwrap_or_else(|| "unknown".to_string());
    if !state.rate_limiter.allow(&source).await {
        return (StatusCode::TOO_MANY_REQUESTS, "Slow down").into_response();
    }

    let link = match state.links.resolve(&link_id, &source).await {
        Resolution::Found(link) => link,
        Resolution::NotFound => {
            return (StatusCode::NOT_FOUND, "Unknown link").into_response();
        }
        Resolution::Unavailable => {
            // Fail closed: never redirect a ticket we could not verify.
            return (StatusCode::SERVICE_UNAVAILABLE, "Try again shortly").into_response();
        }
    };

    // Extract metadata from request
    let user_agent = headers
        .get(header::USER_AGENT)
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string());

    // Security gateways and link previewers follow every URL in a message;
    // serve them the destination but never count a click.
    if is_prefetch(&headers) || is_scanner(user_agent.as_deref()) {
        return Redirect::temporary(&link.destination).into_response();
    }

    // Dedupe repeat clicks of the same ticket from the same source
    if state.is_duplicate("CLICK", &link_id, &ip_hash).await {
        return Redirect::temporary(&link.destination).into_response();
    }

    // Publish event asynchronously (fire and forget)
    let producer = state.producer.clone();
    let destination = link.destination.clone();
    tokio::spawn(async move {
        producer
            .publish(TrackingEvent {
                event_type: "EMAIL_CLICKED".to_string(),
                task_id: link.task_id,
                original_url: Some(destination),
                timestamp: Utc::now().to_rfc3339(),
                user_agent,
                ip_hash,
            })
            .await;
    });

    Redirect::temporary(&link.destination).into_response()
}

/// Return the transparent pixel response
fn pixel_response() -> Response {
    (
        StatusCode::OK,
        [
            (header::CONTENT_TYPE, "image/gif"),
            (header::CACHE_CONTROL, "no-cache, no-store, must-revalidate"),
            (header::PRAGMA, "no-cache"),
            (header::EXPIRES, "0"),
        ],
        TRANSPARENT_GIF,
    )
        .into_response()
}

/// Extract and hash IP address for privacy
fn extract_ip_hash(headers: &HeaderMap) -> Option<String> {
    // Try various headers for the real IP
    let ip = headers
        .get("x-forwarded-for")
        .and_then(|h| h.to_str().ok())
        .and_then(|s| s.split(',').next())
        .map(|s| s.trim().to_string())
        .or_else(|| {
            headers
                .get("x-real-ip")
                .and_then(|h| h.to_str().ok())
                .map(|s| s.to_string())
        })
        .or_else(|| {
            headers
                .get("cf-connecting-ip")
                .and_then(|h| h.to_str().ok())
                .map(|s| s.to_string())
        });

    ip.map(|ip| {
        // Hash the IP for privacy
        let mut hasher = Sha256::new();
        hasher.update(ip.as_bytes());
        let result = hasher.finalize();
        format!("{:x}", result)[..16].to_string() // Take first 16 chars
    })
}
