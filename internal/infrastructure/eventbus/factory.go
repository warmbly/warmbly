package eventbus

import (
	"fmt"
	"os"
	"strconv"

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
	readBoth := false
	if raw := os.Getenv("EVENTBUS_READ_BOTH"); raw != "" {
		var err error
		readBoth, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("eventbus: EVENTBUS_READ_BOTH must be a boolean")
		}
	}
	hold := os.Getenv("EVENTBUS_HOLD_READS")
	if hold != "" && (!readBoth || hold != "nats" && hold != "kafka") {
		return nil, fmt.Errorf("eventbus: EVENTBUS_HOLD_READS requires EVENTBUS_READ_BOTH and must be nats or kafka")
	}
	primary, err := busFromEnv(provider, bootstrap, sasl)
	if err != nil || !readBoth {
		return primary, err
	}
	otherProvider := "nats"
	if provider == "nats" {
		otherProvider = "kafka"
	}
	other, err := busFromEnv(otherProvider, bootstrap, sasl)
	if err != nil {
		_ = primary.Close()
		return nil, err
	}
	return &migrationBus{primary: primary, other: other, hold: hold}, nil
}

func busFromEnv(provider, bootstrap string, sasl *kafka.SASLConfig) (EventBus, error) {
	switch provider {
	case "kafka":
		return NewKafka(KafkaConfig{
			Bootstrap: bootstrap,
			SASL:      sasl,
		})
	case "nats":
		maxBytes, err := parseByteSize(os.Getenv("NATS_MAX_BYTES"))
		if err != nil {
			return nil, err
		}
		replicas := 0
		if raw := os.Getenv("NATS_REPLICAS"); raw != "" {
			replicas, err = strconv.Atoi(raw)
			if err != nil || replicas < 1 || replicas > 5 {
				return nil, fmt.Errorf("eventbus nats: NATS_REPLICAS must be an integer between 1 and 5")
			}
		}
		discardNew := false
		if raw := os.Getenv("NATS_DISCARD_NEW"); raw != "" {
			discardNew, err = strconv.ParseBool(raw)
			if err != nil {
				return nil, fmt.Errorf("eventbus nats: NATS_DISCARD_NEW must be a boolean")
			}
		}
		return NewNATS(NATSConfig{
			URL:           natsURLFromEnv(),
			StreamName:    os.Getenv("NATS_STREAM_NAME"),
			SubjectPrefix: os.Getenv("NATS_SUBJECT_PREFIX"),
			MaxBytes:      maxBytes,
			Replicas:      replicas,
			DiscardNew:    discardNew,
		})
	default:
		return nil, fmt.Errorf("eventbus: unknown EVENTBUS_PROVIDER %q (want: kafka, nats)", provider)
	}
}
