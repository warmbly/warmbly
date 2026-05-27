package codec

import (
	"context"
	"errors"

	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
)

// AvroCodec wraps the existing kafka.Avrov2 Schema Registry serializer so the
// EventBus can hold a transport-agnostic Codec without growing a hard
// dependency on the kafka package. Behavior is intentionally identical to
// calling kafka.Avrov2.Ser.Serialize / Deser.DeserializeInto directly: this
// type adds no framing, no headers, no per-message branching.
//
// The underlying Avrov2 holds a Schema Registry client which manages its own
// HTTP connection pool and is safe for concurrent use.
type AvroCodec struct {
	a *kafka.Avrov2
}

// NewAvro constructs an AvroCodec from Schema Registry credentials. The
// arguments mirror kafka.NewAvrov2Client so call sites that already source
// these from secrets can pass them through unchanged.
func NewAvro(schemaRegistryURL, schemaRegistryKey, schemaRegistrySecret string) (*AvroCodec, error) {
	client, err := kafka.NewAvrov2Client(schemaRegistryURL, schemaRegistryKey, schemaRegistrySecret)
	if err != nil {
		return nil, err
	}
	return &AvroCodec{a: client}, nil
}

// NewAvroFromClient wraps an already-constructed kafka.Avrov2. Useful during
// migration so callers that still hold a *kafka.Avrov2 directly can share the
// same Schema Registry connection rather than opening a second one.
func NewAvroFromClient(client *kafka.Avrov2) *AvroCodec {
	return &AvroCodec{a: client}
}

// Underlying returns the wrapped kafka.Avrov2. Exposed so callers in the
// middle of the transport refactor can still reach the original serializer /
// deserializer (e.g. the kafka.Producer.WithAvrov2 hookup) without a second
// Schema Registry handshake. Will be removed once all call sites go through
// the Codec interface.
func (c *AvroCodec) Underlying() *kafka.Avrov2 { return c.a }

// Name satisfies Codec.
func (c *AvroCodec) Name() string { return "avro" }

// Serialize satisfies Codec. The Schema Registry serializer ignores ctx
// internally, but the parameter is part of the interface so future codecs
// (gRPC-style schema registries, remote codecs) can honour cancellation.
func (c *AvroCodec) Serialize(_ context.Context, topic string, value any) ([]byte, error) {
	if c == nil || c.a == nil || c.a.Ser == nil {
		return nil, errors.New("codec: avro serializer not configured")
	}
	return c.a.Ser.Serialize(topic, value)
}

// Deserialize satisfies Codec. target must be a non-nil pointer to the
// expected message type, matching avrov2.Deserializer.DeserializeInto.
func (c *AvroCodec) Deserialize(_ context.Context, topic string, payload []byte, target any) error {
	if c == nil || c.a == nil || c.a.Deser == nil {
		return errors.New("codec: avro deserializer not configured")
	}
	return c.a.Deser.DeserializeInto(topic, payload, target)
}
