use axum::{
    extract::{Path, Query, State},
    http::{header, HeaderMap, StatusCode},
    response::{IntoResponse, Redirect, Response},
};
use chrono::Utc;
use moka::future::Cache;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;

use crate::abuse::{is_prefetch, is_scanner, verify_signature, RateLimiter};
use crate::config::Config;
use crate::kafka::{KafkaProducer, TrackingEvent};

/// Raw (still-encoded) `?url=` values longer than this are rejected before
/// decoding; decoded URLs are capped at the practical browser URL limit.
const MAX_RAW_URL_LEN: usize = 4096;
const MAX_URL_LEN: usize = 2048;

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
    pub kafka: KafkaProducer,
    /// Cache to deduplicate tracking events
    /// Each event type + task + IP is cached for 1 hour
    pub dedupe_cache: Arc<DedupeCache>,
    /// Per-source request budget (anti-flood)
    pub rate_limiter: Arc<RateLimiter>,
    /// Accepted signing secrets for click redirects, newest first (the
    /// retired key rides along during a rotation so in-flight emails keep
    /// working). None = legacy unsigned links.
    pub link_secrets: Option<Arc<Vec<String>>>,
}

impl AppState {
    pub fn new(kafka: KafkaProducer, config: &Config) -> Self {
        // Create cache with:
        // - Max 100k entries
        // - TTL of 1 hour per entry
        // - TTI (time to idle) of 30 minutes
        let dedupe_cache = Cache::builder()
            .max_capacity(100_000)
            .time_to_live(Duration::from_secs(3600)) // 1 hour
            .time_to_idle(Duration::from_secs(1800)) // 30 min idle
            .build();

        // Enforcement is keyed on the CURRENT secret: a leftover previous
        // secret with no current one means signing was turned off.
        let link_secrets = config.link_secret.clone().map(|current| {
            let mut secrets = vec![current];
            if let Some(previous) = config.link_secret_previous.clone() {
                secrets.push(previous);
            }
            Arc::new(secrets)
        });

        Self {
            kafka,
            dedupe_cache: Arc::new(dedupe_cache),
            rate_limiter: Arc::new(RateLimiter::new(config.rate_limit_per_min)),
            link_secrets,
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
    let kafka = state.kafka.clone();
    tokio::spawn(async move {
        kafka
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
/// GET /t/c/{task_id}?url={original_url}
pub async fn track_click(
    State(state): State<AppState>,
    Path(task_id): Path<String>,
    Query(params): Query<HashMap<String, String>>,
    headers: HeaderMap,
) -> Response {
    // Get original URL from query params first (we need to redirect regardless)
    let original_url = match params.get("url") {
        Some(url) => {
            if url.len() > MAX_RAW_URL_LEN {
                return (StatusCode::BAD_REQUEST, "URL too long").into_response();
            }
            // Decode URL
            urlencoding::decode(url)
                .map(|s| s.into_owned())
                .unwrap_or_else(|_| url.clone())
        }
        None => {
            return (StatusCode::BAD_REQUEST, "Missing url parameter").into_response();
        }
    };

    // Basic URL validation
    if original_url.len() > MAX_URL_LEN {
        return (StatusCode::BAD_REQUEST, "URL too long").into_response();
    }
    if !original_url.starts_with("http://") && !original_url.starts_with("https://") {
        return (StatusCode::BAD_REQUEST, "Invalid URL").into_response();
    }

    // Signed-link enforcement: when the shared secret is configured, only
    // redirects minted by our own send pipeline are honored. This is what
    // stops the tracking domain from being abused as an open redirector.
    // Any configured key may match (current, or the previous one during a
    // rotation grace window).
    if let Some(secrets) = &state.link_secrets {
        let sig = params.get("s").map(String::as_str);
        if !secrets
            .iter()
            .any(|secret| verify_signature(secret, &task_id, &original_url, sig))
        {
            return (StatusCode::NOT_FOUND, "Unknown link").into_response();
        }
    }

    // Anti-flood: refuse the redirect outright over budget. Unlike the pixel
    // there is no rendering concern, and serving unlimited redirects would
    // keep the redirector attractive to abusers even with events suppressed.
    let ip_hash = extract_ip_hash(&headers);
    let source = ip_hash.clone().unwrap_or_else(|| "unknown".to_string());
    if !state.rate_limiter.allow(&source).await {
        return (StatusCode::TOO_MANY_REQUESTS, "Slow down").into_response();
    }

    // Validate task_id is a valid UUID format
    if uuid::Uuid::parse_str(&task_id).is_err() {
        // Still redirect but don't track
        return Redirect::temporary(&original_url).into_response();
    }

    // Create a unique key for this specific link click (task + URL + IP)
    let url_hash = {
        let mut hasher = Sha256::new();
        hasher.update(original_url.as_bytes());
        let result = hasher.finalize();
        format!("{:x}", result)[..8].to_string()
    };

    let dedupe_key = format!("{}:{}", task_id, url_hash);

    // Extract metadata from request
    let user_agent = headers
        .get(header::USER_AGENT)
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string());

    // Security gateways and link previewers follow every URL in a message;
    // serve them the destination but never count a click.
    if is_prefetch(&headers) || is_scanner(user_agent.as_deref()) {
        return Redirect::temporary(&original_url).into_response();
    }

    // Check for duplicate (same task + URL + IP within 1 hour)
    if state.is_duplicate("CLICK", &dedupe_key, &ip_hash).await {
        // Still redirect but don't publish event
        return Redirect::temporary(&original_url).into_response();
    }

    // Publish event asynchronously (fire and forget)
    let kafka = state.kafka.clone();
    let original_url_clone = original_url.clone();
    tokio::spawn(async move {
        kafka
            .publish(TrackingEvent {
                event_type: "EMAIL_CLICKED".to_string(),
                task_id,
                original_url: Some(original_url_clone),
                timestamp: Utc::now().to_rfc3339(),
                user_agent,
                ip_hash,
            })
            .await;
    });

    // Redirect to original URL
    Redirect::temporary(&original_url).into_response()
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
