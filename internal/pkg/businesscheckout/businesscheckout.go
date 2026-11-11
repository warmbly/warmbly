// Package businesscheckout holds the terms every paid Stripe Checkout Warmbly
// opens carries: it is sold to businesses, with taxes excluded.
package businesscheckout

import "github.com/stripe/stripe-go/v86"

// Notice sits beside the pay button; it states the terms' business-only and tax clauses.
const Notice = "Warmbly is sold to businesses for business use only. Prices exclude taxes: where we do not charge tax, your business accounts for any tax due in its own country, for example under the reverse charge."

// Apply puts those terms on a subscription or payment Checkout: the buying
// company's legal name, billing address and tax ID are asked for, tax is not
// calculated by Stripe, and the notice is shown.
func Apply(p *stripe.CheckoutSessionParams) {
	p.AutomaticTax = &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(false)}
	p.BillingAddressCollection = stripe.String(string(stripe.CheckoutSessionBillingAddressCollectionRequired))
	p.TaxIDCollection = &stripe.CheckoutSessionTaxIDCollectionParams{Enabled: stripe.Bool(true)}
	p.NameCollection = &stripe.CheckoutSessionNameCollectionParams{
		Business: &stripe.CheckoutSessionNameCollectionBusinessParams{Enabled: stripe.Bool(true), Optional: stripe.Bool(false)},
	}
	p.CustomText = &stripe.CheckoutSessionCustomTextParams{
		Submit: &stripe.CheckoutSessionCustomTextSubmitParams{Message: stripe.String(Notice)},
	}
}

// TaxExclusive is the tax behaviour of every price built inside a Checkout.
const TaxExclusive = "exclusive"
