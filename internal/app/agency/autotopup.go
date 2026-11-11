package agency

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
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
	// CardMissing is true when the saved card was looked up and there is none; a lookup that failed leaves it false.
	CardMissing bool `json:"card_missing,omitempty"`
	// Available is false when the agency has no pool to top up: no plan in force, or a plan with no cap.
	Available bool `json:"available"`
}

// chargeRequest is one automatic top-up to carry to its end: paid, refused or called off.
type chargeRequest struct {
	ClaimID uuid.UUID
	// InvoiceID is the invoice already made for it; empty until Saved has run.
	InvoiceID   string
	Cents       int64
	Description string
	Meta        map[string]string
	// Cancel closes an unpaid invoice without charging it.
	Cancel bool
	// Saved records the invoice the moment it exists, before anything is charged to it.
	Saved func(invoiceID string) error
}

// chargeResult says how a top-up ended. None set means it is still open, with an error saying why.
type chargeResult struct {
	// Ref is the payment a refund would name, or the invoice when no card payment was taken.
	Ref       string
	Paid      bool
	Declined  bool
	Cancelled bool
}

// packCharger charges an automatic top-up to an agency's saved card.
type packCharger interface {
	// Card is the agency's saved card; nil when it has none.
	Card(ctx context.Context, a *models.Agency) (*SavedCard, error)
	// Charge finds or makes the top-up's one invoice and pays it. Asked again for the same top-up it answers
	// from that invoice, so nothing is charged twice. Declined is only reported once the invoice is closed.
	Charge(ctx context.Context, a *models.Agency, card *SavedCard, req chargeRequest) (chargeResult, error)
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
	bought, _, err := s.repo.AutoTopupUsed(ctx, a.ID, models.AgencyAutoTopupSends, start)
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
			out.Card, out.CardMissing = card, card == nil
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
	}
	if err := s.repo.SetAutoTopup(ctx, agencyID, enabled, threshold, maxPacks); err != nil {
		return nil, internal(err)
	}
	// Turned on, a pool already under the threshold is topped up now; turned off, a top-up still in flight is called off.
	s.autoTopup(ctx, a)
	fresh, err := s.repo.Get(ctx, agencyID)
	if err != nil {
		return nil, internal(err)
	}
	if fresh == nil {
		return nil, errNotFound
	}
	s.hydrate(ctx, fresh)
	// Read with the card, so the page that keeps this answer still knows whether one is on file.
	return s.autoTopupView(ctx, fresh, true)
}

// autoTopup buys one pack when the agency's pool is at or under its threshold
// and the period has packs left to buy. One pack a call: the next sweep buys
// another if the pool is still low. Best-effort; a refusal by the card turns
// it off and tells the people who manage the agency.
func (s *Service) autoTopup(ctx context.Context, a *models.Agency) {
	s.runAutoTopup(ctx, a.ID, models.AgencyAutoTopupSends)
}

// autoTopupAI adds the agency's chosen amount to its shared AI balance when the
// balance is under its threshold and the period's limit has room for it.
func (s *Service) autoTopupAI(ctx context.Context, a *models.Agency) {
	s.runAutoTopup(ctx, a.ID, models.AgencyAutoTopupAI)
}

// autoTopupDue is what one kind of automatic top-up would buy now.
type autoTopupDue struct {
	period time.Time
	cents  int64
	sends  int
	// level is what is left: sends in the pool, or micro-dollars of AI balance.
	level int64
}

func autoTopupOn(a *models.Agency, kind models.AgencyAutoTopupKind) bool {
	if kind == models.AgencyAutoTopupAI {
		return a.AIAutoTopupEnabled
	}
	return a.AutoTopupEnabled
}

func (s *Service) runAutoTopup(ctx context.Context, agencyID uuid.UUID, kind models.AgencyAutoTopupKind) {
	if !s.billing || (kind == models.AgencyAutoTopupAI && (s.ai == nil || !s.aiMetered)) {
		return
	}
	lock := s.switching(agencyID)
	lock.Lock()
	defer lock.Unlock()

	// Read under the lock, so a setting saved a moment ago is the one acted on.
	a, err := s.repo.Get(ctx, agencyID)
	if err != nil || a == nil {
		return
	}
	s.hydrate(ctx, a)
	if a.StripeCustomerID == nil || *a.StripeCustomerID == "" {
		return
	}
	// A top-up already in flight is carried to its end before another is considered.
	open, err := s.resumeAutoTopups(ctx, a, kind)
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency auto top-up %s: %w", a.ID, err))
		return
	}
	if open || !autoTopupOn(a, kind) {
		return
	}
	due, ok := s.autoTopupDue(ctx, a, kind)
	if !ok {
		return
	}
	card, err := s.packs().Card(ctx, a)
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency auto top-up card: %w", err))
		return
	}
	if card == nil {
		s.autoTopupOff(ctx, a, kind, models.AgencyAutoTopupNoCard)
		return
	}
	// The claim is the decision: it reads the stored settings and the period's limit as it is written.
	claim, err := s.repo.ClaimAutoTopup(ctx, a.ID, kind, due.period, due.cents, due.sends, due.level)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	if claim == nil {
		return
	}
	if err := s.settleAutoTopup(ctx, a, claim, card); err != nil {
		errs.CaptureException(fmt.Errorf("agency auto top-up %s: %w", a.ID, err))
	}
}

// autoTopupDue reports whether a top-up is called for; the claim checks the limits again as it is written.
func (s *Service) autoTopupDue(ctx context.Context, a *models.Agency, kind models.AgencyAutoTopupKind) (autoTopupDue, bool) {
	if kind == models.AgencyAutoTopupAI {
		if _, _, ok := s.grantPlan(a); !ok {
			return autoTopupDue{}, false
		}
		period := s.aiPeriod(ctx, a)
		stored, err := s.ai.Ledger(ctx, a.ID)
		if err != nil {
			errs.CaptureException(err)
			return autoTopupDue{}, false
		}
		l := stored.In(period)
		balance := l.Allowance + l.Purchased
		if balance >= a.AIAutoTopupThreshold {
			return autoTopupDue{}, false
		}
		return autoTopupDue{period: period.Start, cents: a.AIAutoTopupAmount.Cents(), level: int64(balance)}, true
	}
	if _, unlimited, active := s.poolSize(ctx, a); unlimited || !active {
		return autoTopupDue{}, false
	}
	s.forgetAgency(a.ID)
	u, xerr := s.usage(ctx, a)
	if xerr != nil || u.Remaining > int64(a.AutoTopupThreshold) {
		return autoTopupDue{}, false
	}
	return autoTopupDue{
		period: u.PeriodStart, cents: models.AgencyTopupPriceCents(a.EffectivePlanID(s.now())),
		sends: models.AgencyTopupSends, level: u.Remaining,
	}, true
}

// resumeAutoTopups carries the kind's top-ups still in flight to their end; open reports one that could not be ended yet.
func (s *Service) resumeAutoTopups(ctx context.Context, a *models.Agency, kind models.AgencyAutoTopupKind) (open bool, err error) {
	pending, err := s.repo.PendingAutoTopups(ctx, a.ID)
	if err != nil {
		return false, err
	}
	for i := range pending {
		if pending[i].Kind != kind {
			continue
		}
		if err := s.settleAutoTopup(ctx, a, &pending[i], nil); err != nil {
			errs.CaptureException(fmt.Errorf("agency auto top-up %s: %w", a.ID, err))
			open = true
			s.tellStuckAutoTopup(ctx, a, &pending[i])
		}
	}
	return open, nil
}

// tellStuckAutoTopup tells the managers, once, about a top-up that has stayed unsettled. It is never
// called off on a guess: whether it was paid is not known, and it blocks another until it is.
func (s *Service) tellStuckAutoTopup(ctx context.Context, a *models.Agency, c *models.AgencyAutoTopupClaim) {
	if s.now().Sub(c.CreatedAt) < models.AgencyAutoTopupStuckAfter {
		return
	}
	first, err := s.repo.MarkAutoTopupStuck(ctx, c.ID)
	if err != nil || !first {
		return
	}
	what := "sends"
	if c.Kind == models.AgencyAutoTopupAI {
		what = "AI balance"
	}
	told := s.tellManagers(ctx, a, "An automatic top-up could not be confirmed",
		fmt.Sprintf("We could not confirm with the payment provider whether an automatic top-up of %s for $%.2f went through. "+
			"No further automatic top-up of %s is made until it is settled, and it is still being checked. "+
			"If it was paid, what it bought is added as soon as that is confirmed. You can add %s yourself under Agency > Billing meanwhile.",
			what, float64(c.AmountCents)/100, what, what),
		map[string]any{"agency_id": a.ID.String(), "auto_topup": "unconfirmed", "kind": string(c.Kind)})
	// Nobody could be told this time, so the next pass tells them.
	if !told {
		if err := s.repo.ClearAutoTopupStuck(ctx, c.ID); err != nil {
			errs.CaptureException(err)
		}
	}
}

// settleAutoTopup carries one claimed top-up to its end. Money already taken is always honoured;
// an unpaid one is called off when the setting was turned off or the card is gone.
func (s *Service) settleAutoTopup(ctx context.Context, a *models.Agency, c *models.AgencyAutoTopupClaim, card *SavedCard) error {
	charger := s.packs()
	cancel := !autoTopupOn(a, c.Kind)
	if card == nil && !cancel {
		found, err := charger.Card(ctx, a)
		if err != nil {
			return fmt.Errorf("saved card: %w", err)
		}
		card = found
	}
	noCard := card == nil && !cancel
	description := "Agency send pack: 50,000 sends (automatic top-up)"
	if c.Kind == models.AgencyAutoTopupAI {
		description = "Agency AI balance (automatic top-up)"
	}
	res, err := charger.Charge(ctx, a, card, chargeRequest{
		ClaimID: c.ID, InvoiceID: c.StripeInvoiceID, Cents: c.AmountCents, Description: description,
		Meta: map[string]string{
			metaPurpose: purposeAgencyAutoTopup, metaAgencyID: a.ID.String(), "claim_id": c.ID.String(), "kind": string(c.Kind),
			"sends": strconv.Itoa(c.Sends), "amount_cents": strconv.FormatInt(c.AmountCents, 10),
		},
		Cancel: cancel || noCard,
		Saved: func(invoiceID string) error {
			c.StripeInvoiceID = invoiceID
			return s.repo.SetAutoTopupInvoice(ctx, c.ID, invoiceID)
		},
	})
	if err != nil {
		return err
	}
	switch {
	case res.Paid:
		return s.fulfilAutoTopup(ctx, a, c, res.Ref)
	case res.Declined:
		done, err := s.repo.FinishAutoTopup(ctx, c.ID, models.AgencyAutoTopupWasRefused, "")
		if err != nil {
			return err
		}
		if done {
			s.autoTopupOff(ctx, a, c.Kind, models.AgencyAutoTopupDeclined)
		}
	case res.Cancelled:
		if _, err := s.repo.FinishAutoTopup(ctx, c.ID, models.AgencyAutoTopupCancelled, ""); err != nil {
			return err
		}
		if noCard {
			s.autoTopupOff(ctx, a, c.Kind, models.AgencyAutoTopupNoCard)
		}
	default:
		return errors.New("the top-up is not settled yet")
	}
	return nil
}

// fulfilAutoTopup adds what a paid top-up bought, once per payment, and says so.
func (s *Service) fulfilAutoTopup(ctx context.Context, a *models.Agency, c *models.AgencyAutoTopupClaim, ref string) error {
	var added bool
	var title, body string
	if c.Kind == models.AgencyAutoTopupAI {
		if s.ai == nil {
			return errors.New("no AI balance to add a paid top-up to")
		}
		amount := models.MicroFromCents(c.AmountCents)
		granted, err := s.ai.GrantAutoPurchased(ctx, a.ID, amount, s.aiPeriod(ctx, a), ref)
		if err != nil {
			return err
		}
		added = granted
		_, usedCents, _ := s.repo.AutoTopupUsed(ctx, a.ID, c.Kind, c.PeriodStart)
		title = "AI balance was added automatically"
		body = fmt.Sprintf("Your agency's shared AI balance ran low, so $%.2f was added on the saved card. $%.2f of your $%.2f limit is used this period.",
			amount.USD(), float64(usedCents)/100, a.AIAutoTopupLimit.USD())
	} else {
		// Sends that were paid for land in the period that is running now.
		start, _ := models.AgencyPeriod(a.CurrentPeriodStart, a.CurrentPeriodEnd, s.now())
		reason := "auto_topup"
		recorded, err := s.repo.RecordTopup(ctx, &models.AgencyPoolTopup{
			AgencyID: a.ID, Sends: c.Sends, AmountCents: c.AmountCents, StripePaymentIntentID: &ref,
			PeriodStart: start, Reason: &reason, Automatic: true,
		})
		if err != nil {
			return err
		}
		added = recorded
		s.forgetAgency(a.ID)
		bought, _, _ := s.repo.AutoTopupUsed(ctx, a.ID, c.Kind, c.PeriodStart)
		title = "A top-up pack was added automatically"
		body = fmt.Sprintf("Your agency's send pool ran low, so %d sends were added for $%.2f on the saved card. Pack %d of %d this period.",
			c.Sends, float64(c.AmountCents)/100, bought, a.AutoTopupMaxPacks)
	}
	if _, err := s.repo.FinishAutoTopup(ctx, c.ID, models.AgencyAutoTopupPaid, ref); err != nil {
		return err
	}
	if added {
		s.tellManagers(ctx, a, title, body, map[string]any{"agency_id": a.ID.String(), "auto_topup": "bought", "kind": string(c.Kind)})
	}
	return nil
}

// SettleAutoTopupInvoice settles the automatic top-up an invoice belongs to, for a payment that
// completed after the request that asked for it had ended.
func (s *Service) SettleAutoTopupInvoice(ctx context.Context, invoiceID string) error {
	c, err := s.repo.AutoTopupByInvoice(ctx, invoiceID)
	if err != nil || c == nil || c.Status != models.AgencyAutoTopupPending {
		return err
	}
	lock := s.switching(c.AgencyID)
	lock.Lock()
	defer lock.Unlock()
	a, err := s.repo.Get(ctx, c.AgencyID)
	if err != nil || a == nil {
		return err
	}
	s.hydrate(ctx, a)
	return s.settleAutoTopup(ctx, a, c, nil)
}

// autoTopupOff turns an automatic top-up off after a failure and says so.
func (s *Service) autoTopupOff(ctx context.Context, a *models.Agency, kind models.AgencyAutoTopupKind, failure string) {
	what, title, disable := "automatic top-up", "Automatic top-up could not add sends", s.repo.DisableAutoTopup
	if kind == models.AgencyAutoTopupAI {
		what, title, disable = "automatic top-up of the AI balance", "Automatic top-up could not add AI balance", s.repo.DisableAIAutoTopup
	}
	if err := disable(ctx, a.ID, failure); err != nil {
		errs.CaptureException(err)
		return
	}
	body := fmt.Sprintf("The card on file was declined, so %s is off and nothing was charged. Update the card under Agency > Billing and turn it on again.", what)
	if failure == models.AgencyAutoTopupNoCard {
		body = fmt.Sprintf("There is no saved card to charge, so %s is off. Add a card under Agency > Billing and turn it on again.", what)
	}
	s.tellManagers(ctx, a, title, body, map[string]any{"agency_id": a.ID.String(), "auto_topup": failure, "kind": string(kind)})
}

// tellManagers notifies the agency's owner and admins, in a client workspace when there is one to address it to.
// It reports false when the people to tell could not be read, so a caller that tells them once can try again.
func (s *Service) tellManagers(ctx context.Context, a *models.Agency, title, body string, meta map[string]any) bool {
	if s.notifier == nil {
		return true
	}
	staff, err := s.repo.ListMembers(ctx, a.ID)
	if err != nil {
		errs.CaptureException(err)
		return false
	}
	var orgID *uuid.UUID
	if clients, err := s.repo.ListClientOrganizations(ctx, a.ID); err == nil && len(clients) > 0 {
		orgID = &clients[0].OrganizationID
	}
	for _, m := range staff {
		if m.Role.CanManage() {
			s.notifier.Notify(ctx, m.UserID, orgID, models.NotifBillingAlert, title, body, "/app/agency/settings/billing", meta)
		}
	}
	return true
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

// invoiceFor is the top-up's one invoice: the one recorded, else the one carrying its claim, else a new one.
func (stripeCharger) invoiceFor(ctx context.Context, a *models.Agency, req chargeRequest) (*stripe.Invoice, error) {
	if req.InvoiceID != "" {
		inv, err := invoice.Get(req.InvoiceID, &stripe.InvoiceParams{Params: stripe.Params{Context: ctx}})
		var serr *stripe.Error
		if errors.As(err, &serr) && serr.Code == stripe.ErrorCodeResourceMissing {
			// Only a draft can be deleted, and a draft was never charged: the top-up is over, unpaid.
			return nil, nil
		}
		return inv, err
	}
	// An invoice made by a request whose answer was lost is found by its claim, so it is never made twice.
	lp := &stripe.InvoiceListParams{Customer: a.StripeCustomerID}
	lp.Context = ctx
	lp.Limit = stripe.Int64(100)
	it := invoice.List(lp)
	for seen := 0; seen < 100 && it.Next(); seen++ {
		if inv := it.Invoice(); inv.Metadata["claim_id"] == req.ClaimID.String() {
			return inv, nil
		}
	}
	if err := it.Err(); err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	if req.Cancel {
		return nil, nil
	}
	ip := &stripe.InvoiceParams{
		Params:                      stripe.Params{Context: ctx},
		Customer:                    a.StripeCustomerID,
		CollectionMethod:            stripe.String("charge_automatically"),
		AutoAdvance:                 stripe.Bool(false),
		PendingInvoiceItemsBehavior: stripe.String("exclude"),
		AutomaticTax:                &stripe.InvoiceAutomaticTaxParams{Enabled: stripe.Bool(false)},
		Description:                 stripe.String(req.Description),
		Metadata:                    req.Meta,
	}
	ip.SetIdempotencyKey("agency-autotopup-" + req.ClaimID.String() + "-invoice")
	inv, err := invoice.New(ip)
	if err != nil {
		return nil, fmt.Errorf("create invoice: %w", err)
	}
	return inv, nil
}

func (c stripeCharger) Charge(ctx context.Context, a *models.Agency, card *SavedCard, req chargeRequest) (chargeResult, error) {
	inv, err := c.invoiceFor(ctx, a, req)
	if err != nil {
		return chargeResult{}, err
	}
	if inv == nil {
		return chargeResult{Cancelled: true}, nil
	}
	// Recorded before anything is charged to it, so a payment is always found again.
	if req.InvoiceID == "" {
		if err := req.Saved(inv.ID); err != nil {
			return chargeResult{}, fmt.Errorf("record invoice: %w", err)
		}
	}
	if inv.Status == stripe.InvoiceStatusDraft {
		if req.Cancel {
			if _, err := invoice.Del(inv.ID, &stripe.InvoiceParams{Params: stripe.Params{Context: ctx}}); err != nil {
				return chargeResult{}, fmt.Errorf("delete draft invoice: %w", err)
			}
			return chargeResult{Cancelled: true}, nil
		}
		if inv.Total == 0 {
			item := &stripe.InvoiceItemParams{
				Params: stripe.Params{Context: ctx}, Customer: a.StripeCustomerID, Invoice: stripe.String(inv.ID),
				Amount: stripe.Int64(req.Cents), Currency: stripe.String("usd"), Description: stripe.String(req.Description),
			}
			item.SetIdempotencyKey("agency-autotopup-" + req.ClaimID.String() + "-item")
			if _, err := invoiceitem.New(item); err != nil {
				return chargeResult{}, fmt.Errorf("add invoice line: %w", err)
			}
		}
		fin := &stripe.InvoiceFinalizeInvoiceParams{Params: stripe.Params{Context: ctx}, AutoAdvance: stripe.Bool(false)}
		if inv, err = invoice.FinalizeInvoice(inv.ID, fin); err != nil {
			return chargeResult{}, fmt.Errorf("finalize invoice: %w", err)
		}
	}
	void := func() error {
		_, err := invoice.VoidInvoice(inv.ID, &stripe.InvoiceVoidInvoiceParams{Params: stripe.Params{Context: ctx}})
		return err
	}
	switch inv.Status {
	case stripe.InvoiceStatusVoid, stripe.InvoiceStatusUncollectible:
		// Closed unpaid: by a refusal, unless it was being called off.
		return chargeResult{Cancelled: req.Cancel, Declined: !req.Cancel}, nil
	case stripe.InvoiceStatusOpen:
		if req.Cancel || card == nil {
			if err := void(); err != nil {
				return chargeResult{}, fmt.Errorf("void invoice: %w", err)
			}
			return chargeResult{Cancelled: true}, nil
		}
		// An invoice is paid once whoever asks, so a repeat of this call cannot charge it again.
		pay := &stripe.InvoicePayParams{Params: stripe.Params{Context: ctx}, OffSession: stripe.Bool(true), PaymentMethod: stripe.String(card.ID)}
		paid, err := invoice.Pay(inv.ID, pay)
		var serr *stripe.Error
		if errors.As(err, &serr) && serr.Type == stripe.ErrorTypeCard {
			// A refusal is only final once its invoice is closed; until then the top-up stays open and is tried again.
			if verr := void(); verr != nil {
				return chargeResult{}, fmt.Errorf("void refused invoice: %w", verr)
			}
			return chargeResult{Declined: true}, nil
		}
		if err != nil {
			return chargeResult{}, fmt.Errorf("pay invoice: %w", err)
		}
		if paid == nil || paid.Status != stripe.InvoiceStatusPaid {
			return chargeResult{}, errors.New("the invoice is not paid yet")
		}
	case stripe.InvoiceStatusPaid:
	default:
		return chargeResult{}, fmt.Errorf("unexpected invoice status %q", inv.Status)
	}
	// The payment intent is what a refund names. An invoice settled with no card payment has none, and no refund to find.
	gp := &stripe.InvoiceParams{Params: stripe.Params{Context: ctx}}
	gp.AddExpand("payments")
	full, err := invoice.Get(inv.ID, gp)
	if err != nil {
		return chargeResult{}, fmt.Errorf("read invoice payment: %w", err)
	}
	if full.Payments != nil {
		for _, p := range full.Payments.Data {
			if p.Payment != nil && p.Payment.PaymentIntent != nil && p.Payment.PaymentIntent.ID != "" {
				return chargeResult{Paid: true, Ref: p.Payment.PaymentIntent.ID}, nil
			}
		}
	}
	return chargeResult{Paid: true, Ref: inv.ID}, nil
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
	// CardMissing is true when the saved card was looked up and there is none; a lookup that failed leaves it false.
	CardMissing bool `json:"card_missing,omitempty"`
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
		_, cents, err := s.repo.AutoTopupUsed(ctx, a.ID, models.AgencyAutoTopupAI, s.aiPeriod(ctx, a).Start)
		if err != nil {
			return nil, internal(err)
		}
		out.AddedThisPeriodUSD = float64(cents) / 100
	}
	if withCard && s.billing {
		if card, cerr := s.packs().Card(ctx, a); cerr == nil {
			out.Card, out.CardMissing = card, card == nil
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
	}
	if err := s.repo.SetAIAutoTopup(ctx, agencyID, enabled, threshold, amount, limit); err != nil {
		return nil, internal(err)
	}
	// Turned on, a balance already under the threshold is topped up now; turned off, a top-up still in flight is called off.
	s.autoTopupAI(ctx, a)
	fresh, err := s.repo.Get(ctx, agencyID)
	if err != nil {
		return nil, internal(err)
	}
	if fresh == nil {
		return nil, errNotFound
	}
	s.hydrate(ctx, fresh)
	// Read with the card, so the page that keeps this answer still knows whether one is on file.
	return s.aiAutoTopupView(ctx, fresh, true)
}

// aiCheck is one agency's AI balance check: at most one running and one waiting, however many charges arrive.
type aiCheck struct {
	mu             sync.Mutex
	running, again bool
}

// OnAgencyAISpend is called after a client workspace's charge drew on its agency's shared AI balance.
func (s *Service) OnAgencyAISpend(orgID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	a, err := s.repo.GetAgencyForOrganization(ctx, orgID)
	cancel()
	if err != nil || a == nil || !a.AIAutoTopupEnabled {
		return
	}
	state, _ := s.aiChecks.LoadOrStore(a.ID, &aiCheck{})
	c := state.(*aiCheck)
	c.mu.Lock()
	if c.running {
		// The check under way may have read the balance before this charge, so one more follows it.
		c.again = true
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		s.runAutoTopup(ctx, a.ID, models.AgencyAutoTopupAI)
		cancel()
		c.mu.Lock()
		if !c.again {
			c.running = false
			c.mu.Unlock()
			return
		}
		c.again = false
		c.mu.Unlock()
	}
}
