package worker

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"

	"github.com/warmbly/warmbly/internal/pkg/emailverify"
)

// A mailbox that never loaded says nothing about the recipient.
func TestMailboxNotLoadedIsNotRecipientEvidence(t *testing.T) {
	if emailverify.NamesRecipient(errMailboxNotLoaded) {
		t.Fatalf("%q reads as a recipient refusal", errMailboxNotLoaded)
	}
}

func TestMissingMailboxRequestsReloadBeforeRedelivery(t *testing.T) {
	w := newLoadingWorker(&capturedEvents{})
	reloads := 0
	w.OnMailboxMissing = func() { reloads++ }
	ctx := context.WithValue(t.Context(), deliveryKey{}, delivery{attempt: 1, redelivers: true})
	if err := w.HandleSendEmail(ctx, models.SendEmail{TaskID: uuid.New(), EmailID: uuid.New()}); err == nil {
		t.Fatal("missing mailbox was not left for redelivery")
	}
	if reloads != 1 {
		t.Fatal("missing mailbox did not request a reload")
	}
}
