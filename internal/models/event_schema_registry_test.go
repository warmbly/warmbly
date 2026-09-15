//go:build kafka

package models

import (
	"os"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry"
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry/serde"
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry/serde/avrov2"
)

// Against a real Schema Registry, which is the only thing that judges a schema
// document rather than the objects it was built from. Skipped without one, so
// it costs CI nothing and is there to run before a codec cutover:
//
//	SR_URL=... SR_KEY=... SR_SECRET=... go test -tags kafka ./internal/models/ -run Registry
//
// It earned its place immediately. The in-memory round-trip passed while the
// document the registry was handed defined warmbly.events.Token three times,
// because the same oauth2.Token is reachable through three fields of one worker
// command and Avro names a record once. Nothing but a registry rejects that.
func TestSchemasRegister(t *testing.T) {
	url := os.Getenv("SR_URL")
	if url == "" {
		t.Skip("set SR_URL, SR_KEY and SR_SECRET to run against a registry")
	}
	client, err := schemaregistry.NewClient(
		schemaregistry.NewConfigWithAuthentication(url, os.Getenv("SR_KEY"), os.Getenv("SR_SECRET")),
	)
	if err != nil {
		t.Fatalf("registry client: %v", err)
	}
	ser, err := avrov2.NewSerializer(client, serde.ValueSerde, avrov2.NewSerializerConfig())
	if err != nil {
		t.Fatalf("serializer: %v", err)
	}
	deser, err := avrov2.NewDeserializer(client, serde.ValueSerde, avrov2.NewDeserializerConfig())
	if err != nil {
		t.Fatalf("deserializer: %v", err)
	}

	t.Run("worker command", func(t *testing.T) {
		in := WorkerEvent{Type: WorkerEventTypeSendEmail, Body: &SendEmail{}}
		payload, err := ser.Serialize("warmbly-schema-check", &in)
		if err != nil {
			t.Fatalf("serialize: %v", err)
		}
		var out WorkerEvent
		if err := deser.DeserializeInto("warmbly-schema-check", payload, &out); err != nil {
			t.Fatalf("deserialize: %v", err)
		}
		if out.Type != in.Type {
			t.Fatalf("type = %q, want %q", out.Type, in.Type)
		}
	})

	t.Run("worker result", func(t *testing.T) {
		in := JobEvent{Type: JobEventTypeEmailSent, Body: SendEmailResult{}}
		payload, err := ser.Serialize("warmbly-schema-check-results", &in)
		if err != nil {
			t.Fatalf("serialize: %v", err)
		}
		var out JobEvent
		if err := deser.DeserializeInto("warmbly-schema-check-results", payload, &out); err != nil {
			t.Fatalf("deserialize: %v", err)
		}
		if out.Type != in.Type {
			t.Fatalf("type = %q, want %q", out.Type, in.Type)
		}
	})
}
