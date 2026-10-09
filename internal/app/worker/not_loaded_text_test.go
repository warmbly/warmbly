package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
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

func TestUnloadedMailboxPreparationResultMustPublishBeforeAcknowledgement(t *testing.T) {
	w := newLoadingWorker(&capturedEvents{})
	bus := &filingBus{err: errors.New("broker unavailable")}
	w.Bus, w.Codec = bus, codec.NewJSON()
	w.InitEvents()
	event := &models.WorkerEvent{Type: models.WorkerEventTypeSendEmail, Body: &models.SendEmail{TaskID: uuid.New(), EmailID: uuid.New()}}
	payload, err := w.Codec.Serialize(t.Context(), "commands", event)
	if err != nil {
		t.Fatal(err)
	}
	msg := eventbus.Message{Payload: payload, Attempt: 5, Redelivers: true}
	if err := w.Receive(t.Context(), msg); !errors.Is(err, bus.err) || len(bus.events) != 0 {
		t.Fatalf("unpublished failure was acknowledged: events=%v err=%v", bus.events, err)
	}
	bus.err = nil
	if err := w.Receive(t.Context(), msg); err != nil || len(bus.events) != 1 {
		t.Fatalf("preparation did not resolve: events=%v err=%v", bus.events, err)
	}
	raw, err := w.Codec.Serialize(t.Context(), "result", bus.events[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	var result models.SendEmailResult
	if err := w.Codec.Deserialize(t.Context(), "result", raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Success || result.Error == nil || result.Error.Failure == nil || result.Error.Failure.Stage != "prepare" || result.Error.Failure.Protocol != "internal" {
		t.Fatalf("unattempted send misrepresented as provider evidence: %+v", result)
	}
}
