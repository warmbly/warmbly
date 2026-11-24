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
	deleted bool
}

func (r *inboxDrafts) HasActiveDraft(context.Context, uuid.UUID, string, *uuid.UUID) (bool, error) {
	return false, nil
}

func (r *inboxDrafts) CreateDraft(_ context.Context, d *models.AIThreadDraft) error {
	r.saved = d
	r.created = true
	return nil
}

func (r *inboxDrafts) DeleteDraft(context.Context, uuid.UUID) error {
	r.deleted = true
	r.saved = nil
	return nil
}

type inboxCredits struct {
	credits.CreditService
	reserves   int
	estimate   models.Micro
	reason     string
	reserveErr error
	chargeErr  error
	drafts     *inboxDrafts
}

func (c *inboxCredits) Reserve(_ context.Context, _ uuid.UUID, estimate models.Micro) error {
	c.reserves++
	c.estimate = estimate
	return c.reserveErr
}

func (c *inboxCredits) Charge(_ context.Context, _ uuid.UUID, _ credits.Usage, reason, _ string) (models.Micro, error) {
	if !c.drafts.created {
		return 0, errx.New(errx.Internal, "charged before the draft row existed")
	}
	c.reason = reason
	if c.chargeErr != nil {
		return 0, c.chargeErr
	}
	return 1, nil
}

type inboxProvider struct {
	generation.Provider
	completes int
	system    string
	fail      error
	text      string
}

func (p *inboxProvider) Complete(_ context.Context, req generation.CompletionRequest) (*generation.WritingResult, error) {
	p.completes++
	p.system = req.System
	if p.fail != nil {
		return nil, p.fail
	}
	text := p.text
	if text == "" {
		text = "Thursday works for me."
	}
	return &generation.WritingResult{Text: text, Model: "m", InputTokens: 5, OutputTokens: 6}, nil
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

func inboxService(p *inboxProvider, drafts *inboxDrafts, ledger *inboxCredits, pub *inboxPub) *service {
	return &service{
		provider: p, credits: ledger, feature: inboxGate{}, orgs: inboxOrgs{},
		threads: inboxThreads{}, draftRepo: drafts, publisher: pub,
	}
}

func TestDraftGateSkipsWithoutACall(t *testing.T) {
	p := &inboxProvider{}
	drafts := &inboxDrafts{}
	ledger := &inboxCredits{drafts: drafts}
	s := inboxService(p, drafts, ledger, &inboxPub{})
	s.gate = denyGate{}
	s.draft(context.Background(), models.InboxAgentReply{
		OrganizationID: uuid.New(), ThreadID: "thread-1", Counterpart: "a@b.co",
		BodyText: "Can we talk about the proposal tomorrow?", InReplyTo: "<m@x>",
	})
	if p.completes != 0 || drafts.created || ledger.reserves != 0 {
		t.Fatalf("completes %d created %v reserves %d", p.completes, drafts.created, ledger.reserves)
	}
}

type denyGate struct{}

func (denyGate) ShouldDraft(context.Context, uuid.UUID, string) (bool, string) {
	return false, "the reply closed the conversation: not_interested"
}

func TestShortReplySkipsTheDraft(t *testing.T) {
	p := &inboxProvider{}
	drafts := &inboxDrafts{}
	ledger := &inboxCredits{drafts: drafts}
	s := inboxService(p, drafts, ledger, &inboxPub{})
	s.draft(context.Background(), models.InboxAgentReply{
		OrganizationID: uuid.New(), ThreadID: "thread-1", Counterpart: "a@b.co", BodyText: "Thanks",
	})
	if p.completes != 0 || drafts.created {
		t.Fatalf("completes %d created %v", p.completes, drafts.created)
	}
}

func TestLowBalanceSkipsTheDraft(t *testing.T) {
	p := &inboxProvider{}
	drafts := &inboxDrafts{}
	ledger := &inboxCredits{drafts: drafts, reserveErr: credits.ErrInsufficientCredits}
	s := inboxService(p, drafts, ledger, &inboxPub{})
	s.draft(context.Background(), models.InboxAgentReply{
		OrganizationID: uuid.New(), ThreadID: "thread-1", Counterpart: "a@b.co",
		BodyText: "Can we talk about the proposal tomorrow?",
	})
	if p.completes != 0 || drafts.created || ledger.reserves != 1 {
		t.Fatalf("completes %d created %v reserves %d", p.completes, drafts.created, ledger.reserves)
	}
}

func TestDraftChargeFailureDeletesTheRow(t *testing.T) {
	p := &inboxProvider{}
	drafts := &inboxDrafts{}
	ledger := &inboxCredits{drafts: drafts, chargeErr: credits.ErrInsufficientCredits}
	pub := &inboxPub{}
	s := inboxService(p, drafts, ledger, pub)
	s.draft(context.Background(), models.InboxAgentReply{
		OrganizationID: uuid.New(), ThreadID: "thread-1", Counterpart: "a@b.co",
		BodyText: "Can we talk about the proposal tomorrow?",
	})
	if !drafts.deleted || pub.n != 0 || drafts.saved != nil {
		t.Fatalf("deleted %v published %d saved %+v", drafts.deleted, pub.n, drafts.saved)
	}
}

func TestDraftProviderErrorWritesNothing(t *testing.T) {
	p := &inboxProvider{fail: credits.ErrInsufficientCredits}
	drafts := &inboxDrafts{}
	ledger := &inboxCredits{drafts: drafts}
	s := inboxService(p, drafts, ledger, &inboxPub{})
	s.draft(context.Background(), models.InboxAgentReply{
		OrganizationID: uuid.New(), ThreadID: "thread-1", Counterpart: "a@b.co",
		BodyText: "Can we talk about the proposal tomorrow?",
	})
	if drafts.created || ledger.reason != "" {
		t.Fatalf("created %v reason %q", drafts.created, ledger.reason)
	}
}
