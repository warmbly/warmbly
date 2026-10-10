package scheduler

import (
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func senderCandidate(id uuid.UUID) AccountCandidate {
	return AccountCandidate{Account: models.Email{ID: id}}
}

func TestWithoutSenderExcludesThePreviousMailbox(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	candidates := []AccountCandidate{senderCandidate(a), senderCandidate(b), senderCandidate(c)}

	filtered := withoutSender(candidates, b)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 candidates after excluding the previous sender, got %d", len(filtered))
	}
	for _, candidate := range filtered {
		if candidate.Account.ID == b {
			t.Fatal("the previous sender must not survive the exclusion")
		}
	}
}

func TestWithoutSenderFallsBackWhenNothingElseIsEligible(t *testing.T) {
	a := uuid.New()
	candidates := []AccountCandidate{senderCandidate(a)}

	filtered := withoutSender(candidates, a)
	if len(filtered) != 1 || filtered[0].Account.ID != a {
		t.Fatal("a sole eligible mailbox must still send rather than strand the sequence")
	}
}

func TestResolvePreviousSenderPrefersTheDeliveredMailbox(t *testing.T) {
	assigned, delivered := uuid.New(), uuid.New()

	if resolvePreviousSender(false, &assigned, &delivered) != nil {
		t.Fatal("rotation off must not exclude any mailbox")
	}
	if got := resolvePreviousSender(true, &assigned, &delivered); got == nil || *got != delivered {
		t.Fatal("a delivered step's mailbox is what the next step must exclude")
	}
	if got := resolvePreviousSender(true, &assigned, nil); got == nil || *got != assigned {
		t.Fatal("without a delivered step the binding is the best exclusion available")
	}
	if resolvePreviousSender(true, nil, nil) != nil {
		t.Fatal("a new lead has nothing to exclude")
	}
}
