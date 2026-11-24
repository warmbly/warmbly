package advisor

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

type narrateProvider struct {
	generation.Provider
	t     *testing.T
	calls int
}

func (p *narrateProvider) Complete(context.Context, generation.CompletionRequest) (*generation.WritingResult, error) {
	p.calls++
	return &generation.WritingResult{Text: `{"title":"Mailbox cap is high","detail":"The cap is 80.","remedy":"Set the cap to 50."}`, Model: "m"}, nil
}

func (p *narrateProvider) RunAgent(context.Context, generation.AgentRequest) (*generation.AgentResult, error) {
	p.t.Fatal("narration called RunAgent")
	return nil, nil
}

func (p *narrateProvider) ModelForTier(bool) string { return "m" }
func (p *narrateProvider) IsLocal() bool            { return false }

func TestNarrateDoesNotUseTheAgent(t *testing.T) {
	p := &narrateProvider{t: t}
	n := NewNarrator(p, nil, nil, nil)
	f := &models.AdvisorFinding{DetectorKey: "copy_spam_phrases", Evidence: []byte(`{}`)}
	if !n.Narrate(context.Background(), uuid.New(), f) {
		t.Fatal("narration did not replace the copy")
	}
	if p.calls != 1 {
		t.Fatalf("Complete calls = %d", p.calls)
	}
	if f.Title == "" || f.Detail == "" || f.Remedy == "" || !f.Narrated {
		t.Fatalf("finding = %+v", f)
	}
}

type advisorRepo struct {
	repository.AdvisorRepository
	finding *models.AdvisorFinding
}

func (r advisorRepo) GetFinding(context.Context, uuid.UUID, uuid.UUID) (*models.AdvisorFinding, error) {
	return r.finding, nil
}

type fixAgent struct {
	req generation.AgentRequest
}

func (a *fixAgent) RunAgent(ctx context.Context, req generation.AgentRequest) (*generation.AgentResult, error) {
	a.req = req
	if req.PreIteration != nil {
		if err := req.PreIteration(ctx, 1); err != nil {
			return &generation.AgentResult{}, nil
		}
	}
	return &generation.AgentResult{Text: "rewrote the subject", InputTokens: 2, OutputTokens: 3, TokensUsed: 5}, nil
}

func (a *fixAgent) ModelForTier(bool) string { return "m" }
func (a *fixAgent) IsLocal() bool            { return false }

type fixToolList struct{}

func (fixToolList) ToolDefsByName(aitools.Invocation, ...string) []generation.ToolDef {
	return []generation.ToolDef{{Name: "update_campaign_step", Risk: generation.RiskWrite}}
}

type fixCredits struct {
	reason     string
	reserveErr error
}

func (c *fixCredits) Reserve(context.Context, uuid.UUID, models.Micro) error { return c.reserveErr }

func (c *fixCredits) Charge(_ context.Context, _ uuid.UUID, _ credits.Usage, reason, _ string) (models.Micro, error) {
	c.reason = reason
	return 1, nil
}

func TestFixWithAgentChargesTheRun(t *testing.T) {
	org, id := uuid.New(), uuid.New()
	agent := &fixAgent{}
	ledger := &fixCredits{}
	s := &service{
		repo: advisorRepo{finding: &models.AdvisorFinding{
			ID: id, OrganizationID: org, DetectorKey: "copy_spam_phrases", Category: models.AdvisorCategoryCopy,
		}},
		agent:    agent,
		toolList: fixToolList{},
		credits:  ledger,
	}
	if _, xerr := s.FixWithAgent(context.Background(), aitools.Invocation{OrgID: org, UserID: uuid.New()}, id); xerr != nil {
		t.Fatal(xerr)
	}
	if agent.req.Approve != nil {
		t.Fatal("advisor fix set an approval hook")
	}
	if agent.req.Tools == nil {
		t.Fatal("advisor fix was not an agent run")
	}
	if ledger.reason != "advisor_agent_fix" {
		t.Fatalf("charge reason %q", ledger.reason)
	}
	if agent.req.MaxIterations != agentMaxIterations {
		t.Fatalf("iterations = %d", agent.req.MaxIterations)
	}
}

func TestFixWithAgentAllowanceKeepsItsSentence(t *testing.T) {
	org, id := uuid.New(), uuid.New()
	s := &service{
		repo: advisorRepo{finding: &models.AdvisorFinding{
			ID: id, OrganizationID: org, DetectorKey: "copy_spam_phrases", Category: models.AdvisorCategoryCopy,
		}},
		agent: &fixAgent{}, toolList: fixToolList{},
		credits: &fixCredits{reserveErr: credits.ErrAIAllowanceUsed},
	}
	_, xerr := s.FixWithAgent(context.Background(), aitools.Invocation{OrgID: org, UserID: uuid.New()}, id)
	if xerr == nil || xerr.Identifier != "ai_allowance_used" || xerr.Code != errx.PaymentRequired {
		t.Fatalf("err = %+v", xerr)
	}
}

func TestFixWithAgentCapIsNotAPaymentError(t *testing.T) {
	org, id := uuid.New(), uuid.New()
	s := &service{
		repo: advisorRepo{finding: &models.AdvisorFinding{
			ID: id, OrganizationID: org, DetectorKey: "copy_spam_phrases", Category: models.AdvisorCategoryCopy,
		}},
		agent: &fixAgent{}, toolList: fixToolList{},
		credits: &fixCredits{reserveErr: credits.ErrCapExceeded},
	}
	_, xerr := s.FixWithAgent(context.Background(), aitools.Invocation{OrgID: org, UserID: uuid.New()}, id)
	if xerr == nil || xerr.Code != errx.TooManyRequests {
		t.Fatalf("err = %+v", xerr)
	}
}
