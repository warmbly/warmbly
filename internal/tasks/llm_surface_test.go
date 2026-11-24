package tasks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

type surfaceProvider struct {
	generation.Provider
	agents      int
	completions int
	lastAgent   generation.AgentRequest
	lastWrite   generation.CompletionRequest
	agentText   string
	writeText   string
	failAgent   bool
	failWrite   bool
}

func (p *surfaceProvider) RunAgent(_ context.Context, req generation.AgentRequest) (*generation.AgentResult, error) {
	if p.failAgent {
		panic("RunAgent")
	}
	p.agents++
	p.lastAgent = req
	text := p.agentText
	if text == "" {
		text = "done"
	}
	return &generation.AgentResult{Text: text, InputTokens: 2, OutputTokens: 3, TokensUsed: 5}, nil
}

func (p *surfaceProvider) Complete(_ context.Context, req generation.CompletionRequest) (*generation.WritingResult, error) {
	if p.failWrite {
		panic("Complete")
	}
	p.completions++
	p.lastWrite = req
	text := p.writeText
	if text == "" {
		text = "no"
	}
	return &generation.WritingResult{Text: text, Model: req.Model, InputTokens: 4, OutputTokens: 5, TokensUsed: 9}, nil
}

func (p *surfaceProvider) ModelForTier(bool) string { return "m" }
func (p *surfaceProvider) IsLocal() bool            { return false }
func (p *surfaceProvider) Name() string             { return "test" }

type surfaceCredits struct {
	credits.CreditService
	reasons []string
}

func (c *surfaceCredits) Reserve(context.Context, uuid.UUID, models.Micro) error { return nil }
func (c *surfaceCredits) Charge(_ context.Context, _ uuid.UUID, _ credits.Usage, reason, _ string) (models.Micro, error) {
	c.reasons = append(c.reasons, reason)
	return 1, nil
}

func (c *surfaceCredits) last() string {
	if len(c.reasons) == 0 {
		return ""
	}
	return c.reasons[len(c.reasons)-1]
}

func surfaceCampaign() (*models.Campaign, *models.Contact, uuid.UUID) {
	org := uuid.New()
	return &models.Campaign{ID: uuid.New(), OrganizationID: &org, Name: "Outbound"},
		&models.Contact{ID: uuid.New(), Email: "a@b.co", FirstName: "Ann"},
		uuid.New()
}

func TestCampaignSwitchValueModeSkipsTheProvider(t *testing.T) {
	p := &surfaceProvider{}
	c := &surfaceCredits{}
	s := &tasksService{aiProvider: p, aiCredits: c}
	campaign, contact, step := surfaceCampaign()
	err := s.execSequenceSwitchStep(context.Background(), campaign, contact, step, &models.ActionConfig{
		SwitchOn: "value", SwitchCases: []string{"yes"}, SwitchValue: "zzz",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.agents != 0 || p.completions != 0 {
		t.Fatalf("provider calls agent=%d complete=%d", p.agents, p.completions)
	}
}

func TestCampaignSwitchAIModeIsOneCompletion(t *testing.T) {
	p := &surfaceProvider{writeText: "no"}
	c := &surfaceCredits{}
	s := &tasksService{aiProvider: p, aiCredits: c}
	campaign, contact, step := surfaceCampaign()
	err := s.execSequenceSwitchStep(context.Background(), campaign, contact, step, &models.ActionConfig{
		SwitchOn: "ai", SwitchCases: []string{"yes"}, AIInstruction: "pick a case",
		AINoEngagement: true, AINoReplies: true,
	})
	if err == nil {
		t.Fatal("unmatched case returned nil")
	}
	if p.completions != 1 || p.agents != 0 {
		t.Fatalf("provider calls agent=%d complete=%d", p.agents, p.completions)
	}
	if p.lastWrite.Temperature == nil || *p.lastWrite.Temperature != 0 {
		t.Fatalf("temperature = %v", p.lastWrite.Temperature)
	}
	if c.last() != "campaign_ai" {
		t.Fatalf("charge reason %q", c.last())
	}
}

func TestCampaignAIStepRunsToolsWithoutApproval(t *testing.T) {
	p := &surfaceProvider{agentText: "unsubscribed"}
	c := &surfaceCredits{}
	s := &tasksService{aiProvider: p, aiCredits: c}
	campaign, contact, step := surfaceCampaign()
	err := s.execSequenceAIAgentStep(context.Background(), campaign, contact, step, &models.ActionConfig{
		AIInstruction: "unsubscribe them", AIAllowedActions: []string{"unsubscribe"},
		AINoEngagement: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.agents != 1 || p.completions != 0 {
		t.Fatalf("provider calls agent=%d complete=%d", p.agents, p.completions)
	}
	if p.lastAgent.Approve != nil {
		t.Fatal("agent step set an approval hook")
	}
	if len(p.lastAgent.Tools) == 0 {
		t.Fatal("agent step had no tools")
	}
	if c.last() != "campaign_ai_agent" {
		t.Fatalf("charge reason %q", c.last())
	}
}

func TestAIVariableInstantModeIsOneCompletion(t *testing.T) {
	p := &surfaceProvider{failAgent: true, writeText: "a concrete line"}
	c := &surfaceCredits{}
	s := &tasksService{aiProvider: p, aiCredits: c}
	_, contact, _ := surfaceCampaign()
	text, err := s.generateAIVariable(context.Background(), uuid.New(), contact, aiVarConfig{Prompt: "one line"}, "one line", "", "", "k")
	if err != nil {
		t.Fatal(err)
	}
	if text != "a concrete line" || p.completions != 1 {
		t.Fatalf("text %q completions %d", text, p.completions)
	}
}

// toolSource stands in for the tool registry. A tasks test cannot import aitools: that package imports tasks.
type toolSource struct{ tools []generation.ToolDef }

func (s toolSource) WebResearchTools(uuid.UUID) []generation.ToolDef { return s.tools }

func TestAIVariableResearchModeUsesTheAgent(t *testing.T) {
	p := &surfaceProvider{failWrite: true, agentText: "a researched line"}
	c := &surfaceCredits{}
	noop := func(context.Context, json.RawMessage) (string, error) { return `{}`, nil }
	s := &tasksService{aiProvider: p, aiCredits: c, aiTools: toolSource{tools: []generation.ToolDef{
		{Name: "search_web", Risk: generation.RiskRead, Handler: noop},
		{Name: "fetch_url", Risk: generation.RiskRead, Handler: noop},
	}}}
	_, contact, _ := surfaceCampaign()
	text, err := s.generateAIVariable(context.Background(), uuid.New(), contact, aiVarConfig{Mode: "research", Prompt: "find one fact"}, "find one fact", "", "", "k2")
	if err != nil {
		t.Fatal(err)
	}
	if text != "a researched line" || p.agents != 1 {
		t.Fatalf("text %q agents %d", text, p.agents)
	}
	if c.last() != "campaign_ai_var" {
		t.Fatalf("charge reason %q", c.last())
	}
}

func TestAIVariableResearchWithoutToolsFallsBack(t *testing.T) {
	p := &surfaceProvider{failAgent: true, writeText: "a safe line"}
	c := &surfaceCredits{}
	s := &tasksService{aiProvider: p, aiCredits: c, aiTools: toolSource{}}
	_, contact, _ := surfaceCampaign()
	text, err := s.generateAIVariable(context.Background(), uuid.New(), contact, aiVarConfig{Mode: "research", Prompt: "find one fact"}, "find one fact", "", "", "k3")
	if err != nil {
		t.Fatal(err)
	}
	if text != "a safe line" || p.completions != 1 {
		t.Fatalf("text %q completions %d", text, p.completions)
	}
}
