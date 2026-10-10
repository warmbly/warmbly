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
	"strings"
	"sync"

	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry"
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry/rest"
	"github.com/iskorotkov/avro/v2"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

// Confluent's framing: a zero byte, the schema's registry id big-endian, then
// the Avro body. Five bytes, and the only thing standing between a registry and
// a payload.
const (
	wireMagic  = 0
	wireHeader = 5
)

// AvroCodec uses the bounded API that owns the envelopes' union type registry.
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
	body, err := models.EventAvro.Marshal(schema, value)
	if err != nil {
		return nil, fmt.Errorf("codec: encode %T: %w", value, err)
	}

	out := make([]byte, wireHeader, wireHeader+len(body))
	out[0] = wireMagic
	binary.BigEndian.PutUint32(out[1:], uint32(id))
	return append(out, body...), nil
}

// RegisterSchemas satisfies SchemaRegistrar.
func (c *AvroCodec) RegisterSchemas(_ context.Context, schemas map[string]avro.Schema) error {
	var all []string
	var errs []error
	for topic, schema := range schemas {
		topics := []string{topic}
		if prefix, ok := strings.CutSuffix(topic, "*"); ok {
			if all == nil {
				var err error
				if all, err = c.client.GetAllSubjects(); err != nil {
					return fmt.Errorf("codec: list subjects: %w", err)
				}
			}
			topics = topics[:0]
			for _, subject := range all {
				if t, ok := strings.CutSuffix(subject, "-value"); ok && strings.HasPrefix(t, prefix) {
					topics = append(topics, t)
				}
			}
		}
		for _, t := range topics {
			if _, err := c.register(subjectFor(t), schema); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

var _ SchemaRegistrar = (*AvroCodec)(nil)

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
	if err := models.EventAvro.Unmarshal(schema, payload[wireHeader:], target); err != nil {
		return fmt.Errorf("codec: decode into %T: %w", target, err)
	}
	return nil
}

// register is memoised because the registry answers the same question with the
// same id forever, and a lookup per published event would put the registry on
// the send path.
func (c *AvroCodec) register(subject string, schema avro.Schema) (int, error) {
	// The registered document, not String(): String() omits every field
	// default, so registering it hands the registry a schema where no field
	// can be added later without breaking compatibility (#583), and the raw
	// marshal writes a fixed default in a form no reader parses (#586). The
	// cache key has to be the same text, or two schemas differing only in
	// their defaults share an id.
	doc, err := models.SchemaDocument(schema)
	if err != nil {
		return 0, fmt.Errorf("codec: schema document for %s: %w", subject, err)
	}
	key := subject + "\x00" + string(doc)
	c.mu.RLock()
	id, ok := c.ids[key]
	c.mu.RUnlock()
	if ok {
		return id, nil
	}
	id, err = c.client.Register(subject, schemaregistry.SchemaInfo{Schema: string(doc)}, true)
	if isIncompatible(err) && c.pinBackward(subject) {
		id, err = c.client.Register(subject, schemaregistry.SchemaInfo{Schema: string(doc)}, true)
	}
	if err != nil {
		return 0, fmt.Errorf("codec: register %s: %w", subject, err)
	}
	c.mu.Lock()
	c.ids[key] = id
	c.mu.Unlock()
	return id, nil
}

// isIncompatible is the registry refusing a schema against an earlier version.
func isIncompatible(err error) bool {
	var rerr *rest.Error
	return errors.As(err, &rerr) && rerr.Code == 409
}

// pinBackward sets the subject to BACKWARD when its effective level refuses a
// new union branch, since every new event type adds one to the envelope. A
// level that already admits it is left alone, and so is the refusal.
func (c *AvroCodec) pinBackward(subject string) bool {
	level, err := c.client.GetCompatibility(subject)
	if err != nil {
		if level, err = c.client.GetDefaultCompatibility(); err != nil {
			return false
		}
	}
	switch level {
	case schemaregistry.Forward, schemaregistry.ForwardTransitive,
		schemaregistry.Full, schemaregistry.FullTransitive:
	default:
		return false
	}
	if _, err := c.client.UpdateCompatibility(subject, schemaregistry.Backward); err != nil {
		return false
	}
	log.Warn().Str("subject", subject).Str("was", level.String()).
		Msg("schema registry: subject set to BACKWARD so a new event type can be registered")
	return true
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
