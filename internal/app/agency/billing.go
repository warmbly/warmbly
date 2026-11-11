package agency

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	portalsession "github.com/stripe/stripe-go/v86/billingportal/session"
	"github.com/stripe/stripe-go/v86/checkout/session"
	"github.com/stripe/stripe-go/v86/invoice"
	"github.com/stripe/stripe-go/v86/subscription"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/businesscheckout"
	"github.com/warmbly/warmbly/internal/repository"
)

// Stripe metadata marking agency objects, so the workspace billing path never reads them.
const (
	metaAgencyID       = "agency_id"
	metaPurpose        = "purpose"
	purposeAgencyPlan  = "agency_subscription"
	purposeAgencyTopup = "agency_topup"
)

var errBillingDisabled = errx.NewWithIdentifier(errx.ServiceUnavailable, "agency_billing_disabled",
	"This instance does not bill agencies; every agency feature is already available.")

// Checkout opens Stripe Checkout for an agency plan.
func (s *Service) Checkout(ctx context.Context, agencyID, actorID, planID uuid.UUID, yearly bool, successURL, cancelURL string) (string, *errx.Error) {
	if !s.billing {
		return "", errBillingDisabled
	}
	a, _, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return "", xerr
	}
	if a.StripeSubscriptionID != nil && stripeWillCharge(a.Status) {
		return "", errx.NewWithIdentifier(errx.Conflict, "agency_already_subscribed", "This agency already has a plan. Switch it under Agency > Billing.")
	}
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return "", internal(err)
	}
	price := ""
	if plan != nil && plan.IsAgencyPlan() {
		if yearly && plan.StripePriceIDYearly != nil {
			price = *plan.StripePriceIDYearly
		} else if !yearly && plan.StripePriceID != nil {
			price = *plan.StripePriceID
		}
	}
	if price == "" {
		return "", errx.NewWithIdentifier(errx.BadRequest, "agency_plan_unavailable", "That agency plan is not available for purchase yet.")
	}
	meta := map[string]string{metaPurpose: purposeAgencyPlan, metaAgencyID: a.ID.String(), "user_id": actorID.String()}
	params := &stripe.CheckoutSessionParams{
		Params:            stripe.Params{Context: ctx},
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems:         []*stripe.CheckoutSessionLineItemParams{{Price: stripe.String(price), Quantity: stripe.Int64(1)}},
		ClientReferenceID: stripe.String(a.ID.String()),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		Metadata:          meta,
		SubscriptionData:  &stripe.CheckoutSessionSubscriptionDataParams{Metadata: meta},
	}
	businesscheckout.Apply(params)
	s.attachCustomer(ctx, a, actorID, params)
	sess, err := session.New(params)
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency checkout: %w", err))
		return "", errx.New(errx.Internal, "failed to create checkout session")
	}
	return sess.URL, nil
}

func (s *Service) attachCustomer(ctx context.Context, a *models.Agency, actorID uuid.UUID, params *stripe.CheckoutSessionParams) {
	if a.StripeCustomerID != nil && *a.StripeCustomerID != "" {
		params.Customer = a.StripeCustomerID
		params.CustomerUpdate = &stripe.CheckoutSessionCustomerUpdateParams{Address: stripe.String("auto"), Name: stripe.String("auto")}
		return
	}
	if u, err := s.users.GetUser(ctx, actorID); err == nil && u != nil && u.Email != "" {
		params.CustomerEmail = stripe.String(u.Email)
	}
	if params.Mode != nil && *params.Mode == string(stripe.CheckoutSessionModePayment) {
		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	}
}

// PlanChangePreview is what switching the agency's subscription to another plan or billing period would bill.
type PlanChangePreview struct {
	PlanID   uuid.UUID `json:"plan_id"`
	Interval string    `json:"interval"`
	// ProrationCents is the prorated difference for the rest of the period; negative is a credit.
	ProrationCents int64 `json:"proration_cents"`
	// AmountDueCents is the total of the next invoice with the switch applied.
	AmountDueCents int64  `json:"amount_due_cents"`
	Currency       string `json:"currency"`
	// ProrationDate is the moment the amounts were calculated at; sent back with the switch, it bills exactly them.
	ProrationDate int64 `json:"proration_date"`
	// FromPriceID is the price the subscription was on; sent back with the switch, it refuses one priced from another plan.
	FromPriceID string `json:"from_price_id"`
}

// planChangePreviewTTL is how long a previewed price may be confirmed at; an older one is priced again.
const planChangePreviewTTL = 30 * time.Minute

// planChange is one validated switch of the agency's own Stripe subscription.
type planChange struct {
	agency *models.Agency
	plan   *models.Plan
	price  string
	sub    *stripe.Subscription
	itemID string
	// held is true when Stripe already has the subscription on this price.
	held bool
}

// planChangeFor checks that the agency can move its subscription to the plan and period now.
func (s *Service) planChangeFor(ctx context.Context, agencyID, actorID, planID uuid.UUID, yearly bool) (*planChange, *errx.Error) {
	if !s.billing {
		return nil, errBillingDisabled
	}
	a, _, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return nil, xerr
	}
	if a.StripeSubscriptionID == nil || *a.StripeSubscriptionID == "" || !a.Status.IsActive() {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "agency_no_subscription", "This agency has no subscription to switch. Choose a plan first.")
	}
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return nil, internal(err)
	}
	price := ""
	if plan != nil && plan.IsAgencyPlan() {
		if yearly && plan.StripePriceIDYearly != nil {
			price = *plan.StripePriceIDYearly
		} else if !yearly && plan.StripePriceID != nil {
			price = *plan.StripePriceID
		}
	}
	if price == "" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "agency_plan_unavailable", "That agency plan is not available for purchase yet.")
	}
	// A smaller pool than the period has already used would be a plan the agency is over from the first minute.
	if a.PoolOverride == nil && plan.MonthlySendPool != nil && *plan.MonthlySendPool > 0 {
		_, _, e, perr := s.period(ctx, a, poolTTL)
		if perr != nil {
			return nil, internal(perr)
		}
		if used := sum(e.sends); used > int64(*plan.MonthlySendPool)+e.topups {
			return nil, errx.NewWithIdentifier(errx.Conflict, "agency_pool_exceeds_plan",
				fmt.Sprintf("This period has already used %d sends, more than that plan's pool. Switch once the period renews.", used))
		}
	}
	sub, serr := subscription.Get(*a.StripeSubscriptionID, &stripe.SubscriptionParams{Params: stripe.Params{Context: ctx}})
	if serr != nil {
		errs.CaptureException(fmt.Errorf("agency plan change: %w", serr))
		return nil, errx.New(errx.Internal, "failed to read the subscription")
	}
	if sub.Items == nil || len(sub.Items.Data) != 1 {
		return nil, errx.New(errx.Internal, "the subscription has an unexpected shape")
	}
	item := sub.Items.Data[0]
	return &planChange{agency: a, plan: plan, price: price, sub: sub, itemID: item.ID, held: item.Price != nil && item.Price.ID == price}, nil
}

var (
	errPlanUnchanged  = errx.NewWithIdentifier(errx.Conflict, "agency_plan_unchanged", "The agency is already on that plan and billing period.")
	errPreviewExpired = errx.NewWithIdentifier(errx.Conflict, "agency_plan_preview_expired", "That price is no longer current. Review the switch again.")
)

// PlanChangeQuote is what a preview returned, sent back so the switch bills what was shown; the zero value prices it now.
type PlanChangeQuote struct {
	ProrationDate int64
	FromPriceID   string
}

func intervalName(yearly bool) string {
	if yearly {
		return "year"
	}
	return "month"
}

// PreviewPlanChange prices a switch without making it.
func (s *Service) PreviewPlanChange(ctx context.Context, agencyID, actorID, planID uuid.UUID, yearly bool) (*PlanChangePreview, *errx.Error) {
	c, xerr := s.planChangeFor(ctx, agencyID, actorID, planID, yearly)
	if xerr != nil {
		return nil, xerr
	}
	at := s.now().Unix()
	if c.held {
		if c.shows(yearly) {
			return nil, errPlanUnchanged
		}
		// Stripe already holds the switch and nothing more is billed; confirming it applies it here.
		return &PlanChangePreview{PlanID: c.plan.ID, Interval: intervalName(yearly), Currency: string(c.sub.Currency), ProrationDate: at, FromPriceID: c.fromPrice()}, nil
	}
	params := &stripe.InvoiceCreatePreviewParams{
		Params:       stripe.Params{Context: ctx},
		AutomaticTax: &stripe.InvoiceCreatePreviewAutomaticTaxParams{Enabled: stripe.Bool(false)},
		Subscription: stripe.String(c.sub.ID),
		SubscriptionDetails: &stripe.InvoiceCreatePreviewSubscriptionDetailsParams{
			Items:             []*stripe.InvoiceCreatePreviewSubscriptionDetailsItemParams{{ID: stripe.String(c.itemID), Price: stripe.String(c.price)}},
			ProrationBehavior: stripe.String("create_prorations"),
			ProrationDate:     stripe.Int64(at),
		},
	}
	if c.agency.StripeCustomerID != nil {
		params.Customer = c.agency.StripeCustomerID
	}
	inv, err := invoice.CreatePreview(params)
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency plan change preview: %w", err))
		return nil, errx.New(errx.Internal, "failed to price the switch")
	}
	out := &PlanChangePreview{PlanID: c.plan.ID, Interval: intervalName(yearly), AmountDueCents: inv.AmountDue,
		Currency: string(inv.Currency), ProrationDate: at, FromPriceID: c.fromPrice()}
	if inv.Lines != nil {
		for _, line := range inv.Lines.Data {
			if lineIsProration(line) {
				out.ProrationCents += line.Amount
			}
		}
	}
	return out, nil
}

func lineIsProration(line *stripe.InvoiceLineItem) bool {
	if line == nil || line.Parent == nil {
		return false
	}
	if d := line.Parent.InvoiceItemDetails; d != nil {
		return d.Proration
	}
	if d := line.Parent.SubscriptionItemDetails; d != nil {
		return d.Proration
	}
	return false
}

// switching is the lock an agency's switches share, so one is validated, made and applied before the next starts.
// A fixed set of locks keyed by the agency id: two agencies may share one, and nothing accumulates.
func (s *Service) switching(agencyID uuid.UUID) *sync.Mutex {
	return &s.switches[int(agencyID[0])%len(s.switches)]
}

// shows reports an agency that already has the switch applied.
func (c *planChange) shows(yearly bool) bool {
	return c.agency.PlanID != nil && *c.agency.PlanID == c.plan.ID && c.agency.SubscriptionInterval() == intervalName(yearly)
}

// fromPrice is the price the subscription is on before the switch.
func (c *planChange) fromPrice() string {
	if item := c.sub.Items.Data[0]; item.Price != nil {
		return item.Price.ID
	}
	return ""
}

// ChangePlan moves the agency's subscription to another agency plan or billing
// period, prorated, and gives every client the new plan at once. prorationDate
// is the moment a preview priced the switch at; zero prices it now. A switch
// Stripe already holds is applied here again, so one whose result was not
// recorded the first time completes on the next attempt.
func (s *Service) ChangePlan(ctx context.Context, agencyID, actorID, planID uuid.UUID, yearly bool, quote PlanChangeQuote) (*models.Agency, *errx.Error) {
	now := s.now()
	prorationDate := quote.ProrationDate
	if prorationDate != 0 {
		at := time.Unix(prorationDate, 0)
		if at.After(now.Add(time.Minute)) || now.Sub(at) > planChangePreviewTTL {
			return nil, errPreviewExpired
		}
	}
	lock := s.switching(agencyID)
	lock.Lock()
	defer lock.Unlock()

	c, xerr := s.planChangeFor(ctx, agencyID, actorID, planID, yearly)
	if xerr != nil {
		return nil, xerr
	}
	sub := c.sub
	if c.held {
		// Nothing left to bill; only an agency that already shows the switch has nothing left to do.
		if c.shows(yearly) {
			return nil, errPlanUnchanged
		}
	} else {
		// The quote was for a switch from one price inside one billing period; from anywhere else it is priced again.
		if quote.FromPriceID != "" && quote.FromPriceID != c.fromPrice() {
			return nil, errPreviewExpired
		}
		if start := c.sub.Items.Data[0].CurrentPeriodStart; prorationDate != 0 && start > 0 && prorationDate < start {
			return nil, errPreviewExpired
		}
		params := &stripe.SubscriptionParams{
			Params:            stripe.Params{Context: ctx},
			Items:             []*stripe.SubscriptionItemsParams{{ID: stripe.String(c.itemID), Price: stripe.String(c.price)}},
			ProrationBehavior: stripe.String("create_prorations"),
			AutomaticTax:      &stripe.SubscriptionAutomaticTaxParams{Enabled: stripe.Bool(false)},
		}
		if prorationDate != 0 {
			params.ProrationDate = stripe.Int64(prorationDate)
		}
		updated, err := subscription.Update(c.sub.ID, params)
		if err != nil {
			errs.CaptureException(fmt.Errorf("agency plan change: %w", err))
			return nil, errx.New(errx.Internal, "failed to switch the plan")
		}
		sub = updated
	}
	// Applied here as well as by the webhook, so the dashboard shows the new plan without waiting for it.
	var raw json.RawMessage
	if sub.LastResponse != nil {
		raw = sub.LastResponse.RawJSON
	}
	if xerr := s.applySubscription(ctx, agencyID, sub, raw, false); xerr != nil {
		return nil, xerr
	}
	a, gerr := s.repo.Get(ctx, agencyID)
	if gerr != nil || a == nil {
		return nil, internal(gerr)
	}
	a.Role = c.agency.Role
	s.hydrate(ctx, a)
	return a, nil
}

// Topup opens Checkout for one top-up pack for the current period.
func (s *Service) Topup(ctx context.Context, agencyID, actorID uuid.UUID, successURL, cancelURL string) (string, *errx.Error) {
	if !s.billing {
		return "", errBillingDisabled
	}
	a, _, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return "", xerr
	}
	_, unlimited, active := s.poolSize(ctx, a)
	if !active {
		return "", errx.NewWithIdentifier(errx.BadRequest, "agency_no_subscription", "Choose an agency plan before adding top-up packs.")
	}
	if unlimited {
		return "", errx.NewWithIdentifier(errx.BadRequest, "agency_pool_unlimited", "This agency's plan has no send pool to top up.")
	}
	cents := models.AgencyTopupPriceCents(a.EffectivePlanID(s.now()))
	meta := map[string]string{
		metaPurpose: purposeAgencyTopup, metaAgencyID: a.ID.String(), "user_id": actorID.String(),
		"sends": strconv.Itoa(models.AgencyTopupSends), "amount_cents": strconv.FormatInt(cents, 10),
	}
	params := &stripe.CheckoutSessionParams{
		Params:          stripe.Params{Context: ctx},
		Mode:            stripe.String(string(stripe.CheckoutSessionModePayment)),
		InvoiceCreation: &stripe.CheckoutSessionInvoiceCreationParams{Enabled: stripe.Bool(true)},
		LineItems: []*stripe.CheckoutSessionLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency:    stripe.String("usd"),
				UnitAmount:  stripe.Int64(cents),
				TaxBehavior: stripe.String(businesscheckout.TaxExclusive),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String("Agency send pack: 50,000 sends"),
				},
			},
		}},
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{Metadata: meta},
		ClientReferenceID: stripe.String(a.ID.String()),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		Metadata:          meta,
	}
	businesscheckout.Apply(params)
	s.attachCustomer(ctx, a, actorID, params)
	sess, err := session.New(params)
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency top-up checkout: %w", err))
		return "", errx.New(errx.Internal, "failed to create checkout session")
	}
	return sess.URL, nil
}

// Portal opens the Stripe billing portal for the agency's customer.
func (s *Service) Portal(ctx context.Context, agencyID, actorID uuid.UUID, returnURL string) (string, *errx.Error) {
	if !s.billing {
		return "", errBillingDisabled
	}
	a, _, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return "", xerr
	}
	if a.StripeCustomerID == nil || *a.StripeCustomerID == "" {
		return "", errx.NewWithIdentifier(errx.BadRequest, "agency_no_subscription", "This agency has no billing account yet. Choose a plan first.")
	}
	sess, err := portalsession.New(&stripe.BillingPortalSessionParams{
		Params: stripe.Params{Context: ctx}, Customer: a.StripeCustomerID, ReturnURL: stripe.String(returnURL),
	})
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency portal: %w", err))
		return "", errx.New(errx.Internal, "failed to create billing portal session")
	}
	return sess.URL, nil
}

// HandleStripeEvent applies an event that belongs to an agency. False leaves
// it to the workspace billing path.
func (s *Service) HandleStripeEvent(ctx context.Context, event *stripe.Event) (bool, *errx.Error) {
	switch event.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			return false, nil
		}
		agencyID, err := uuid.Parse(sess.Metadata[metaAgencyID])
		if err != nil {
			return false, nil
		}
		switch sess.Metadata[metaPurpose] {
		case purposeAgencyPlan:
			return true, s.completeSubscriptionCheckout(ctx, agencyID, &sess)
		case purposeAgencyTopup:
			return true, s.completeTopup(ctx, agencyID, &sess)
		case purposeAgencyAITopup:
			return true, s.completeAITopup(ctx, agencyID, &sess)
		}
		return false, nil
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return false, nil
		}
		agencyID, ok, xerr := s.agencyForSubscription(ctx, &sub)
		if xerr != nil {
			return true, xerr
		}
		if !ok {
			return false, nil
		}
		return true, s.applySubscription(ctx, agencyID, &sub, event.Data.Raw, event.Type == "customer.subscription.deleted")
	case "charge.refunded":
		var ch stripe.Charge
		if err := json.Unmarshal(event.Data.Raw, &ch); err != nil {
			return false, nil
		}
		return s.refundTopup(ctx, &ch)
	case "invoice.paid":
		var inv struct {
			Parent *struct {
				SubscriptionDetails *struct {
					Subscription string `json:"subscription"`
				} `json:"subscription_details"`
			} `json:"parent"`
			Subscription string `json:"subscription"`
		}
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return false, nil
		}
		subID := inv.Subscription
		if subID == "" && inv.Parent != nil && inv.Parent.SubscriptionDetails != nil {
			subID = inv.Parent.SubscriptionDetails.Subscription
		}
		if subID == "" {
			return false, nil
		}
		a, err := s.repo.GetByStripeSubscriptionID(ctx, subID)
		if err != nil {
			return true, internal(err)
		}
		if a == nil {
			return false, nil
		}
		// The subscription events carry the state; an invoice only prompts a resync.
		if err := s.SyncAgency(ctx, a.ID); err != nil {
			return true, internal(err)
		}
		return true, nil
	}
	return false, nil
}

// agencyForSubscription names the agency a Stripe subscription bills, by id or by checkout metadata.
func (s *Service) agencyForSubscription(ctx context.Context, sub *stripe.Subscription) (uuid.UUID, bool, *errx.Error) {
	a, err := s.repo.GetByStripeSubscriptionID(ctx, sub.ID)
	if err != nil {
		return uuid.Nil, false, internal(err)
	}
	if a != nil {
		return a.ID, true, nil
	}
	agencyID, perr := uuid.Parse(sub.Metadata[metaAgencyID])
	if perr != nil {
		return uuid.Nil, false, nil
	}
	return agencyID, true, nil
}

func (s *Service) completeSubscriptionCheckout(ctx context.Context, agencyID uuid.UUID, sess *stripe.CheckoutSession) *errx.Error {
	a, err := s.repo.Get(ctx, agencyID)
	if err != nil {
		return internal(err)
	}
	if a == nil {
		return nil
	}
	if sess.Customer != nil && sess.Customer.ID != "" {
		if err := s.repo.SetStripeCustomer(ctx, agencyID, sess.Customer.ID); err != nil {
			return internal(err)
		}
	}
	if sess.Subscription == nil || sess.Subscription.ID == "" {
		return nil
	}
	sub, err := subscription.Get(sess.Subscription.ID, &stripe.SubscriptionParams{Params: stripe.Params{Context: ctx}})
	if err != nil {
		return errx.New(errx.Internal, "failed to load the agency subscription")
	}
	return s.applySubscription(ctx, agencyID, sub, nil, false)
}

func (s *Service) applySubscription(ctx context.Context, agencyID uuid.UUID, sub *stripe.Subscription, raw json.RawMessage, deleted bool) *errx.Error {
	a, err := s.repo.Get(ctx, agencyID)
	if err != nil {
		return internal(err)
	}
	if a == nil {
		return nil
	}
	// A late event for a subscription the agency has since replaced changes nothing.
	if a.StripeSubscriptionID != nil && *a.StripeSubscriptionID != sub.ID && a.Status.IsActive() {
		return nil
	}
	b := repository.AgencyBilling{
		StripeSubscriptionID: stripe.String(sub.ID),
		Status:               mapStatus(sub.Status),
		CancelAtPeriodEnd:    sub.CancelAtPeriodEnd,
	}
	if deleted {
		b.Status = models.SubscriptionStatusCanceled
	}
	if sub.Customer != nil && sub.Customer.ID != "" {
		b.StripeCustomerID = stripe.String(sub.Customer.ID)
	}
	if sub.Items != nil && len(sub.Items.Data) > 0 {
		item := sub.Items.Data[0]
		if item.CurrentPeriodEnd > 0 {
			start, end := time.Unix(item.CurrentPeriodStart, 0).UTC(), time.Unix(item.CurrentPeriodEnd, 0).UTC()
			b.CurrentPeriodStart, b.CurrentPeriodEnd = &start, &end
		}
		if item.Price != nil {
			if plan, err := s.plans.GetByStripePriceID(ctx, item.Price.ID); err == nil && plan.IsAgencyPlan() {
				b.PlanID = &plan.ID
			}
		}
	}
	if b.CurrentPeriodEnd == nil && raw != nil {
		var legacy struct {
			CurrentPeriodStart int64 `json:"current_period_start"`
			CurrentPeriodEnd   int64 `json:"current_period_end"`
		}
		if json.Unmarshal(raw, &legacy) == nil && legacy.CurrentPeriodEnd > 0 {
			start, end := time.Unix(legacy.CurrentPeriodStart, 0).UTC(), time.Unix(legacy.CurrentPeriodEnd, 0).UTC()
			b.CurrentPeriodStart, b.CurrentPeriodEnd = &start, &end
		}
	}
	if err := s.repo.UpdateBilling(ctx, agencyID, b); err != nil {
		return internal(err)
	}
	if err := s.SyncAgency(ctx, agencyID); err != nil {
		return internal(err)
	}
	return nil
}

func (s *Service) completeTopup(ctx context.Context, agencyID uuid.UUID, sess *stripe.CheckoutSession) *errx.Error {
	if sess.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
		return nil
	}
	a, err := s.repo.Get(ctx, agencyID)
	if err != nil {
		return internal(err)
	}
	if a == nil {
		return nil
	}
	sends, err := strconv.Atoi(sess.Metadata["sends"])
	if err != nil || sends <= 0 {
		return errx.New(errx.BadRequest, "invalid sends in agency top-up metadata")
	}
	cents, _ := strconv.ParseInt(sess.Metadata["amount_cents"], 10, 64)
	ref := sess.ID
	if sess.PaymentIntent != nil && sess.PaymentIntent.ID != "" {
		ref = sess.PaymentIntent.ID
	}
	if a.StripeCustomerID == nil && sess.Customer != nil && sess.Customer.ID != "" {
		if err := s.repo.SetStripeCustomer(ctx, agencyID, sess.Customer.ID); err != nil {
			return internal(err)
		}
	}
	start, _ := models.AgencyPeriod(a.CurrentPeriodStart, a.CurrentPeriodEnd, s.now())
	if _, err := s.repo.RecordTopup(ctx, &models.AgencyPoolTopup{
		AgencyID: agencyID, Sends: sends, AmountCents: cents, StripePaymentIntentID: &ref, PeriodStart: start,
	}); err != nil {
		return internal(err)
	}
	s.forgetAgency(agencyID)
	return nil
}

// refundTopup takes a fully refunded pack out of the pool, or a refunded AI
// top-up out of the AI balance. A partial refund leaves it in; a charge that
// paid for neither belongs to the workspace path.
func (s *Service) refundTopup(ctx context.Context, ch *stripe.Charge) (bool, *errx.Error) {
	isTopup := ch.Metadata[metaPurpose] == purposeAgencyTopup || ch.Metadata[metaPurpose] == purposeAgencyAITopup
	if ch.PaymentIntent == nil || ch.PaymentIntent.ID == "" {
		return isTopup, nil
	}
	if !ch.Refunded && (ch.Amount == 0 || ch.AmountRefunded < ch.Amount) {
		return isTopup, nil
	}
	agencyID, found, err := s.repo.RefundTopup(ctx, ch.PaymentIntent.ID)
	if err == nil && !found && s.ai != nil {
		agencyID, found, err = s.ai.RefundPurchase(ctx, ch.PaymentIntent.ID)
	}
	if err != nil {
		return true, internal(err)
	}
	if found {
		s.forgetAgency(agencyID)
	}
	return found || isTopup, nil
}

func mapStatus(st stripe.SubscriptionStatus) models.SubscriptionStatus {
	switch st {
	case stripe.SubscriptionStatusActive:
		return models.SubscriptionStatusActive
	case stripe.SubscriptionStatusTrialing:
		return models.SubscriptionStatusTrialing
	case stripe.SubscriptionStatusPastDue:
		return models.SubscriptionStatusPastDue
	case stripe.SubscriptionStatusCanceled:
		return models.SubscriptionStatusCanceled
	case stripe.SubscriptionStatusUnpaid:
		return models.SubscriptionStatusUnpaid
	case stripe.SubscriptionStatusIncompleteExpired:
		return models.SubscriptionStatusIncompleteExpired
	case stripe.SubscriptionStatusPaused:
		return models.SubscriptionStatusPaused
	}
	return models.SubscriptionStatusIncomplete
}
