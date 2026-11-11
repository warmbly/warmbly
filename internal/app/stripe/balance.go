package stripe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/checkout/session"
	"github.com/stripe/stripe-go/v86/customer"
	"github.com/stripe/stripe-go/v86/invoice"
	"github.com/stripe/stripe-go/v86/invoiceitem"
	"github.com/stripe/stripe-go/v86/paymentmethod"
	"github.com/stripe/stripe-go/v86/setupintent"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/businesscheckout"
)

// Metadata purposes that route a webhook to the balance flow.
const (
	purposeBalanceCharge   = "balance_charge"
	purposeBalanceCheckout = "balance_top_up"
	purposeCardSetup       = "card_setup"
)

// metaActor names who opened a balance Checkout; never user_id, which the previous release reads as a subscription checkout.
const metaActor = "actor_user_id"

// BalanceSettler is the slice of the balance service the webhook flow drives:
// a saved-card charge's invoice was paid. Injected via WireBalance so this
// package needs no import of it.
type BalanceSettler interface {
	SettleChargeInvoice(ctx context.Context, chargeID uuid.UUID, invoiceID string) error
}

// SavedCardResult is how one attempt at a saved-card charge ended.
type SavedCardResult struct {
	Paid      bool
	InvoiceID string
	// Declined is a models.Reload* failure code when the card refused; Soft
	// marks a refusal worth retrying later (insufficient funds and the like).
	Declined string
	Soft     bool
}

// PaymentMethodSummary is the saved card as the dashboard shows it.
type PaymentMethodSummary struct {
	ID       string `json:"-"`
	Brand    string `json:"brand"`
	Last4    string `json:"last4"`
	ExpMonth int64  `json:"exp_month"`
	ExpYear  int64  `json:"exp_year"`
	Wallet   string `json:"wallet,omitempty"`
}

// InvoiceSummary is one invoice in the billing page's list.
type InvoiceSummary struct {
	ID          string    `json:"id"`
	Number      string    `json:"number"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	CreatedAt   time.Time `json:"created_at"`
	HostedURL   string    `json:"hosted_url,omitempty"`
	PDFURL      string    `json:"pdf_url,omitempty"`
}

// Declines worth retrying: the card is fine, the moment was not.
var softDeclines = map[stripe.DeclineCode]bool{
	"insufficient_funds": true, "try_again_later": true, "issuer_not_available": true,
	"processing_error": true, "reenter_transaction": true, "approve_with_id": true,
}

func (s *stripeService) WireBalance(b BalanceSettler) { s.balance = b }

func (s *stripeService) DefaultPaymentMethod(ctx context.Context, customerID string) (*PaymentMethodSummary, error) {
	if customerID == "" {
		return nil, nil
	}
	cust, err := customer.Get(customerID, &stripe.CustomerParams{
		Params: stripe.Params{Context: ctx, Expand: []*string{stripe.String("invoice_settings.default_payment_method")}},
	})
	if err != nil {
		return nil, err
	}
	var pm *stripe.PaymentMethod
	if cust.InvoiceSettings != nil && cust.InvoiceSettings.DefaultPaymentMethod != nil {
		pm = cust.InvoiceSettings.DefaultPaymentMethod
	} else {
		// No default yet: the newest card on the customer is the one to charge.
		it := paymentmethod.List(&stripe.PaymentMethodListParams{
			Customer:   stripe.String(customerID),
			Type:       stripe.String("card"),
			ListParams: stripe.ListParams{Context: ctx, Limit: stripe.Int64(1)},
		})
		if it.Next() {
			pm = it.PaymentMethod()
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
	}
	if pm == nil || pm.ID == "" {
		return nil, nil
	}
	out := &PaymentMethodSummary{ID: pm.ID}
	if pm.Card != nil {
		out.Brand, out.Last4, out.ExpMonth, out.ExpYear = string(pm.Card.Brand), pm.Card.Last4, pm.Card.ExpMonth, pm.Card.ExpYear
		if pm.Card.Wallet != nil {
			out.Wallet = string(pm.Card.Wallet.Type)
		}
	}
	return out, nil
}

// ChargeSavedCard bills a saved-card charge as a Stripe invoice: create it,
// add the line, finalize, pay. Every step is keyed on the charge id, and an
// invoice already on the charge is resumed from whatever state it reached, so
// a retry converges on one payment. tries numbers this payment attempt.
func (s *stripeService) ChargeSavedCard(ctx context.Context, c *models.SavedCardCharge, tries int, offSession bool) (*SavedCardResult, error) {
	key := "warmbly-balance-" + c.ID.String()
	var inv *stripe.Invoice
	var err error
	if c.InvoiceID != "" {
		if inv, err = invoice.Get(c.InvoiceID, &stripe.InvoiceParams{Params: stripe.Params{Context: ctx}}); err != nil {
			return nil, fmt.Errorf("load charge invoice: %w", err)
		}
	} else {
		params := &stripe.InvoiceParams{
			Params:                      stripe.Params{Context: ctx},
			Customer:                    stripe.String(c.CustomerID),
			CollectionMethod:            stripe.String("charge_automatically"),
			AutoAdvance:                 stripe.Bool(false),
			PendingInvoiceItemsBehavior: stripe.String("exclude"),
			Description:                 stripe.String(chargeDescription(c)),
			Metadata: map[string]string{
				"purpose": purposeBalanceCharge, "org_id": c.OrgID.String(), "charge_id": c.ID.String(),
				"kind": c.Kind, "amount_micro": strconv.FormatInt(int64(c.Amount), 10),
			},
		}
		params.SetIdempotencyKey(key + "-invoice")
		if inv, err = invoice.New(params); err != nil {
			return nil, fmt.Errorf("create charge invoice: %w", err)
		}
		if s.balanceCharges != nil {
			if err := s.balanceCharges.SetInvoice(ctx, c.ID, inv.ID); err != nil {
				return nil, fmt.Errorf("record charge invoice: %w", err)
			}
		}
	}
	out := &SavedCardResult{InvoiceID: inv.ID}

	switch inv.Status {
	case stripe.InvoiceStatusPaid:
		out.Paid = true
		return out, nil
	case stripe.InvoiceStatusVoid, stripe.InvoiceStatusUncollectible:
		out.Declined = models.ReloadPaymentFailed
		return out, nil
	case stripe.InvoiceStatusDraft:
		if inv.Total == 0 {
			item := &stripe.InvoiceItemParams{
				Params:      stripe.Params{Context: ctx},
				Customer:    stripe.String(c.CustomerID),
				Invoice:     stripe.String(inv.ID),
				Amount:      stripe.Int64(c.AmountCents),
				Currency:    stripe.String(c.Currency),
				Description: stripe.String(chargeDescription(c)),
			}
			item.SetIdempotencyKey(key + "-item")
			if _, err := invoiceitem.New(item); err != nil {
				return nil, fmt.Errorf("add charge line: %w", err)
			}
		}
		fin := &stripe.InvoiceFinalizeInvoiceParams{Params: stripe.Params{Context: ctx}, AutoAdvance: stripe.Bool(false)}
		fin.SetIdempotencyKey(key + "-finalize")
		if inv, err = invoice.FinalizeInvoice(inv.ID, fin); err != nil {
			return nil, fmt.Errorf("finalize charge invoice: %w", err)
		}
		if inv.Status == stripe.InvoiceStatusPaid {
			out.Paid = true
			return out, nil
		}
	}

	pay := &stripe.InvoicePayParams{
		Params:        stripe.Params{Context: ctx},
		OffSession:    stripe.Bool(offSession),
		PaymentMethod: stripe.String(c.PaymentMethodID),
	}
	pay.SetIdempotencyKey(fmt.Sprintf("%s-pay-%d", key, tries))
	paid, err := invoice.Pay(inv.ID, pay)
	if err == nil {
		out.Paid = paid != nil && paid.Status == stripe.InvoiceStatusPaid
		return out, nil
	}
	if code, soft, ok := refusal(err); ok {
		out.Declined, out.Soft = code, soft
		return out, nil
	}
	return nil, fmt.Errorf("pay charge invoice: %w", err)
}

func chargeDescription(c *models.SavedCardCharge) string {
	if c.Kind == models.SavedCardAutoReload {
		return fmt.Sprintf("Warmbly balance, automatic reload of $%.2f", c.Amount.USD())
	}
	return fmt.Sprintf("Warmbly balance, $%.2f added", c.Amount.USD())
}

// refusal reads a Stripe error as a card refusal; ok=false means anything
// else, which the caller retries as a transient failure.
func refusal(err error) (code string, soft, ok bool) {
	var se *stripe.Error
	if !errors.As(err, &se) {
		return "", false, false
	}
	switch {
	case se.Code == stripe.ErrorCodeAuthenticationRequired || se.Code == stripe.ErrorCodeInvoicePaymentIntentRequiresAction ||
		se.DeclineCode == "authentication_required":
		return models.ReloadAuthenticationRequired, false, true
	case se.Type == stripe.ErrorTypeCard:
		return models.ReloadCardDeclined, softDeclines[se.DeclineCode], true
	case se.Type == stripe.ErrorTypeInvalidRequest && se.HTTPStatusCode < http.StatusInternalServerError:
		return models.ReloadPaymentFailed, false, true
	}
	return "", false, false
}

// VoidChargeInvoice closes an unpaid charge invoice so it never collects: a
// draft is deleted, an open one voided. Best-effort.
func (s *stripeService) VoidChargeInvoice(ctx context.Context, invoiceID string) {
	if invoiceID == "" {
		return
	}
	inv, err := invoice.Get(invoiceID, &stripe.InvoiceParams{Params: stripe.Params{Context: ctx}})
	if err != nil {
		return
	}
	switch inv.Status {
	case stripe.InvoiceStatusDraft:
		_, err = invoice.Del(invoiceID, &stripe.InvoiceParams{Params: stripe.Params{Context: ctx}})
	case stripe.InvoiceStatusOpen:
		_, err = invoice.VoidInvoice(invoiceID, &stripe.InvoiceVoidInvoiceParams{Params: stripe.Params{Context: ctx}})
	}
	if err != nil {
		errs.CaptureException(fmt.Errorf("void balance charge invoice: %w", err))
	}
}

// CreateBalanceCheckout opens Stripe Checkout to add an amount to the balance.
// It saves the card for later, so the next top-up and automatic reload need no
// form. The webhook grants the amount; nothing changes here.
func (s *stripeService) CreateBalanceCheckout(ctx context.Context, userID, orgID uuid.UUID, amount models.Micro, successURL, cancelURL string) (*stripe.CheckoutSession, *errx.Error) {
	meta := map[string]string{
		"purpose": purposeBalanceCheckout, metaActor: userID.String(), "org_id": orgID.String(),
		"amount_micro": strconv.FormatInt(int64(amount), 10),
	}
	params := &stripe.CheckoutSessionParams{
		Params:          stripe.Params{Context: ctx},
		Mode:            stripe.String(string(stripe.CheckoutSessionModePayment)),
		InvoiceCreation: &stripe.CheckoutSessionInvoiceCreationParams{Enabled: stripe.Bool(true)},
		LineItems: []*stripe.CheckoutSessionLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency:    stripe.String("usd"),
				UnitAmount:  stripe.Int64(amount.Cents()),
				TaxBehavior: stripe.String(businesscheckout.TaxExclusive),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String("Warmbly balance"),
				},
			},
		}},
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{
			SetupFutureUsage: stripe.String("off_session"),
			Metadata:         meta,
		},
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		Metadata:   meta,
	}
	businesscheckout.Apply(params)
	if xerr := s.attachCustomer(ctx, orgID, params); xerr != nil {
		return nil, xerr
	}
	sess, err := session.New(params)
	if err != nil {
		errs.CaptureException(fmt.Errorf("stripe balance checkout failed: %w", err))
		return nil, errx.New(errx.Internal, "failed to open checkout")
	}
	return sess, nil
}

// CreateCardSetupCheckout opens Stripe Checkout to save a card without paying.
func (s *stripeService) CreateCardSetupCheckout(ctx context.Context, userID, orgID uuid.UUID, successURL, cancelURL string) (*stripe.CheckoutSession, *errx.Error) {
	meta := map[string]string{"purpose": purposeCardSetup, metaActor: userID.String(), "org_id": orgID.String()}
	params := &stripe.CheckoutSessionParams{
		Params:          stripe.Params{Context: ctx},
		Mode:            stripe.String(string(stripe.CheckoutSessionModeSetup)),
		Currency:        stripe.String("usd"),
		SetupIntentData: &stripe.CheckoutSessionSetupIntentDataParams{Metadata: meta},
		SuccessURL:      stripe.String(successURL),
		CancelURL:       stripe.String(cancelURL),
		Metadata:        meta,
	}
	if xerr := s.attachCustomer(ctx, orgID, params); xerr != nil {
		return nil, xerr
	}
	sess, err := session.New(params)
	if err != nil {
		errs.CaptureException(fmt.Errorf("stripe card setup checkout failed: %w", err))
		return nil, errx.New(errx.Internal, "failed to open checkout")
	}
	return sess, nil
}

// attachCustomer bills the workspace's existing Stripe customer; a workspace
// without one gets it created by Checkout.
func (s *stripeService) attachCustomer(ctx context.Context, orgID uuid.UUID, params *stripe.CheckoutSessionParams) *errx.Error {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return errx.New(errx.Internal, "failed to get subscription")
	}
	if sub != nil && sub.StripeCustomerID != "" {
		params.Customer = stripe.String(sub.StripeCustomerID)
		if params.Mode != nil && *params.Mode == string(stripe.CheckoutSessionModePayment) {
			params.CustomerUpdate = &stripe.CheckoutSessionCustomerUpdateParams{
				Address: stripe.String("auto"), Name: stripe.String("auto"),
			}
		}
		return nil
	}
	params.CustomerCreation = stripe.String("always")
	return nil
}

// ListInvoices is the customer's latest invoices, drafts and voids left out.
func (s *stripeService) ListInvoices(ctx context.Context, customerID string, limit int) ([]InvoiceSummary, error) {
	if customerID == "" {
		return nil, nil
	}
	it := invoice.List(&stripe.InvoiceListParams{
		Customer:   stripe.String(customerID),
		ListParams: stripe.ListParams{Context: ctx, Limit: stripe.Int64(int64(limit))},
	})
	out := make([]InvoiceSummary, 0, limit)
	for it.Next() && len(out) < limit {
		inv := it.Invoice()
		if inv.Status == stripe.InvoiceStatusDraft || inv.Status == stripe.InvoiceStatusVoid {
			continue
		}
		kind := "subscription"
		switch {
		case inv.Metadata["kind"] == models.SavedCardAutoReload:
			kind = "auto_reload"
		case inv.Metadata["purpose"] == purposeBalanceCharge || inv.Metadata["purpose"] == purposeBalanceCheckout:
			kind = "top_up"
		case inv.Metadata["purpose"] == "credit_topup":
			kind = "top_up"
		}
		out = append(out, InvoiceSummary{
			ID: inv.ID, Number: inv.Number, Kind: kind, Status: string(inv.Status),
			AmountCents: inv.Total, Currency: string(inv.Currency), CreatedAt: time.Unix(inv.Created, 0).UTC(),
			HostedURL: inv.HostedInvoiceURL, PDFURL: inv.InvoicePDF,
		})
	}
	return out, it.Err()
}

// fulfillBalanceCheckout adds a completed Checkout's amount to the balance,
// keyed on the session so a retried webhook adds it once, and makes the card
// it saved the one later charges use.
func (s *stripeService) fulfillBalanceCheckout(ctx context.Context, sess *stripe.CheckoutSession) *errx.Error {
	if s.credits == nil || sess.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
		return nil
	}
	orgID, err := uuid.Parse(sess.Metadata["org_id"])
	if err != nil {
		return errx.New(errx.BadRequest, "invalid org_id in balance checkout metadata")
	}
	amount, err := strconv.ParseInt(sess.Metadata["amount_micro"], 10, 64)
	if err != nil || amount <= 0 {
		return errx.New(errx.BadRequest, "invalid amount in balance checkout metadata")
	}
	if xerr := s.adoptCheckoutCustomer(ctx, orgID, sess); xerr != nil {
		return xerr
	}
	if _, gerr := s.credits.GrantPurchased(ctx, orgID, models.Micro(amount), "balance_top_up", "checkout:"+sess.ID); gerr != nil {
		return errx.New(errx.Internal, "failed to add to the balance")
	}
	if sess.PaymentIntent != nil {
		s.ensureDefaultPaymentMethod(ctx, sess.Customer, sess.PaymentIntent.PaymentMethod)
	}
	if s.audit != nil {
		actorID, _ := uuid.Parse(sess.Metadata[metaActor])
		s.audit.LogAction(ctx, orgID, actorID, models.AuditActionCreate, models.AuditEntityCreditPurchase, nil, "", "", nil, map[string]string{
			"amount_usd": strconv.FormatFloat(models.Micro(amount).USD(), 'f', 2, 64),
		})
	}
	return nil
}

// fulfillCardSetup makes the card a setup Checkout saved the default.
func (s *stripeService) fulfillCardSetup(ctx context.Context, sess *stripe.CheckoutSession) *errx.Error {
	orgID, err := uuid.Parse(sess.Metadata["org_id"])
	if err != nil {
		return errx.New(errx.BadRequest, "invalid org_id in card setup metadata")
	}
	if xerr := s.adoptCheckoutCustomer(ctx, orgID, sess); xerr != nil {
		return xerr
	}
	if sess.SetupIntent != nil {
		si, err := setupintent.Get(sess.SetupIntent.ID, &stripe.SetupIntentParams{Params: stripe.Params{Context: ctx}})
		if err == nil {
			s.ensureDefaultPaymentMethod(ctx, sess.Customer, si.PaymentMethod)
		}
	}
	if s.audit != nil {
		actorID, _ := uuid.Parse(sess.Metadata[metaActor])
		s.audit.LogAction(ctx, orgID, actorID, models.AuditActionUpdate, models.AuditEntitySubscription, nil, "", "", nil, map[string]string{
			"payment_method": "saved",
		})
	}
	return nil
}

// adoptCheckoutCustomer records the customer Checkout created for a workspace
// that had none.
func (s *stripeService) adoptCheckoutCustomer(ctx context.Context, orgID uuid.UUID, sess *stripe.CheckoutSession) *errx.Error {
	if sess.Customer == nil || sess.Customer.ID == "" {
		return nil
	}
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil || sub == nil {
		return errx.New(errx.Internal, "failed to get subscription for the payment")
	}
	if sub.StripeCustomerID == "" {
		sub.StripeCustomerID = sess.Customer.ID
		if err := s.subRepo.Update(ctx, sub); err != nil {
			return errx.New(errx.Internal, "failed to save billing customer")
		}
	}
	return nil
}

// ensureDefaultPaymentMethod sets a freshly saved card as the customer's
// default when none is set, so off-session charges know which card to use.
func (s *stripeService) ensureDefaultPaymentMethod(ctx context.Context, cust *stripe.Customer, pm *stripe.PaymentMethod) {
	if cust == nil || cust.ID == "" || pm == nil || pm.ID == "" {
		return
	}
	current, err := customer.Get(cust.ID, &stripe.CustomerParams{Params: stripe.Params{Context: ctx}})
	if err != nil || (current.InvoiceSettings != nil && current.InvoiceSettings.DefaultPaymentMethod != nil) {
		return
	}
	if _, err := customer.Update(cust.ID, &stripe.CustomerParams{
		Params:          stripe.Params{Context: ctx},
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{DefaultPaymentMethod: stripe.String(pm.ID)},
	}); err != nil {
		errs.CaptureException(fmt.Errorf("set default payment method: %w", err))
	}
}

// settleChargeInvoice hands a paid saved-card charge invoice to the balance
// service, which adds the amount once.
func (s *stripeService) settleChargeInvoice(ctx context.Context, inv *stripe.Invoice) *errx.Error {
	if s.balance == nil || inv.Status != stripe.InvoiceStatusPaid {
		return nil
	}
	chargeID, err := uuid.Parse(inv.Metadata["charge_id"])
	if err != nil {
		return errx.New(errx.BadRequest, "invalid charge_id in balance invoice metadata")
	}
	if err := s.balance.SettleChargeInvoice(ctx, chargeID, inv.ID); err != nil {
		errs.CaptureException(fmt.Errorf("settle balance charge %s: %w", chargeID, err))
		return errx.New(errx.Internal, "failed to settle the balance charge")
	}
	return nil
}
