use serde::Serialize;

/// A tracking event published to the event bus. The JSON field names match the
/// Go `events.TrackingEvent` struct tags so the consumer decodes it whether it
/// arrives as JSON (NATS) or Avro (Kafka).
#[derive(Debug, Serialize, Clone)]
pub struct TrackingEvent {
    pub event_type: String,
    pub task_id: String,
    pub original_url: Option<String>,
    /// Click ticket id, so the consumer can name the link (destination and
    /// anchor text) without matching URLs.
    pub link_id: Option<String>,
    pub timestamp: String,
    pub user_agent: Option<String>,
    pub ip_hash: Option<String>,
    /// The source network (last IPv4 octet zeroed, IPv6 cut to 48 bits),
    /// enough for the consumer's location lookup without naming a host.
    pub client_ip: Option<String>,
    /// Names the known-scanner source the request came from, when it came
    /// from one: a mail-filtering network rather than a person's own device.
    /// The consumer records such an event as a machine open or click. None
    /// for every ordinary request, and absent from events written before the
    /// field existed.
    pub scanner: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::TrackingEvent;

    /// The JSON these fields serialize to is the wire format on both buses now:
    /// NATS has always carried it, and the Kafka path writes it too whenever
    /// CODEC_PROVIDER is json. The Go consumer decodes it into
    /// internal/events.TrackingEvent by these exact names, and a rename on
    /// either side costs every open and click with nothing but a deserialize
    /// warning, so the names are pinned here.
    #[test]
    fn json_field_names_match_the_go_consumer() {
        let event = TrackingEvent {
            event_type: "EMAIL_OPENED".to_string(),
            task_id: "11111111-2222-3333-4444-555555555555".to_string(),
            original_url: None,
            link_id: None,
            timestamp: "2026-09-12T07:00:00Z".to_string(),
            user_agent: Some("Mozilla/5.0".to_string()),
            ip_hash: Some("deadbeef".to_string()),
            client_ip: Some("203.0.113.0".to_string()),
            scanner: None,
        };

        let value: serde_json::Value = serde_json::from_slice(
            &serde_json::to_vec(&event).expect("tracking events must serialize"),
        )
        .expect("and the result must be an object");

        let mut got: Vec<&str> = value
            .as_object()
            .expect("a tracking event is a JSON object")
            .keys()
            .map(String::as_str)
            .collect();
        got.sort_unstable();

        let want = [
            "client_ip",
            "event_type",
            "ip_hash",
            "link_id",
            "original_url",
            "scanner",
            "task_id",
            "timestamp",
            "user_agent",
        ];
        assert_eq!(
            got, want,
            "tracking event JSON keys drifted from the Go side"
        );
        assert_eq!(value["event_type"], "EMAIL_OPENED");
        // Nullable fields travel as null rather than being omitted, which is
        // what the Go pointer fields decode.
        assert!(value["original_url"].is_null());
    }
}
