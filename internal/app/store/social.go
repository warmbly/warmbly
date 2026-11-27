package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// Social is the social add-on: it shares the store's GraphKit connection,
// receives its social.* webhooks, and has its seats collected in the
// workspace's mailbox billing run, in the same charge.
type Social interface {
	ApplySocialEvent(ctx context.Context, orgID uuid.UUID, ev graphkit.Event) error
	// ReconcileSocial runs with the store's reconcile pass.
	ReconcileSocial(ctx context.Context) error

	// FirstSeatAt is when the workspace's first billed seat connected, nil when it has none.
	FirstSeatAt(ctx context.Context, orgID uuid.UUID) (*time.Time, error)
	// OrgsWithSeats is every workspace with a billed seat.
	OrgsWithSeats(ctx context.Context, limit int) ([]uuid.UUID, error)
	// PendingSeatKeys are charge keys seats are reserved under and not settled since before.
	PendingSeatKeys(ctx context.Context, orgID uuid.UUID, before time.Time) ([]string, error)
	// ReserveSeats reserves under key the seats whose month runs out before dueBefore and the
	// unpaid ones last tried before retryBefore, at the price in force now; it returns how many.
	ReserveSeats(ctx context.Context, orgID uuid.UUID, key string, dueBefore, retryBefore time.Time) (int, error)
	// SeatBatch is what the seats reserved under key take.
	SeatBatch(ctx context.Context, orgID uuid.UUID, key string) (SeatBatch, error)
	// SettleSeats moves the seats key collected a month on and gives back the share of any removed since.
	SettleSeats(ctx context.Context, orgID uuid.UUID, key string) error
	// SeatsUnpaid releases the seats key could not collect and tells the workspace.
	SeatsUnpaid(ctx context.Context, orgID uuid.UUID, key string, cause error) error
	// ExtendUnmeteredSeats moves on, with no charge, the seats due before dueBefore on an instance that meters nothing.
	ExtendUnmeteredSeats(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) error
	// EnforceUnpaidSeats pauses at GraphKit the seats whose month went unpaid, and removes them a week later.
	EnforceUnpaidSeats(ctx context.Context, orgID uuid.UUID) error
	// SeatsDue is what the seats due before dueBefore would take now.
	SeatsDue(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) (SeatBatch, error)
}

// SeatBatch is a set of social seats in one charge.
type SeatBatch struct {
	Count  int
	Micros int64
	// Reason is the ledger and invoice line for them.
	Reason string
}

// WireSocial attaches the social add-on, built after the store.
func (s *Service) WireSocial(sc Social) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.social = sc
	s.mu.Unlock()
}

func (s *Service) socialAddon() Social {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.social
}

// API is the GraphKit connection in use.
func (s *Service) API() graphkit.API { return s.api() }

// EnsureAccount makes sure the workspace has a GraphKit account.
func (s *Service) EnsureAccount(ctx context.Context, orgID uuid.UUID) error {
	if !s.Enabled() {
		return errDisabled
	}
	return s.ensureAccount(ctx, orgID)
}

// Metered reports an instance that charges the workspace balance.
func (s *Service) Metered() bool { return s.metered() }

// StoreRetail is the instance's store price list.
func (s *Service) StoreRetail(ctx context.Context) instancesettings.StoreRetail { return s.retail(ctx) }

// PartnerManaged reports a workspace a partner manages.
func (s *Service) PartnerManaged(ctx context.Context, orgID uuid.UUID) bool {
	return s.managed(ctx, orgID)
}

// PlanActive reports a paid plan in force.
func (s *Service) PlanActive(ctx context.Context, orgID uuid.UUID) (bool, error) {
	return s.planActive(ctx, orgID)
}

// PaymentFor is how due would be paid: the balance first, the card for the rest; addMonthly sizes the reload.
func (s *Service) PaymentFor(ctx context.Context, orgID uuid.UUID, due, addMonthly int64) Payment {
	return s.payment(ctx, orgID, due, addMonthly)
}

// Collect tops the balance up from the card for what it does not cover, keyed by key, before a debit of due.
func (s *Service) Collect(ctx context.Context, orgID uuid.UUID, due int64, key, description string, offSession bool) error {
	return s.collect(ctx, orgID, due, key, description, offSession)
}

// NotifyBilling tells whoever manages billing.
func (s *Service) NotifyBilling(ctx context.Context, orgID uuid.UUID, title, body, link, group string) {
	s.notify(ctx, orgID, title, body, link, group)
}

// Upstream maps a GraphKit refusal no social code covers to the store's own, logging the cause.
func (s *Service) Upstream(ctx context.Context, op string, err error) *errx.Error {
	return s.upstream(ctx, op, err)
}

// PaymentFailed reports a charge the workspace has to fix (a card, the balance), as opposed to an outage.
func PaymentFailed(err error) bool { return paymentFailed(err) }

// PayFix says what fixes a failed charge.
func PayFix(cause error) string { return payFix(cause) }

// PayLink opens the billing page's card setup.
const PayLink = payLink

// MonthLabel is the ledger's month label for a period.
func MonthLabel(t time.Time) string { return monthLabel(t) }

// USD formats micros as dollars.
func USD(micros int64) string { return usd(micros) }

// seatBatch is what the seats under key take, empty without the add-on.
func (s *Service) seatBatch(ctx context.Context, orgID uuid.UUID, key string) (SeatBatch, error) {
	sc := s.socialAddon()
	if sc == nil {
		return SeatBatch{}, nil
	}
	return sc.SeatBatch(ctx, orgID, key)
}

// pendingSeatKeys adds the keys only seats are reserved under to the mailboxes' own.
func (s *Service) pendingSeatKeys(ctx context.Context, orgID uuid.UUID, keys []string, before time.Time) ([]string, error) {
	sc := s.socialAddon()
	if sc == nil {
		return keys, nil
	}
	more, err := sc.PendingSeatKeys(ctx, orgID, before)
	if err != nil {
		return keys, err
	}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		seen[k] = true
	}
	for _, k := range more {
		if !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	return keys, nil
}

// reserveSeats reserves the workspace's due seats under key; 0 without the add-on.
func (s *Service) reserveSeats(ctx context.Context, orgID uuid.UUID, key string, dueBefore, retryBefore time.Time) (int, error) {
	sc := s.socialAddon()
	if sc == nil {
		return 0, nil
	}
	return sc.ReserveSeats(ctx, orgID, key, dueBefore, retryBefore)
}

// batchReason is the ledger line for mailboxes and seats charged together.
func batchReason(rows []models.StoreMailbox, seats SeatBatch) string {
	switch {
	case seats.Count == 0:
		return bundleReason(rows)
	case len(rows) == 0:
		return seats.Reason
	}
	return bundleReason(rows) + " + " + seats.Reason
}

// firstSeatAt is the workspace's first billed seat, for its billing day.
func (s *Service) firstSeatAt(ctx context.Context, orgID uuid.UUID) *time.Time {
	sc := s.socialAddon()
	if sc == nil {
		return nil
	}
	t, err := sc.FirstSeatAt(ctx, orgID)
	if err != nil {
		log.Warn().Err(err).Str("org_id", orgID.String()).Msg("store: reading the first social seat failed")
		return nil
	}
	return t
}

func (s *Service) reconcileSocial(ctx context.Context) error {
	if sc := s.socialAddon(); sc != nil {
		return sc.ReconcileSocial(ctx)
	}
	return nil
}
