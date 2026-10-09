package worker

import (
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/models"
)

func TestCommandKeysGroupOldSendKeysWithMailboxMutations(t *testing.T) {
	w := &WorkerService{Codec: codec.NewJSON()}
	checkCommandKeys(t, w)
	for _, body := range []string{`{"type":"SEND_EMAIL","body":{}}`, `{"type":"REMOVE_EMAIL","body":{"email_id":"invalid"}}`, "invalid"} {
		if _, err := w.ResolveCommandKey(t.Context(), eventbus.Message{Payload: []byte(body)}); err == nil {
			t.Fatal("malformed command was assigned an unsafe fallback key")
		}
	}
}

func checkCommandKeys(t *testing.T, w *WorkerService) {
	t.Helper()
	mailbox, task, process := uuid.New(), uuid.New(), uuid.New()
	for _, test := range []struct {
		kind models.WorkerEventType
		body any
		want uuid.UUID
	}{
		{models.WorkerEventTypeSendEmail, &models.SendEmail{EmailID: mailbox, TaskID: task}, mailbox},
		{models.WorkerEventTypeWarmupAction, &models.WarmupEmailAction{EmailID: mailbox}, mailbox},
		{models.WorkerEventTypeMessageSeen, &models.MessageSeenAction{EmailID: mailbox}, mailbox},
		{models.WorkerEventTypeMessageFolder, &models.MessageFolderAction{EmailID: mailbox}, mailbox},
		{models.WorkerEventTypeMailboxIdentity, models.EventWorkerMailboxIdentity{EmailID: mailbox}, mailbox},
		{models.WorkerEventTypeAddEmail, &models.AddWorkerEmail{ID: mailbox}, mailbox},
		{models.WorkerEventTypeRemoveEmail, &models.RemoveWorkerEmail{EmailID: mailbox.String()}, mailbox},
		{models.WorkerEventTypeEmailValidation, models.EventWorkerEmailValidation{OrgID: mailbox, ProcessID: process}, process},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			payload, err := w.Codec.Serialize(t.Context(), "commands", models.WorkerEvent{Type: test.kind, Body: test.body})
			if err != nil {
				t.Fatal(err)
			}
			key, err := w.ResolveCommandKey(t.Context(), eventbus.Message{Topic: "commands", Key: task.String(), Payload: payload})
			if err != nil || key != test.want.String() {
				t.Fatalf("key=%q want=%q err=%v", key, test.want, err)
			}
		})
	}
}

func TestCommandConcurrencyDefaultsAndBoundedOverride(t *testing.T) {
	for _, test := range []struct {
		env  string
		want int
	}{{"", 8}, {"invalid", 8}, {"0", 8}, {"65", 8}, {"1", 1}, {"16", 16}, {"64", 64}} {
		t.Setenv("WORKER_COMMAND_CONCURRENCY", test.env)
		if got := CommandConcurrency(); got != test.want {
			t.Fatalf("env=%q got=%d want=%d", test.env, got, test.want)
		}
	}
}
