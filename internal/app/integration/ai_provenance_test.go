package integration

import (
	"encoding/json"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestActionRunOutputMarksAINodes pins the provenance tags run history carries
// for AI nodes: every AI node's preview names the surface, and agent mode adds
// the allowlist decision that authorized its tools. Non-AI nodes stay clean.
func TestActionRunOutputMarksAINodes(t *testing.T) {
	data := map[string]any{automationRunIDKey: "run-1"}

	agentCfg := mustJSON(t, map[string]any{"mode": "agent", "instruction": "triage"})
	out := actionRunOutput(models.AutomationNode{ID: "n1", Action: models.IntegrationActionAIStep, Config: agentCfg}, data)
	if out == nil {
		t.Fatal("expected a preview for an agent AI node")
	}
	if out[models.MetaKeyAISurface] != string(models.AISurfaceAutomationAI) {
		t.Fatalf("agent AI node preview missing ai_surface: %v", out)
	}
	if out[models.MetaKeyAIDecision] != string(models.AIDecisionAllowlisted) {
		t.Fatalf("agent AI node preview missing ai_decision=allowlisted: %v", out)
	}

	generateCfg := mustJSON(t, map[string]any{"mode": "generate", "instruction": "write a line"})
	out = actionRunOutput(models.AutomationNode{ID: "n2", Action: models.IntegrationActionAIStep, Config: generateCfg}, data)
	if out == nil {
		t.Fatal("expected a preview for a generate AI node")
	}
	if out[models.MetaKeyAISurface] != string(models.AISurfaceAutomationAI) {
		t.Fatalf("generate AI node preview missing ai_surface: %v", out)
	}
	if _, ok := out[models.MetaKeyAIDecision]; ok {
		t.Fatalf("non-agent AI node must not carry ai_decision: %v", out)
	}

	out = actionRunOutput(models.AutomationNode{ID: "n3", Action: models.IntegrationActionAddTag}, data)
	for _, key := range []string{models.MetaKeyAISurface, models.MetaKeyAIDecision} {
		if out != nil && out[key] != "" {
			t.Fatalf("non-AI node must not carry %s: %v", key, out)
		}
	}
}
