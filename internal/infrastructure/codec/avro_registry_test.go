//go:build kafka

package codec

import (
	"context"
	"os"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// Against a real Schema Registry, which is the only thing that exercises the
// whole path: derive a schema, register it, frame the payload, and read it back
// through the id the payload names. Skipped without one, so it costs CI nothing
// and is there to run before a codec cutover:
//
//	SR_URL=... SR_KEY=... SR_SECRET=... go test -tags kafka ./internal/infrastructure/codec/ -run Registry
//
// It is the test that found why Confluent's own avrov2 serializer cannot carry
// these envelopes: it marshals through a private avro.API whose type resolver
// avro.Register cannot reach, so a union body fails with "unable to resolve
// type" while encoding cleanly against the same schema through the default API.
func TestAvroRegistryRoundTrip(t *testing.T) {
	if os.Getenv("SR_URL") == "" {
		t.Skip("set SR_URL, SR_KEY and SR_SECRET to run against a registry")
	}
	c, err := NewAvro(os.Getenv("SR_URL"), os.Getenv("SR_KEY"), os.Getenv("SR_SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		topic string
		in    any
		out   any
	}{
		{"worker command", "warmbly-codec-check", models.WorkerEvent{Type: models.WorkerEventTypeSendEmail, Body: &models.SendEmail{Subject: "hi"}}, &models.WorkerEvent{}},
		{"worker result", "warmbly-codec-check-results", models.JobEvent{Type: models.JobEventTypeEmailSent, Body: models.SendEmailResult{}}, &models.JobEvent{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := c.Serialize(ctx, tc.topic, tc.in)
			if err != nil {
				t.Fatalf("serialize: %v", err)
			}
			if err := c.Deserialize(ctx, tc.topic, b, tc.out); err != nil {
				t.Fatalf("deserialize: %v", err)
			}
			t.Logf("%d bytes -> %+v", len(b), tc.out)
		})
	}
}
