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
mod redirects;
mod scanners;
mod serve_tls;
mod unsubscribe;
mod unsubscribe_i18n;

use axum::{
    extract::{DefaultBodyLimit, MatchedPath},
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
    health, not_found, redirect_first, track_click, track_open, track_page_hit, tracking_js,
    unsubscribe_page, unsubscribe_submit, unsubscribe_undo, AppState,
};
use crate::observability::report_error;
use crate::producer::Producer;

/// No response here is ever content-sniffed. HSTS is left to the proxy: this
/// service also answers on customers' own tracking and redirect domains.
async fn nosniff(mut res: axum::response::Response) -> axum::response::Response {
    res.headers_mut().insert(
        axum::http::header::X_CONTENT_TYPE_OPTIONS,
        axum::http::HeaderValue::from_static("nosniff"),
    );
    res
}

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

    let listeners = match serve_tls::Listeners::from_env(|k| std::env::var(k).ok()) {
        Ok(l) => l,
        Err(e) => {
            observability::report_issue("Invalid TLS listener setting", &e);
            std::process::exit(1);
        }
    };
    match serve_tls::load_backend_roots(|k| std::env::var(k).ok()) {
        Ok(roots) => serve_tls::init_backend_roots(roots),
        Err(e) => {
            observability::report_issue("Invalid BACKEND_INTERNAL_CA", &e);
            std::process::exit(1);
        }
    }

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

    // The scanner ASN database: a mounted file, or a download held in memory.
    // Resolved here because it may go to the network, and never fatal: without
    // it the catalogue's asn: entries match nothing and everything else works.
    let asn_db = asndb::AsnDb::resolve(&config.scanner_asn_db, &config.scanner_asn_db_url).await;

    let state = AppState::new(producer, &config, asn_db);

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
        .fallback(not_found)
        // A sending domain pointed here for its redirect answers only with it.
        .layer(axum::middleware::from_fn_with_state(
            state.clone(),
            redirect_first,
        ))
        .layer(axum::middleware::map_response(nosniff))
        .layer(
            CorsLayer::new()
                .allow_origin(Any)
                .allow_methods(Any)
                .allow_headers(Any),
        )
        // Spans name the route, never the URI: unsubscribe and click paths carry tokens.
        .layer(
            TraceLayer::new_for_http().make_span_with(|req: &axum::http::Request<_>| {
                let route = req
                    .extensions()
                    .get::<MatchedPath>()
                    .map(MatchedPath::as_str)
                    .unwrap_or("unmatched");
                tracing::info_span!("request", method = %req.method(), route)
            }),
        )
        .with_state(state);

    // Start server
    let plain_addr = format!("{}:{}", listeners.plain_host(&config.host), config.port);
    let addr: SocketAddr = match plain_addr.parse() {
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

    let secure = match &listeners.tls {
        Some(tls) => match tls.rustls_config().await {
            Ok(rustls) => {
                let tls_addr = SocketAddr::from(([0, 0, 0, 0], tls.port));
                info!("Tracking service listening with TLS on {}", tls_addr);
                let service = app
                    .clone()
                    .into_make_service_with_connect_info::<SocketAddr>();
                Some(tokio::spawn(async move {
                    axum_server::bind_rustls(tls_addr, rustls)
                        .serve(service)
                        .await
                }))
            }
            Err(e) => {
                observability::report_issue("TLS listener", &e);
                observability::flush();
                std::process::exit(1);
            }
        },
        None => None,
    };

    // Connect info carries the socket peer, which is the client unless it is a
    // trusted proxy (TRACKING_TRUSTED_PROXIES).
    let plain = axum::serve(
        listener,
        app.into_make_service_with_connect_info::<SocketAddr>(),
    );
    let failure = match secure {
        // Either listener ending takes the service down, so the orchestrator replaces it.
        Some(secure) => Some(tokio::select! {
            r = plain => r.err().map_or_else(|| "plain listener stopped".to_string(), |e| e.to_string()),
            r = secure => match r {
                Ok(Ok(())) => "TLS listener stopped".to_string(),
                Ok(Err(e)) => e.to_string(),
                Err(e) => e.to_string(),
            },
        }),
        None => plain.await.err().map(|e| e.to_string()),
    };
    if let Some(e) = failure {
        observability::report_issue("Tracking server terminated unexpectedly", &e);
        observability::flush();
        std::process::exit(1);
    }
}
