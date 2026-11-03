//go:build kafka

package codec

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry"
	"github.com/google/uuid"
	"github.com/iskorotkov/avro/v2"
	"github.com/warmbly/warmbly/internal/models"
)

// The production encoder must see the original concrete envelope.
func encodeTransportSchema(ctx context.Context, c *AvroCodec, topic string, schema avro.Schema, value any) ([]byte, error) {
	id, err := c.register(subjectFor(topic), schema)
	if err != nil {
		return nil, err
	}
	body, err := models.EventAvro.Marshal(schema, value)
	if err != nil {
		return nil, err
	}
	out := []byte{0, byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	return append(out, body...), nil
}

func priorTransportSchema(t *testing.T, schema avro.Schema) avro.Schema {
	t.Helper()
	doc, err := models.SchemaDocument(schema)
	if err != nil {
		t.Fatal(err)
	}
	var root any
	if err := json.Unmarshal([]byte(doc), &root); err != nil {
		t.Fatal(err)
	}
	var strip func(any)
	strip = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if fields, ok := x["fields"].([]any); ok {
				kept := fields[:0]
				for _, f := range fields {
					record := f.(map[string]any)
					if record["name"] == "transport_generation" || record["name"] == "folder_coverage_observed_at" || x["name"] == "warmbly.events.EmailErrorEvent" && record["name"] == "operation" || x["name"] == "warmbly.events.JobEventSyncState" && (record["name"] == "worker_id" || record["name"] == "report_generation") {
						continue
					}
					strip(f)
					kept = append(kept, f)
				}
				x["fields"] = kept
			}
			for k, value := range x {
				if k != "fields" {
					strip(value)
				}
			}
		case []any:
			for _, value := range x {
				strip(value)
			}
		}
	}
	strip(root)
	old, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := avro.Parse(string(old))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestPriorTransportSchemaOmitsReportingIdentity(t *testing.T) {
	current := models.JobEvent{}.Schema()
	for _, tc := range []struct {
		schema  avro.Schema
		present bool
	}{
		{current, true}, {priorTransportSchema(t, current), false},
	} {
		found := false
		branches := tc.schema.(*avro.RecordSchema).Fields()[1].Type().(*avro.UnionSchema).Types()
		for _, branch := range branches {
			record, ok := branch.(*avro.RecordSchema)
			if !ok || record.FullName() != "warmbly.events.JobEventSyncState" {
				continue
			}
			found = true
			for _, name := range []string{"worker_id", "report_generation"} {
				present := false
				for _, field := range record.Fields() {
					present = present || field.Name() == name
				}
				if present != tc.present {
					t.Fatalf("%s present=%t want=%t", name, present, tc.present)
				}
			}
		}
		if !found {
			t.Fatal("sync record missing from schema")
		}
	}
}

func TestMailTransportCodecRegistryCompatibility(t *testing.T) {
	if encoded := os.Getenv("WARMBLY_TRANSPORT_LEGACY_PAYLOAD"); encoded != "" {
		for _, name := range []string{"AddWorkerEmail", "SendEmailResult", "JobEventSyncState"} {
			models.EventAvro.Register("warmbly.events."+name, priorTransportBody{})
		}
		type priorError struct {
			EmailAccountID string `avro:"email_account_id"`
		}
		models.EventAvro.Register("warmbly.events.EmailErrorEvent", priorError{})
		payload, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		c, err := NewAvro(os.Getenv("SR_URL"), "", "")
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Type string `avro:"type"`
			Body any    `avro:"body"`
		}
		if err := c.Deserialize(t.Context(), os.Getenv("WARMBLY_TRANSPORT_TOPIC"), payload, &out); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("WARMBLY_TRANSPORT_CASE") == "error" {
			body, ok := out.Body.(priorError)
			if !ok || body.EmailAccountID != os.Getenv("WARMBLY_TRANSPORT_ID") {
				t.Fatal("old consumer lost failure")
			}
			t.Log("LEGACY-CODEC-DECODED")
			return
		}
		body, ok := out.Body.(priorTransportBody)
		if !ok {
			t.Fatalf("legacy body = %T", out.Body)
		}
		switch os.Getenv("WARMBLY_TRANSPORT_CASE") {
		case "add":
			if body.ID.String() != os.Getenv("WARMBLY_TRANSPORT_ID") {
				t.Fatal("old worker lost mailbox")
			}
		case "send":
			if body.TaskID.String() != os.Getenv("WARMBLY_TRANSPORT_ID") || !body.Success {
				t.Fatal("old consumer lost outcome")
			}
		case "sync":
			if body.EmailID.String() != os.Getenv("WARMBLY_TRANSPORT_ID") || body.State.LastSyncedAt == nil {
				t.Fatal("old consumer lost checkpoint")
			}
		case "error":
			if body.EmailAccountID != os.Getenv("WARMBLY_TRANSPORT_ID") {
				t.Fatal("old consumer lost failure")
			}
		}
		t.Log("LEGACY-CODEC-DECODED")
		return
	}
	url := os.Getenv("SR_URL")
	if url == "" {
		t.Skip("SR_URL must name an isolated local Schema Registry")
	}
	generation, reportGeneration, worker, task, mailbox := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Millisecond)
	for _, tc := range []struct {
		name   string
		value  any
		schema avro.Schema
		target func() any
		proof  func(any) uuid.UUID
	}{
		{"add", models.WorkerEvent{Type: models.WorkerEventTypeAddEmail, Body: &models.AddWorkerEmail{ID: mailbox, TransportGeneration: generation}}, models.WorkerEvent{}.Schema(), func() any { return &models.WorkerEvent{} }, func(v any) uuid.UUID {
			return v.(*models.WorkerEvent).Body.(*models.AddWorkerEmail).TransportGeneration
		}},
		{"send", models.JobEvent{Type: models.JobEventTypeEmailSent, Body: models.SendEmailResult{TaskID: task, Success: true, SentAt: at, TransportGeneration: generation}}, models.JobEvent{}.Schema(), func() any { return &models.JobEvent{} }, func(v any) uuid.UUID { return v.(*models.JobEvent).Body.(models.SendEmailResult).TransportGeneration }},
		{"sync", models.JobEvent{Type: models.JobEventTypeSyncState, Body: &models.JobEventSyncState{WorkerID: &worker, ReportGeneration: reportGeneration, EmailID: mailbox, State: models.SyncState{LastSyncedAt: &at, FolderCoverageObservedAt: &at, TransportGeneration: generation}}}, models.JobEvent{}.Schema(), func() any { return &models.JobEvent{} }, func(v any) uuid.UUID {
			return v.(*models.JobEvent).Body.(*models.JobEventSyncState).State.TransportGeneration
		}},
		{"error", models.JobEvent{Type: models.JobEventTypeEmailServerError, Body: models.EmailErrorEvent{EmailAccountID: mailbox.String(), Operation: models.MailTransportSync, TransportGeneration: generation}}, models.JobEvent{}.Schema(), func() any { return &models.JobEvent{} }, func(v any) uuid.UUID { return v.(*models.JobEvent).Body.(models.EmailErrorEvent).TransportGeneration }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, err := NewAvro(url, "", "")
			if err != nil {
				t.Fatal(err)
			}
			writer := current.(*AvroCodec)
			previous := priorTransportSchema(t, tc.schema)
			topic := fmt.Sprintf("transport-generation-%s-%s", tc.name, uuid.New())
			if _, err := writer.client.UpdateCompatibility(subjectFor(topic), schemaregistry.FullTransitive); err != nil {
				t.Fatal(err)
			}
			oldPayload, err := encodeTransportSchema(t.Context(), writer, topic, previous, tc.value)
			if err != nil {
				t.Fatal("old native encoding", err)
			}
			// A cold consumer must fetch the actual registered writer schema by id.
			cold, err := NewAvro(url, "", "")
			if err != nil {
				t.Fatal(err)
			}
			target := tc.target()
			if err := cold.Deserialize(t.Context(), topic, oldPayload, target); err != nil {
				t.Fatal("new reader/old writer", err)
			}
			if tc.proof(target) != uuid.Nil {
				t.Fatal("old payload fabricated proof")
			}
			if tc.name == "sync" && target.(*models.JobEvent).Body.(*models.JobEventSyncState).WorkerID != nil {
				t.Fatal("old sync payload manufactured a reporting worker")
			}
			if tc.name == "sync" && target.(*models.JobEvent).Body.(*models.JobEventSyncState).ReportGeneration != uuid.Nil {
				t.Fatal("old sync payload manufactured a report generation")
			}
			fresh, err := writer.Serialize(t.Context(), topic, tc.value)
			if err != nil {
				t.Fatal("FULL_TRANSITIVE registry rejected additive schema", err)
			}
			target = tc.target()
			if err := cold.Deserialize(t.Context(), topic, fresh, target); err != nil {
				t.Fatal(err)
			}
			if tc.proof(target) != generation {
				t.Fatal("typed wire lost generation")
			}
			if tc.name == "sync" {
				report := target.(*models.JobEvent).Body.(*models.JobEventSyncState)
				if report.WorkerID == nil || *report.WorkerID != worker {
					t.Fatal("typed wire lost reporting worker")
				}
				if report.ReportGeneration != reportGeneration {
					t.Fatal("typed wire lost report generation")
				}
				if report.State.FolderCoverageObservedAt == nil || !report.State.FolderCoverageObservedAt.Equal(at) {
					t.Fatal("typed wire lost folder coverage observation")
				}
			}
			id := mailbox
			if tc.name == "send" {
				id = task
			}
			process := exec.Command(os.Args[0], "-test.run=^TestMailTransportCodecRegistryCompatibility$", "-test.v")
			process.Env = append(os.Environ(), "WARMBLY_TRANSPORT_LEGACY_PAYLOAD="+base64.StdEncoding.EncodeToString(fresh), "WARMBLY_TRANSPORT_TOPIC="+topic, "WARMBLY_TRANSPORT_CASE="+tc.name, "WARMBLY_TRANSPORT_ID="+id.String())
			output, err := process.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "LEGACY-CODEC-DECODED") {
				t.Fatalf("cold old reader/new native writer: %v\n%s", err, output)
			}
			// The registry checks both directions, including returning to an old writer.
			older, err := NewAvro(url, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := encodeTransportSchema(t.Context(), older.(*AvroCodec), topic, previous, tc.value); err != nil {
				t.Fatal("old writer after upgrade rejected", err)
			}
		})
	}
}
