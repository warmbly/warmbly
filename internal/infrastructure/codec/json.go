package codec

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/rs/zerolog/log"
)

// JSONCodec encodes payloads using the standard library encoding/json. No
// Schema Registry, no external services. Suitable for self-hosted deployments
// that pair the NATS JetStream EventBus with the simpler codec.
//
// Wire format is plain JSON: a JSONCodec consumer cannot decode Avro-framed
// bytes (and the reverse), so the operator must pick one codec per cluster.
//
// The codec is stateless and safe for concurrent use.
type JSONCodec struct{}

// NewJSON constructs a JSONCodec.
func NewJSON() *JSONCodec { return &JSONCodec{} }

// Name satisfies Codec.
func (c *JSONCodec) Name() string { return "json" }

// Serialize satisfies Codec. The topic argument is ignored — JSON has no
// per-topic schema lookup — but is logged at debug level so operators can
// confirm wiring during a cutover.
func (c *JSONCodec) Serialize(_ context.Context, topic string, value any) ([]byte, error) {
	if value == nil {
		return nil, errors.New("codec: json serialize requires non-nil value")
	}
	log.Debug().Str("codec", "json").Str("topic", topic).Msg("serialize")
	return json.Marshal(value)
}

// Deserialize satisfies Codec. target must be a non-nil pointer; encoding/json
// rejects anything else with a clear error, which is forwarded as-is.
func (c *JSONCodec) Deserialize(_ context.Context, topic string, payload []byte, target any) error {
	if target == nil {
		return errors.New("codec: json deserialize requires non-nil target pointer")
	}
	log.Debug().Str("codec", "json").Str("topic", topic).Int("bytes", len(payload)).Msg("deserialize")
	return json.Unmarshal(payload, target)
}
