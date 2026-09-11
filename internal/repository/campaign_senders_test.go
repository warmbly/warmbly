package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type stubSenderSource struct {
	explicit []CampaignSenderAccount
	tagged   []models.Email
	all      []models.Email
	allCalls int
}

func (s *stubSenderSource) GetByCampaignSenders(context.Context, AccountScope, uuid.UUID) ([]CampaignSenderAccount, *errx.Error) {
	return s.explicit, nil
}

func (s *stubSenderSource) GetByTags(context.Context, AccountScope, []string) ([]models.Email, *errx.Error) {
	return s.tagged, nil
}

func (s *stubSenderSource) GetAllActiveInScope(context.Context, AccountScope) ([]models.Email, *errx.Error) {
	s.allCalls++
	return s.all, nil
}

func testCampaign(strategy string) *models.Campaign {
	org := uuid.New()
	return &models.Campaign{ID: uuid.New(), OrganizationID: &org, SenderStrategy: strategy}
}

// The incident this guards: a campaign that named three mailboxes by hand lost
// them (revoked, or the pool replaced with nothing) and silently carried on
// sending from every mailbox in the workspace.
func TestResolveCampaignSenderPoolExplicitDoesNotWidenToEveryMailbox(t *testing.T) {
	src := &stubSenderSource{all: []models.Email{{ID: uuid.New()}, {ID: uuid.New()}}}

	pool, err := ResolveCampaignSenderPool(context.Background(), src, testCampaign("explicit"))
	if err != nil {
		t.Fatalf("ResolveCampaignSenderPool: %v", err)
	}
	if len(pool.Accounts) != 0 {
		t.Errorf("an empty explicit pool resolved to %d mailboxes, want none", len(pool.Accounts))
	}
	if src.allCalls != 0 {
		t.Error("the explicit strategy fell back to every active mailbox in the workspace")
	}
}

// Everything the dashboard writes is sender_strategy='tags', so the fallback
// that campaigns have always relied on has to be untouched.
func TestResolveCampaignSenderPoolTagsStrategyStillFallsBackToAll(t *testing.T) {
	all := []models.Email{{ID: uuid.New()}, {ID: uuid.New()}}
	src := &stubSenderSource{all: all}

	pool, err := ResolveCampaignSenderPool(context.Background(), src, testCampaign("tags"))
	if err != nil {
		t.Fatalf("ResolveCampaignSenderPool: %v", err)
	}
	if len(pool.Accounts) != len(all) {
		t.Errorf("pool has %d mailboxes, want the %d active ones", len(pool.Accounts), len(all))
	}
}

// An explicit pool that still has its mailboxes resolves to exactly those.
func TestResolveCampaignSenderPoolExplicitUsesItsOwnMailboxes(t *testing.T) {
	picked := models.Email{ID: uuid.New()}
	src := &stubSenderSource{
		explicit: []CampaignSenderAccount{{Account: picked}},
		all:      []models.Email{{ID: uuid.New()}, {ID: uuid.New()}},
	}

	pool, err := ResolveCampaignSenderPool(context.Background(), src, testCampaign("explicit"))
	if err != nil {
		t.Fatalf("ResolveCampaignSenderPool: %v", err)
	}
	if len(pool.Accounts) != 1 || pool.Accounts[0].ID != picked.ID {
		t.Errorf("pool = %+v, want only the picked mailbox", pool.Accounts)
	}
	if src.allCalls != 0 {
		t.Error("a populated explicit pool still consulted every active mailbox")
	}
}
