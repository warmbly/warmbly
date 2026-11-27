package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

type countingProvider struct {
	generation.Provider
	completes int
}

func (p *countingProvider) Complete(_ context.Context, _ generation.CompletionRequest) (*generation.WritingResult, error) {
	p.completes++
	return &generation.WritingResult{Text: "one concrete detail about Thursday", Model: "test-model", InputTokens: 2, OutputTokens: 3, TokensUsed: 5}, nil
}

func (p *countingProvider) RunAgent(context.Context, generation.AgentRequest) (*generation.AgentResult, error) {
	panic("RunAgent")
}

func (p *countingProvider) ModelForTier(bool) string { return "test-model" }
func (p *countingProvider) IsLocal() bool            { return false }
func (p *countingProvider) Name() string             { return "test" }

func TestAIVariablePreviewStaysOneCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &countingProvider{}
	h := &Handler{FeatureGateService: editGate{}, CreditService: editCredits{}, AIProvider: provider}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set(middleware.OrganizationIDKey, uuid.New())
	c.Request = httptest.NewRequest(http.MethodPost, "/generation/ai-variable",
		strings.NewReader(`{"mode":"research","web_search":true,"prompt":"one concrete detail"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.GenerateAIVariable(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if provider.completes != 1 {
		t.Fatalf("Complete calls = %d", provider.completes)
	}
}

type writingStub struct {
	generation.Provider
	writes int
}

func (w *writingStub) GenerateWriting(_ context.Context, model, _ string, _ generation.VoiceContext) (*generation.WritingResult, error) {
	w.writes++
	return &generation.WritingResult{Text: "A short note.", Model: model, InputTokens: 1, OutputTokens: 2, TokensUsed: 3}, nil
}

func (w *writingStub) Complete(context.Context, generation.CompletionRequest) (*generation.WritingResult, error) {
	panic("Complete")
}

func (w *writingStub) RunAgent(context.Context, generation.AgentRequest) (*generation.AgentResult, error) {
	panic("RunAgent")
}

func (w *writingStub) ModelForTier(bool) string { return "test-model" }
func (w *writingStub) IsLocal() bool            { return false }

func TestGenerateWritingUsesTheWritingGenerator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := &writingStub{}
	h := &Handler{FeatureGateService: editGate{}, CreditService: editCredits{}, WritingGenerator: writer}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set(middleware.OrganizationIDKey, uuid.New())
	c.Request = httptest.NewRequest(http.MethodPost, "/generation/write",
		strings.NewReader(`{"prompt":"A short note about Thursday's meeting."}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.GenerateWriting(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if writer.writes != 1 {
		t.Fatalf("GenerateWriting calls = %d", writer.writes)
	}
}
