package inboxagent

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

type inboxGate struct{ feature.FeatureGateService }

func (inboxGate) CanUseInboxAgent(context.Context, uuid.UUID) (bool, *errx.Error) {
	return true, nil
}

type inboxOrgs struct{}

func (inboxOrgs) GetByID(context.Context, uuid.UUID) (*models.Organization, error) {
	return &models.Organization{InboxAgentEnabled: true}, nil
}

type inboxThreads struct{}

func (inboxThreads) GroundingByThread(context.Context, uuid.UUID, string, int) ([]models.MessageGrounding, error) {
	return []models.MessageGrounding{{
		FromAddr: []string{"a@b.co"},
		Subject:  "Thursday",
		BodyText: "Can we talk about the proposal tomorrow?",
	}}, nil
}

type inboxDrafts struct {
	repository.AIDraftRepository
	saved   *models.AIThreadDraft
	created bool
}

func (r *inboxDrafts) HasActiveDraft(context.Context, uuid.UUID, string, *uuid.UUID) (bool, error) {
	return false, nil
}

func (r *inboxDrafts) CreateDraft(_ context.Context, d *models.AIThreadDraft) error {
	r.saved = d
	r.created = true
	return nil
}

type inboxCredits struct {
	credits.CreditService
	reserves int
	estimate models.Micro
	reason   string
	drafts   *inboxDrafts
}

func (c *inboxCredits) Reserve(_ context.Context, _ uuid.UUID, estimate models.Micro) error {
	c.reserves++
	c.estimate = estimate
	return nil
}

func (c *inboxCredits) Charge(_ context.Context, _ uuid.UUID, _ credits.Usage, reason, _ string) (models.Micro, error) {
	if !c.drafts.created {
		return 0, errx.New(errx.Internal, "charged before the draft row existed")
	}
	c.reason = reason
	return 1, nil
}

type inboxProvider struct {
	generation.Provider
	completes int
	system    string
}

func (p *inboxProvider) Complete(_ context.Context, req generation.CompletionRequest) (*generation.WritingResult, error) {
	p.completes++
	p.system = req.System
	return &generation.WritingResult{Text: "Thursday works for me.", Model: "m", InputTokens: 5, OutputTokens: 6}, nil
}

func (p *inboxProvider) RunAgent(context.Context, generation.AgentRequest) (*generation.AgentResult, error) {
	panic("RunAgent")
}

func (p *inboxProvider) ModelForTier(bool) string { return "m" }
func (p *inboxProvider) IsLocal() bool            { return false }

type inboxPub struct{ n int }

func (p *inboxPub) PublishAIDraftReady(context.Context, uuid.UUID, uuid.UUID, string, string, string) {
	p.n++
}

func TestDraftUsesOneCompletionAndDoesNotSend(t *testing.T) {
	drafts := &inboxDrafts{}
	provider := &inboxProvider{}
	ledger := &inboxCredits{drafts: drafts}
	pub := &inboxPub{}
	s := &service{
		provider:  provider,
		credits:   ledger,
		feature:   inboxGate{},
		orgs:      inboxOrgs{},
		threads:   inboxThreads{},
		draftRepo: drafts,
		publisher: pub,
	}
	s.draft(context.Background(), models.InboxAgentReply{
		OrganizationID:  uuid.New(),
		EmailAccountID:  uuid.New(),
		SourceMessageID: uuid.New(),
		ThreadID:        "thread-1",
		Counterpart:     "a@b.co",
		Subject:         "Thursday",
		BodyText:        "Can we talk about the proposal tomorrow?",
	})
	if provider.completes != 1 {
		t.Fatalf("Complete calls = %d, want 1", provider.completes)
	}
	if provider.system == "" {
		t.Fatal("completion system prompt is empty")
	}
	if drafts.saved == nil || drafts.saved.Status != models.AIDraftPending {
		t.Fatalf("draft = %+v", drafts.saved)
	}
	if ledger.reserves != 1 || ledger.estimate != credits.EstimateInboxAgentThread {
		t.Fatalf("Reserve calls = %d estimate = %d", ledger.reserves, ledger.estimate)
	}
	if ledger.reason != "inbox_agent_draft" {
		t.Fatalf("charge reason %q", ledger.reason)
	}
	if pub.n != 1 {
		t.Fatalf("draft-ready events = %d, want 1", pub.n)
	}
}
