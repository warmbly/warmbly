package partner

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/repository"
)

type lockRepo struct {
	repository.PartnerMailboxRepository
	org, account uuid.UUID
}

func (r *lockRepo) PartnerMailboxLock(_ context.Context, orgID, accountID uuid.UUID) (string, error) {
	if orgID == r.org && accountID == r.account {
		return "Reseller", nil
	}
	return "", nil
}

// The lock is asked for within the caller's workspace, so another workspace's mailbox id yields nothing.
func TestMailboxLockIsReadWithinTheWorkspace(t *testing.T) {
	org, account := uuid.New(), uuid.New()
	s := NewService(Deps{})
	s.WireMailboxes(MailboxDeps{Repo: &lockRepo{org: org, account: account}})
	if name, err := s.MailboxLock(context.Background(), org, account); err != nil || name != "Reseller" {
		t.Fatalf("own workspace: %q %v", name, err)
	}
	if name, err := s.MailboxLock(context.Background(), uuid.New(), account); err != nil || name != "" {
		t.Fatalf("another workspace: lock %q err %v, want none", name, err)
	}
}
