package agency

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/customer"
	"github.com/stripe/stripe-go/v86/invoice"
	"github.com/stripe/stripe-go/v86/invoiceitem"
	"github.com/stripe/stripe-go/v86/subscription"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// Both automatic top-ups mark their invoices with one purpose, so agency billing claims their events.
const purposeAgencyAutoTopup = "agency_topup_auto"

// SavedCard is the card an agency's subscription is paid with, as the dashboard shows it.
type SavedCard struct {
	ID    string `json:"-"`
	Brand string `json:"brand"`
	Last4 string `json:"last4"`
}

// AutoTopup is an agency's automatic top-up as its billing page shows it.
type AutoTopup struct {
	Enabled        bool `json:"enabled"`
	ThresholdSends int  `json:"threshold_sends"`
	MaxPacks       int  `json:"max_packs"`
	// BoughtThisPeriod is how many packs it has bought in the current period.
	BoughtThisPeriod int `json:"bought_this_period"`
	// Failure says why it turned itself off: no_payment_method or card_declined.
	Failure   string `json:"failure,omitempty"`
	PackSends int    `json:"pack_sends"`
	PackCents int64  `json:"pack_price_cents"`
	// Card is the card a pack is charged to; absent when the agency has none saved.
	Card *SavedCard `json:"card,omitempty"`
	// Available is false when the agency has no pool to top up: no plan in force, or a plan with no cap.
	Available bool `json:"available"`
}

// packCharger charges a send pack to an agency's saved card.
type packCharger interface {
	// Card is the agency's saved card; nil when it has none.
	Card(ctx context.Context, a *models.Agency) (*SavedCard, error)
	// Charge bills cents to the card once per key and returns the payment's reference; declined reports a refusal by the card.
	Charge(ctx context.Context, a *models.Agency, card *SavedCard, cents int64, description, key string, meta map[string]string) (ref string, declined bool, err error)
}

func (s *Service) packs() packCharger {
	if s.charger != nil {
		return s.charger
	}
	return stripeCharger{}
}

func (s *Service) autoTopupView(ctx context.Context, a *models.Agency, withCard bool) (*AutoTopup, *errx.Error) {
	now := s.now()
	start, _ := models.AgencyPeriod(a.CurrentPeriodStart, a.CurrentPeriodEnd, now)
	bought, err := s.repo.AutoTopupCount(ctx, a.ID, start)
	if err != nil {
		return nil, internal(err)
	}
	_, unlimited, active := s.poolSize(ctx, a)
	out := &AutoTopup{
		Enabled: a.AutoTopupEnabled, ThresholdSends: a.AutoTopupThreshold, MaxPacks: a.AutoTopupMaxPacks, BoughtThisPeriod: bought,
		PackSends: models.AgencyTopupSends, PackCents: models.AgencyTopupPriceCents(a.EffectivePlanID(now)),
		Available: active && !unlimited,
	}
	if a.AutoTopupFailure != nil {
		out.Failure = *a.AutoTopupFailure
	}
	if withCard && s.billing {
		// A card that cannot be read is shown as none; the page then asks for one.
		if card, cerr := s.packs().Card(ctx, a); cerr == nil {
			out.Card = card
		} else {
			errs.CaptureException(fmt.Errorf("agency saved card: %w", cerr))
		}
	}
	return out, nil
}

// AutoTopup is the agency's automatic top-up settings, with the card a pack would be charged to.
func (s *Service) AutoTopup(ctx context.Context, agencyID, userID uuid.UUID) (*AutoTopup, *errx.Error) {
	if !s.billing {
		return nil, errBillingDisabled
	}
	a, _, xerr := s.access(ctx, agencyID, userID, false)
	if xerr != nil {
		return nil, xerr
	}
	return s.autoTopupView(ctx, a, true)
}

// SetAutoTopup turns the automatic top-up on or off and sets when it buys and how many packs a period.
func (s *Service) SetAutoTopup(ctx context.Context, agencyID, actorID uuid.UUID, enabled bool, threshold, maxPacks int) (*AutoTopup, *errx.Error) {
	if !s.billing {
		return nil, errBillingDisabled
	}
	a, _, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return nil, xerr
	}
	if threshold < 0 || threshold > models.AgencyAutoTopupMaxThreshold {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_auto_topup", "The threshold must be between 0 and 10,000,000 sends.")
	}
	if maxPacks < 1 || maxPacks > models.AgencyAutoTopupMaxPacks {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_auto_topup", "Choose between 1 and 20 packs a period.")
	}
	var card *SavedCard
	if enabled {
		if _, unlimited, active := s.poolSize(ctx, a); !active {
			return nil, errx.NewWithIdentifier(errx.BadRequest, "agency_no_subscription", "Choose an agency plan before turning on automatic top-up.")
		} else if unlimited {
			return nil, errx.NewWithIdentifier(errx.BadRequest, "agency_pool_unlimited", "This agency's plan has no send pool to top up.")
		}
		// A pack is charged without anyone present, so there has to be a card to charge.
		c, err := s.packs().Card(ctx, a)
		if err != nil {
			errs.CaptureException(fmt.Errorf("agency saved card: %w", err))
			return nil, errx.New(errx.Internal, "failed to read the saved card")
		}
		if c == nil {
			return nil, errx.NewWithIdentifier(errx.Conflict, "agency_no_payment_method",
				"This agency has no saved card. Automatic top-up charges the card its subscription is paid with.")
		}
		card = c
	}
	if err := s.repo.SetAutoTopup(ctx, agencyID, enabled, threshold, maxPacks); err != nil {
		return nil, internal(err)
	}
	a.AutoTopupEnabled, a.AutoTopupThreshold, a.AutoTopupMaxPacks, a.AutoTopupFailure = enabled, threshold, maxPacks, nil
	view, xerr := s.autoTopupView(ctx, a, false)
	if xerr != nil {
		return nil, xerr
	}
	view.Card = card
	// A pool already under the new threshold is topped up now, not at the next sweep.
	if enabled {
		s.autoTopup(ctx, a)
		if fresh, err := s.repo.Get(ctx, agencyID); err == nil && fresh != nil {
			if v, xerr := s.autoTopupView(ctx, fresh, false); xerr == nil {
				v.Card = card
				view = v
			}
		}
	}
	return view, nil
}

// autoTopup buys one pack when the agency's pool is at or under its threshold
// and the period has packs left to buy. One pack a call: the next sweep buys
// another if the pool is still low. Best-effort; a refusal by the card turns
// it off and tells the people who manage the agency.
func (s *Service) autoTopup(ctx context.Context, a *models.Agency) {
	if !s.billing || !a.AutoTopupEnabled || a.StripeCustomerID == nil || *a.StripeCustomerID == "" {
		return
	}
	if _, unlimited, active := s.poolSize(ctx, a); unlimited || !active {
		return
	}
	lock := s.switching(a.ID)
	lock.Lock()
	defer lock.Unlock()

	s.forgetAgency(a.ID)
	u, xerr := s.usage(ctx, a)
	if xerr != nil || u.Remaining > int64(a.AutoTopupThreshold) {
		return
	}
	bought, err := s.repo.AutoTopupCount(ctx, a.ID, u.PeriodStart)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	if bought >= a.AutoTopupMaxPacks {
		return
	}
	charger := s.packs()
	card, err := charger.Card(ctx, a)
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency auto top-up card: %w", err))
		return
	}
	if card == nil {
		s.autoTopupOff(ctx, a, models.AgencyAutoTopupNoCard)
		return
	}
	cents := models.AgencyTopupPriceCents(a.EffectivePlanID(s.now()))
	// One key per pack of a period, so every replica that reaches this asks Stripe for the same charge.
	key := fmt.Sprintf("agency-autotopup-%s-%d-%d", a.ID, u.PeriodStart.Unix(), bought+1)
	meta := map[string]string{
		metaPurpose: purposeAgencyAutoTopup, metaAgencyID: a.ID.String(),
		"sends": strconv.Itoa(models.AgencyTopupSends), "amount_cents": strconv.FormatInt(cents, 10),
	}
	ref, declined, err := charger.Charge(ctx, a, card, cents, "Agency send pack: 50,000 sends (automatic top-up)", key, meta)
	if declined {
		s.autoTopupOff(ctx, a, models.AgencyAutoTopupDeclined)
		return
	}
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency auto top-up %s: %w", a.ID, err))
		return
	}
	reason := "auto_topup"
	added, err := s.repo.RecordTopup(ctx, &models.AgencyPoolTopup{
		AgencyID: a.ID, Sends: models.AgencyTopupSends, AmountCents: cents, StripePaymentIntentID: &ref,
		PeriodStart: u.PeriodStart, Reason: &reason, Automatic: true,
	})
	if err != nil {
		errs.CaptureException(err)
		return
	}
	s.forgetAgency(a.ID)
	if added {
		s.tellManagers(ctx, a, "A top-up pack was added automatically",
			fmt.Sprintf("Your agency was down to %d sends, so %d more were added for $%.2f. Pack %d of %d this period.",
				u.Remaining, models.AgencyTopupSends, float64(cents)/100, bought+1, a.AutoTopupMaxPacks),
			map[string]any{"agency_id": a.ID.String(), "auto_topup": "bought"})
	}
}

// autoTopupOff turns the automatic top-up off after a failure and says so.
func (s *Service) autoTopupOff(ctx context.Context, a *models.Agency, failure string) {
	if err := s.repo.DisableAutoTopup(ctx, a.ID, failure); err != nil {
		errs.CaptureException(err)
		return
	}
	body := "The card on file was declined, so automatic top-up is off. Update the card under Agency > Billing and turn it on again, or buy a pack there."
	if failure == models.AgencyAutoTopupNoCard {
		body = "There is no saved card to charge, so automatic top-up is off. Add a card under Agency > Billing and turn it on again, or buy a pack there."
	}
	s.tellManagers(ctx, a, "Automatic top-up could not add sends", body, map[string]any{"agency_id": a.ID.String(), "auto_topup": failure})
}

// tellManagers notifies the agency's owner and admins. Notifications are addressed within a workspace; staff are members of every client.
func (s *Service) tellManagers(ctx context.Context, a *models.Agency, title, body string, meta map[string]any) {
	if s.notifier == nil {
		return
	}
	clients, err := s.repo.ListClientOrganizations(ctx, a.ID)
	if err != nil || len(clients) == 0 {
		return
	}
	staff, err := s.repo.ListMembers(ctx, a.ID)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	orgID := clients[0].OrganizationID
	for _, m := range staff {
		if m.Role.CanManage() {
			s.notifier.Notify(ctx, m.UserID, &orgID, models.NotifBillingAlert, title, body, "/app/agency/settings/billing", meta)
		}
	}
}

// stripeCharger charges packs through Stripe invoices, so each one has an invoice like a pack bought at checkout.
type stripeCharger struct{}

func cardOf(pm *stripe.PaymentMethod) *SavedCard {
	if pm == nil || pm.ID == "" {
		return nil
	}
	c := &SavedCard{ID: pm.ID}
	if pm.Card != nil {
		c.Brand, c.Last4 = string(pm.Card.Brand), pm.Card.Last4
	}
	return c
}

// Card reads the subscription's own card first, then the customer's default.
func (stripeCharger) Card(ctx context.Context, a *models.Agency) (*SavedCard, error) {
	if a.StripeSubscriptionID != nil && *a.StripeSubscriptionID != "" {
		p := &stripe.SubscriptionParams{Params: stripe.Params{Context: ctx}}
		p.AddExpand("default_payment_method")
		sub, err := subscription.Get(*a.StripeSubscriptionID, p)
		if err != nil {
			return nil, err
		}
		if c := cardOf(sub.DefaultPaymentMethod); c != nil {
			return c, nil
		}
	}
	if a.StripeCustomerID == nil || *a.StripeCustomerID == "" {
		return nil, nil
	}
	p := &stripe.CustomerParams{Params: stripe.Params{Context: ctx}}
	p.AddExpand("invoice_settings.default_payment_method")
	cust, err := customer.Get(*a.StripeCustomerID, p)
	if err != nil {
		return nil, err
	}
	if cust.InvoiceSettings == nil {
		return nil, nil
	}
	return cardOf(cust.InvoiceSettings.DefaultPaymentMethod), nil
}

func (stripeCharger) Charge(ctx context.Context, a *models.Agency, card *SavedCard, cents int64, description, key string, meta map[string]string) (string, bool, error) {
	ip := &stripe.InvoiceParams{
		Params:                      stripe.Params{Context: ctx},
		Customer:                    a.StripeCustomerID,
		CollectionMethod:            stripe.String("charge_automatically"),
		AutoAdvance:                 stripe.Bool(false),
		PendingInvoiceItemsBehavior: stripe.String("exclude"),
		AutomaticTax:                &stripe.InvoiceAutomaticTaxParams{Enabled: stripe.Bool(false)},
		Description:                 stripe.String(description),
		Metadata:                    meta,
	}
	ip.SetIdempotencyKey(key + "-invoice")
	inv, err := invoice.New(ip)
	if err != nil {
		return "", false, fmt.Errorf("create invoice: %w", err)
	}
	if inv.Status == stripe.InvoiceStatusDraft {
		item := &stripe.InvoiceItemParams{
			Params: stripe.Params{Context: ctx}, Customer: a.StripeCustomerID, Invoice: stripe.String(inv.ID),
			Amount: stripe.Int64(cents), Currency: stripe.String("usd"), Description: stripe.String(description),
		}
		item.SetIdempotencyKey(key + "-item")
		if _, err := invoiceitem.New(item); err != nil {
			return "", false, fmt.Errorf("add invoice line: %w", err)
		}
		fin := &stripe.InvoiceFinalizeInvoiceParams{Params: stripe.Params{Context: ctx}, AutoAdvance: stripe.Bool(false)}
		fin.SetIdempotencyKey(key + "-finalize")
		if inv, err = invoice.FinalizeInvoice(inv.ID, fin); err != nil {
			return "", false, fmt.Errorf("finalize invoice: %w", err)
		}
	}
	if inv.Status == stripe.InvoiceStatusVoid || inv.Status == stripe.InvoiceStatusUncollectible {
		return "", true, nil
	}
	if inv.Status != stripe.InvoiceStatusPaid {
		pay := &stripe.InvoicePayParams{Params: stripe.Params{Context: ctx}, OffSession: stripe.Bool(true), PaymentMethod: stripe.String(card.ID)}
		pay.SetIdempotencyKey(key + "-pay")
		paid, err := invoice.Pay(inv.ID, pay)
		var serr *stripe.Error
		if errors.As(err, &serr) && serr.Type == stripe.ErrorTypeCard {
			// A refused invoice is closed, so nothing stays open on the agency's account.
			void := &stripe.InvoiceVoidInvoiceParams{Params: stripe.Params{Context: ctx}}
			if _, verr := invoice.VoidInvoice(inv.ID, void); verr != nil {
				errs.CaptureException(fmt.Errorf("void refused agency top-up invoice: %w", verr))
			}
			return "", true, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("pay invoice: %w", err)
		}
		if paid == nil || paid.Status != stripe.InvoiceStatusPaid {
			return "", false, errors.New("the invoice is not paid yet")
		}
	}
	// The payment intent is what a refund names, so a refunded pack can be found again.
	gp := &stripe.InvoiceParams{Params: stripe.Params{Context: ctx}}
	gp.AddExpand("payments")
	if full, gerr := invoice.Get(inv.ID, gp); gerr == nil && full.Payments != nil {
		for _, p := range full.Payments.Data {
			if p.Payment != nil && p.Payment.PaymentIntent != nil && p.Payment.PaymentIntent.ID != "" {
				return p.Payment.PaymentIntent.ID, false, nil
			}
		}
	}
	return inv.ID, false, nil
}

// AIAutoTopup is the automatic top-up of an agency's shared AI balance as its billing page shows it.
type AIAutoTopup struct {
	Enabled bool `json:"enabled"`
	// Under ThresholdUSD left, AmountUSD is added, never more than LimitUSD in one period.
	ThresholdUSD float64 `json:"threshold_usd"`
	AmountUSD    float64 `json:"amount_usd"`
	LimitUSD     float64 `json:"limit_usd"`
	// AddedThisPeriodUSD is what it has added in the current period.
	AddedThisPeriodUSD float64 `json:"added_this_period_usd"`
	// Failure says why it turned itself off: no_payment_method or card_declined.
	Failure string     `json:"failure,omitempty"`
	Card    *SavedCard `json:"card,omitempty"`
	// Available is false where there is no shared AI balance to add to: no plan in force, or AI is not charged.
	Available bool    `json:"available"`
	MinUSD    float64 `json:"min_usd"`
	MaxUSD    float64 `json:"max_usd"`
}

func (s *Service) aiAutoTopupView(ctx context.Context, a *models.Agency, withCard bool) (*AIAutoTopup, *errx.Error) {
	_, _, planned := s.grantPlan(a)
	out := &AIAutoTopup{
		Enabled: a.AIAutoTopupEnabled, ThresholdUSD: a.AIAutoTopupThreshold.USD(), AmountUSD: a.AIAutoTopupAmount.USD(), LimitUSD: a.AIAutoTopupLimit.USD(),
		Available: planned && s.ai != nil && s.aiMetered,
		MinUSD:    credits.TopUpMin.USD(), MaxUSD: credits.TopUpMax.USD(),
	}
	if a.AIAutoTopupFailure != nil {
		out.Failure = *a.AIAutoTopupFailure
	}
	if s.ai != nil && s.aiMetered {
		added, _, err := s.ai.AutoPurchased(ctx, a.ID, s.aiPeriod(ctx, a).Start)
		if err != nil {
			return nil, internal(err)
		}
		out.AddedThisPeriodUSD = added.USD()
	}
	if withCard && s.billing {
		if card, cerr := s.packs().Card(ctx, a); cerr == nil {
			out.Card = card
		} else {
			errs.CaptureException(fmt.Errorf("agency saved card: %w", cerr))
		}
	}
	return out, nil
}

// AIAutoTopup is the shared AI balance's automatic top-up settings, with the card a top-up would be charged to.
func (s *Service) AIAutoTopup(ctx context.Context, agencyID, userID uuid.UUID) (*AIAutoTopup, *errx.Error) {
	if !s.billing {
		return nil, errBillingDisabled
	}
	a, _, xerr := s.access(ctx, agencyID, userID, false)
	if xerr != nil {
		return nil, xerr
	}
	return s.aiAutoTopupView(ctx, a, true)
}

// SetAIAutoTopup turns the shared AI balance's automatic top-up on or off and sets when it adds money, how much, and the most a period.
func (s *Service) SetAIAutoTopup(ctx context.Context, agencyID, actorID uuid.UUID, enabled bool, threshold, amount, limit models.Micro) (*AIAutoTopup, *errx.Error) {
	if !s.billing || s.ai == nil || !s.aiMetered {
		return nil, errBillingDisabled
	}
	a, _, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return nil, xerr
	}
	invalid := func(msg string) *errx.Error {
		return errx.NewWithIdentifier(errx.BadRequest, "invalid_ai_auto_topup", msg)
	}
	switch {
	case amount < credits.TopUpMin || amount > credits.TopUpMax:
		return nil, invalid("Each top-up must be between $5 and $5,000.")
	case threshold < 0 || threshold > credits.TopUpMax:
		return nil, invalid("The balance to top up at must be between $0 and $5,000.")
	case limit < amount:
		return nil, invalid("The limit for a billing period must be at least one top-up.")
	case limit > models.AgencyAIAutoTopupMaxLimit:
		return nil, invalid("The limit for a billing period can be at most $50,000.")
	}
	var card *SavedCard
	if enabled {
		if _, _, ok := s.grantPlan(a); !ok {
			return nil, errx.NewWithIdentifier(errx.BadRequest, "agency_no_subscription", "Choose an agency plan before turning on automatic top-up.")
		}
		c, err := s.packs().Card(ctx, a)
		if err != nil {
			errs.CaptureException(fmt.Errorf("agency saved card: %w", err))
			return nil, errx.New(errx.Internal, "failed to read the saved card")
		}
		if c == nil {
			return nil, errx.NewWithIdentifier(errx.Conflict, "agency_no_payment_method",
				"This agency has no saved card. Automatic top-up charges the card its subscription is paid with.")
		}
		card = c
	}
	if err := s.repo.SetAIAutoTopup(ctx, agencyID, enabled, threshold, amount, limit); err != nil {
		return nil, internal(err)
	}
	a.AIAutoTopupEnabled, a.AIAutoTopupThreshold, a.AIAutoTopupAmount, a.AIAutoTopupLimit, a.AIAutoTopupFailure = enabled, threshold, amount, limit, nil
	// A balance already under the new threshold is topped up now, not at the next check.
	if enabled {
		s.autoTopupAI(ctx, a)
		if fresh, err := s.repo.Get(ctx, agencyID); err == nil && fresh != nil {
			a = fresh
		}
	}
	view, xerr := s.aiAutoTopupView(ctx, a, false)
	if xerr != nil {
		return nil, xerr
	}
	view.Card = card
	return view, nil
}

// OnAgencyAISpend is called after a client workspace's charge drew on its agency's shared AI balance.
func (s *Service) OnAgencyAISpend(orgID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	a, err := s.repo.GetAgencyForOrganization(ctx, orgID)
	if err != nil || a == nil {
		return
	}
	s.autoTopupAI(ctx, a)
}

// autoTopupAI adds the agency's chosen amount to its shared AI balance when the
// balance is under its threshold and the period's limit has room for it.
// Best-effort; a refusal by the card turns it off and tells the people who manage the agency.
func (s *Service) autoTopupAI(ctx context.Context, a *models.Agency) {
	if !s.billing || s.ai == nil || !s.aiMetered || !a.AIAutoTopupEnabled || a.StripeCustomerID == nil || *a.StripeCustomerID == "" {
		return
	}
	if _, _, ok := s.grantPlan(a); !ok {
		return
	}
	lock := s.switching(a.ID)
	lock.Lock()
	defer lock.Unlock()

	period := s.aiPeriod(ctx, a)
	stored, err := s.ai.Ledger(ctx, a.ID)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	l := stored.In(period)
	balance := l.Allowance + l.Purchased
	if balance >= a.AIAutoTopupThreshold {
		return
	}
	added, count, err := s.ai.AutoPurchased(ctx, a.ID, period.Start)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	amount := a.AIAutoTopupAmount
	if added+amount > a.AIAutoTopupLimit {
		return
	}
	charger := s.packs()
	card, err := charger.Card(ctx, a)
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency AI auto top-up card: %w", err))
		return
	}
	off := func(failure string) {
		if err := s.repo.DisableAIAutoTopup(ctx, a.ID, failure); err != nil {
			errs.CaptureException(err)
			return
		}
		body := "The card on file was declined, so automatic top-up of the AI balance is off. Update the card under Agency > Billing and turn it on again."
		if failure == models.AgencyAutoTopupNoCard {
			body = "There is no saved card to charge, so automatic top-up of the AI balance is off. Add a card under Agency > Billing and turn it on again."
		}
		s.tellManagers(ctx, a, "Automatic top-up could not add AI balance", body, map[string]any{"agency_id": a.ID.String(), "ai_auto_topup": failure})
	}
	if card == nil {
		off(models.AgencyAutoTopupNoCard)
		return
	}
	cents := amount.Cents()
	// One key per top-up of a period, so every replica that reaches this asks Stripe for the same charge.
	key := fmt.Sprintf("agency-ai-autotopup-%s-%d-%d", a.ID, period.Start.Unix(), count+1)
	meta := map[string]string{
		metaPurpose: purposeAgencyAutoTopup, metaAgencyID: a.ID.String(), "kind": "ai", "amount_cents": strconv.FormatInt(cents, 10),
	}
	ref, declined, err := charger.Charge(ctx, a, card, cents, "Agency AI balance (automatic top-up)", key, meta)
	if declined {
		off(models.AgencyAutoTopupDeclined)
		return
	}
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency AI auto top-up %s: %w", a.ID, err))
		return
	}
	granted, err := s.ai.GrantAutoPurchased(ctx, a.ID, models.MicroFromCents(cents), period, ref)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	if granted {
		s.tellManagers(ctx, a, "AI balance was added automatically",
			fmt.Sprintf("Your agency's shared AI balance was down to $%.2f, so $%.2f was added. $%.2f of your $%.2f limit is used this period.",
				balance.USD(), amount.USD(), (added+amount).USD(), a.AIAutoTopupLimit.USD()),
			map[string]any{"agency_id": a.ID.String(), "ai_auto_topup": "bought"})
	}
}
