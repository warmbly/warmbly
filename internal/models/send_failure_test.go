package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hamba/avro/v2"
	"github.com/warmbly/warmbly/internal/errx"
)

func TestSendResultLegacyJSONAndNativeFailureRoundTrip(t *testing.T) {
	var legacy SendEmailResult
	if err := json.Unmarshal([]byte(`{"task_id":"41ef4f52-9d11-4401-b91c-6d61fb2d9f76","success":false,"error":{"code":"SENDING_TOO_FAST"}}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Error == nil || legacy.Error.Failure != nil {
		t.Fatal("legacy absence was not preserved")
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	retry := now.Add(time.Minute)
	want := &SendEmailResult{TaskID: uuid.New(), Error: &EmailSendError{Code: "SENDING_TOO_FAST", Failure: &errx.SendFailure{Provider: "google", Protocol: "http", Status: 429, Cause: "rateLimitExceeded", Disposition: errx.SendThrottle, Scope: "mailbox", ObservedAt: now, RetryAt: &retry}}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got SendEmailResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	assertBody(t, &got, want)
	in := JobEvent{Type: JobEventTypeEmailFailed, Body: *want}
	var out JobEvent
	if err := avro.Unmarshal(JobEvent{}.Schema(), encode(t, JobEvent{}.Schema(), in), &out); err != nil {
		t.Fatal(err)
	}
	assertBody(t, out.Body, *want)
}

func TestSendResultAvroConsumesLegacyWriter(t *testing.T) {
	reader := JobEvent{}.Schema()
	doc, err := SchemaDocument(reader)
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err := json.Unmarshal(doc, &tree); err != nil {
		t.Fatal(err)
	}
	var remove func(any)
	remove = func(node any) {
		switch v := node.(type) {
		case []any:
			for _, item := range v {
				remove(item)
			}
		case map[string]any:
			if fields, ok := v["fields"].([]any); ok {
				filtered := []any{}
				for _, field := range fields {
					f := field.(map[string]any)
					if f["name"] != "failure" {
						filtered = append(filtered, field)
						remove(f["type"])
					}
				}
				v["fields"] = filtered
			} else {
				remove(v["type"])
			}
		}
	}
	remove(tree)
	legacyDoc, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := avro.Parse(string(legacyDoc))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := avro.NewSchemaCompatibility().Resolve(reader, writer)
	if err != nil {
		t.Fatal(err)
	}
	want := &SendEmailResult{TaskID: uuid.New(), Error: &EmailSendError{Code: "SENDING_TOO_FAST"}}
	payload := encode(t, writer, JobEvent{Type: JobEventTypeEmailFailed, Body: *want})
	var got JobEvent
	if err := avro.Unmarshal(resolved, payload, &got); err != nil {
		t.Fatal(err)
	}
	assertBody(t, got.Body, *want)
	if _, err := avro.NewSchemaCompatibility().Resolve(writer, reader); err != nil {
		t.Fatalf("old readers must ignore optional evidence: %v", err)
	}
}
