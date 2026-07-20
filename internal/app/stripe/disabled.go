package stripe

import (
	"context"

	"github.com/google/uuid"
	stripe "github.com/stripe/stripe-go/v76"

	"github.com/warmbly/warmbly/internal/errx"
)

// disabledService is the StripeService used when BILLING_PROVIDER=none (the
// self-host default). It lets the backend boot and wire the credit/referral
// hooks without any Stripe keys: the customer-facing billing calls return a
// clean "billing disabled" error, the webhook verify rejects, and the Wire*
// hooks are no-ops. Feature access does not depend on this — the feature gate
// unlocks everything in self-host mode.
type disabledService struct{}

// NewDisabledService returns a no-op StripeService for billing-disabled installs.
func NewDisabledService() StripeService { return &disabledService{} }

func billingDisabled() *errx.Error {
	return errx.New(errx.ServiceUnavailable, "billing is disabled on this self-hosted instance")
}

func (d *disabledService) CreateCustomer(_ context.Context, _ uuid.UUID, _, _ string) (string, *errx.Error) {
	return "", billingDisabled()
}

func (d *disabledService) GetCustomer(_ context.Context, _ string) (*stripe.Customer, *errx.Error) {
	return nil, billingDisabled()
}

func (d *disabledService) CreateCheckoutSession(_ context.Context, _, _ uuid.UUID, _, _, _, _ string) (*stripe.CheckoutSession, *errx.Error) {
	return nil, billingDisabled()
}

func (d *disabledService) CreatePortalSession(_ context.Context, _, _ string) (string, *errx.Error) {
	return "", billingDisabled()
}

func (d *disabledService) GetSubscription(_ context.Context, _ string) (*stripe.Subscription, *errx.Error) {
	return nil, billingDisabled()
}

func (d *disabledService) CancelSubscription(_ context.Context, _ string, _ bool) *errx.Error {
	return billingDisabled()
}

func (d *disabledService) ChangePlan(_ context.Context, _, _ uuid.UUID, _, _, _ string) (*stripe.Subscription, *errx.Error) {
	return nil, billingDisabled()
}

func (d *disabledService) PreviewPlanChange(_ context.Context, _, _ uuid.UUID) (*ProrationPreview, *errx.Error) {
	return nil, billingDisabled()
}

func (d *disabledService) VerifyWebhook(_ []byte, _ string) (*stripe.Event, *errx.Error) {
	return nil, billingDisabled()
}

func (d *disabledService) ProcessWebhookEvent(_ context.Context, _ *stripe.Event) *errx.Error {
	return nil
}

func (d *disabledService) ApplyCustomerCredit(_ context.Context, _ string, _ int64, _, _ string) (string, *errx.Error) {
	return "", billingDisabled()
}

func (d *disabledService) CreateCreditCheckoutSession(_ context.Context, _, _ uuid.UUID, _ string, _ int, _, _ string) (*stripe.CheckoutSession, *errx.Error) {
	return nil, billingDisabled()
}

func (d *disabledService) AutoTopUpCredits(_ context.Context, _ uuid.UUID, _ string, _ int) (bool, error) {
	return false, nil
}

func (d *disabledService) WireReferral(_ ReferralRewarder) {}

func (d *disabledService) WireCredits(_ CreditGranter, _ AuditLogger) {}
