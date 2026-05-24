use apache_avro::types::Value;
use rdkafka::config::ClientConfig;
use rdkafka::producer::{FutureProducer, FutureRecord};
use rdkafka::util::Timeout;
use schema_registry_converter::async_impl::avro::AvroEncoder;
use schema_registry_converter::async_impl::schema_registry::SrSettings;
use schema_registry_converter::schema_registry_common::SubjectNameStrategy;
use serde::Serialize;
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::RwLock;
use tracing::info;

use crate::config::Config;
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
        {"name": "timestamp", "type": "string", "avro.java.string": "String"},
        {"name": "user_agent", "type": ["null", "string"], "default": null},
        {"name": "ip_hash", "type": ["null", "string"], "default": null}
    ]
}
"#;

#[derive(Clone)]
pub struct KafkaProducer {
    producer: Arc<FutureProducer>,
    topic: String,
    encoder: Arc<RwLock<AvroEncoder<'static>>>,
}

#[derive(Debug, Serialize, Clone)]
pub struct TrackingEvent {
    pub event_type: String,
    pub task_id: String,
    pub original_url: Option<String>,
    pub timestamp: String,
    pub user_agent: Option<String>,
    pub ip_hash: Option<String>,
}

impl TrackingEvent {
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

        // Configure Schema Registry
        let sr_settings = if let Some((key, secret)) = config.schema_registry_auth() {
            SrSettings::new_builder(config.schema_registry_url.clone())
                .set_basic_authorization(&key, Some(&secret))
                .build()?
        } else {
            SrSettings::new(config.schema_registry_url.clone())
        };

        let encoder = AvroEncoder::new(sr_settings);
        info!(
            "Schema Registry connected to {}",
            config.schema_registry_url
        );

        Ok(Self {
            producer: Arc::new(producer),
            topic: config.kafka_topic.clone(),
            encoder: Arc::new(RwLock::new(encoder)),
        })
    }

    pub async fn publish(&self, event: TrackingEvent) {
        // Serialize event using Avro with Schema Registry
        let payload = match self.serialize_avro(&event).await {
            Ok(p) => p,
            Err(e) => {
                observability::report_issue(
                    "Failed to serialize tracking event with Avro",
                    &e.to_string(),
                );
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

    async fn serialize_avro(
        &self,
        event: &TrackingEvent,
    ) -> Result<Vec<u8>, Box<dyn std::error::Error + Send + Sync>> {
        let encoder = self.encoder.read().await;

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
