package integration

import (
	"context"
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
}

func (p *surfaceProvider) RunAgent(_ context.Context, req generation.AgentRequest) (*generation.AgentResult, error) {
	p.agents++
	p.lastAgent = req
	text := p.agentText
	if text == "" {
		text = "did the step"
	}
	return &generation.AgentResult{Text: text, InputTokens: 2, OutputTokens: 3, TokensUsed: 5}, nil
}

func (p *surfaceProvider) Complete(_ context.Context, req generation.CompletionRequest) (*generation.WritingResult, error) {
	p.completions++
	p.lastWrite = req
	text := p.writeText
	if text == "" {
		text = "hello"
	}
	return &generation.WritingResult{Text: text, Model: req.Model, InputTokens: 1, OutputTokens: 2, TokensUsed: 3}, nil
}

func (p *surfaceProvider) ModelForTier(bool) string { return "m" }
func (p *surfaceProvider) IsLocal() bool            { return false }
func (p *surfaceProvider) Name() string             { return "test" }

type surfaceCredits struct{ credits.CreditService }

func (surfaceCredits) Reserve(context.Context, uuid.UUID, models.Micro) error { return nil }
func (surfaceCredits) Charge(context.Context, uuid.UUID, credits.Usage, string, string) (models.Micro, error) {
	return 1, nil
}

func TestAutomationValueSwitchSkipsTheProvider(t *testing.T) {
	p := &surfaceProvider{}
	s := &service{aiProvider: p, credits: surfaceCredits{}}
	n := models.AutomationNode{
		ID:     "n1",
		Action: models.IntegrationActionAISwitch,
		Config: []byte(`{"switch_on":"value","switch_value":"zzz","cases":["yes","no"]}`),
	}
	err := s.execAIAction(context.Background(), models.Automation{ID: uuid.New(), OrganizationID: uuid.New()}, n, map[string]any{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.agents != 0 || p.completions != 0 {
		t.Fatalf("provider calls agent=%d complete=%d", p.agents, p.completions)
	}
}

func TestAutomationGenerateModeIsOneCompletion(t *testing.T) {
	p := &surfaceProvider{writeText: "a short line"}
	s := &service{aiProvider: p, credits: surfaceCredits{}}
	n := models.AutomationNode{
		ID:     "n2",
		Action: models.IntegrationActionAIStep,
		Config: []byte(`{"instruction":"write one line"}`),
	}
	err := s.execAIAction(context.Background(), models.Automation{ID: uuid.New(), OrganizationID: uuid.New()}, n, map[string]any{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.completions != 1 || p.agents != 0 {
		t.Fatalf("provider calls agent=%d complete=%d", p.agents, p.completions)
	}
}

func TestAutomationAgentHasNoApprovalHook(t *testing.T) {
	p := &surfaceProvider{agentText: "unsubscribed"}
	s := &service{aiProvider: p, credits: surfaceCredits{}}
	data := map[string]any{}
	err := s.execAIAgentStep(context.Background(), models.Automation{ID: uuid.New(), OrganizationID: uuid.New()}, models.AutomationNode{ID: "n3"}, aiActionConfig{
		Allowlist: []string{string(models.IntegrationActionUnsubscribe)},
	}, "unsubscribe if they asked", data, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.agents != 1 || p.completions != 0 {
		t.Fatalf("provider calls agent=%d complete=%d", p.agents, p.completions)
	}
	if p.lastAgent.Approve != nil {
		t.Fatal("automation agent set an approval hook")
	}
	if data["ai_agent"] != "unsubscribed" {
		t.Fatalf("ai_agent = %#v", data["ai_agent"])
	}
}

func TestAskAIIsOneYesNoCompletion(t *testing.T) {
	p := &surfaceProvider{writeText: "YES"}
	s := &service{aiProvider: p, credits: surfaceCredits{}}
	n := models.AutomationNode{ID: "n4", Condition: &models.AutomationCondition{Prompt: "Did they agree?"}}
	ok, err := s.evalAICondition(context.Background(), models.Automation{ID: uuid.New(), OrganizationID: uuid.New()}, n, map[string]any{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("YES did not take the true branch")
	}
	if p.completions != 1 || p.agents != 0 {
		t.Fatalf("provider calls agent=%d complete=%d", p.agents, p.completions)
	}
	if p.lastWrite.MaxTokens != 8 || p.lastWrite.Temperature == nil || *p.lastWrite.Temperature != 0 {
		t.Fatalf("completion max=%d temp=%v", p.lastWrite.MaxTokens, p.lastWrite.Temperature)
	}
}
