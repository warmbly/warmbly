package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCreditContextAISurfaceJSON pins the durable encoding of AI attribution
// on credit-ledger rows: the context jsonb carries ai_surface on AI-attributed
// spends and omits the key entirely on non-AI spends, so existing ledger rows
// and readers see no difference.
func TestCreditContextAISurfaceJSON(t *testing.T) {
	b, err := json.Marshal(CreditContext{CampaignID: "c-1", AISurface: AISurfaceSequenceAI})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"ai_surface":"sequence_ai"`) {
		t.Fatalf("expected ai_surface in the persisted context jsonb, got %s", b)
	}

	zero, err := json.Marshal(CreditContext{CampaignID: "c-1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(zero), "ai_surface") {
		t.Fatalf("non-AI context must not carry ai_surface, got %s", zero)
	}

	if (CreditContext{AISurface: AISurfaceAgent}).Empty() {
		t.Fatal("a context with only AISurface set is not empty")
	}
	if !(CreditContext{}).Empty() {
		t.Fatal("the zero context must stay empty")
	}
}
