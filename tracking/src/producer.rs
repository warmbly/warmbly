use crate::config::Config;
use crate::events::TrackingEvent;
use crate::nats::NatsProducer;

/// Producer is the transport-agnostic tracking-event publisher. NATS is the
/// default; the Kafka variant is only present when compiled with
/// `--features kafka` and selected at runtime by EVENTBUS_PROVIDER=kafka.
#[derive(Clone)]
pub enum Producer {
    Nats(NatsProducer),
    #[cfg(feature = "kafka")]
    Kafka(crate::kafka::KafkaProducer),
}

impl Producer {
    /// Build the active producer from config. EVENTBUS_PROVIDER selects the
    /// backend; anything other than "kafka" (the default) uses NATS. Selecting
    /// "kafka" without the `kafka` build feature is a hard error, mirroring the
    /// Go eventbus factory.
    pub async fn from_config(
        config: &Config,
    ) -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        if config.eventbus_provider == "kafka" {
            #[cfg(feature = "kafka")]
            {
                return Ok(Producer::Kafka(
                    crate::kafka::KafkaProducer::new(config).await?,
                ));
            }
            #[cfg(not(feature = "kafka"))]
            {
                return Err("EVENTBUS_PROVIDER=kafka but the tracking service was built without the `kafka` feature; rebuild with --features kafka or use NATS".into());
            }
        }
        Ok(Producer::Nats(NatsProducer::new(config).await?))
    }

    pub async fn publish(&self, event: TrackingEvent) {
        match self {
            Producer::Nats(p) => p.publish(event).await,
            #[cfg(feature = "kafka")]
            Producer::Kafka(p) => p.publish(event).await,
        }
    }
}
