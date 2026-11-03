package codec

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

type priorTransportBody struct {
	ID             uuid.UUID `json:"id" avro:"id"`
	TaskID         uuid.UUID `json:"task_id" avro:"task_id"`
	Success        bool      `json:"success" avro:"success"`
	MessageID      string    `json:"message_id" avro:"message_id"`
	EmailID        uuid.UUID `json:"email_id" avro:"email_id"`
	EmailAccountID string    `json:"email_account_id" avro:"email_account_id"`
	State          struct {
		LastSyncedAt *time.Time `json:"last_synced_at" avro:"last_synced_at"`
	} `json:"state" avro:"state"`
}
type priorTransportEnvelope struct {
	Type string             `json:"type" avro:"type"`
	Body priorTransportBody `json:"body" avro:"body"`
}

func TestMailTransportCodecJSONCompatibility(t *testing.T) {
	c := NewJSON()
	generation, task := uuid.New(), uuid.New()
	at := time.Now().UTC()
	event := models.JobEvent{Type: models.JobEventTypeEmailSent, Body: models.SendEmailResult{TaskID: task, Success: true, MessageID: "accepted@example.test", SentAt: at, TransportGeneration: generation}}
	payload, err := c.Serialize(t.Context(), "results", event)
	if err != nil {
		t.Fatal(err)
	}
	var prior priorTransportEnvelope
	if err := c.Deserialize(t.Context(), "results", payload, &prior); err != nil {
		t.Fatal(err)
	}
	if prior.Body.TaskID != task || !prior.Body.Success || prior.Body.MessageID != "accepted@example.test" {
		t.Fatal("old reader lost genuine outcome")
	}
	old := priorTransportEnvelope{Type: string(models.JobEventTypeEmailSent), Body: priorTransportBody{TaskID: task, Success: true}}
	payload, err = c.Serialize(t.Context(), "results", old)
	if err != nil {
		t.Fatal(err)
	}
	var current struct {
		Body models.SendEmailResult `json:"body"`
	}
	if err := c.Deserialize(t.Context(), "results", payload, &current); err != nil {
		t.Fatal(err)
	}
	result := current.Body
	if result.TransportGeneration != uuid.Nil || !result.Success || result.TaskID != task {
		t.Fatal("old writer changed evidence or manufactured proof")
	}
	mailbox := uuid.New()
	for _, kind := range []string{"add", "sync", "error"} {
		t.Run(kind, func(t *testing.T) {
			var body any
			var eventType models.JobEventType
			switch kind {
			case "add":
				body = models.AddWorkerEmail{ID: mailbox, TransportGeneration: generation}
				eventType = models.JobEventType(models.WorkerEventTypeAddEmail)
			case "sync":
				body = &models.JobEventSyncState{WorkerID: &generation, EmailID: mailbox, State: models.SyncState{LastSyncedAt: &at, FolderCoverageObservedAt: &at, TransportGeneration: generation}}
				eventType = models.JobEventTypeSyncState
			case "error":
				body = models.EmailErrorEvent{EmailAccountID: mailbox.String(), TransportGeneration: generation, Operation: models.MailTransportSync}
				eventType = models.JobEventTypeEmailServerError
			}
			var envelope any = models.JobEvent{Type: eventType, Body: body}
			if kind == "add" {
				envelope = models.WorkerEvent{Type: models.WorkerEventTypeAddEmail, Body: body}
			}
			payload, err := c.Serialize(t.Context(), "transport", envelope)
			if err != nil {
				t.Fatal(err)
			}
			var legacy struct {
				Body struct {
					ID             uuid.UUID `json:"id"`
					EmailID        uuid.UUID `json:"email_id"`
					EmailAccountID string    `json:"email_account_id"`
				}
			}
			if err := c.Deserialize(t.Context(), "transport", payload, &legacy); err != nil {
				t.Fatal(err)
			}
			if legacy.Body.ID != mailbox && legacy.Body.EmailID != mailbox && legacy.Body.EmailAccountID != mailbox.String() {
				t.Fatal("old reader lost identity")
			}
			var old map[string]any
			if err := json.Unmarshal(payload, &old); err != nil {
				t.Fatal(err)
			}
			oldBody := old["body"].(map[string]any)
			delete(oldBody, "transport_generation")
			delete(oldBody, "operation")
			delete(oldBody, "worker_id")
			if state, ok := oldBody["state"].(map[string]any); ok {
				delete(state, "transport_generation")
				delete(state, "folder_coverage_observed_at")
			}
			payload, err = c.Serialize(t.Context(), "transport", old)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "add":
				var current struct{ Body models.AddWorkerEmail }
				if err := c.Deserialize(t.Context(), "transport", payload, &current); err != nil {
					t.Fatal(err)
				}
				if current.Body.TransportGeneration != uuid.Nil || current.Body.ID != mailbox {
					t.Fatal("old add manufactured proof")
				}
			case "sync":
				var current struct{ Body models.JobEventSyncState }
				if err := c.Deserialize(t.Context(), "transport", payload, &current); err != nil {
					t.Fatal(err)
				}
				if current.Body.WorkerID != nil || current.Body.State.TransportGeneration != uuid.Nil || current.Body.State.FolderCoverageObservedAt != nil || current.Body.State.LastSyncedAt == nil || !current.Body.State.LastSyncedAt.Equal(at) {
					t.Fatal("old sync changed checkpoint/proof")
				}
			case "error":
				var current struct{ Body models.EmailErrorEvent }
				if err := c.Deserialize(t.Context(), "transport", payload, &current); err != nil {
					t.Fatal(err)
				}
				if current.Body.TransportGeneration != uuid.Nil || current.Body.Operation.Known() || current.Body.EmailAccountID != mailbox.String() {
					t.Fatal("old error manufactured proof/operation")
				}
			}
		})
	}
}
