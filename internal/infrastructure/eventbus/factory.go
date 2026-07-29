package eventbus

import (
	"fmt"
	"os"

	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
)

// KafkaConfig builds a KafkaBus. bootstrap is the comma-separated broker list;
// sasl is optional (nil for plaintext / no auth). Defined here (untagged) so the
// factory can construct it whether or not the Kafka backend is compiled in.
type KafkaConfig struct {
	Bootstrap string
	SASL      *kafka.SASLConfig
}

// FromEnv constructs the active EventBus from environment variables.
//
//	EVENTBUS_PROVIDER=kafka  -> NewKafka (defaults: existing kafka package)
//	EVENTBUS_PROVIDER=nats   -> NewNATS  (JetStream)
//	(unset)                  -> defaults to "kafka" for backwards compatibility
//
// Kafka selection inputs:
//
//	bootstrap, sasl — passed in by the caller (already loaded from secrets).
//
// NATS selection inputs (all env):
//
//	NATS_URL                  -> nats://host:port (default: nats://localhost:4222)
//	NATS_STREAM_NAME          -> JetStream stream name (default: "warmbly")
//	NATS_SUBJECT_PREFIX       -> subject prefix under the stream (default: "warmbly")
//
// The Kafka inputs are passed positionally rather than via env to match the
// existing wiring: bootstrap / SASL config already flow from cfg.Load* helpers
// in cmd/*. Self-hosters who pick NATS will leave those unset.
func FromEnv(bootstrap string, sasl *kafka.SASLConfig) (EventBus, error) {
	provider := os.Getenv("EVENTBUS_PROVIDER")
	if provider == "" {
		provider = "kafka"
	}
	switch provider {
	case "kafka":
		return NewKafka(KafkaConfig{
			Bootstrap: bootstrap,
			SASL:      sasl,
		})
	case "nats":
		return NewNATS(NATSConfig{
			URL:           natsURLFromEnv(),
			StreamName:    os.Getenv("NATS_STREAM_NAME"),
			SubjectPrefix: os.Getenv("NATS_SUBJECT_PREFIX"),
		})
	default:
		return nil, fmt.Errorf("eventbus: unknown EVENTBUS_PROVIDER %q (want: kafka, nats)", provider)
	}
}
