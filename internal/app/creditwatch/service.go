// Package creditwatch reacts to AI credit balance changes: it fires the
// low-balance alert (at most once per day per org) and drives auto top-up
// (buy the configured pack off-session when the balance dips below the org's
// threshold, bounded per month). It hangs off credits.CreditService.SetMonitor
// so the credits package itself never depends on Stripe or pubsub.
package creditwatch

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// autoTopUpper is the slice of the Stripe service the watch drives.
type autoTopUpper interface {
	AutoTopUpCredits(ctx context.Context, orgID uuid.UUID, packKey string, creditAmount int) (bool, error)
}

const (
	// opTimeout bounds one watch pass (it runs on a detached goroutine).
	opTimeout = 30 * time.Second
	// topupLockTTL dedups concurrent auto top-up attempts for one org.
	topupLockTTL  = 10 * time.Minute
	topupLockKey  = "credits:autotopup:"
	topupReason   = "credit_auto_topup"
	alertCooldown = 24 * time.Hour
)

type Watch struct {
	settings  repository.AISettingsRepository
	creditRep repository.CreditRepository
	cache     *cache.Cache
	publisher *pubsub.StreamingPublisher
	topup     autoTopUpper
}

func New(settings repository.AISettingsRepository, creditRep repository.CreditRepository, c *cache.Cache, publisher *pubsub.StreamingPublisher, topup autoTopUpper) *Watch {
	return &Watch{settings: settings, creditRep: creditRep, cache: c, publisher: publisher, topup: topup}
}

// OnBalanceChanged is the credits monitor hook: called (already on a detached
// goroutine) with the resulting balance after every fresh debit. Everything in
// here is best-effort; a failure only means a missed alert or top-up attempt.
func (w *Watch) OnBalanceChanged(orgID uuid.UUID, balance int) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	// Broadcast every balance change so meters across the dashboard move the
	// moment a charge lands (scheduled campaign/automation spends included).
	if w.publisher != nil {
		w.publisher.PublishCreditsChanged(ctx, orgID, balance)
	}

	cfg := w.loadSettings(ctx, orgID)

	if balance <= cfg.LowBalanceThreshold {
		w.maybeAlert(ctx, orgID, balance, cfg)
	}
	if cfg.AutoTopupEnabled && balance < cfg.AutoTopupThreshold {
		w.maybeTopUp(ctx, orgID, cfg)
	}
}

func (w *Watch) loadSettings(ctx context.Context, orgID uuid.UUID) *models.AISpendSettings {
	if w.settings != nil {
		if cfg, err := w.settings.Get(ctx, orgID); err == nil && cfg != nil {
			return cfg
		}
	}
	return &models.AISpendSettings{
		OrgID:               orgID,
		LowBalanceThreshold: credits.DefaultLowBalanceThreshold,
	}
}

// maybeAlert publishes BILLING_CREDITS_LOW when this call wins the once-per-
// day stamp (the stamp is a conditional SQL update, so concurrent consumes
// can't double-alert).
func (w *Watch) maybeAlert(ctx context.Context, orgID uuid.UUID, balance int, cfg *models.AISpendSettings) {
	if w.settings == nil || w.publisher == nil {
		return
	}
	won, err := w.settings.StampLowBalanceNotified(ctx, orgID, alertCooldown)
	if err != nil || !won {
		return
	}
	w.publisher.PublishCreditsLow(ctx, orgID, balance, cfg.LowBalanceThreshold)
}

// maybeTopUp buys the configured pack off-session, guarded by a Redis lock
// (one attempt at a time per org) and the per-month purchase bound.
func (w *Watch) maybeTopUp(ctx context.Context, orgID uuid.UUID, cfg *models.AISpendSettings) {
	if w.topup == nil || w.creditRep == nil {
		return
	}
	pack := credits.PackByKey(cfg.AutoTopupPack)
	if pack == nil || cfg.AutoTopupMaxPerMonth <= 0 {
		return
	}
	if w.cache != nil {
		ok, err := w.cache.SetNX(ctx, topupLockKey+orgID.String(), "1", topupLockTTL).Result()
		if err != nil || !ok {
			return
		}
	}

	monthStart := time.Now().UTC()
	monthStart = time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, time.UTC)
	n, err := w.creditRep.CountGrantsSince(ctx, orgID, topupReason, monthStart)
	if err != nil || n >= cfg.AutoTopupMaxPerMonth {
		return
	}

	granted, err := w.topup.AutoTopUpCredits(ctx, orgID, pack.Key, pack.Credits)
	if err != nil {
		log.Warn().Err(err).Str("org_id", orgID.String()).Str("pack", pack.Key).Msg("credit auto top-up failed")
		return
	}
	if granted {
		log.Info().Str("org_id", orgID.String()).Str("pack", pack.Key).Int("credits", pack.Credits).Msg("credit auto top-up fulfilled")
	}
}
