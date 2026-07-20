mod abuse;
mod aws;
mod config;
mod events;
mod handlers;
#[cfg(feature = "kafka")]
mod kafka;
mod links;
mod nats;
mod observability;
mod producer;

use axum::{routing::get, Router};
use std::net::SocketAddr;
use tower_http::{
    cors::{Any, CorsLayer},
    trace::TraceLayer,
};
use tracing::info;
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

use crate::config::Config;
use crate::handlers::{health, track_click, track_open, AppState};
use crate::observability::report_error;
use crate::producer::Producer;

#[tokio::main]
async fn main() {
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
            std::process::exit(1);
        }
    };
    observability::init(&config.env);
    info!("Starting tracking service on {}", config.addr());

    // Event-bus producer (NATS by default; Kafka when EVENTBUS_PROVIDER=kafka
    // and the `kafka` feature is compiled in).
    let producer = match Producer::from_config(&config).await {
        Ok(p) => p,
        Err(e) => {
            report_error("Failed to create tracking event producer", e.as_ref());
            std::process::exit(1);
        }
    };

    let state = AppState::new(producer, &config);

    // Build router
    let app = Router::new()
        .route("/health", get(health))
        .route("/t/o/:task_id", get(track_open))
        .route("/c/:link_id", get(track_click))
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
            std::process::exit(1);
        }
    };
    info!("Tracking service listening on {}", addr);

    let listener = match tokio::net::TcpListener::bind(addr).await {
        Ok(l) => l,
        Err(e) => {
            observability::report_issue("Failed to bind tracking listener", &e.to_string());
            std::process::exit(1);
        }
    };

    if let Err(e) = axum::serve(listener, app).await {
        observability::report_issue("Tracking server terminated unexpectedly", &e.to_string());
        std::process::exit(1);
    }
}
