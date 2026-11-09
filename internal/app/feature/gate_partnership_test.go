package feature

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// Partnership tools open on the Partnership plan alone, and on a self-hosted
// instance, which has no plans; the own-OAuth-app rule follows the plan only.
func TestPartnershipGates(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	must := noError(t)
	for name, c := range map[string]struct {
		sub        *models.Subscription
		selfHost   bool
		plan, tool bool
	}{
		"partnership": {sub: paidSub(models.PartnershipPlanID), plan: true, tool: true},
		"warmup plan": {sub: paidSub(models.WarmupPlanID)},
		"product":     {sub: paidSub(uuid.New())},
		"none":        {},
		"self-host":   {selfHost: true, tool: true},
	} {
		g := &featureGateService{subRepo: stubSubs{sub: c.sub}, planRepo: stubPlans{}, selfHost: c.selfHost}
		if got := must(g.HasPartnership(ctx, org)); got != c.plan {
			t.Errorf("%s: HasPartnership = %v, want %v", name, got, c.plan)
		}
		if got := must(g.CanUsePartnerTools(ctx, org)); got != c.tool {
			t.Errorf("%s: CanUsePartnerTools = %v, want %v", name, got, c.tool)
		}
	}
}
