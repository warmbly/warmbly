package email

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/models"
)

type identityPublisher struct {
	events.Publisher
	sent []models.EventWorkerMailboxIdentity
	to   []uuid.UUID
}

func (p *identityPublisher) PublishMailboxIdentity(_ context.Context, workerID uuid.UUID, body models.EventWorkerMailboxIdentity) error {
	p.sent = append(p.sent, body)
	p.to = append(p.to, workerID)
	return nil
}

// A mailbox with nowhere to run has no machine that may talk to the provider,
// and the control plane must not stand in for it. The refusal is a 503 naming
// the machine, not a silent local answer.
func TestReadSendIdentityRefusesAnUnplacedMailbox(t *testing.T) {
	pub := &identityPublisher{}
	s := &emailService{publisher: pub}

	_, xerr := s.readSendIdentity(context.Background(), &models.Email{ID: uuid.New()}, false)

	if xerr == nil {
		t.Fatal("an unplaced mailbox must not be read")
	}
	if xerr.Identifier != errx.ErrEmailIdentityUnavailable.Identifier {
		t.Errorf("unexpected error: %s", xerr.Identifier)
	}
	if len(pub.sent) != 0 {
		t.Errorf("published %d events for a mailbox with no worker", len(pub.sent))
	}
}

// Without a bus there is no worker to ask, and the answer is the same refusal
// rather than a fall back to calling the provider from here.
func TestReadSendIdentityRefusesWithoutABus(t *testing.T) {
	s := &emailService{}
	worker := uuid.New()

	_, xerr := s.readSendIdentity(context.Background(), &models.Email{ID: uuid.New(), WorkerID: &worker}, false)

	if xerr == nil || xerr.Identifier != errx.ErrEmailIdentityUnavailable.Identifier {
		t.Fatalf("expected the unavailable refusal, got %v", xerr)
	}
}
