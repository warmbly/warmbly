use async_nats::jetstream;

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
        let client = async_nats::connect(&config.nats_url).await?;
        let js = jetstream::new(client);
        let subject = format!("{}.{}", config.nats_subject_prefix, config.kafka_topic);
        tracing::info!(
            "NATS producer connected to {}, publishing to subject {}",
            config.nats_url,
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
