package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

type surfaceProvider struct {
	generation.Provider
	agents      int
	completions int
	lastAgent   generation.AgentRequest
	lastWrite   generation.CompletionRequest
	agentText   string
	writeText   string
	fail        error
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
	if p.fail != nil {
		return nil, p.fail
	}
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
	if p.lastAgent.MaxIterations != aiAgentMaxIterations {
		t.Fatalf("iterations = %d", p.lastAgent.MaxIterations)
	}
	if len(p.lastAgent.Tools) != 1 || p.lastAgent.Tools[0].Name != "unsubscribe" {
		t.Fatalf("tools = %+v", p.lastAgent.Tools)
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

func TestAutomationModesAreOneCompletion(t *testing.T) {
	cases := []struct {
		name   string
		action models.IntegrationAction
		config string
		reply  string
		key    string
		want   string
		temp   bool
	}{
		{"classify", models.IntegrationActionAIStep, `{"mode":"classify","instruction":"label it","labels":["yes","no"]}`, "yes", "ai_class", "yes", true},
		{"extract", models.IntegrationActionAIStep, `{"mode":"extract","instruction":"pull the company","output_keys":["company"]}`, `{"company":"Acme"}`, "company", "Acme", true},
		{"decide", models.IntegrationActionAISwitch, `{"instruction":"pick a case","cases":["now","later"]}`, "later", "ai_switch_case", "later", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &surfaceProvider{writeText: tc.reply}
			s := &service{aiProvider: p, credits: surfaceCredits{}}
			data := map[string]any{}
			err := s.execAIAction(context.Background(), models.Automation{ID: uuid.New(), OrganizationID: uuid.New()}, models.AutomationNode{
				ID: "n", Action: tc.action, Config: []byte(tc.config),
			}, data, false)
			if err != nil {
				t.Fatal(err)
			}
			if p.completions != 1 || p.agents != 0 {
				t.Fatalf("agent=%d complete=%d", p.agents, p.completions)
			}
			if data[tc.key] != tc.want {
				t.Fatalf("%s = %#v", tc.key, data[tc.key])
			}
			if tc.temp && (p.lastWrite.Temperature == nil || *p.lastWrite.Temperature != 0) {
				t.Fatalf("temperature = %v", p.lastWrite.Temperature)
			}
		})
	}
}

type recordingCredits struct {
	credits.CreditService
	reserveErr error
	reasons    []string
}

func (c *recordingCredits) Reserve(context.Context, uuid.UUID, models.Micro) error {
	return c.reserveErr
}
func (c *recordingCredits) Charge(context.Context, uuid.UUID, credits.Usage, string, string) (models.Micro, error) {
	c.reasons = append(c.reasons, "charged")
	return 1, nil
}

type pauseRepo struct {
	repository.IntegrationRepository
	bumps  int
	paused bool
	at     int
}

func (r *pauseRepo) BumpAutomationAICreditFailures(context.Context, uuid.UUID) (int, error) {
	r.bumps++
	if r.at > 0 {
		return r.at, nil
	}
	return r.bumps, nil
}
func (r *pauseRepo) DisableAutomationForCredits(context.Context, uuid.UUID) error {
	r.paused = true
	return nil
}

func TestAutomationAllowancePausesAtTheThreshold(t *testing.T) {
	p := &surfaceProvider{}
	repo := &pauseRepo{at: 20}
	s := &service{aiProvider: p, credits: &recordingCredits{reserveErr: credits.ErrAIAllowanceUsed}, repo: repo}
	err := s.execAIAction(context.Background(), models.Automation{ID: uuid.New(), OrganizationID: uuid.New()}, models.AutomationNode{
		ID: "n", Action: models.IntegrationActionAIStep, Config: []byte(`{"instruction":"write one line"}`),
	}, map[string]any{}, true)
	if err == nil || err.Error() != credits.ErrAIAllowanceUsed.Error() {
		t.Fatalf("err = %v", err)
	}
	if p.completions != 0 || !repo.paused || repo.bumps != 1 {
		t.Fatalf("calls %d bumps %d paused %v", p.completions, repo.bumps, repo.paused)
	}
}

func TestAutomationCapDoesNotPause(t *testing.T) {
	p := &surfaceProvider{}
	repo := &pauseRepo{}
	s := &service{aiProvider: p, credits: &recordingCredits{reserveErr: credits.ErrCapExceeded}, repo: repo}
	err := s.execAIAction(context.Background(), models.Automation{ID: uuid.New(), OrganizationID: uuid.New()}, models.AutomationNode{
		ID: "n", Action: models.IntegrationActionAIStep, Config: []byte(`{"instruction":"write one line"}`),
	}, map[string]any{}, true)
	if err == nil || err.Error() != "AI usage cap reached; try again later" {
		t.Fatalf("err = %v", err)
	}
	if p.completions != 0 || repo.bumps != 0 || repo.paused {
		t.Fatalf("calls %d bumps %d paused %v", p.completions, repo.bumps, repo.paused)
	}
}

func TestAutomationFailedCompletionIsNotCharged(t *testing.T) {
	p := &surfaceProvider{fail: errors.New("down")}
	ledger := &recordingCredits{}
	s := &service{aiProvider: p, credits: ledger}
	err := s.execAIAction(context.Background(), models.Automation{ID: uuid.New(), OrganizationID: uuid.New()}, models.AutomationNode{
		ID: "n", Action: models.IntegrationActionAIStep, Config: []byte(`{"instruction":"write one line"}`),
	}, map[string]any{}, false)
	if err == nil {
		t.Fatal("provider error was ignored")
	}
	if len(ledger.reasons) != 0 {
		t.Fatalf("charges %v", ledger.reasons)
	}
}
