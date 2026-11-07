package codec

import (
	"context"

	"github.com/iskorotkov/avro/v2"
)

type migrationCodec struct {
	writer Codec
	avro   Codec
	json   Codec
}

func (c *migrationCodec) Name() string { return c.writer.Name() }

func (c *migrationCodec) Serialize(ctx context.Context, topic string, value any) ([]byte, error) {
	return c.writer.Serialize(ctx, topic, value)
}

func (c *migrationCodec) Deserialize(ctx context.Context, topic string, payload []byte, target any) error {
	if len(payload) > 0 && payload[0] == 0 {
		return c.avro.Deserialize(ctx, topic, payload, target)
	}
	return c.json.Deserialize(ctx, topic, payload, target)
}

func (c *migrationCodec) RegisterSchemas(ctx context.Context, schemas map[string]avro.Schema) error {
	if registrar, ok := c.writer.(SchemaRegistrar); ok {
		return registrar.RegisterSchemas(ctx, schemas)
	}
	return nil
}
