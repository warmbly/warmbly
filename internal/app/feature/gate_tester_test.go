package feature

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestTestPlanAccessEndsWithReviewerExpiry(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	granted := time.Now().Add(-time.Hour)
	for _, expired := range []bool{false, true} {
		until := time.Now().Add(time.Hour)
		if expired {
			until = time.Now().Add(-time.Minute)
		}
		sub := &models.Subscription{PlanID: uuid.New(), ManagedAt: &granted, ManagedUntil: &until, ManagedPlanID: &models.TestPlanID}
		gate := &featureGateService{subRepo: stubSubs{sub: sub}}
		for name, check := range map[string]func(context.Context, uuid.UUID) (bool, *errx.Error){
			"product":           gate.IsPaidOrganization,
			"campaigns":         gate.CanSendCampaignEmail,
			"premium pool":      gate.HasPremiumWarmup,
			"Unibox":            gate.CanUseUnibox,
			"writing assistant": gate.CanUseWritingAssistant,
			"inbox agent":       gate.CanUseInboxAgent,
			"warmup":            gate.CanUseWarmup,
		} {
			allowed, xerr := check(ctx, orgID)
			if xerr != nil || allowed == expired {
				t.Errorf("%s with expired=%v: allowed=%v, error=%v", name, expired, allowed, xerr)
			}
		}
	}
}
