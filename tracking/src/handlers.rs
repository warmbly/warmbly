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

use crate::kafka::{KafkaProducer, TrackingEvent};

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
}

impl AppState {
    pub fn new(kafka: KafkaProducer) -> Self {
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
            kafka,
            dedupe_cache: Arc::new(dedupe_cache),
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

    // Extract IP hash for deduplication
    let ip_hash = extract_ip_hash(&headers);

    // Check for duplicate (same task + IP within 1 hour)
    if state.is_duplicate("OPEN", &task_id, &ip_hash).await {
        // Still return pixel but don't publish event
        return pixel_response();
    }

    // Extract metadata from request
    let user_agent = headers
        .get(header::USER_AGENT)
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string());

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
    if !original_url.starts_with("http://") && !original_url.starts_with("https://") {
        return (StatusCode::BAD_REQUEST, "Invalid URL").into_response();
    }

    // Validate task_id is a valid UUID format
    if uuid::Uuid::parse_str(&task_id).is_err() {
        // Still redirect but don't track
        return Redirect::temporary(&original_url).into_response();
    }

    // Extract IP hash for deduplication
    let ip_hash = extract_ip_hash(&headers);

    // Create a unique key for this specific link click (task + URL + IP)
    let url_hash = {
        let mut hasher = Sha256::new();
        hasher.update(original_url.as_bytes());
        let result = hasher.finalize();
        format!("{:x}", result)[..8].to_string()
    };

    let dedupe_key = format!("{}:{}", task_id, url_hash);

    // Check for duplicate (same task + URL + IP within 1 hour)
    if state.is_duplicate("CLICK", &dedupe_key, &ip_hash).await {
        // Still redirect but don't publish event
        return Redirect::temporary(&original_url).into_response();
    }

    // Extract metadata from request
    let user_agent = headers
        .get(header::USER_AGENT)
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string());

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
