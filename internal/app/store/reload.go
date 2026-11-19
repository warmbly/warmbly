package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/models"
)

// Card charges are few and large: a store charge that needs the card tops the
// balance up by at least the store's minimum, and the store keeps the
// workspace's automatic reload sized to its mailboxes, so a month's bundle
// normally comes out of the balance with no card charge on the day.

// preBundleReload is how long before a billing run the balance is reloaded for it.
const preBundleReload = 48 * time.Hour

func (s *Service) reloadSettings(ctx context.Context, orgID uuid.UUID) *models.AISpendSettings {
	if s.Cards != nil {
		if st, err := s.Cards.State(ctx, orgID); err == nil && st != nil && st.Settings != nil {
			return st.Settings
		}
	}
	return credits.DefaultSpendSettings(orgID)
}

// cardFloor is the least a card is charged now: the store's minimum, or the reload amount when reload is on.
func (s *Service) cardFloor(ctx context.Context, cfg *models.AISpendSettings) int64 {
	floor := s.retail(ctx).MinCardCharge()
	if cfg.AutoReloadEnabled {
		floor = max(floor, int64(cfg.AutoReloadAmount))
	}
	return floor
}

// reloadHeadroom is what a card charge adds so that, with automatic reload on,
// the store's debit does not leave the balance under the reload threshold and
// set off a second card charge minutes later: at most the reload that would
// follow (its amount, within the month's reload limit), 0 when none would.
// change is what the charge and the debit together do to the balance.
func (s *Service) reloadHeadroom(ctx context.Context, orgID uuid.UUID, cfg *models.AISpendSettings, change int64) int64 {
	if !cfg.AutoReloadEnabled || s.Cards == nil || s.Credits == nil {
		return 0
	}
	whole, xerr := s.Credits.GetBalance(ctx, orgID)
	if xerr != nil {
		return 0
	}
	after := int64(whole) + change
	if after >= int64(cfg.AutoReloadThreshold) {
		return 0
	}
	var used models.Micro
	if st, err := s.Cards.State(ctx, orgID); err == nil && st != nil {
		used = st.ReloadedThisMonth
	}
	reload := min(cfg.AutoReloadAmount, cfg.AutoReloadMonthlyMax-used)
	if reload < credits.TopUpMin {
		return 0
	}
	return ceilCent(min(int64(cfg.AutoReloadThreshold)-after, int64(reload)))
}

// neverConfigured reports automatic reload nobody has touched.
func neverConfigured(cfg *models.AISpendSettings) bool {
	def := credits.DefaultSpendSettings(cfg.OrgID)
	return !cfg.AutoReloadEnabled && cfg.AutoReloadThreshold == def.AutoReloadThreshold && cfg.AutoReloadAmount == def.AutoReloadAmount &&
		cfg.AutoReloadFailureCode == "" && cfg.AutoReloadFailedAt == nil
}

// monthlyMailboxes is what the workspace's mailboxes cost a month, the size of its next bundle.
func (s *Service) monthlyMailboxes(ctx context.Context, orgID uuid.UUID) int64 {
	boxes, err := s.Repo.OpenMailboxes(ctx, orgID)
	if err != nil {
		return 0
	}
	var total int64
	for i := range boxes {
		m := &boxes[i]
		if m.Status == "active" || m.Status == "suspended" || m.Status == "provisioning" || (m.Status == "cancelling" && m.UnpaidCancel) {
			total += int64(m.MonthlyRetailMicros)
		}
	}
	return total
}

// reloadTargets keeps the balance able to pay the next bundle: reload below it, by at least the minimum, within the monthly limit.
func (s *Service) reloadTargets(ctx context.Context, orgID uuid.UUID, cfg *models.AISpendSettings, addMonthly int64) (threshold, amount int64) {
	threshold = s.monthlyMailboxes(ctx, orgID) + addMonthly
	amount = max(s.retail(ctx).MinCardCharge(), threshold)
	if cfg.AutoReloadMonthlyMax > 0 {
		amount = min(amount, int64(cfg.AutoReloadMonthlyMax))
	}
	return threshold, amount
}

// reloadPlan is the workspace's automatic reload as it stands after a purchase adding addMonthly.
func (s *Service) reloadPlan(ctx context.Context, orgID uuid.UUID, cfg *models.AISpendSettings, card bool, addMonthly int64) AutoReload {
	acct, _ := s.Repo.GetAccount(ctx, orgID)
	switch {
	case acct != nil && acct.ReloadManaged:
		thr, amt := s.reloadTargets(ctx, orgID, cfg, addMonthly)
		return AutoReload{Enabled: true, ThresholdMicros: thr, AmountMicros: amt, StoreManaged: true}
	case (acct == nil || !acct.ReloadDecided) && card && neverConfigured(cfg):
		thr, amt := s.reloadTargets(ctx, orgID, cfg, addMonthly)
		return AutoReload{Enabled: true, ThresholdMicros: thr, AmountMicros: amt, StoreManaged: true}
	}
	return AutoReload{Enabled: cfg.AutoReloadEnabled, ThresholdMicros: int64(cfg.AutoReloadThreshold), AmountMicros: int64(cfg.AutoReloadAmount)}
}

// adoptAutoReload turns automatic reload on with a workspace's first store
// purchase, unless the buyer said not to or the workspace set it up itself.
func (s *Service) adoptAutoReload(ctx context.Context, orgID uuid.UUID, want *bool) {
	if !s.metered() || s.Cards == nil {
		return
	}
	acct, err := s.Repo.GetAccount(ctx, orgID)
	if err != nil || acct == nil || acct.ReloadDecided {
		return
	}
	cfg := s.reloadSettings(ctx, orgID)
	if (want != nil && !*want) || !neverConfigured(cfg) {
		_ = s.Repo.SetReloadState(ctx, orgID, false, 0, 0)
		return
	}
	thr, amt := s.reloadTargets(ctx, orgID, cfg, 0)
	if amt <= 0 {
		return
	}
	if _, err := s.Cards.SetAutoReload(ctx, orgID, true, models.Micro(thr), models.Micro(amt), cfg.AutoReloadMonthlyMax); err != nil {
		// No card yet: the next purchase decides again.
		log.Info().Str("org_id", orgID.String()).Str("reason", reasonOf(err)).Msg("store: automatic reload not turned on")
		return
	}
	if err := s.Repo.SetReloadState(ctx, orgID, true, models.Micro(thr), models.Micro(amt)); err != nil {
		log.Warn().Err(err).Str("org_id", orgID.String()).Msg("store: recording automatic reload failed")
	}
}

// syncAutoReload keeps a store-managed reload sized to the mailboxes, and
// lets go of it for good once the workspace changed it by hand.
func (s *Service) syncAutoReload(ctx context.Context, orgID uuid.UUID) {
	if !s.metered() || s.Cards == nil {
		return
	}
	acct, err := s.Repo.GetAccount(ctx, orgID)
	if err != nil || acct == nil || !acct.ReloadManaged {
		return
	}
	cfg := s.reloadSettings(ctx, orgID)
	if !cfg.AutoReloadEnabled || cfg.AutoReloadThreshold != acct.ReloadThreshold || cfg.AutoReloadAmount != acct.ReloadAmount {
		_ = s.Repo.SetReloadState(ctx, orgID, false, 0, 0)
		return
	}
	thr, amt := s.reloadTargets(ctx, orgID, cfg, 0)
	if models.Micro(thr) == cfg.AutoReloadThreshold && models.Micro(amt) == cfg.AutoReloadAmount {
		return
	}
	if _, err := s.Cards.SetAutoReload(ctx, orgID, true, models.Micro(thr), models.Micro(amt), cfg.AutoReloadMonthlyMax); err != nil {
		log.Info().Str("org_id", orgID.String()).Str("reason", reasonOf(err)).Msg("store: resizing automatic reload failed")
		return
	}
	_ = s.Repo.SetReloadState(ctx, orgID, true, models.Micro(thr), models.Micro(amt))
}

// reloadBeforeBundle tops the balance up in the days before a billing run, so
// the run is paid from the balance rather than by a card charge on the day.
func (s *Service) reloadBeforeBundle(ctx context.Context, orgID uuid.UUID, acct *models.StoreAccount) {
	if !s.metered() || s.Cards == nil || acct == nil || acct.NextBillAnchor == nil {
		return
	}
	run := acct.NextBillAnchor.Add(-s.billLead(ctx))
	now := s.now()
	if now.Before(run.Add(-preBundleReload)) || !now.Before(run) {
		return
	}
	if !s.reloadSettings(ctx, orgID).AutoReloadEnabled {
		return
	}
	// The bundle draws on the purchased pool alone, so that is what is topped up: the charge the run would make, two days early.
	next := s.nextBundle(ctx, acct)
	if next == nil {
		return
	}
	key := "reload:" + orgID.String() + ":" + acct.NextBillAnchor.Format("20060102")
	if err := s.collect(ctx, orgID, next.AmountMicros, key, fmt.Sprintf("Balance top-up · %d mailboxes · %s", next.Mailboxes, monthLabel(*acct.NextBillAnchor)), true); err != nil {
		log.Info().Str("org_id", orgID.String()).Str("reason", reasonOf(err)).Msg("store: reload before the mailbox bundle failed")
	}
}
