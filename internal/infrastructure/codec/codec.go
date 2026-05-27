// Package codec is the abstraction over event-payload serialization.
//
// Two implementations:
//   - AvroCodec  (Confluent Schema Registry + Avro v2 — historical default,
//     wraps internal/infrastructure/kafka.Avrov2)
//   - JSONCodec  (encoding/json — self-hostable alternative with no external
//     schema registry dependency)
//
// The interface is intentionally small so the EventBus abstraction in
// internal/infrastructure/eventbus can stay transport-agnostic: the bus
// carries opaque bytes, the codec decides how those bytes are framed.
//
// Switching codecs requires that publishers and consumers agree: a JSONCodec
// consumer cannot decode an Avro-framed message and vice versa. There is no
// in-band marker, this is an operator-level configuration choice.
package codec

import "context"

// Codec serializes and deserializes event payloads. Implementations may
// require external services (Schema Registry for Avro) or be standalone
// (JSON). Implementations must be safe for concurrent use.
type Codec interface {
	// Serialize encodes value into a transport-ready byte slice. topic is
	// passed through because some codecs (Avro with subject naming strategy)
	// need it to look up the right schema; codecs that don't care may ignore
	// it.
	Serialize(ctx context.Context, topic string, value any) ([]byte, error)

	// Deserialize decodes payload into target, which must be a non-nil
	// pointer to a value of the expected type. Same topic contract as
	// Serialize.
	Deserialize(ctx context.Context, topic string, payload []byte, target any) error

	// Name returns a short identifier ("avro", "json") used in admin UI,
	// startup logs, and audit trails.
	Name() string
}

// Compile-time interface checks.
var (
	_ Codec = (*AvroCodec)(nil)
	_ Codec = (*JSONCodec)(nil)
)
