package credits

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestSelfHostReserveAndChargeDoNotTouchTheLedger(t *testing.T) {
	s := &creditService{selfHost: true}
	org := uuid.New()
	if err := s.Reserve(context.Background(), org, EstimateResearchRun); err != nil {
		t.Fatal(err)
	}
	got, err := s.Charge(context.Background(), org, Usage{
		Model: "m", InputTokens: 10, OutputTokens: 4, Unit: "web_search", Quantity: 2,
	}, "research_run", "k")
	if err != nil || got != 0 {
		t.Fatalf("charge = %d, %v", got, err)
	}
}
