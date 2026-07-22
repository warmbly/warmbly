package aitools

import (
	"context"
	"encoding/json"

	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Billing tools are read-only. Plan changes, checkout, cancellation, and the
// billing portal move money or change the paid plan and their write routes are
// not gated by a billing permission an agent could inherit, so they are
// deliberately NOT exposed. JWT-only (no API-key billing scope exists).
func (d Deps) registerBillingTools(r *Registry) {
	if d.Subscription == nil {
		return
	}

	r.Register(Tool{
		Name:            "get_subscription",
		Description:     "Get the workspace's current subscription (plan, status, trial state).",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		JWTOnly:         true,
		RequiredOrgPerm: 0,
		Handler:         d.getSubscription,
	})

	r.Register(Tool{
		Name:            "get_subscription_limits",
		Description:     "Get the workspace's plan limits and quotas (mailboxes, contacts, sends, members).",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		JWTOnly:         true,
		RequiredOrgPerm: 0,
		Handler:         d.getSubscriptionLimits,
	})
}

func (d Deps) getSubscription(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	sub, xerr := d.Subscription.Get(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(sub)
}

func (d Deps) getSubscriptionLimits(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	sub, xerr := d.Subscription.GetWithLimits(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(sub)
}
