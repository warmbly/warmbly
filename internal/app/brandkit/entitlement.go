package brandkit

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// FullWhiteLabel is the one definition of who may remove "Powered by": an
// active enterprise subscription, or an active plan that reserves isolated
// egress (Agency Pro and Agency Scale; Agency and the single-workspace plans do not).
func FullWhiteLabel(sub *models.Subscription, plan *models.Plan) bool {
	if config.BillingProvider() == "none" {
		return true
	}
	if !sub.HasProductPlan() {
		return false
	}
	return sub.IsEnterprise || plan.IsolatedEgress()
}

// SubscriptionReader is the subscription lookup FullWhiteLabel needs.
type SubscriptionReader interface {
	GetByOrganizationID(ctx context.Context, orgID uuid.UUID) (*models.Subscription, error)
}

// PlanReader is the plan lookup FullWhiteLabel needs.
type PlanReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*models.Plan, error)
}

// PlanEntitlements reads a workspace's effective plan, which for an agency client is the agency's managed grant.
type PlanEntitlements struct {
	Subs  SubscriptionReader
	Plans PlanReader
}

// FullWhiteLabel implements Entitlements.
func (e PlanEntitlements) FullWhiteLabel(ctx context.Context, orgID uuid.UUID) (bool, error) {
	if config.BillingProvider() == "none" {
		return true, nil
	}
	sub, err := e.Subs.GetByOrganizationID(ctx, orgID)
	if err != nil || sub == nil {
		return false, err
	}
	plan, err := e.Plans.GetByID(ctx, sub.EffectivePlanID())
	if err != nil {
		return false, err
	}
	return FullWhiteLabel(sub, plan), nil
}
