use axum::{
    body::Bytes,
    extract::{ConnectInfo, Path, State},
    http::{header, HeaderMap, StatusCode},
    response::{IntoResponse, Redirect, Response},
};
use chrono::Utc;
use moka::future::Cache;
use sha2::{Digest, Sha256};
use std::net::{IpAddr, SocketAddr};
use std::sync::Arc;
use std::time::Duration;

use crate::abuse::{is_prefetch, is_scanner, RateLimiter};
use crate::asndb::AsnDb;
use crate::config::Config;
use crate::events::TrackingEvent;
use crate::hits::{ForwardedHit, HitForwarder, HitPayload, Outcome};
use crate::links::{LinkResolver, Resolution};
use crate::producer::Producer;
use crate::scanners::{AsnSources, Request, ScannerNetworks};
use crate::unsubscribe::{body_content_type, invalid_token, valid_token, UnsubscribeProxy};

// 1x1 transparent GIF (43 bytes)
const TRANSPARENT_GIF: &[u8] = &[
    0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0xFF, 0xFF, 0xFF,
    0x00, 0x00, 0x00, 0x21, 0xF9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2C, 0x00, 0x00, 0x00, 0x00,
    0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3B,
];

/// Cache key format: {event_type}:{task_id}:{ip_hash}
/// Prevents duplicate events from the same IP within a time window
type DedupeCache = Cache<String, ()>;

/// Claims `key`, reporting whether somebody already had it.
///
/// One coalesced operation, not a check followed by an insert. Scanners fetch a
/// message's pixel and every link in parallel, so several requests carrying one
/// key are genuinely concurrent, and under check-then-insert every one of them
/// saw a miss and every one published.
///
/// `or_insert_with` is what gives the guarantee, and `or_insert` is not: only
/// the closure form runs once per key under a lock and tells the losers their
/// value was already there. An eager value races exactly like the code this
/// replaced.
async fn claim(cache: &DedupeCache, key: String) -> bool {
    !cache.entry(key).or_insert_with(async {}).await.is_fresh()
}

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
    /// Website page-view forwarder (backend internal API + layered caches)
    pub hits: Arc<HitForwarder>,
    /// Tighter per-source budget for page views than for pixels
    pub hit_rate_limiter: Arc<RateLimiter>,
    /// Opt-out pages get a budget of their own rather than sharing the pixel
    /// and click one. A single NAT or mail gateway can spend that shared
    /// counter on tracking alone, and the recipient behind it who then opens
    /// the unsubscribe link would be refused: the one request that must never
    /// be turned away. Same size, separate bucket; a source loading opt-out
    /// pages this fast is probing, not opting out.
    pub unsubscribe_rate_limiter: Arc<RateLimiter>,
    /// Known automated-scanner sources. A match labels the event and nothing
    /// else: the pixel is still served and the click still redirected.
    pub scanners: Arc<ScannerNetworks>,
    /// Proxies whose forwarded-IP header is believed, and which header
    pub trusted_proxies: Arc<Vec<ipnet::IpNet>>,
    pub client_ip_header: Arc<String>,
    /// Key for the source-address token (see `hash_ip`)
    pub ip_hash_key: Arc<String>,
    /// Recipient opt-out, proxied to the backend that owns the pages
    pub unsubscribe: Arc<UnsubscribeProxy>,
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
            hits: Arc::new(HitForwarder::new(
                config.backend_internal_url.clone(),
                config.internal_api_token.clone(),
            )),
            hit_rate_limiter: Arc::new(RateLimiter::new(config.pagehit_rate_limit_per_min)),
            unsubscribe_rate_limiter: Arc::new(RateLimiter::new(config.rate_limit_per_min)),
            scanners: Arc::new(ScannerNetworks::new(
                config.scanner_builtins,
                &config.scanner_networks,
                &config.scanner_click_networks,
                AsnSources {
                    header: Some(config.scanner_asn_header.clone()),
                    // An unset TRACKING_TRUSTED_PROXIES means no peer is ever
                    // trusted, so a named header is never read. The catalogue
                    // has to know that or it reports itself as able to match
                    // ASNs when it cannot.
                    trusted_proxies: !config.trusted_proxies.is_empty(),
                    db: AsnDb::open(&config.scanner_asn_db),
                },
            )),
            trusted_proxies: Arc::new(config.trusted_proxies.clone()),
            client_ip_header: Arc::new(config.client_ip_header.clone()),
            ip_hash_key: Arc::new(config.ip_hash_key.clone()),
            unsubscribe: Arc::new(UnsubscribeProxy::new(config.backend_internal_url.clone())),
        }
    }

    /// Whether this event was already processed, claiming it when it was not.
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
        claim(&self.dedupe_cache, key).await
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
    ConnectInfo(peer): ConnectInfo<SocketAddr>,
    Path(task_id): Path<String>,
    headers: HeaderMap,
) -> Response {
    // Remove .png suffix if present
    let task_id = task_id.trim_end_matches(".png").to_string();

    // Validate task_id is a valid UUID format
    if uuid::Uuid::parse_str(&task_id).is_err() {
        return pixel_response();
    }

    let trusted = peer_is_trusted(peer, &state.trusted_proxies);
    // The address is hashed for deduplication + rate limiting; only its
    // network travels with the event, for the location lookup downstream.
    let ip = client_ip(
        peer,
        &headers,
        &state.trusted_proxies,
        &state.client_ip_header,
    );
    let ip_hash = Some(hash_ip(&state.ip_hash_key, &ip));

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

    // A mail-filtering network fetching the pixel is delivery evidence, not a
    // read. The event is published and labelled rather than dropped, so the
    // consumer can record it as a machine open.
    let matched = state
        .scanners
        .classify(&ip, &headers, trusted, Request::Open);
    let scanner_probable = matched.as_ref().is_some_and(|m| m.probable);
    let scanner = matched.map(|m| m.label.to_string());

    // Publish event asynchronously (fire and forget)
    let producer = state.producer.clone();
    tokio::spawn(async move {
        producer
            .publish(TrackingEvent {
                event_type: "EMAIL_OPENED".to_string(),
                task_id,
                original_url: None,
                link_id: None,
                timestamp: Utc::now().to_rfc3339(),
                user_agent,
                ip_hash,
                client_ip: Some(anonymize_ip(&ip)).filter(|n| !n.is_empty()),
                scanner,
                scanner_probable,
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
    ConnectInfo(peer): ConnectInfo<SocketAddr>,
    Path(link_id): Path<String>,
    headers: HeaderMap,
) -> Response {
    // Garbage dies before any lookup or counter work
    if uuid::Uuid::parse_str(&link_id).is_err() {
        return (StatusCode::NOT_FOUND, "Unknown link").into_response();
    }

    let trusted = peer_is_trusted(peer, &state.trusted_proxies);
    // Anti-flood: cap total request rate per source. Only the address's
    // network rides along, for the location lookup downstream.
    let ip = client_ip(
        peer,
        &headers,
        &state.trusted_proxies,
        &state.client_ip_header,
    );
    let ip_hash = Some(hash_ip(&state.ip_hash_key, &ip));
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
    // serve them the destination but never count a click, and never hand
    // them the identification ticket.
    if is_prefetch(&headers) || is_scanner(user_agent.as_deref()) {
        return Redirect::temporary(&link.destination).into_response();
    }

    // Safe Links and its peers redirect the browser to the destination rather
    // than fetching it, so a person's click always reaches us from the
    // person's own address. A ticket walked from a mail-filtering network is
    // a scan: it is still redirected, and labelled so it never counts.
    let matched = state
        .scanners
        .classify(&ip, &headers, trusted, Request::Click);
    let scanner_probable = matched.as_ref().is_some_and(|m| m.probable);
    let scanner = matched.map(|m| m.label.to_string());

    // A probable source loses the identification ticket too. The edge does not
    // know when the send was dispatched, so it cannot tell the delivery-time
    // scan from the isolated human click the way the consumer can, and filing
    // a scanner's page load against the recipient is the worse of the two
    // mistakes available here.
    let target = redirect_target(
        &link.destination,
        &link_id,
        link.identify,
        scanner.as_deref(),
    );

    // Dedupe repeat clicks of the same ticket from the same source
    if state.is_duplicate("CLICK", &link_id, &ip_hash).await {
        return Redirect::temporary(&target).into_response();
    }

    // Publish event asynchronously (fire and forget)
    let producer = state.producer.clone();
    let destination = link.destination.clone();
    let ticket = link_id.clone();
    tokio::spawn(async move {
        producer
            .publish(TrackingEvent {
                event_type: "EMAIL_CLICKED".to_string(),
                task_id: link.task_id,
                original_url: Some(destination),
                link_id: Some(ticket),
                timestamp: Utc::now().to_rfc3339(),
                user_agent,
                ip_hash,
                client_ip: Some(anonymize_ip(&ip)).filter(|n| !n.is_empty()),
                scanner,
                scanner_probable,
            })
            .await;
    });

    Redirect::temporary(&target).into_response()
}

/// Recipient opt-out, served here because a workspace's verified tracking
/// domain is the host its campaign mail carries. The backend owns the pages
/// and the suppression; these three only shape-check the token, spend the
/// source's budget and hand the request on.
///
/// GET /unsubscribe/{token}
pub async fn unsubscribe_page(
    State(state): State<AppState>,
    ConnectInfo(peer): ConnectInfo<SocketAddr>,
    Path(token): Path<String>,
    headers: HeaderMap,
) -> Response {
    if !valid_token(&token) {
        return invalid_token();
    }
    if let Some(limited) = spend_unsubscribe_budget(&state, peer, &headers).await {
        return limited;
    }
    state.unsubscribe.get(&token).await
}

/// POST /unsubscribe/{token} — the confirm button, or a provider's RFC 8058
/// one-click. Never rate limited: a provider POSTing an opt-out is the one
/// request that must not be refused, and a refusal here is a spam complaint.
pub async fn unsubscribe_submit(
    State(state): State<AppState>,
    Path(token): Path<String>,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    if !valid_token(&token) {
        return invalid_token();
    }
    let content_type = body_content_type(&headers);
    state.unsubscribe.post(&token, body, content_type).await
}

/// POST /unsubscribe/{token}/resubscribe — the "unsubscribed by mistake" button.
pub async fn unsubscribe_undo(
    State(state): State<AppState>,
    ConnectInfo(peer): ConnectInfo<SocketAddr>,
    Path(token): Path<String>,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    if !valid_token(&token) {
        return invalid_token();
    }
    if let Some(limited) = spend_unsubscribe_budget(&state, peer, &headers).await {
        return limited;
    }
    let content_type = body_content_type(&headers);
    state
        .unsubscribe
        .resubscribe(&token, body, content_type)
        .await
}

/// Charges one request against the source's opt-out budget, returning the
/// refusal when it is spent. A recipient opts out once, so this budget is
/// never reached by real traffic; it exists to cap token spraying, and it is
/// deliberately not the counter that pixels and clicks spend.
async fn spend_unsubscribe_budget(
    state: &AppState,
    peer: SocketAddr,
    headers: &HeaderMap,
) -> Option<Response> {
    let ip = client_ip(
        peer,
        headers,
        &state.trusted_proxies,
        &state.client_ip_header,
    );
    let source = hash_ip(&state.ip_hash_key, &ip);
    if state.unsubscribe_rate_limiter.allow(&source).await {
        return None;
    }
    Some((StatusCode::TOO_MANY_REQUESTS, "Slow down").into_response())
}

/// Where a click is sent. When the workspace registered the destination's host
/// for website tracking, the identification ticket rides along so the snippet
/// can tie the browser to the recipient; it is opaque and per-recipient,
/// naming no destination and no secret, only "the click the backend already
/// knows".
///
/// A recognised scanner is sent to the bare destination, the same as one
/// caught by its user agent: the ticket reaches the page's snippet, which
/// posts it to the hit endpoint, and the backend files that page view against
/// the recipient. A gateway walking the link would show up as the person
/// browsing the site.
fn redirect_target(
    destination: &str,
    ticket: &str,
    identify: bool,
    scanner: Option<&str>,
) -> String {
    if identify && scanner.is_none() {
        with_identify_param(destination, ticket)
    } else {
        destination.to_string()
    }
}

/// Query parameter the click redirect appends and the snippet strips.
const IDENTIFY_PARAM: &str = "wbly_t";

/// Appends the identification ticket to a destination, keeping any existing
/// query and fragment intact.
fn with_identify_param(destination: &str, ticket: &str) -> String {
    let (base, fragment) = match destination.split_once('#') {
        Some((b, f)) => (b, Some(f)),
        None => (destination, None),
    };
    let sep = if base.contains('?') { '&' } else { '?' };
    let mut out = format!("{}{}{}={}", base, sep, IDENTIFY_PARAM, ticket);
    if let Some(f) = fragment {
        out.push('#');
        out.push_str(f);
    }
    out
}

/// The tracking snippet customers embed.
/// GET /tracking.js
pub async fn tracking_js() -> Response {
    (
        StatusCode::OK,
        [
            (
                header::CONTENT_TYPE,
                "application/javascript; charset=utf-8",
            ),
            (header::CACHE_CONTROL, "public, max-age=3600"),
        ],
        TRACKING_JS,
    )
        .into_response()
}

const TRACKING_JS: &str = include_str!("../static/tracking.js");

/// Website page-view ingest.
/// POST /p  (JSON body, sent as text/plain so browsers skip the preflight)
///
/// Same controls as the pixel, then the payload is validated and forwarded to
/// the backend, which owns consent policy, enrichment and storage. Nothing in
/// the URL, and nothing in the body names a contact.
pub async fn track_page_hit(
    State(state): State<AppState>,
    ConnectInfo(peer): ConnectInfo<SocketAddr>,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    let ip = client_ip(
        peer,
        &headers,
        &state.trusted_proxies,
        &state.client_ip_header,
    );
    let source = hash_ip(&state.ip_hash_key, &ip);

    // Anti-flood: page views have their own, tighter budget on top of the
    // shared one, and the shared one counts too so a flood here also
    // throttles the same source's pixels and clicks.
    if !state.rate_limiter.allow(&source).await || !state.hit_rate_limiter.allow(&source).await {
        return (StatusCode::TOO_MANY_REQUESTS, "Slow down").into_response();
    }

    let user_agent = headers
        .get(header::USER_AGENT)
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string());

    // Prefetches, crawlers, and browsers signalling Global Privacy Control
    // are acknowledged and never counted.
    if is_prefetch(&headers) || is_scanner(user_agent.as_deref()) || has_gpc(&headers) {
        return StatusCode::NO_CONTENT.into_response();
    }

    let payload: HitPayload = match serde_json::from_slice(&body) {
        Ok(p) => p,
        Err(_) => return (StatusCode::BAD_REQUEST, "Invalid payload").into_response(),
    };
    if !payload.validate() {
        return (StatusCode::BAD_REQUEST, "Invalid payload").into_response();
    }

    // Reloads and double-fires: acknowledged, not counted.
    let Some(claim) = state.hits.claim(&payload.v, &payload.u).await else {
        return StatusCode::NO_CONTENT.into_response();
    };

    let origin_host = headers
        .get(header::ORIGIN)
        .or_else(|| headers.get(header::REFERER))
        .and_then(|h| h.to_str().ok())
        .map(host_of)
        .unwrap_or_default();

    let hit = ForwardedHit {
        site_key: payload.k,
        visitor_key: payload.v,
        session_key: payload.s,
        consent: payload.c,
        identify_token: payload.t,
        url: payload.u,
        title: payload.ti,
        referrer: payload.r,
        language: payload.l,
        timezone: payload.tz,
        screen_width: payload.sw,
        screen_height: payload.sh,
        landing: payload.ld,
        user_agent: user_agent.unwrap_or_default(),
        ip,
        origin_host,
    };

    let dedupe_visitor = hit.visitor_key.clone();
    let dedupe_url = hit.url.clone();
    match state.hits.forward(hit, &source).await {
        Outcome::Accepted(None) => StatusCode::NO_CONTENT.into_response(),
        Outcome::Accepted(Some(vid)) => (
            StatusCode::OK,
            [(header::CONTENT_TYPE, "application/json")],
            format!("{{\"vid\":\"{}\"}}", vid),
        )
            .into_response(),
        // An unknown key is not told apart from a declined hit: the browser
        // learns nothing about which keys exist.
        Outcome::UnknownSite => StatusCode::NO_CONTENT.into_response(),
        Outcome::Malformed => (StatusCode::BAD_REQUEST, "Invalid payload").into_response(),
        Outcome::Unavailable => {
            // Nothing was stored, so the next attempt must not be deduped away.
            state.hits.forget(&dedupe_visitor, &dedupe_url, claim).await;
            (StatusCode::SERVICE_UNAVAILABLE, "Try again shortly").into_response()
        }
    }
}

/// Sec-GPC: 1 is the browser's own opt-out signal.
fn has_gpc(headers: &HeaderMap) -> bool {
    headers
        .get("sec-gpc")
        .and_then(|h| h.to_str().ok())
        .map(|v| v.trim() == "1")
        .unwrap_or(false)
}

/// Bare host of an Origin/Referer value ("https://www.example.com/x" ->
/// "www.example.com").
fn host_of(value: &str) -> String {
    let rest = value.split_once("://").map(|(_, r)| r).unwrap_or(value);
    rest.split(['/', '?', '#'])
        .next()
        .unwrap_or("")
        .to_ascii_lowercase()
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

/// The client address. The configured header is believed only when the
/// socket peer is a trusted proxy, and no other header is consulted, so a
/// caller cannot pick its rate-limit bucket or the location stored with a
/// page view by adding a header the proxy passes through.
fn client_ip(
    peer: SocketAddr,
    headers: &HeaderMap,
    trusted: &[ipnet::IpNet],
    header: &str,
) -> String {
    let peer_ip = peer.ip();
    if !peer_is_trusted(peer, trusted) {
        return peer_ip.to_string();
    }
    let raw = headers
        .get(header)
        .and_then(|h| h.to_str().ok())
        .map(str::trim)
        .filter(|v| !v.is_empty());
    // A proxy appends the address it saw as the LAST X-Forwarded-For entry;
    // the earlier ones are whatever the client claimed.
    let candidate = match raw {
        Some(v) if header == "x-forwarded-for" => v.rsplit(',').next().map(str::trim),
        Some(v) => Some(v),
        None => None,
    };
    match candidate.and_then(|v| v.parse::<IpAddr>().ok()) {
        Some(ip) => ip.to_string(),
        None => peer_ip.to_string(),
    }
}

/// Whether the socket peer is one of the proxies the operator named. Every
/// header that can change how a request is treated, the client address and the
/// source ASN, is read only when this is true.
fn peer_is_trusted(peer: SocketAddr, trusted: &[ipnet::IpNet]) -> bool {
    let peer_ip = peer.ip();
    trusted.iter().any(|net| net.contains(&peer_ip))
}

/// The network an address belongs to, for the location lookup downstream:
/// the last IPv4 octet zeroed, an IPv6 address cut to its first 48 bits.
/// City-level resolution survives; a single host is no longer named, so the
/// bus can retain the event without retaining the address.
fn anonymize_ip(ip: &str) -> String {
    match ip.parse::<IpAddr>() {
        Ok(IpAddr::V4(v4)) => {
            let o = v4.octets();
            format!("{}.{}.{}.0", o[0], o[1], o[2])
        }
        Ok(IpAddr::V6(v6)) => {
            let s = v6.segments();
            std::net::Ipv6Addr::new(s[0], s[1], s[2], 0, 0, 0, 0, 0).to_string()
        }
        Err(_) => String::new(),
    }
}

/// A stable, keyed token for a source address: the same source gets the same
/// token (dedupe, rate limits, the burst rule), and nobody holding the token
/// can enumerate IPv4 space to get the address back, because the key is
/// secret. The key goes in first so the digest is not one of a public value.
fn hash_ip(key: &str, ip: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(key.as_bytes());
    hasher.update([0u8]);
    hasher.update(ip.as_bytes());
    let result = hasher.finalize();
    format!("{:x}", result)[..16].to_string() // Take first 16 chars
}

#[cfg(test)]
mod tests {
    use super::*;

    // A scanner walks a message's links in parallel, so several requests
    // carrying one key genuinely arrive at once. Under check-then-insert every
    // one of them saw a miss and every one published. Racing real threads on a
    // barrier is the only way to reach that window, so this runs many keys to
    // make hitting it reliable rather than lucky.
    #[tokio::test(flavor = "multi_thread", worker_threads = 4)]
    async fn exactly_one_racing_claim_wins_each_key() {
        const KEYS: usize = 200;
        const RACERS: usize = 8;
        let cache: Arc<DedupeCache> = Arc::new(Cache::builder().max_capacity(10_000).build());

        for key in 0..KEYS {
            let gate = Arc::new(tokio::sync::Barrier::new(RACERS));
            let mut racing = Vec::with_capacity(RACERS);
            for _ in 0..RACERS {
                let (cache, gate) = (cache.clone(), gate.clone());
                let key = format!("CLICK:task-{key}:source");
                racing.push(tokio::spawn(async move {
                    gate.wait().await;
                    claim(&cache, key).await
                }));
            }
            let mut fresh = 0;
            for task in racing {
                if !task.await.expect("claim must not panic") {
                    fresh += 1;
                }
            }
            assert_eq!(fresh, 1, "exactly one caller may publish key {key}");
        }

        // Sequentially, the same key is a duplicate and a different one is not.
        assert!(claim(&cache, "CLICK:task-0:source".to_string()).await);
        assert!(!claim(&cache, "OPEN:task-0:source".to_string()).await);
    }

    // The ticket identifies the recipient to the destination's own analytics.
    // Handing it to a security gateway files the gateway's fetch as that
    // person's visit, which is the same reason the user-agent path above
    // redirects scanners to the bare destination.
    #[test]
    fn a_scanner_is_never_handed_the_identification_ticket() {
        assert_eq!(
            redirect_target("https://x.com/p", "abc", true, None),
            "https://x.com/p?wbly_t=abc"
        );
        assert_eq!(
            redirect_target(
                "https://x.com/p",
                "abc",
                true,
                Some("microsoft-365-protection")
            ),
            "https://x.com/p"
        );
        // A destination the workspace never registered carries no ticket
        // either way, and the redirect itself always happens.
        assert_eq!(
            redirect_target("https://x.com/p", "abc", false, None),
            "https://x.com/p"
        );
        assert_eq!(
            redirect_target("https://x.com/p", "abc", false, Some("scanner")),
            "https://x.com/p"
        );
    }

    #[test]
    fn identify_param_keeps_query_and_fragment() {
        assert_eq!(
            with_identify_param("https://x.com/p", "abc"),
            "https://x.com/p?wbly_t=abc"
        );
        assert_eq!(
            with_identify_param("https://x.com/p?a=1#top", "abc"),
            "https://x.com/p?a=1&wbly_t=abc#top"
        );
    }

    fn hdr(pairs: &[(&'static str, &'static str)]) -> HeaderMap {
        let mut h = HeaderMap::new();
        for (k, v) in pairs {
            h.insert(*k, v.parse().unwrap());
        }
        h
    }

    #[test]
    fn client_ip_ignores_forwarded_headers_from_untrusted_peers() {
        let peer: SocketAddr = "203.0.113.9:4000".parse().unwrap();
        let h = hdr(&[("x-forwarded-for", "1.1.1.1, 2.2.2.2")]);
        assert_eq!(client_ip(peer, &h, &[], "x-forwarded-for"), "203.0.113.9");
    }

    #[test]
    fn client_ip_reads_only_the_configured_header_from_trusted_peers() {
        let trusted = vec!["10.0.0.0/8".parse::<ipnet::IpNet>().unwrap()];
        let peer: SocketAddr = "10.1.2.3:4000".parse().unwrap();
        // Proxy-appended last entry wins over what the client claimed.
        let h = hdr(&[("x-forwarded-for", "1.1.1.1, 198.51.100.7")]);
        assert_eq!(
            client_ip(peer, &h, &trusted, "x-forwarded-for"),
            "198.51.100.7"
        );
        // A client-supplied CF-Connecting-IP passed through a generic proxy is
        // ignored unless that header is the configured one.
        let h = hdr(&[
            ("cf-connecting-ip", "8.8.8.8"),
            ("x-forwarded-for", "198.51.100.7"),
        ]);
        assert_eq!(
            client_ip(peer, &h, &trusted, "x-forwarded-for"),
            "198.51.100.7"
        );
        assert_eq!(client_ip(peer, &h, &trusted, "cf-connecting-ip"), "8.8.8.8");
        // Garbage or a missing header falls back to the proxy itself.
        let h = hdr(&[("x-forwarded-for", "not an ip")]);
        assert_eq!(client_ip(peer, &h, &trusted, "x-forwarded-for"), "10.1.2.3");
        assert_eq!(
            client_ip(peer, &hdr(&[]), &trusted, "x-forwarded-for"),
            "10.1.2.3"
        );
    }

    #[test]
    fn hash_ip_is_keyed_and_stable() {
        assert_eq!(hash_ip("k", "203.0.113.9"), hash_ip("k", "203.0.113.9"));
        assert_ne!(hash_ip("k", "203.0.113.9"), hash_ip("other", "203.0.113.9"));
        assert_eq!(hash_ip("k", "203.0.113.9").len(), 16);
    }

    #[test]
    fn anonymize_ip_keeps_only_the_network() {
        assert_eq!(anonymize_ip("203.0.113.9"), "203.0.113.0");
        assert_eq!(
            anonymize_ip("2001:db8:abcd:1234:5678::1"),
            "2001:db8:abcd::"
        );
        assert_eq!(anonymize_ip("not an ip"), "");
    }

    #[test]
    fn host_of_strips_scheme_and_path() {
        assert_eq!(host_of("https://WWW.Example.com/a?b#c"), "www.example.com");
        assert_eq!(host_of("example.com"), "example.com");
    }
}
