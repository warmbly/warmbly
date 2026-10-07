package jobs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hamba/avro/v2"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func legacySendResultSchema(t *testing.T) (avro.Schema, avro.Schema) {
	t.Helper()
	reader := models.JobEvent{}.Schema()
	doc, err := models.SchemaDocument(reader)
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err = json.Unmarshal(doc, &tree); err != nil {
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
				var kept []any
				for _, raw := range fields {
					f := raw.(map[string]any)
					if f["name"] != "failure" {
						kept = append(kept, f)
						remove(f["type"])
					}
				}
				v["fields"] = kept
			}
			for key, value := range v {
				if key != "fields" {
					remove(value)
				}
			}
		}
	}
	remove(tree)
	doc, err = json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := avro.Parse(string(doc))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := avro.NewSchemaCompatibility().Resolve(reader, writer)
	if err != nil {
		t.Fatal(err)
	}
	return writer, resolved
}

func TestLiveLegacyJSONAndAvroFailurePersistConservativeAdmission(t *testing.T) {
	writer, reader := legacySendResultSchema(t)
	for _, format := range []string{"json", "avro"} {
		for _, code := range []string{"SENDING_TOO_FAST", "QUOTA_EXCEEDED", "RECIPIENT_REJECTED", "UNKNOWN"} {
			t.Run(format+"/"+code, func(t *testing.T) {
				h := liveDB(t)
				f := newSendResultFixture(t, h)
				svc := liveJobsService(h)
				id := f.stampSend(t, svc)
				now := time.Now().UTC()
				original := models.SendEmailResult{TaskID: id, SentAt: now, Error: &models.EmailSendError{Code: code}}
				var result models.SendEmailResult
				if format == "json" {
					data, err := json.Marshal(original)
					if err != nil {
						t.Fatal(err)
					}
					if err = json.Unmarshal(data, &result); err != nil {
						t.Fatal(err)
					}
				} else {
					data, err := avro.Marshal(writer, models.JobEvent{Type: models.JobEventTypeEmailFailed, Body: original})
					if err != nil {
						t.Fatal(err)
					}
					var event models.JobEvent
					if err = avro.Unmarshal(reader, data, &event); err != nil {
						t.Fatal(err)
					}
					var ok bool
					result, ok = event.Body.(models.SendEmailResult)
					if !ok {
						t.Fatalf("decoded body %T", event.Body)
					}
				}
				if result.Error == nil || result.Error.Failure != nil {
					t.Fatal("legacy transport invented native evidence")
				}
				for range 2 {
					if err := svc.HandleEmailFailed(t.Context(), result); err != nil {
						t.Fatal(err)
					}
				}
				replaced := liveJobsService(h)
				admission, err := replaced.TaskRepo.(repository.SendResultRecovery).GetSendAdmission(t.Context(), f.org, f.mailbox, models.InboxProviderSMTPIMAP, now)
				if err != nil {
					t.Fatal(err)
				}
				switch code {
				case "SENDING_TOO_FAST", "QUOTA_EXCEEDED":
					if admission.Allowed || admission.RecoveryHold || admission.Scope != "mailbox" || admission.RetryAt == nil || admission.RetryAt.Sub(now).Abs() < 4*time.Minute {
						t.Fatalf("legacy throttle admission %+v", admission)
					}
					after, err := replaced.TaskRepo.(repository.SendResultRecovery).GetSendAdmission(t.Context(), f.org, f.mailbox, models.InboxProviderSMTPIMAP, now.Add(6*time.Minute))
					if err != nil || !after.Allowed {
						t.Fatal("transient throttle became auth hold")
					}
				case "RECIPIENT_REJECTED":
					if !admission.Allowed || admission.RetryAt != nil {
						t.Fatal("recipient refusal became mailbox hold")
					}
				default:
					if admission.Allowed || !admission.RecoveryHold {
						t.Fatal("unknown old result became success")
					}
				}
				var status string
				if err = h.Pool.QueryRow(t.Context(), `SELECT status FROM email_accounts WHERE id=$1`, f.mailbox).Scan(&status); err != nil || status != "active" {
					t.Fatal("legacy failure disabled mailbox")
				}
			})
		}
	}
}
