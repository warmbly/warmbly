package research

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

type researchContacts struct {
	contact.ContactService
	t       *testing.T
	updates int
}

func (c *researchContacts) GetDetail(context.Context, uuid.UUID, *uuid.UUID, uuid.UUID) (*models.ContactDetail, *errx.Error) {
	return &models.ContactDetail{Contact: models.Contact{Email: "a@b.co", FirstName: "Ann"}}, nil
}

func (c *researchContacts) Update(context.Context, string, string, uuid.UUID, *models.UpdateContact) (*models.Contact, *errx.Error) {
	c.updates++
	c.t.Fatal("research wrote a contact field")
	return nil, nil
}

type researchOrgs struct {
	organization.OrganizationService
}

func (researchOrgs) Get(context.Context, uuid.UUID) (*models.Organization, *errx.Error) {
	return nil, nil
}

type researchFeature struct{}

func (researchFeature) IsPaidOrganization(context.Context, uuid.UUID) (bool, *errx.Error) {
	return false, nil
}

type researchRepo struct{ repository.ResearchRepository }

func (researchRepo) UpdateRun(context.Context, *models.ContactResearchRun) error { return nil }

type researchCredits struct {
	credits.CreditService
	usage  credits.Usage
	reason string
}

func (researchCredits) Reserve(context.Context, uuid.UUID, models.Micro) error { return nil }

func (c *researchCredits) Charge(_ context.Context, _ uuid.UUID, u credits.Usage, reason, _ string) (models.Micro, error) {
	c.usage = u
	c.reason = reason
	return 1, nil
}

type researchProvider struct {
	generation.Provider
	t   *testing.T
	req generation.AgentRequest
}

func (p *researchProvider) ModelForTier(bool) string { return "m" }
func (p *researchProvider) IsLocal() bool            { return false }
func (p *researchProvider) Name() string             { return "test" }
func (p *researchProvider) Complete(context.Context, generation.CompletionRequest) (*generation.WritingResult, error) {
	p.t.Fatal("research called Complete")
	return nil, nil
}

func (p *researchProvider) RunAgent(ctx context.Context, req generation.AgentRequest) (*generation.AgentResult, error) {
	p.req = req
	var save generation.ToolDef
	names := map[string]bool{}
	for _, tool := range req.Tools {
		if tool.Risk != generation.RiskRead {
			p.t.Fatalf("tool %s risk %s", tool.Name, tool.Risk)
		}
		names[tool.Name] = true
		if tool.Name == "save_research" {
			save = tool
		}
	}
	for _, name := range []string{"search_web", "fetch_url", "load_skill", "save_research"} {
		if !names[name] {
			p.t.Fatalf("missing tool %s", name)
		}
	}
	if save.Handler == nil {
		p.t.Fatal("save_research has no handler")
	}
	if _, err := save.Handler(ctx, json.RawMessage(`{"nothing_found":true,"custom_field_updates":{"title":"CEO"}}`)); err != nil {
		p.t.Fatal(err)
	}
	return &generation.AgentResult{Text: "nothing public", InputTokens: 3, OutputTokens: 4, TokensUsed: 7}, nil
}

func TestExecuteSavesResearchOnTheRun(t *testing.T) {
	contacts := &researchContacts{t: t}
	provider := &researchProvider{t: t}
	ledger := &researchCredits{}
	reg := aitools.NewRegistry()
	noop := func(context.Context, aitools.Invocation, json.RawMessage) (string, error) { return `{}`, nil }
	for _, name := range []string{"search_web", "fetch_url", "load_skill"} {
		reg.Register(aitools.Tool{Name: name, Risk: generation.RiskRead, Handler: noop})
	}
	org := uuid.New()
	run := &models.ContactResearchRun{ID: uuid.New(), OrgID: org, ContactID: uuid.New()}
	s := &service{
		repo: providerRepo(), registry: reg, provider: provider, credits: ledger,
		feature: researchFeature{}, contacts: contacts, orgs: researchOrgs{},
	}
	got := s.execute(context.Background(), aitools.Invocation{OrgID: org, UserID: uuid.New()}, run)
	if provider.req.Approve != nil {
		t.Fatal("research set an approval hook")
	}
	if !got.Result.NothingFound || got.Result.CustomFieldUpdates["title"] != "CEO" {
		t.Fatalf("result = %+v", got.Result)
	}
	if contacts.updates != 0 {
		t.Fatalf("contact updates = %d", contacts.updates)
	}
	if ledger.reason != "research_run" || ledger.usage.Quantity != 0 || ledger.usage.InputTokens != 3 {
		t.Fatalf("charge reason %q usage %+v", ledger.reason, ledger.usage)
	}
}

func providerRepo() repository.ResearchRepository { return researchRepo{} }

type nosaveProvider struct {
	researchProvider
}

func (p *nosaveProvider) RunAgent(_ context.Context, req generation.AgentRequest) (*generation.AgentResult, error) {
	p.req = req
	return &generation.AgentResult{Text: "stopped", InputTokens: 1, OutputTokens: 1}, nil
}

func TestExecuteDoesNotChargeWithoutASave(t *testing.T) {
	provider := &nosaveProvider{researchProvider: researchProvider{t: t}}
	ledger := &researchCredits{}
	reg := aitools.NewRegistry()
	noop := func(context.Context, aitools.Invocation, json.RawMessage) (string, error) { return `{}`, nil }
	for _, name := range []string{"search_web", "fetch_url", "load_skill"} {
		reg.Register(aitools.Tool{Name: name, Risk: generation.RiskRead, Handler: noop})
	}
	org := uuid.New()
	run := &models.ContactResearchRun{ID: uuid.New(), OrgID: org, ContactID: uuid.New()}
	s := &service{
		repo: providerRepo(), registry: reg, provider: provider, credits: ledger,
		feature: researchFeature{}, contacts: &researchContacts{t: t}, orgs: researchOrgs{},
	}
	got := s.execute(context.Background(), aitools.Invocation{OrgID: org}, run)
	if got.Status != models.ResearchFailed || ledger.reason != "" {
		t.Fatalf("status %s charge %q", got.Status, ledger.reason)
	}
}
