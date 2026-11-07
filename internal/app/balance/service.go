// Package balance adds money to a workspace's balance on its saved card: the
// automatic reload when the balance falls under the workspace's threshold, and
// a one-click top-up. It also sends the low and empty balance notices. It hangs
// off the credits monitor hook and a sweep, so the credits package itself never
// depends on Stripe.
package balance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/stripe"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

var (
	// ErrNoPaymentMethod means the workspace has no saved card to charge.
	ErrNoPaymentMethod = errors.New("no saved payment method")
	// ErrChargeInFlight means another charge has not settled yet.
	ErrChargeInFlight = repository.ErrChargeInFlight
)

// DeclinedError is a card refusal on a one-click top-up; the buyer can pay
// another way.
type DeclinedError struct{ Code string }

func (e *DeclinedError) Error() string { return "card declined: " + e.Code }

// After a soft decline the reload is retried this long after each attempt.
var retryAfter = []time.Duration{time.Hour, 6 * time.Hour, 18 * time.Hour}

const (
	opTimeout = 45 * time.Second
	lockTTL   = 2 * time.Minute
	lockKey   = "balance:charge:"
	// staleAfter is how long a pending charge may sit untouched (a crash
	// between steps) before the sweep resumes it.
	staleAfter = 5 * time.Minute
	// maxTries bounds the payment attempts on one charge, failed Stripe calls included.
	maxTries = 8
	// legacyPendingFor is how long a credit-pack charge the previous release started may stay pending.
	legacyPendingFor = 24 * time.Hour
)

// Card is the slice of the Stripe service a charge needs.
type Card interface {
	ChargeSavedCard(ctx context.Context, c *models.SavedCardCharge, tries int, offSession bool) (*stripe.SavedCardResult, error)
	VoidChargeInvoice(ctx context.Context, invoiceID string)
	DefaultPaymentMethod(ctx context.Context, customerID string) (*stripe.PaymentMethodSummary, error)
}

// Notifier tells the people who manage billing.
type Notifier interface {
	NotifyOrg(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, exclude uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string)
}

// AuditLogger fires AUDIT_CREATED so every teammate's billing views refresh.
type AuditLogger interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ip, userAgent string, changes, metadata map[string]string)
}

type Service struct {
	credits   credits.CreditService
	settings  repository.AISettingsRepository
	charges   repository.SavedCardChargeRepository
	subs      repository.SubscriptionRepository
	card      Card
	cache     *cache.Cache
	publisher *pubsub.StreamingPublisher
	notifier  Notifier
	audit     AuditLogger
}

// New builds the service. card nil (a process without Stripe, or billing
// off) leaves charging to whichever process has it; notices still go out.
func New(cr credits.CreditService, settings repository.AISettingsRepository, charges repository.SavedCardChargeRepository,
	subs repository.SubscriptionRepository, card Card, c *cache.Cache, publisher *pubsub.StreamingPublisher, notifier Notifier, audit AuditLogger) *Service {
	return &Service{credits: cr, settings: settings, charges: charges, subs: subs, card: card, cache: c, publisher: publisher, notifier: notifier, audit: audit}
}

// WireNotifier attaches the notifier once it exists (post-construction).
func (s *Service) WireNotifier(n Notifier) { s.notifier = n }

func monthStart(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func (s *Service) config(ctx context.Context, orgID uuid.UUID) *models.AISpendSettings {
	if cfg, err := s.settings.Get(ctx, orgID); err == nil && cfg != nil {
		return cfg
	}
	return credits.DefaultSpendSettings(orgID)
}

// OnBalanceChanged is the credits monitor hook, called on a detached goroutine
// after every fresh debit. Best-effort throughout.
func (s *Service) OnBalanceChanged(orgID uuid.UUID, before, after models.Micro) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	s.publisher.PublishCreditsChanged(ctx, orgID, after)

	cfg := s.config(ctx, orgID)
	if cfg.AutoReloadEnabled && after < cfg.AutoReloadThreshold {
		used, err := s.charges.ReloadedSince(ctx, orgID, monthStart(time.Now()))
		if err == nil && cfg.AutoReloadMonthlyMax-used >= credits.TopUpMin {
			if s.card != nil {
				if err := s.Reload(ctx, orgID); err != nil {
					log.Warn().Err(err).Str("org_id", orgID.String()).Msg("automatic reload failed")
				}
			}
			// The reload covers it (or the sweep starts it): no notice.
			return
		}
	}
	switch {
	case before >= credits.MinBalance && after < credits.MinBalance:
		s.notify(ctx, orgID, "out", after, cfg)
	case before >= cfg.LowBalanceThreshold && after < cfg.LowBalanceThreshold:
		s.notify(ctx, orgID, "low", after, cfg)
	}
}

func (s *Service) notify(ctx context.Context, orgID uuid.UUID, kind string, balance models.Micro, cfg *models.AISpendSettings) {
	won, err := s.settings.StampNotice(ctx, orgID, kind, monthStart(time.Now()))
	if err != nil || !won {
		return
	}
	s.publisher.PublishCreditsLow(ctx, orgID, balance, cfg.LowBalanceThreshold, kind == "out")
	if s.notifier == nil {
		return
	}
	title, body := "Your balance is running low", fmt.Sprintf("$%.2f is left. AI and data features stop when it runs out. Add money or turn on automatic reload.", balance.USD())
	if kind == "out" {
		title, body = "Your balance has run out", "AI and data features are paused until money is added. Add money or turn on automatic reload."
	}
	s.notifier.NotifyOrg(ctx, orgID, models.PermManageBilling, uuid.Nil, models.NotifBillingAlert, title, body,
		"/app/settings/billing/balance", map[string]any{"balance_usd": balance.USD()}, "balance_"+kind)
}

func (s *Service) lock(ctx context.Context, orgID uuid.UUID) (func(), bool) {
	if s.cache == nil {
		return func() {}, true
	}
	key := lockKey + orgID.String()
	ok, err := s.cache.SetNX(ctx, key, "1", lockTTL).Result()
	if err != nil || !ok {
		return nil, false
	}
	return func() { s.cache.Del(context.WithoutCancel(ctx), key) }, true
}

// Reload starts, or resumes, an automatic reload for a workspace whose
// balance is under its threshold. A month near its limit reloads only what
// the limit has left, and nothing under the minimum top-up.
func (s *Service) Reload(ctx context.Context, orgID uuid.UUID) error {
	if s.card == nil {
		return nil
	}
	unlock, ok := s.lock(ctx, orgID)
	if !ok {
		return nil
	}
	defer unlock()

	if pending, err := s.charges.Pending(ctx, orgID); err != nil {
		return err
	} else if pending != nil {
		if pending.Legacy || (pending.RetryAt != nil && pending.RetryAt.After(time.Now())) {
			return nil
		}
		return s.run(ctx, pending, true)
	}
	cfg := s.config(ctx, orgID)
	if !cfg.AutoReloadEnabled {
		return nil
	}
	bal, xerr := s.credits.GetBalance(ctx, orgID)
	if xerr != nil {
		return xerr
	}
	if bal >= cfg.AutoReloadThreshold {
		return nil
	}
	used, err := s.charges.ReloadedSince(ctx, orgID, monthStart(time.Now()))
	if err != nil {
		return err
	}
	amount := min(cfg.AutoReloadAmount, cfg.AutoReloadMonthlyMax-used)
	if amount < credits.TopUpMin {
		return nil
	}
	customerID, pm, err := s.paymentMethod(ctx, orgID)
	if err != nil {
		return err
	}
	if pm == nil {
		s.disable(ctx, orgID, models.ReloadNoPaymentMethod)
		return nil
	}
	c, err := s.charges.Create(ctx, &models.SavedCardCharge{
		OrgID: orgID, Kind: models.SavedCardAutoReload, Amount: amount, AmountCents: amount.Cents(),
		Currency: "usd", CustomerID: customerID, PaymentMethodID: pm.ID,
	})
	if errors.Is(err, repository.ErrChargeInFlight) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.run(ctx, c, true)
}

func (s *Service) paymentMethod(ctx context.Context, orgID uuid.UUID) (string, *stripe.PaymentMethodSummary, error) {
	sub, err := s.subs.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return "", nil, err
	}
	if sub == nil || sub.StripeCustomerID == "" {
		return "", nil, nil
	}
	pm, err := s.card.DefaultPaymentMethod(ctx, sub.StripeCustomerID)
	return sub.StripeCustomerID, pm, err
}

// run makes one payment attempt on a pending charge and acts on how it ended.
func (s *Service) run(ctx context.Context, c *models.SavedCardCharge, offSession bool) error {
	if c.Kind == models.SavedCardAutoReload {
		// An automatic reload is charged only while reload is on and the balance under its threshold.
		if wanted, err := s.reloadWanted(ctx, c.OrgID); err != nil {
			return err
		} else if !wanted {
			return s.cancel(ctx, c)
		}
		// A card changed while a soft decline waited is the one to retry on.
		if c.Tries > 0 {
			if _, pm, err := s.paymentMethod(ctx, c.OrgID); err == nil && pm != nil && pm.ID != c.PaymentMethodID {
				if err := s.charges.SetPaymentMethod(ctx, c.ID, pm.ID); err == nil {
					c.PaymentMethodID = pm.ID
				}
			}
		}
	}
	tries, err := s.charges.BumpTries(ctx, c.ID)
	if err != nil {
		return err
	}
	res, err := s.card.ChargeSavedCard(ctx, c, tries, offSession)
	if err != nil && tries < maxTries {
		return err // left pending; the sweep resumes it
	}
	if err != nil {
		return s.giveUp(ctx, c, err)
	}
	c.InvoiceID = res.InvoiceID
	switch {
	case res.Paid:
		return s.settle(ctx, c, res.InvoiceID)
	case res.Declined != "" && res.Soft && c.Kind == models.SavedCardAutoReload && tries <= len(retryAfter):
		at := time.Now().Add(retryAfter[tries-1])
		if err := s.charges.Snooze(ctx, c.ID, at, res.Declined); err != nil {
			return err
		}
		s.changed(ctx, c.OrgID)
		return nil
	case res.Declined != "":
		s.card.VoidChargeInvoice(ctx, res.InvoiceID)
		if err := s.charges.MarkFailed(ctx, c.ID, res.Declined); err != nil {
			return err
		}
		if c.Kind == models.SavedCardAutoReload {
			s.disable(ctx, c.OrgID, res.Declined)
		}
		s.changed(ctx, c.OrgID)
		return &DeclinedError{Code: res.Declined}
	}
	// Open but not yet paid (a slow card network): invoice.paid settles it.
	return nil
}

func (s *Service) reloadWanted(ctx context.Context, orgID uuid.UUID) (bool, error) {
	cfg := s.config(ctx, orgID)
	if !cfg.AutoReloadEnabled {
		return false, nil
	}
	bal, xerr := s.credits.GetBalance(ctx, orgID)
	if xerr != nil {
		return false, xerr
	}
	return bal < cfg.AutoReloadThreshold, nil
}

// cancel closes a pending charge nothing should collect any more.
func (s *Service) cancel(ctx context.Context, c *models.SavedCardCharge) error {
	if s.card != nil {
		s.card.VoidChargeInvoice(ctx, c.InvoiceID)
	}
	if err := s.charges.MarkFailed(ctx, c.ID, models.ChargeCanceled); err != nil {
		return err
	}
	s.changed(ctx, c.OrgID)
	return nil
}

// cancelPendingReload closes the workspace's pending automatic reload, if any.
func (s *Service) cancelPendingReload(ctx context.Context, orgID uuid.UUID) {
	c, err := s.charges.Pending(ctx, orgID)
	if err != nil || c == nil || c.Legacy || c.Kind != models.SavedCardAutoReload {
		return
	}
	if err := s.cancel(ctx, c); err != nil {
		log.Warn().Err(err).Str("charge_id", c.ID.String()).Msg("cancel pending automatic reload")
	}
}

// giveUp fails a charge whose Stripe calls kept failing for maxTries attempts.
func (s *Service) giveUp(ctx context.Context, c *models.SavedCardCharge, cause error) error {
	if cur, err := s.charges.Get(ctx, c.ID); err == nil && cur != nil {
		c.InvoiceID = cur.InvoiceID
	}
	s.card.VoidChargeInvoice(ctx, c.InvoiceID)
	if err := s.charges.MarkFailed(ctx, c.ID, models.ReloadUnavailable); err != nil {
		return err
	}
	if c.Kind == models.SavedCardAutoReload {
		s.disable(ctx, c.OrgID, models.ReloadUnavailable)
	}
	s.changed(ctx, c.OrgID)
	return fmt.Errorf("charge %s failed after %d tries: %w", c.ID, maxTries, cause)
}

// disable turns automatic reload off after a charge it cannot recover from,
// so nothing else is charged, and tells whoever manages billing.
func (s *Service) disable(ctx context.Context, orgID uuid.UUID, code string) {
	s.cancelPendingReload(ctx, orgID)
	turnedOff, err := s.settings.DisableAutoReload(ctx, orgID, code)
	if err != nil || !turnedOff {
		return
	}
	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, uuid.Nil, models.AuditActionUpdate, models.AuditEntityCreditGrant, nil, "", "", nil, map[string]string{
			"auto_reload": "off", "reason": code,
		})
	}
	if s.notifier != nil {
		s.notifier.NotifyOrg(ctx, orgID, models.PermManageBilling, uuid.Nil, models.NotifBillingAlert,
			"Automatic reload is off", reloadFailureCopy(code), "/app/settings/billing/balance", map[string]any{"reason": code}, "balance_reload_failed")
	}
}

func reloadFailureCopy(code string) string {
	switch code {
	case models.ReloadAuthenticationRequired:
		return "Your bank asked to confirm the last automatic reload, which can't happen without you. Automatic reload is off, so nothing else is charged. Add money once to confirm the card, then turn it back on."
	case models.ReloadNoPaymentMethod:
		return "There is no saved card to charge. Automatic reload is off. Add a card, then turn it back on."
	case models.ReloadCardDeclined:
		return "The card on file was declined. Automatic reload is off, so nothing else is charged. Update the card, then turn it back on."
	}
	return "The last automatic reload could not be charged. Automatic reload is off, so nothing else is charged. Check the card, then turn it back on."
}

// SettleChargeInvoice settles a charge whose invoice Stripe reports paid.
func (s *Service) SettleChargeInvoice(ctx context.Context, chargeID uuid.UUID, invoiceID string) error {
	c, err := s.charges.Get(ctx, chargeID)
	if err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("unknown balance charge %s", chargeID)
	}
	return s.settle(ctx, c, invoiceID)
}

// settle adds a paid charge to the balance. The grant is keyed on the invoice,
// so whichever of the request, the sweep or the webhook sees it paid first adds
// it, and the others change nothing.
func (s *Service) settle(ctx context.Context, c *models.SavedCardCharge, invoiceID string) error {
	reason := "balance_top_up"
	if c.Kind == models.SavedCardAutoReload {
		reason = "auto_reload"
	}
	if _, err := s.credits.GrantPurchased(ctx, c.OrgID, c.Amount, reason, "invoice:"+invoiceID); err != nil {
		return err
	}
	won, err := s.charges.MarkSucceeded(ctx, c.ID)
	if err != nil {
		return err
	}
	if !won {
		return nil
	}
	_ = s.settings.RearmNotices(ctx, c.OrgID)
	if s.audit != nil {
		s.audit.LogAction(ctx, c.OrgID, uuid.Nil, models.AuditActionCreate, models.AuditEntityCreditPurchase, nil, "", "", nil, map[string]string{
			"amount_usd": fmt.Sprintf("%.2f", c.Amount.USD()), "kind": c.Kind,
		})
	}
	s.changed(ctx, c.OrgID)
	return nil
}

func (s *Service) changed(ctx context.Context, orgID uuid.UUID) {
	if bal, xerr := s.credits.GetBalance(ctx, orgID); xerr == nil {
		s.publisher.PublishCreditsChanged(ctx, orgID, bal)
	}
}

// TopUpResult is how a one-click top-up ended: "paid", or "processing" while the
// charge is still pending and the webhook or the sweep will finish it.
type TopUpResult struct {
	Status string
	Amount models.Micro
}

// TopUp charges the saved card now, with the buyer present. A refusal comes
// back as *DeclinedError so the dashboard can offer Checkout instead.
func (s *Service) TopUp(ctx context.Context, orgID uuid.UUID, amount models.Micro) (*TopUpResult, error) {
	if s.card == nil {
		return nil, ErrNoPaymentMethod
	}
	customerID, pm, err := s.paymentMethod(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if pm == nil {
		return nil, ErrNoPaymentMethod
	}
	unlock, ok := s.lock(ctx, orgID)
	if !ok {
		return nil, ErrChargeInFlight
	}
	defer unlock()
	c, err := s.charges.Create(ctx, &models.SavedCardCharge{
		OrgID: orgID, Kind: models.SavedCardTopUp, Amount: amount, AmountCents: amount.Cents(),
		Currency: "usd", CustomerID: customerID, PaymentMethodID: pm.ID,
	})
	if err != nil {
		return nil, err
	}
	runErr := s.run(ctx, c, false)
	var declined *DeclinedError
	if errors.As(runErr, &declined) {
		return nil, runErr
	}
	done, err := s.charges.Get(ctx, c.ID)
	if err != nil {
		return nil, errors.Join(runErr, err)
	}
	if done == nil {
		return nil, errors.Join(runErr, fmt.Errorf("top-up charge %s is gone", c.ID))
	}
	switch done.Status {
	case "succeeded":
		return &TopUpResult{Status: "paid", Amount: amount}, nil
	case "pending":
		if runErr != nil {
			log.Warn().Err(runErr).Str("charge_id", c.ID.String()).Msg("top-up left for the sweep to finish")
		}
		return &TopUpResult{Status: "processing", Amount: amount}, nil
	}
	return nil, errors.Join(runErr, fmt.Errorf("top-up charge %s ended %s", c.ID, done.Status))
}

// SetAutoReload saves automatic reload. Turning it on needs a saved card, and
// starts a reload right away when the balance is already under the threshold;
// turning it off cancels a reload still pending.
func (s *Service) SetAutoReload(ctx context.Context, orgID uuid.UUID, enabled bool, threshold, amount, monthlyLimit models.Micro) (*models.AISpendSettings, error) {
	if enabled && s.card != nil {
		if _, pm, err := s.paymentMethod(ctx, orgID); err != nil {
			return nil, err
		} else if pm == nil {
			return nil, ErrNoPaymentMethod
		}
	}
	out, err := s.settings.UpsertAutoReload(ctx, orgID, enabled, threshold, amount, monthlyLimit)
	if err != nil {
		return nil, err
	}
	if !enabled {
		s.cancelPendingReload(ctx, orgID)
		return out, nil
	}
	go func() {
		rctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		if err := s.Reload(rctx, orgID); err != nil {
			log.Warn().Err(err).Str("org_id", orgID.String()).Msg("automatic reload after enabling failed")
		}
	}()
	return out, nil
}

// Start resumes charges that are due (a soft decline's retry time, or one a
// crash left half done) and starts reloads the per-charge hook could not, such
// as for work charged in a process without Stripe. Runs until ctx ends.
func (s *Service) Start(ctx context.Context) {
	if s.card == nil {
		return
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) sweep(ctx context.Context) {
	// A yearly subscription's allowance still refreshes every month.
	if n, err := s.credits.RefreshAnnualAllowances(ctx); err != nil {
		log.Warn().Err(err).Msg("balance sweep: refresh annual allowances")
	} else if n > 0 {
		log.Info().Int("count", n).Msg("balance sweep: refreshed annual allowances")
	}
	if n, err := s.charges.CloseLegacy(ctx, time.Now().Add(-legacyPendingFor), models.ChargeExpired); err != nil {
		log.Warn().Err(err).Msg("balance sweep: close legacy charges")
	} else if n > 0 {
		log.Info().Int64("count", n).Msg("balance sweep: closed legacy charges")
	}
	due, err := s.charges.Due(ctx, time.Now(), staleAfter, 50)
	if err != nil {
		log.Warn().Err(err).Msg("balance sweep: list due charges")
	}
	for i := range due {
		c := due[i]
		func() {
			unlock, ok := s.lock(ctx, c.OrgID)
			if !ok {
				return
			}
			defer unlock()
			cctx, cancel := context.WithTimeout(ctx, opTimeout)
			defer cancel()
			if err := s.run(cctx, &c, true); err != nil {
				var declined *DeclinedError
				if !errors.As(err, &declined) {
					log.Warn().Err(err).Str("charge_id", c.ID.String()).Msg("balance sweep: resume charge")
				}
			}
		}()
	}
	orgs, err := s.settings.AutoReloadDue(ctx, 100)
	if err != nil {
		log.Warn().Err(err).Msg("balance sweep: list reloads due")
		return
	}
	for _, orgID := range orgs {
		cctx, cancel := context.WithTimeout(ctx, opTimeout)
		if err := s.Reload(cctx, orgID); err != nil {
			var declined *DeclinedError
			if !errors.As(err, &declined) {
				log.Warn().Err(err).Str("org_id", orgID.String()).Msg("balance sweep: reload")
			}
		}
		cancel()
	}
}

// State is everything the balance page shows about paying.
type State struct {
	Settings          *models.AISpendSettings
	PaymentMethod     *stripe.PaymentMethodSummary
	ReloadedThisMonth models.Micro
	LastReload        *models.SavedCardCharge
	Pending           *models.SavedCardCharge
}

func (s *Service) State(ctx context.Context, orgID uuid.UUID) (*State, error) {
	out := &State{Settings: s.config(ctx, orgID)}
	if s.card != nil {
		if _, pm, err := s.paymentMethod(ctx, orgID); err == nil {
			out.PaymentMethod = pm
		}
	}
	var err error
	if out.ReloadedThisMonth, err = s.charges.ReloadedSince(ctx, orgID, monthStart(time.Now())); err != nil {
		return nil, err
	}
	if out.LastReload, err = s.charges.LastSucceeded(ctx, orgID, models.SavedCardAutoReload); err != nil {
		return nil, err
	}
	if out.Pending, err = s.charges.Pending(ctx, orgID); err != nil {
		return nil, err
	}
	return out, nil
}
