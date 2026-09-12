use async_nats::jetstream;
use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine as _;

use crate::config::Config;
use crate::events::TrackingEvent;
use crate::observability;

/// NatsProducer publishes tracking events to NATS JetStream as JSON. The subject
/// is `<prefix>.<topic>` (e.g. `warmbly.tracking-events`), matching the Go
/// NATSBus subject mapping so the consumer's JetStream subscription captures it.
/// The JetStream stream is created by the Go consumer on startup; this service
/// only publishes.
#[derive(Clone)]
pub struct NatsProducer {
    js: jetstream::Context,
    subject: String,
}

impl NatsProducer {
    pub async fn new(config: &Config) -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        // async-nats ignores credentials in the URL, so lift `user:pass@` or
        // `token@` into the options the way the Go client does on its own.
        let addr: async_nats::ServerAddr = config.nats_url.parse()?;
        let mut opts = async_nats::ConnectOptions::new();
        if let Some(user) = addr.username() {
            opts = match addr.password() {
                Some(pass) => opts.user_and_password(user.to_string(), pass.to_string()),
                None => opts.token(user.to_string()),
            };
        }
        // A managed bus authenticates with a user JWT and nkey seed, which no
        // URL can carry. NATS_CREDS_B64 is the single-line form the fleet needs
        // (docker --env-file cannot express a multi-line value); NATS_CREDS is
        // a path, for containers and local development.
        if let Ok(b64) = std::env::var("NATS_CREDS_B64") {
            if !b64.trim().is_empty() {
                let raw = BASE64.decode(b64.trim())?;
                opts = opts.credentials(std::str::from_utf8(&raw)?)?;
            }
        } else if let Ok(path) = std::env::var("NATS_CREDS") {
            if !path.trim().is_empty() {
                let contents = std::fs::read_to_string(path.trim())?;
                opts = opts.credentials(&contents)?;
            }
        }

        let client = opts.connect(addr).await?;
        let js = jetstream::new(client);
        let subject = format!("{}.{}", config.nats_subject_prefix, config.kafka_topic);
        tracing::info!(
            "NATS producer connected to {}, publishing to subject {}",
            redact_url(&config.nats_url),
            subject
        );
        Ok(Self { js, subject })
    }

    pub async fn publish(&self, event: TrackingEvent) {
        let payload = match serde_json::to_vec(&event) {
            Ok(p) => p,
            Err(e) => {
                observability::report_issue(
                    "Failed to serialize tracking event to JSON",
                    &e.to_string(),
                );
                return;
            }
        };

        // Await the stream ack so a missing stream / no-responders surfaces as an
        // error rather than a silent drop.
        let err: Option<String> = match self.js.publish(self.subject.clone(), payload.into()).await
        {
            Ok(ack) => ack.await.err().map(|e| e.to_string()),
            Err(e) => Some(e.to_string()),
        };

        match err {
            None => tracing::debug!(
                "Published {} event for task {}",
                event.event_type,
                event.task_id
            ),
            Some(e) => observability::report_issue(
                "Failed to publish tracking event to NATS",
                &format!(
                    "event_type={}, task_id={}, error={}",
                    event.event_type, event.task_id, e
                ),
            ),
        }
    }
}

// redact_url drops any userinfo so credentials never reach the logs.
fn redact_url(raw: &str) -> String {
    match (raw.find("://"), raw.rfind('@')) {
        (Some(scheme_end), Some(at)) if at > scheme_end => {
            format!("{}://***@{}", &raw[..scheme_end], &raw[at + 1..])
        }
        _ => raw.to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::redact_url;

    #[test]
    fn redacts_userinfo_only() {
        assert_eq!(redact_url("nats://127.0.0.1:4222"), "nats://127.0.0.1:4222");
        assert_eq!(
            redact_url("tls://tok3n@nats.example.com:4222"),
            "tls://***@nats.example.com:4222"
        );
        assert_eq!(redact_url("nats://u:p@h:4222"), "nats://***@h:4222");
    }
}
