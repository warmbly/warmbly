//go:build kafka

// The Avro codec frames events in Confluent's wire format and resolves schemas
// against a Schema Registry. It is compiled only with the `kafka` build tag,
// because the registry client reaches the cgo kafka package and an instance not
// on Kafka has no registry to resolve against.
package codec

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry"
	"github.com/hamba/avro/v2"
)

// Confluent's framing: a zero byte, the schema's registry id big-endian, then
// the Avro body. Five bytes, and the only thing standing between a registry and
// a payload.
const (
	wireMagic  = 0
	wireHeader = 5
)

// AvroCodec encodes through hamba's default API rather than Confluent's
// avrov2 serializer, which is the one thing here worth explaining.
//
// Both bus envelopes carry `Body any`, and hamba resolves which union branch an
// `any` belongs to through a type resolver that `avro.Register` writes to. That
// registry is `avro.DefaultConfig`'s. avrov2 marshals with `avro.Config{}.Freeze()`,
// a private API holding its own empty resolver, and exposes no way to add to
// it, so every worker command failed there with "unable to resolve type" while
// encoding perfectly against the same schema through the default API.
//
// So the registry client is used for what it is good at, turning a schema into
// an id and back, and the encoding is done where the types are known.
type AvroCodec struct {
	client schemaregistry.Client

	mu      sync.RWMutex
	ids     map[string]int      // subject + schema -> registered id
	schemas map[int]avro.Schema // id -> parsed schema, for decoding
}

// NewAvro constructs an AvroCodec from Schema Registry credentials.
func NewAvro(schemaRegistryURL, schemaRegistryKey, schemaRegistrySecret string) (Codec, error) {
	client, err := schemaregistry.NewClient(schemaregistry.NewConfigWithBasicAuthentication(
		schemaRegistryURL, schemaRegistryKey, schemaRegistrySecret,
	))
	if err != nil {
		return nil, fmt.Errorf("codec: schema registry: %w", err)
	}
	return &AvroCodec{
		client:  client,
		ids:     map[string]int{},
		schemas: map[int]avro.Schema{},
	}, nil
}

// Name satisfies Codec.
func (c *AvroCodec) Name() string { return "avro" }

var _ Codec = (*AvroCodec)(nil)

// Serialize registers the value's schema under the topic's subject, then writes
// the framed payload. A value that describes its own schema is asked for it;
// anything else is refused rather than guessed at, because guessing is what
// produced an interface with nothing to describe.
func (c *AvroCodec) Serialize(_ context.Context, topic string, value any) ([]byte, error) {
	described, ok := value.(interface{ Schema() avro.Schema })
	if !ok {
		return nil, fmt.Errorf("codec: %T does not carry an Avro schema", value)
	}
	schema := described.Schema()

	id, err := c.register(subjectFor(topic), schema)
	if err != nil {
		return nil, err
	}
	body, err := avro.Marshal(schema, value)
	if err != nil {
		return nil, fmt.Errorf("codec: encode %T: %w", value, err)
	}

	out := make([]byte, wireHeader, wireHeader+len(body))
	out[0] = wireMagic
	binary.BigEndian.PutUint32(out[1:], uint32(id))
	return append(out, body...), nil
}

// Deserialize decodes with the schema the payload names, which is the writer's
// rather than whatever this process happens to hold. That is the whole point of
// carrying the id: a consumer reads what was actually written.
func (c *AvroCodec) Deserialize(_ context.Context, topic string, payload []byte, target any) error {
	if len(payload) < wireHeader || payload[0] != wireMagic {
		return errors.New("codec: payload is not in the schema registry wire format")
	}
	id := int(binary.BigEndian.Uint32(payload[1:wireHeader]))
	schema, err := c.schemaByID(subjectFor(topic), id)
	if err != nil {
		return err
	}
	if err := avro.Unmarshal(schema, payload[wireHeader:], target); err != nil {
		return fmt.Errorf("codec: decode into %T: %w", target, err)
	}
	return nil
}

// register is memoised because the registry answers the same question with the
// same id forever, and a lookup per published event would put the registry on
// the send path.
func (c *AvroCodec) register(subject string, schema avro.Schema) (int, error) {
	key := subject + "\x00" + schema.String()
	c.mu.RLock()
	id, ok := c.ids[key]
	c.mu.RUnlock()
	if ok {
		return id, nil
	}
	id, err := c.client.Register(subject, schemaregistry.SchemaInfo{Schema: schema.String()}, true)
	if err != nil {
		return 0, fmt.Errorf("codec: register %s: %w", subject, err)
	}
	c.mu.Lock()
	c.ids[key] = id
	c.mu.Unlock()
	return id, nil
}

func (c *AvroCodec) schemaByID(subject string, id int) (avro.Schema, error) {
	c.mu.RLock()
	schema, ok := c.schemas[id]
	c.mu.RUnlock()
	if ok {
		return schema, nil
	}
	info, err := c.client.GetBySubjectAndID(subject, id)
	if err != nil {
		return nil, fmt.Errorf("codec: schema %d: %w", id, err)
	}
	schema, err = avro.Parse(info.Schema)
	if err != nil {
		return nil, fmt.Errorf("codec: schema %d is not readable: %w", id, err)
	}
	c.mu.Lock()
	c.schemas[id] = schema
	c.mu.Unlock()
	return schema, nil
}

// subjectFor is Confluent's TopicNameStrategy, the default everywhere else.
func subjectFor(topic string) string { return topic + "-value" }
