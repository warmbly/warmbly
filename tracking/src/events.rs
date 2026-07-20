use serde::Serialize;

/// A tracking event published to the event bus. The JSON field names match the
/// Go `events.TrackingEvent` struct tags so the consumer decodes it whether it
/// arrives as JSON (NATS) or Avro (Kafka).
#[derive(Debug, Serialize, Clone)]
pub struct TrackingEvent {
    pub event_type: String,
    pub task_id: String,
    pub original_url: Option<String>,
    pub timestamp: String,
    pub user_agent: Option<String>,
    pub ip_hash: Option<String>,
}
