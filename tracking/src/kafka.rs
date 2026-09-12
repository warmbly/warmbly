use apache_avro::types::Value;
use rdkafka::config::ClientConfig;
use rdkafka::producer::{FutureProducer, FutureRecord};
use rdkafka::util::Timeout;
use schema_registry_converter::async_impl::avro::AvroEncoder;
use schema_registry_converter::async_impl::schema_registry::SrSettings;
use schema_registry_converter::schema_registry_common::SubjectNameStrategy;
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::RwLock;
use tracing::info;

use crate::config::Config;
use crate::events::TrackingEvent;
use crate::observability;

/// Avro schema for tracking events - matches Go events.TrackingEvent
#[allow(dead_code)]
pub const TRACKING_EVENT_SCHEMA: &str = r#"
{
    "type": "record",
    "name": "TrackingEvent",
    "namespace": "com.warmbly.tracking",
    "fields": [
        {"name": "event_type", "type": "string", "avro.java.string": "String"},
        {"name": "task_id", "type": "string", "avro.java.string": "String"},
        {"name": "original_url", "type": ["null", "string"], "default": null},
        {"name": "link_id", "type": ["null", "string"], "default": null},
        {"name": "timestamp", "type": "string", "avro.java.string": "String"},
        {"name": "user_agent", "type": ["null", "string"], "default": null},
        {"name": "ip_hash", "type": ["null", "string"], "default": null},
        {"name": "client_ip", "type": ["null", "string"], "default": null},
        {"name": "scanner", "type": ["null", "string"], "default": null}
    ]
}
"#;

#[derive(Clone)]
pub struct KafkaProducer {
    producer: Arc<FutureProducer>,
    topic: String,
    /// Some only under CODEC_PROVIDER=avro. JSON is the default because the Go
    /// consumer decodes both of its topics with one codec and worker envelopes
    /// cannot be Avro, so a JSON consumer handed Avro drops every open and
    /// click with nothing but a deserialize warning.
    encoder: Option<Arc<RwLock<AvroEncoder<'static>>>>,
}

/// Avro encoding for the shared TrackingEvent (Kafka path only). The struct
/// itself lives in events.rs so the NATS path can use it without the Avro deps.
trait ToAvroValue {
    fn to_avro_value(&self) -> Vec<(&'static str, Value)>;
}

impl ToAvroValue for TrackingEvent {
    /// Convert to Avro Value for schema registry encoding
    fn to_avro_value(&self) -> Vec<(&'static str, Value)> {
        vec![
            ("event_type", Value::String(self.event_type.clone())),
            ("task_id", Value::String(self.task_id.clone())),
            (
                "original_url",
                match &self.original_url {
                    Some(url) => Value::Union(1, Box::new(Value::String(url.clone()))),
                    None => Value::Union(0, Box::new(Value::Null)),
                },
            ),
            (
                "link_id",
                match &self.link_id {
                    Some(id) => Value::Union(1, Box::new(Value::String(id.clone()))),
                    None => Value::Union(0, Box::new(Value::Null)),
                },
            ),
            ("timestamp", Value::String(self.timestamp.clone())),
            (
                "user_agent",
                match &self.user_agent {
                    Some(ua) => Value::Union(1, Box::new(Value::String(ua.clone()))),
                    None => Value::Union(0, Box::new(Value::Null)),
                },
            ),
            (
                "ip_hash",
                match &self.ip_hash {
                    Some(hash) => Value::Union(1, Box::new(Value::String(hash.clone()))),
                    None => Value::Union(0, Box::new(Value::Null)),
                },
            ),
            (
                "client_ip",
                match &self.client_ip {
                    Some(net) => Value::Union(1, Box::new(Value::String(net.clone()))),
                    None => Value::Union(0, Box::new(Value::Null)),
                },
            ),
            (
                "scanner",
                match &self.scanner {
                    Some(label) => Value::Union(1, Box::new(Value::String(label.clone()))),
                    None => Value::Union(0, Box::new(Value::Null)),
                },
            ),
        ]
    }
}

impl KafkaProducer {
    pub async fn new(config: &Config) -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        // Configure Kafka producer
        let mut client_config = ClientConfig::new();
        client_config
            .set("bootstrap.servers", &config.kafka_brokers)
            .set("message.timeout.ms", "5000")
            .set("queue.buffering.max.messages", "100000")
            .set("queue.buffering.max.kbytes", "1048576")
            .set("batch.num.messages", "10000")
            .set("linger.ms", "5")
            .set("compression.type", "lz4");

        // Configure SASL if enabled
        if config.sasl_enabled() {
            client_config
                .set("security.protocol", "SASL_SSL")
                .set("sasl.mechanisms", "PLAIN")
                .set(
                    "sasl.username",
                    config.kafka_sasl_username.as_ref().unwrap(),
                )
                .set(
                    "sasl.password",
                    config.kafka_sasl_password.as_ref().unwrap(),
                );
        }

        let producer: FutureProducer = client_config.create()?;
        info!("Kafka producer connected to {}", config.kafka_brokers);

        // Schema Registry is only built for the Avro codec. Building it for JSON
        // would demand a registry URL that a JSON deployment has no reason to
        // have, and an empty one fails at the first publish rather than at boot.
        let encoder = if config.codec_provider == "avro" {
            if config.schema_registry_url.is_empty() {
                return Err(
                    "CODEC_PROVIDER=avro needs SCHEMA_REGISTRY_URL; set it, or use CODEC_PROVIDER=json"
                        .into(),
                );
            }
            let sr_settings = if let Some((key, secret)) = config.schema_registry_auth() {
                SrSettings::new_builder(config.schema_registry_url.clone())
                    .set_basic_authorization(&key, Some(&secret))
                    .build()?
            } else {
                SrSettings::new(config.schema_registry_url.clone())
            };
            info!(
                "Schema Registry connected to {}",
                config.schema_registry_url
            );
            Some(Arc::new(RwLock::new(AvroEncoder::new(sr_settings))))
        } else {
            info!("Publishing tracking events as JSON");
            None
        };

        Ok(Self {
            producer: Arc::new(producer),
            topic: config.kafka_topic.clone(),
            encoder,
        })
    }

    pub async fn publish(&self, event: TrackingEvent) {
        let payload = match self.serialize(&event).await {
            Ok(p) => p,
            Err(e) => {
                observability::report_issue("Failed to serialize tracking event", &e.to_string());
                return;
            }
        };

        let record = FutureRecord::to(&self.topic)
            .payload(&payload)
            .key(&event.task_id);

        match self
            .producer
            .send(record, Timeout::After(Duration::from_secs(5)))
            .await
        {
            Ok(_) => {
                tracing::debug!(
                    "Published {} event for task {}",
                    event.event_type,
                    event.task_id
                );
            }
            Err((e, _)) => {
                observability::report_issue(
                    "Failed to publish tracking event to Kafka",
                    &format!(
                        "event_type={}, task_id={}, error={}",
                        event.event_type, event.task_id, e
                    ),
                );
            }
        }
    }

    /// JSON unless an Avro encoder was built, which is the same bytes the NATS
    /// path writes, so the Go consumer decodes Kafka and NATS identically.
    async fn serialize(
        &self,
        event: &TrackingEvent,
    ) -> Result<Vec<u8>, Box<dyn std::error::Error + Send + Sync>> {
        let Some(encoder) = &self.encoder else {
            return Ok(serde_json::to_vec(event)?);
        };
        let encoder = encoder.read().await;

        // Use schema registry encoder to serialize with proper schema ID prefix
        let payload = encoder
            .encode(
                event.to_avro_value(),
                SubjectNameStrategy::TopicNameStrategy(self.topic.clone(), false),
            )
            .await?;

        Ok(payload)
    }
}
