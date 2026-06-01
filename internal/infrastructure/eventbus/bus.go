// Package eventbus is the abstraction over a durable message broker.
//
// Two implementations:
//   - KafkaBus  (Confluent / Apache Kafka — historical default, wraps the
//     existing internal/infrastructure/kafka producer + consumer)
//   - NATSBus   (NATS JetStream — self-hostable alternative for single-binary
//     or small-cluster deployments)
//
// The interface is intentionally small. It exposes only what the rest of the
// codebase needs today (publish-by-key, consumer-group subscribe with manual
// ack-after-success). Schema-registry / Avro framing remains the caller's
// concern: this package handles transport only.
//
// Topic naming:
//   - Names use "." as the separator (e.g. "w.<uuid>", "jobs.worker-events").
//     "." is legal in both Kafka topic names and NATS subjects, so the same
//     name addresses either backend. A ":" is NOT legal in a Kafka topic name,
//     so it must never appear in these names.
//
// The Subject() helper still normalises any stray ":" to "." in a single place
// (idempotent for the dotted names callers already produce), so callers can
// keep using the kafka.GetWorkerTopic / kafka.TopicWorkerEvents constants
// without caring which backend is wired in.
package eventbus

import (
	"context"
	"strings"
)

// EventBus is the transport-level interface. Implementations must be safe for
// concurrent use by multiple goroutines.
type EventBus interface {
	// Publish writes a single message to the named topic. The key is used by
	// the underlying broker for partitioning (Kafka) or as a message-level
	// hint (NATS); it may be empty when partition affinity does not matter.
	//
	// Implementations should treat Publish as best-effort at-least-once.
	// Callers that need stronger guarantees should publish inside a retry
	// loop and surface failures upstream.
	Publish(ctx context.Context, topic, key string, payload []byte) error

	// Subscribe registers a Handler against one or more topics under a named
	// consumer group. The call blocks until the context is cancelled or a
	// fatal error occurs. The handler is invoked one message at a time per
	// subscription. A handler that returns nil signals successful processing
	// and the message is acked; a non-nil error leaves the message for
	// redelivery according to the backend's retry policy.
	Subscribe(ctx context.Context, topics []string, group string, handler Handler) error

	// Close releases any underlying connections. Safe to call multiple times.
	Close() error

	// Name returns a short identifier ("kafka", "nats") used in admin UI,
	// startup logs, and audit trails.
	Name() string
}

// Handler is the per-message callback invoked by Subscribe. Returning nil acks
// the message; returning an error leaves it for redelivery.
type Handler func(ctx context.Context, msg Message) error

// Message is the broker-neutral envelope passed to a Handler. Payload bytes
// are owned by the bus and must not be retained past the handler call.
//
// Topic is reported in the backend's native form ("w.<uuid>" on both Kafka
// and NATS). Handlers that need to compare against a known topic name should
// normalise both sides with Subject() rather than relying on byte equality.
type Message struct {
	Topic   string
	Key     string
	Payload []byte
}

// Subject normalises a topic name to the dot-separated form by replacing any
// ":" with ".". Topic names already use ".", so this is a no-op for them; it
// remains as a guard against any stray legacy colon name. Idempotent.
//
// Exposed so callers that need to address a topic directly through the NATS
// bus (e.g. tests, admin tools) can produce a valid subject without knowing
// the substitution rule.
func Subject(topic string) string {
	return strings.ReplaceAll(topic, ":", ".")
}
