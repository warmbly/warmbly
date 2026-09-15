mod abuse;
mod asndb;
mod aws;
mod config;
mod events;
mod handlers;
mod hits;
#[cfg(feature = "kafka")]
mod kafka;
mod links;
mod nats;
mod observability;
mod posthog;
mod producer;
mod scanners;
mod unsubscribe;

use axum::{
    extract::DefaultBodyLimit,
    routing::{get, post},
    Router,
};
use std::net::SocketAddr;
use std::time::Duration;
use tower_http::{
    cors::{Any, CorsLayer},
    trace::TraceLayer,
};
use tracing::{info, warn};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

use crate::config::Config;
use crate::handlers::{
    health, track_click, track_open, track_page_hit, tracking_js, unsubscribe_page,
    unsubscribe_submit, unsubscribe_undo, AppState,
};
use crate::observability::report_error;
use crate::producer::Producer;

/// Connects the event-bus producer, retrying transient failures.
///
/// The whole stack starts at once, so the first lookup of `nats` can fail with
/// "DNS error: failed to lookup address information: Try again" purely because
/// the other container is not up yet. Exiting on that leaves the service dead,
/// and nothing else fails loudly when it is: sends still succeed, so the only
/// symptom is opens and clicks silently never recording.
async fn connect_producer(config: &Config) -> Producer {
    const MAX_ATTEMPTS: u32 = 8;
    const MAX_DELAY: Duration = Duration::from_secs(10);

    let mut delay = Duration::from_millis(500);
    let mut attempt: u32 = 1;

    loop {
        match Producer::from_config(config).await {
            Ok(producer) => {
                if attempt > 1 {
                    info!("Tracking event producer connected after {attempt} attempts");
                }
                return producer;
            }
            Err(e) => {
                if attempt >= MAX_ATTEMPTS {
                    report_error("Failed to create tracking event producer", e.as_ref());
                    observability::flush();
                    std::process::exit(1);
                }
                warn!(
                    "Tracking event producer not ready (attempt {attempt}/{MAX_ATTEMPTS}), retrying in {delay:?}: {e}"
                );
                tokio::time::sleep(delay).await;
                delay = (delay * 2).min(MAX_DELAY);
                attempt += 1;
            }
        }
    }
}

#[tokio::main]
async fn main() {
    // Before any TLS connection. rustls 0.23 cannot choose between aws-lc-rs
    // and ring when both are in the tree, and panics at the first handshake:
    // a tls:// bus or a rediss:// cache takes the whole service down at
    // startup, which plaintext local development never reveals.
    // Ignored rather than unwrapped: a second install is not a failure.
    let _ = rustls::crypto::ring::default_provider().install_default();

    // Initialize tracing
    tracing_subscriber::registry()
        .with(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "tracking=info,tower_http=info".into()),
        )
        .with(tracing_subscriber::fmt::layer())
        .init();

    // Load configuration with env-first approach
    let config = match Config::load().await {
        Ok(c) => c,
        Err(e) => {
            report_error("Failed to load config", e.as_ref());
            observability::flush();
            std::process::exit(1);
        }
    };
    // Held until main returns so queued events are flushed on shutdown.
    let _reporting = observability::init(observability::Settings {
        env: &config.env,
        release: &config.release,
        sentry_dsn: &config.sentry_dsn,
        posthog_key: &config.posthog_key,
        posthog_host: &config.posthog_host,
    });
    info!("Starting tracking service on {}", config.addr());

    // Event-bus producer (NATS by default; Kafka when EVENTBUS_PROVIDER=kafka
    // and the `kafka` feature is compiled in).
    let producer = connect_producer(&config).await;

    let state = AppState::new(producer, &config);

    // Build router
    let app = Router::new()
        .route("/health", get(health))
        .route("/t/o/:task_id", get(track_open))
        .route("/c/:link_id", get(track_click))
        // Website tracking: the snippet and the page-view ingest it posts to.
        // The body cap is the first thing a flood hits.
        .route("/tracking.js", get(tracking_js))
        .route(
            "/p",
            post(track_page_hit).layer(DefaultBodyLimit::max(hits::MAX_BODY_BYTES)),
        )
        // Recipient opt-out. A workspace's verified tracking domain is the
        // host its campaign mail carries, so the unsubscribe address in that
        // mail resolves here; the backend owns the pages behind it.
        .route(
            "/unsubscribe/:token",
            get(unsubscribe_page)
                .post(unsubscribe_submit)
                .layer(DefaultBodyLimit::max(unsubscribe::MAX_BODY_BYTES)),
        )
        .route(
            "/unsubscribe/:token/resubscribe",
            post(unsubscribe_undo).layer(DefaultBodyLimit::max(unsubscribe::MAX_BODY_BYTES)),
        )
        .layer(
            CorsLayer::new()
                .allow_origin(Any)
                .allow_methods(Any)
                .allow_headers(Any),
        )
        .layer(TraceLayer::new_for_http())
        .with_state(state);

    // Start server
    let addr: SocketAddr = match config.addr().parse() {
        Ok(a) => a,
        Err(e) => {
            observability::report_issue("Invalid tracking listen address", &e.to_string());
            observability::flush();
            std::process::exit(1);
        }
    };
    info!("Tracking service listening on {}", addr);

    let listener = match tokio::net::TcpListener::bind(addr).await {
        Ok(l) => l,
        Err(e) => {
            observability::report_issue("Failed to bind tracking listener", &e.to_string());
            observability::flush();
            std::process::exit(1);
        }
    };

    // Connect info carries the socket peer, which is the client unless it is a
    // trusted proxy (TRACKING_TRUSTED_PROXIES).
    if let Err(e) = axum::serve(
        listener,
        app.into_make_service_with_connect_info::<SocketAddr>(),
    )
    .await
    {
        observability::report_issue("Tracking server terminated unexpectedly", &e.to_string());
        observability::flush();
        std::process::exit(1);
    }
}
