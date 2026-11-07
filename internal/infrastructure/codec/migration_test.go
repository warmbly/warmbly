package codec

import (
	"context"
	"testing"
)

type migrationAvroStub struct {
	Codec
	reads int
}

func (c *migrationAvroStub) Deserialize(_ context.Context, _ string, _ []byte, _ any) error {
	c.reads++
	return nil
}

func TestMigrationCodecRecognizesFramingAndKeepsJSONWriter(t *testing.T) {
	avro := &migrationAvroStub{}
	c := &migrationCodec{writer: NewJSON(), json: NewJSON(), avro: avro}
	var result struct {
		ID string `json:"id"`
	}
	if err := c.Deserialize(t.Context(), "results", []byte(`{"id":"accepted"}`), &result); err != nil || result.ID != "accepted" {
		t.Fatalf("JSON result: %v", err)
	}
	if err := c.Deserialize(t.Context(), "results", []byte{0, 0, 0, 0, 7, 42}, &result); err != nil || avro.reads != 1 {
		t.Fatalf("Avro framing: %v", err)
	}
	if err := c.Deserialize(t.Context(), "results", nil, &result); err == nil {
		t.Fatal("empty payload accepted")
	}
	payload, err := c.Serialize(t.Context(), "results", result)
	if err != nil || string(payload) != `{"id":"accepted"}` {
		t.Fatalf("writer changed format: %s, %v", payload, err)
	}
}
