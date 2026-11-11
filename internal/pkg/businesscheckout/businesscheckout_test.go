package businesscheckout

import (
	"testing"

	"github.com/stripe/stripe-go/v86"
)

func TestApplyAsksForTheBuyingBusiness(t *testing.T) {
	p := &stripe.CheckoutSessionParams{Mode: stripe.String(string(stripe.CheckoutSessionModePayment))}
	Apply(p)
	if p.AutomaticTax == nil || *p.AutomaticTax.Enabled {
		t.Fatal("Stripe must not calculate tax")
	}
	if p.BillingAddressCollection == nil || *p.BillingAddressCollection != "required" {
		t.Fatal("the billing address must be required")
	}
	if p.TaxIDCollection == nil || !*p.TaxIDCollection.Enabled {
		t.Fatal("a tax ID must be collectable")
	}
	if p.NameCollection == nil || p.NameCollection.Business == nil || !*p.NameCollection.Business.Enabled || *p.NameCollection.Business.Optional {
		t.Fatal("the business name must be required")
	}
	if p.CustomText == nil || p.CustomText.Submit == nil || *p.CustomText.Submit.Message != Notice {
		t.Fatal("the business-only notice must be shown")
	}
	if *p.Mode != "payment" {
		t.Fatal("the mode is the caller's")
	}
}
