package warmup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// WebhookDispatcher is the minimum dispatch interface the warmup service
// needs. Kept narrow to avoid importing the webhook package (which would
// create a cycle on init order).
type WebhookDispatcher interface {
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error)
}

// HealthRealtimePublisher pushes a health transition to the owning org's
// realtime stream. Narrow + primitive-typed so the warmup package doesn't
// import the pubsub event types. *pubsub.StreamingPublisher satisfies it.
type HealthRealtimePublisher interface {
	PublishAccountHealth(ctx context.Context, orgID, userID, accountID, email, prevState, newState, reason string)
}

const (
	minSpamPlacementSample = 20

	spamPlacementWatchPct        = 10.0
	spamPlacementThrottlePct     = 15.0
	spamPlacementQuarantinePct   = 20.0
	spamPlacementBlockPct        = 40.0
	spamPlacementCatastrophicPct = 80.0

	complaintRateWatchPct      = 0.03
	complaintRateQuarantinePct = 0.10
	complaintRateBlockPct      = 0.30

	// Warmup-internal user complaints are a strong negative content signal
	// (recipient actively rejected the message inside the pool). They are
	// rarer than placement events so the thresholds sit between external
	// complaint rates (0.03 / 0.10 / 0.30) and placement rates (10 / 20 / 40).
	warmupComplaintWatchPct      = 0.5
	warmupComplaintQuarantinePct = 1.5
	warmupComplaintBlockPct      = 3.0

	bounceRateQuarantinePct = 5.0
	bounceRateBlockPct      = 10.0

	minComplaintSample = 100

	invalidTokenBlockThreshold = 3

	// Tampering: harming pool warmup mail (deleting it or marking it as spam)
	// bans the mailbox once this many harm events occur within the window.
	// Default 1 = ban on first harm (Instantly-style zero tolerance); the
	// owner can appeal. Bump to forgive accidental actions.
	warmupTamperingBlockThreshold = 1
	warmupTamperingWindow         = 7 * 24 * time.Hour

	warmupThrottleDuration   = 3 * 24 * time.Hour
	warmupQuarantineDuration = 7 * 24 * time.Hour
	warmupBlockDuration      = 30 * 24 * time.Hour
	warmupCatastrophicBlock  = 90 * 24 * time.Hour
)

type Service interface {
	// EnsurePoolMembershipWithRole puts the mailbox in exactly one pool, moving it (with its
	// reputation) rather than adding a second membership (issue #211).
	EnsurePoolMembershipWithRole(ctx context.Context, accountID uuid.UUID, poolType, role string) *errx.Error
	// MovePoolMembership corrects an existing member's pool, keeping its role. Reports whether it moved.
	MovePoolMembership(ctx context.Context, accountID uuid.UUID, poolType string) (bool, *errx.Error)
	// RemoveFromAllPools takes the mailbox out of warmup; a caller that knows it is not entitled
	// does not know which pool it is in.
	RemoveFromAllPools(ctx context.Context, accountID uuid.UUID) *errx.Error
	CanParticipate(ctx context.Context, accountID uuid.UUID, poolType string) (bool, string, *errx.Error)
	ApplySpamReport(ctx context.Context, reporterAccountID, reportedAccountID uuid.UUID, messageID, reportType string) (*models.WarmupParticipantHealth, *errx.Error)
	// RecordSpamPlacement records that a warmup message landed in the
	// recipient's Junk/Spam folder on arrival. Counted separately from
	// user complaints so the two signals can drive distinct thresholds.
	RecordSpamPlacement(ctx context.Context, reporterAccountID, reportedAccountID uuid.UUID, messageID, contentSource, recipientProvider, recipientDomain string) (*models.WarmupParticipantHealth, *errx.Error)
	ApplyInvalidTokenAttempt(ctx context.Context, accountID uuid.UUID, attemptedToken string, scoreDelta int) (*models.WarmupParticipantHealth, *errx.Error)
	ApplyRateLimitExceeded(ctx context.Context, accountID uuid.UUID, reason string) (*models.WarmupParticipantHealth, *errx.Error)

	// RecordTampering records that a participant harmed a warmup email (deleted
	// it or marked it as spam) and bans the mailbox from warmup once the harm
	// count crosses the threshold. The owner can then appeal.
	RecordTampering(ctx context.Context, accountID uuid.UUID, messageID, kind string) (*models.WarmupParticipantHealth, *errx.Error)

	// SubmitAppeal lets the mailbox owner appeal a warmup ban with a reason.
	SubmitAppeal(ctx context.Context, userID, accountID uuid.UUID, reason string) (uuid.UUID, *errx.Error)
	// GetBanStatus returns the user-facing warmup standing for a mailbox.
	GetBanStatus(ctx context.Context, userID, accountID uuid.UUID) (*models.WarmupBanStatus, *errx.Error)

	// Scheduled health evaluation
	EvaluateAllParticipants(ctx context.Context) (evaluated int, stateChanges int, err *errx.Error)
	GetPoolHealthSummary(ctx context.Context) (*models.WarmupPoolHealthSummary, *errx.Error)

	// WireWebhooks attaches the webhook dispatcher post-construction so
	// health-state transitions fan out to subscribed customer endpoints.
	WireWebhooks(w WebhookDispatcher, emailRepo repository.EmailRepository)
	// WireRealtime attaches the realtime publisher so health transitions are
	// also pushed live to the owning user's dashboard.
	WireRealtime(r HealthRealtimePublisher, emailRepo repository.EmailRepository)
}

// OperatorNotifier is the instance-wide operator alert surface, injected
// post-construction so this package needs no import of it. Nil disables it.
type OperatorNotifier interface {
	NotifyOperator(key, title, summary string, fields map[string]string)
}

type service struct {
	repo      repository.WarmupRepository
	emailRepo repository.EmailRepository
	webhooks  WebhookDispatcher
	realtime  HealthRealtimePublisher
	opsNotify OperatorNotifier
	now       func() time.Time
}

// WireOperatorNotifier attaches the operator alert channel.
func (s *service) WireOperatorNotifier(n OperatorNotifier) { s.opsNotify = n }

func NewService(repo repository.WarmupRepository) Service {
	return &service{
		repo: repo,
		now:  time.Now,
	}
}

// WireWebhooks attaches the webhook dispatcher post-construction so health
// transitions fan out to subscribed customer endpoints. The emailRepo is
// needed to resolve the org for an account (warmup events are recorded
// per-account but dispatched per-org).
func (s *service) WireWebhooks(w WebhookDispatcher, emailRepo repository.EmailRepository) {
	s.webhooks = w
	s.emailRepo = emailRepo
}

// WireRealtime attaches the realtime publisher (and emailRepo, if not already
// set via WireWebhooks) so health transitions push live to the dashboard.
func (s *service) WireRealtime(r HealthRealtimePublisher, emailRepo repository.EmailRepository) {
	s.realtime = r
	if s.emailRepo == nil {
		s.emailRepo = emailRepo
	}
}

// dispatchHealthEvent fans a health-state transition out to (1) the owning
// user's realtime stream so the dashboard updates live, and (2) subscribed
// customer webhooks. Both are best-effort and independent — realtime still
// fires when webhooks aren't wired (e.g. in the consumer). No-op on a
// no-change transition or when the account can't be resolved.
func (s *service) dispatchHealthEvent(ctx context.Context, accountID uuid.UUID, oldState, newState models.WarmupHealthState, reason string) {
	if s.emailRepo == nil || oldState == newState {
		return
	}

	account, _ := s.emailRepo.GetByID(ctx, accountID)
	if account == nil || account.OrganizationID == nil {
		return
	}

	// Realtime push to the dashboard (independent of webhooks).
	if s.realtime != nil {
		s.realtime.PublishAccountHealth(ctx, account.OrganizationID.String(), account.UserID, accountID.String(), account.Email, string(oldState), string(newState), reason)
	}

	if s.webhooks == nil {
		return
	}

	var event models.WebhookEventType
	switch newState {
	case models.WarmupHealthBlocked:
		event = models.WebhookEventWarmupBlocked
	case models.WarmupHealthQuarantined:
		event = models.WebhookEventWarmupQuarantined
	case models.WarmupHealthThrottled, models.WarmupHealthWatch, models.WarmupHealthHealthy:
		// Fire the generic health_changed event for these — quarantine /
		// blocked also re-fire it so subscribers can carry a single handler.
		event = models.WebhookEventWarmupHealthChanged
	default:
		return
	}

	payload := map[string]any{
		"email_account_id": accountID,
		"email":            account.Email,
		"previous_state":   string(oldState),
		"new_state":        string(newState),
		"reason":           reason,
	}
	_, _ = s.webhooks.Dispatch(ctx, *account.OrganizationID, event, payload)

	// For block/quarantine, also fire the specific event in addition to
	// the generic transition so callers can subscribe selectively.
	switch newState {
	case models.WarmupHealthBlocked:
		_, _ = s.webhooks.Dispatch(ctx, *account.OrganizationID, models.WebhookEventWarmupBlocked, payload)
	case models.WarmupHealthQuarantined:
		_, _ = s.webhooks.Dispatch(ctx, *account.OrganizationID, models.WebhookEventWarmupQuarantined, payload)
	}
}

func (s *service) EnsurePoolMembershipWithRole(ctx context.Context, accountID uuid.UUID, poolType, role string) *errx.Error {
	if role != "sender_receiver" && role != "recipient_only" {
		return errx.New(errx.BadRequest, "invalid warmup participant role")
	}

	pool, err := s.repo.GetPoolByType(ctx, poolType)
	if err != nil {
		return errx.InternalError()
	}
	if pool == nil {
		return errx.New(errx.BadRequest, "warmup pool not found")
	}
	if err := s.repo.MoveToPool(ctx, pool.ID, accountID, role); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) MovePoolMembership(ctx context.Context, accountID uuid.UUID, poolType string) (bool, *errx.Error) {
	pool, err := s.repo.GetPoolByType(ctx, poolType)
	if err != nil {
		return false, errx.InternalError()
	}
	if pool == nil {
		return false, errx.New(errx.BadRequest, "warmup pool not found")
	}
	moved, err := s.repo.MoveExistingToPool(ctx, pool.ID, accountID)
	if err != nil {
		return false, errx.InternalError()
	}
	return moved, nil
}

func (s *service) RemoveFromAllPools(ctx context.Context, accountID uuid.UUID) *errx.Error {
	if err := s.repo.LeaveAllPools(ctx, accountID); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) CanParticipate(ctx context.Context, accountID uuid.UUID, poolType string) (bool, string, *errx.Error) {
	health, err := s.repo.GetParticipantHealth(ctx, accountID, poolType)
	if err != nil {
		return false, "", errx.InternalError()
	}
	if health == nil {
		return false, "not_in_pool", nil
	}

	now := s.now().UTC()
	if health.BlockedUntil != nil && !health.BlockedUntil.After(now) {
		// Block period expired. Instead of snapping back to healthy, enter probation
		// (throttled state with a 3-day window at reduced volume).
		wasBlocked := health.HealthState == models.WarmupHealthQuarantined || health.HealthState == models.WarmupHealthBlocked
		health, xerr := s.evaluateAndPersist(ctx, accountID, poolType, health)
		if xerr != nil {
			return false, "", xerr
		}
		if health == nil {
			return false, "not_in_pool", nil
		}
		// If metrics are clean and the mailbox was previously blocked, force probation
		if wasBlocked && health.HealthState == models.WarmupHealthHealthy {
			probationEnd := now.Add(warmupThrottleDuration)
			reason := "re-entry probation after block expiry"
			if err := s.repo.UpdateParticipantHealth(ctx, accountID, models.WarmupHealthThrottled, &probationEnd, reason, 0); err != nil {
				return false, "", errx.InternalError()
			}
			return true, "throttled", nil
		}
	}

	switch health.HealthState {
	case models.WarmupHealthQuarantined, models.WarmupHealthBlocked:
		if health.BlockedUntil == nil || health.BlockedUntil.After(now) {
			if health.BlockedReason != nil && *health.BlockedReason != "" {
				return false, *health.BlockedReason, nil
			}
			return false, string(health.HealthState), nil
		}
	case models.WarmupHealthThrottled:
		// Throttled accounts can still participate but callers should reduce volume
		return true, "throttled", nil
	}

	return true, "", nil
}

// RecordSpamPlacement is a thin wrapper that fires ApplySpamReport with the
// 'spam_placement' type and a smaller spam-score delta (placement is a
// weaker individual signal than a user complaint — it is more likely to
// reflect content rather than malice).
func (s *service) RecordSpamPlacement(ctx context.Context, reporterAccountID, reportedAccountID uuid.UUID, messageID, contentSource, recipientProvider, recipientDomain string) (*models.WarmupParticipantHealth, *errx.Error) {
	inserted, err := s.repo.RecordSpamReport(ctx, &repository.SpamReport{
		ID:                uuid.New(),
		ReporterAccountID: reporterAccountID,
		ReportedAccountID: reportedAccountID,
		MessageID:         messageID,
		ReportType:        "spam_placement",
		ContentSource:     contentSource,
		RecipientProvider: recipientProvider,
		RecipientDomain:   recipientDomain,
	})
	if err != nil {
		return nil, errx.InternalError()
	}
	if !inserted {
		return s.getParticipantForAnyPool(ctx, reportedAccountID)
	}
	if _, err := s.repo.IncrementSpamScore(ctx, reportedAccountID, 5); err != nil {
		return nil, errx.InternalError()
	}
	// Fan a warmup.placement_in_spam webhook for the sender (best-effort).
	s.dispatchPlacementInSpam(ctx, reportedAccountID, contentSource, recipientProvider, recipientDomain)
	return s.evaluateAndPersistAnyPool(ctx, reportedAccountID)
}

// dispatchPlacementInSpam fires the warmup.placement_in_spam customer webhook for
// the sending mailbox. Best-effort; no-op when webhooks aren't wired (consumer
// still records the signal and the health evaluation still runs).
func (s *service) dispatchPlacementInSpam(ctx context.Context, accountID uuid.UUID, contentSource, recipientProvider, recipientDomain string) {
	if s.webhooks == nil || s.emailRepo == nil {
		return
	}
	account, _ := s.emailRepo.GetByID(ctx, accountID)
	if account == nil || account.OrganizationID == nil {
		return
	}
	_, _ = s.webhooks.Dispatch(ctx, *account.OrganizationID, models.WebhookEventWarmupPlacementInSpam, map[string]any{
		"email_account_id":   accountID,
		"email":              account.Email,
		"content_source":     contentSource,
		"recipient_provider": recipientProvider,
		"recipient_domain":   recipientDomain,
	})
}

// RecordTampering records that a participant harmed a warmup email (deleted it
// or marked it as spam) and bans the mailbox from warmup once the harm count
// crosses the threshold within the window. The block carries a clear,
// user-facing reason and fires the health transition so the dashboard updates.
func (s *service) RecordTampering(ctx context.Context, accountID uuid.UUID, messageID, kind string) (*models.WarmupParticipantHealth, *errx.Error) {
	inserted, err := s.repo.RecordWarmupTampering(ctx, accountID, messageID, kind)
	if err != nil {
		return nil, errx.InternalError()
	}
	if !inserted {
		// Already counted this exact harm — don't double-penalise.
		return s.getParticipantForAnyPool(ctx, accountID)
	}

	since := s.now().Add(-warmupTamperingWindow)
	count, err := s.repo.CountWarmupTamperingSince(ctx, accountID, since)
	if err != nil {
		return nil, errx.InternalError()
	}

	if count >= warmupTamperingBlockThreshold {
		prev, _ := s.getParticipantForAnyPool(ctx, accountID)
		reason := fmt.Sprintf("Auto-blocked from warmup: %s a warmup email. Warmup mailboxes must let warmup mail be delivered and engaged with. You can appeal this from your dashboard.", tamperingVerb(kind))
		if count > 1 {
			reason = fmt.Sprintf("Auto-blocked from warmup: harmed %d warmup emails (deleted or marked as spam) in the last %d days. You can appeal this from your dashboard.", count, int(warmupTamperingWindow.Hours()/24))
		}
		if err := s.repo.BlockFromPool(ctx, accountID, reason); err != nil {
			return nil, errx.InternalError()
		}
		if prev != nil && prev.HealthState != models.WarmupHealthBlocked {
			s.dispatchHealthEvent(ctx, accountID, prev.HealthState, models.WarmupHealthBlocked, reason)
		}
	}

	return s.getParticipantForAnyPool(ctx, accountID)
}

func tamperingVerb(kind string) string {
	switch kind {
	case "deletion":
		return "deleted"
	case "spam_flag":
		return "marked as spam"
	default:
		return "tampered with"
	}
}

// SubmitAppeal records a user's appeal against a warmup ban. Verifies the
// mailbox belongs to the user, is actually blocked, and has no open appeal.
func (s *service) SubmitAppeal(ctx context.Context, userID, accountID uuid.UUID, reason string) (uuid.UUID, *errx.Error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return uuid.Nil, errx.New(errx.BadRequest, "an appeal reason is required")
	}
	if len(reason) > 2000 {
		reason = reason[:2000]
	}

	if s.emailRepo != nil {
		acc, _ := s.emailRepo.GetByID(ctx, accountID)
		if acc == nil || acc.UserID != userID.String() {
			return uuid.Nil, errx.New(errx.Forbidden, "this mailbox does not belong to you")
		}
	}

	health, _ := s.getParticipantForAnyPool(ctx, accountID)
	if health == nil || (health.HealthState != models.WarmupHealthBlocked && health.HealthState != models.WarmupHealthQuarantined) {
		return uuid.Nil, errx.New(errx.BadRequest, "this mailbox is not blocked from warmup")
	}

	pending, err := s.repo.HasPendingWarmupAppeal(ctx, accountID)
	if err != nil {
		return uuid.Nil, errx.InternalError()
	}
	if pending {
		return uuid.Nil, errx.New(errx.BadRequest, "an appeal is already pending for this mailbox")
	}

	id, err := s.repo.CreateWarmupAppeal(ctx, accountID, userID, reason)
	if err != nil {
		return uuid.Nil, errx.InternalError()
	}

	if s.opsNotify != nil {
		s.opsNotify.NotifyOperator(
			"warmup_appeal.created",
			"Warmup ban appealed",
			"A blocked mailbox asked to be let back into the warmup pool.",
			map[string]string{
				"Mailbox": accountID.String(),
				"State":   string(health.HealthState),
				"Reason":  reason,
			},
		)
	}

	return id, nil
}

// GetBanStatus returns the user-facing warmup standing for a mailbox.
func (s *service) GetBanStatus(ctx context.Context, userID, accountID uuid.UUID) (*models.WarmupBanStatus, *errx.Error) {
	if s.emailRepo != nil {
		acc, _ := s.emailRepo.GetByID(ctx, accountID)
		if acc == nil || acc.UserID != userID.String() {
			return nil, errx.New(errx.Forbidden, "this mailbox does not belong to you")
		}
	}

	status := &models.WarmupBanStatus{
		EmailAccountID: accountID,
		HealthState:    string(models.WarmupHealthHealthy),
	}

	health, _ := s.getParticipantForAnyPool(ctx, accountID)
	if health != nil {
		status.HealthState = string(health.HealthState)
		status.BlockedAt = health.BlockedAt
		status.BlockedUntil = health.BlockedUntil
		if health.BlockedReason != nil {
			status.Reason = *health.BlockedReason
		}
		if health.HealthState == models.WarmupHealthBlocked || health.HealthState == models.WarmupHealthQuarantined {
			status.Blocked = true
		}
	}

	if status.Blocked {
		pending, _ := s.repo.HasPendingWarmupAppeal(ctx, accountID)
		status.PendingAppeal = pending
		status.CanAppeal = !pending
	}

	return status, nil
}

func (s *service) ApplySpamReport(ctx context.Context, reporterAccountID, reportedAccountID uuid.UUID, messageID, reportType string) (*models.WarmupParticipantHealth, *errx.Error) {
	inserted, err := s.repo.RecordSpamReport(ctx, &repository.SpamReport{
		ID:                uuid.New(),
		ReporterAccountID: reporterAccountID,
		ReportedAccountID: reportedAccountID,
		MessageID:         messageID,
		ReportType:        reportType,
	})
	if err != nil {
		return nil, errx.InternalError()
	}
	if !inserted {
		return s.getParticipantForAnyPool(ctx, reportedAccountID)
	}

	if _, err := s.repo.IncrementSpamScore(ctx, reportedAccountID, 10); err != nil {
		return nil, errx.InternalError()
	}

	return s.evaluateAndPersistAnyPool(ctx, reportedAccountID)
}

func (s *service) ApplyInvalidTokenAttempt(ctx context.Context, accountID uuid.UUID, attemptedToken string, scoreDelta int) (*models.WarmupParticipantHealth, *errx.Error) {
	if err := s.repo.RecordInvalidTokenAttempt(ctx, accountID, attemptedToken); err != nil {
		return nil, errx.InternalError()
	}
	if scoreDelta > 0 {
		if _, err := s.repo.IncrementSpamScore(ctx, accountID, scoreDelta); err != nil {
			return nil, errx.InternalError()
		}
	}
	// The attempt and its score are already persisted, so nothing from here on
	// may be reported as a failure of the whole call: the caller's degraded path
	// re-records both, which doubles every count the band then reads (#195).
	// The evaluation itself already logs its own cause.
	health, err := s.evaluateAndPersistAnyPool(ctx, accountID)
	if err != nil {
		// Best effort: hand back the participant as it stands. If that read
		// fails too (it runs the same probe that just failed), a nil health is
		// still the right answer — the caller treats it as "unknown", not as a
		// reason to record the attempt again.
		health, _ = s.getParticipantForAnyPool(ctx, accountID)
	}
	return health, nil
}

func (s *service) ApplyRateLimitExceeded(ctx context.Context, accountID uuid.UUID, reason string) (*models.WarmupParticipantHealth, *errx.Error) {
	blockedUntil := s.now().UTC().Add(warmupBlockDuration)
	if err := s.repo.UpdateParticipantHealth(ctx, accountID, models.WarmupHealthBlocked, &blockedUntil, reason, 100); err != nil {
		return nil, errx.InternalError()
	}
	return s.getParticipantForAnyPool(ctx, accountID)
}

func (s *service) evaluateAndPersistAnyPool(ctx context.Context, accountID uuid.UUID) (*models.WarmupParticipantHealth, *errx.Error) {
	health, xerr := s.getParticipantForAnyPool(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}
	if health == nil {
		return nil, nil
	}
	return s.evaluateAndPersist(ctx, accountID, health.PoolType, health)
}

// getParticipantForAnyPool reads the row from whichever pool the mailbox is in: one query, not
// one per pool, so there is no probe order to bias toward premium.
func (s *service) getParticipantForAnyPool(ctx context.Context, accountID uuid.UUID) (*models.WarmupParticipantHealth, *errx.Error) {
	health, err := s.repo.GetParticipantHealthForAccount(ctx, accountID)
	if err != nil {
		log.Error().
			Err(err).
			Str("email_account_id", accountID.String()).
			Msg("warmup: pool membership probe failed")
		return nil, errx.InternalError()
	}
	return health, nil
}

func (s *service) evaluateAndPersist(ctx context.Context, accountID uuid.UUID, poolType string, participant *models.WarmupParticipantHealth) (*models.WarmupParticipantHealth, *errx.Error) {
	// errx.Error carries no cause, so every failure below is logged with the
	// real error here. Without that the health bands can stop firing entirely
	// and the only visible symptom is a last_health_evaluated_at that never
	// moves (issue #195).
	fail := func(stage string, cause error) *errx.Error {
		log.Error().
			Err(cause).
			Str("email_account_id", accountID.String()).
			Str("pool_type", poolType).
			Str("stage", stage).
			Msg("warmup: health evaluation failed; no band was applied")
		return errx.InternalError()
	}

	// Signals from before this mailbox was being evaluated are not held against
	// it (see migration 000096).
	signalsFrom := time.Time{}
	priorState := models.WarmupHealthState("")
	if participant != nil {
		signalsFrom = participant.HealthSignalsFrom
		// The prior state is what makes a webhook fire on a real transition
		// rather than on every sweep.
		priorState = participant.HealthState
	}

	metrics, err := s.loadMetrics(ctx, accountID, signalsFrom)
	if err != nil {
		return nil, fail("load_metrics", err)
	}

	decision := evaluateMetrics(metrics, s.now().UTC())
	if err := s.repo.UpdateParticipantHealth(ctx, accountID, decision.State, decision.BlockedUntil, decision.Reason, decision.Score); err != nil {
		return nil, fail("persist", err)
	}

	health, err := s.repo.GetParticipantHealth(ctx, accountID, poolType)
	if err != nil {
		return nil, fail("read_back", err)
	}

	if priorState != "" && health != nil {
		s.dispatchHealthEvent(ctx, accountID, priorState, health.HealthState, decision.Reason)
	}
	return health, nil
}

// loadMetrics counts the signals behind a health decision. No window reaches
// further back than signalsFrom, so a mailbox is never judged on a period it was
// not being judged in.
func (s *service) loadMetrics(ctx context.Context, accountID uuid.UUID, signalsFrom time.Time) (*models.WarmupHealthMetrics, error) {
	now := s.now().UTC()
	since := func(window time.Duration) time.Time {
		start := now.Add(-window)
		if signalsFrom.After(start) {
			return signalsFrom
		}
		return start
	}
	sentLast7d, err := s.repo.SumWarmupSentSince(ctx, accountID, since(7*24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("SumWarmupSentSince: %w", err)
	}

	// Split the warmup spam signal into placement (provider classifier put
	// the mail in Junk) vs user complaint (recipient actively flagged it).
	// These have very different remediation paths so they earn separate
	// rates instead of one combined ratio.
	spamPlacementsLast7d, err := s.repo.CountSpamPlacementsSince(ctx, accountID, since(7*24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("CountSpamPlacementsSince: %w", err)
	}
	userComplaintsLast7d, err := s.repo.CountUserComplaintsSince(ctx, accountID, since(7*24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("CountUserComplaintsSince: %w", err)
	}

	invalidAttemptsLast24h, err := s.repo.CountRecentInvalidAttempts(ctx, accountID, since(24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("CountRecentInvalidAttempts: %w", err)
	}

	spamScore, err := s.repo.GetSpamScore(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("GetSpamScore: %w", err)
	}

	placementRate := 0.0
	warmupComplaintRate := 0.0
	if sentLast7d > 0 {
		placementRate = float64(spamPlacementsLast7d) / float64(sentLast7d) * 100
		warmupComplaintRate = float64(userComplaintsLast7d) / float64(sentLast7d) * 100
	}

	// Load complaint and bounce counts from deliverability events (last 30 days).
	// These cover external (non-warmup) sends and remain on a separate axis.
	since30d := since(30 * 24 * time.Hour)
	complaintsLast30d, err := s.repo.CountDeliverabilityEventsByAccount(ctx, accountID, "complaint", since30d)
	if err != nil {
		return nil, fmt.Errorf("CountDeliverabilityEventsByAccount(complaint): %w", err)
	}
	bouncesLast30d, err := s.repo.CountDeliverabilityEventsByAccount(ctx, accountID, "bounce", since30d)
	if err != nil {
		return nil, fmt.Errorf("CountDeliverabilityEventsByAccount(bounce): %w", err)
	}
	deliveredLast30d, err := s.repo.CountDeliveredByAccount(ctx, accountID, since30d)
	if err != nil {
		return nil, fmt.Errorf("CountDeliveredByAccount: %w", err)
	}

	complaintRate := 0.0
	if deliveredLast30d > 0 {
		complaintRate = float64(complaintsLast30d) / float64(deliveredLast30d) * 100
	}
	bounceRate := 0.0
	if deliveredLast30d > 0 {
		bounceRate = float64(bouncesLast30d) / float64(deliveredLast30d) * 100
	}

	return &models.WarmupHealthMetrics{
		SentLast7d:            sentLast7d,
		SpamReportsLast7d:     spamPlacementsLast7d + userComplaintsLast7d,
		SpamPlacementsLast7d:  spamPlacementsLast7d,
		SpamPlacementRate:     placementRate,
		UserComplaintsLast7d:  userComplaintsLast7d,
		WarmupComplaintRate:   warmupComplaintRate,
		InvalidAttemptsLast24: invalidAttemptsLast24h,
		SpamScore:             spamScore,
		ComplaintsLast30d:     complaintsLast30d,
		DeliveredLast30d:      deliveredLast30d,
		ComplaintRate:         complaintRate,
		BouncesLast30d:        bouncesLast30d,
		BounceRate:            bounceRate,
	}, nil
}

type evaluationDecision struct {
	State        models.WarmupHealthState
	BlockedUntil *time.Time
	Reason       string
	Score        float64
}

func evaluateMetrics(metrics *models.WarmupHealthMetrics, now time.Time) evaluationDecision {
	decision := evaluationDecision{
		State: models.WarmupHealthHealthy,
		Score: metrics.SpamPlacementRate,
	}

	if metrics.InvalidAttemptsLast24 >= invalidTokenBlockThreshold {
		until := now.Add(warmupBlockDuration)
		return evaluationDecision{
			State:        models.WarmupHealthBlocked,
			BlockedUntil: &until,
			Reason:       fmt.Sprintf("invalid warmup token attempts exceeded threshold: %d in 24h", metrics.InvalidAttemptsLast24),
			Score:        maxFloat(100, metrics.SpamPlacementRate),
		}
	}

	// Evaluate complaint rate (requires minimum sample of 100 delivered in 30d)
	if metrics.DeliveredLast30d >= minComplaintSample {
		switch {
		case metrics.ComplaintRate >= complaintRateBlockPct:
			until := now.Add(warmupBlockDuration)
			return evaluationDecision{
				State:        models.WarmupHealthBlocked,
				BlockedUntil: &until,
				Reason:       fmt.Sprintf("complaint rate %.2f%% exceeded block threshold over %d delivered", metrics.ComplaintRate, metrics.DeliveredLast30d),
				Score:        maxFloat(metrics.ComplaintRate*100, metrics.SpamPlacementRate),
			}
		case metrics.ComplaintRate >= complaintRateQuarantinePct:
			until := now.Add(warmupQuarantineDuration)
			return evaluationDecision{
				State:        models.WarmupHealthQuarantined,
				BlockedUntil: &until,
				Reason:       fmt.Sprintf("complaint rate %.2f%% exceeded quarantine threshold", metrics.ComplaintRate),
				Score:        maxFloat(metrics.ComplaintRate*100, metrics.SpamPlacementRate),
			}
		case metrics.ComplaintRate >= complaintRateWatchPct:
			decision = evaluationDecision{
				State:  models.WarmupHealthWatch,
				Reason: fmt.Sprintf("complaint rate %.2f%% in watch band", metrics.ComplaintRate),
				Score:  maxFloat(metrics.ComplaintRate*100, metrics.SpamPlacementRate),
			}
		}
	}

	// Evaluate bounce rate (requires minimum sample of 100 delivered in 30d)
	if metrics.DeliveredLast30d >= minComplaintSample {
		switch {
		case metrics.BounceRate >= bounceRateBlockPct:
			until := now.Add(warmupBlockDuration)
			return evaluationDecision{
				State:        models.WarmupHealthBlocked,
				BlockedUntil: &until,
				Reason:       fmt.Sprintf("bounce rate %.1f%% exceeded block threshold over %d delivered", metrics.BounceRate, metrics.DeliveredLast30d),
				Score:        maxFloat(metrics.BounceRate, metrics.SpamPlacementRate),
			}
		case metrics.BounceRate >= bounceRateQuarantinePct:
			until := now.Add(warmupQuarantineDuration)
			return evaluationDecision{
				State:        models.WarmupHealthQuarantined,
				BlockedUntil: &until,
				Reason:       fmt.Sprintf("bounce rate %.1f%% exceeded quarantine threshold", metrics.BounceRate),
				Score:        maxFloat(metrics.BounceRate, metrics.SpamPlacementRate),
			}
		}
	}

	// Evaluate warmup-internal user-complaint rate. These signals come from
	// recipients actively flagging the warmup mail as spam and warrant their
	// own thresholds — separate from external-recipient complaint rates and
	// from passive folder-placement signals.
	if metrics.SentLast7d >= minSpamPlacementSample {
		switch {
		case metrics.WarmupComplaintRate >= warmupComplaintBlockPct:
			until := now.Add(warmupBlockDuration)
			return evaluationDecision{
				State:        models.WarmupHealthBlocked,
				BlockedUntil: &until,
				Reason:       fmt.Sprintf("warmup user-complaint rate %.2f%% exceeded block threshold", metrics.WarmupComplaintRate),
				Score:        maxFloat(metrics.WarmupComplaintRate*10, metrics.SpamPlacementRate),
			}
		case metrics.WarmupComplaintRate >= warmupComplaintQuarantinePct:
			until := now.Add(warmupQuarantineDuration)
			return evaluationDecision{
				State:        models.WarmupHealthQuarantined,
				BlockedUntil: &until,
				Reason:       fmt.Sprintf("warmup user-complaint rate %.2f%% exceeded quarantine threshold", metrics.WarmupComplaintRate),
				Score:        maxFloat(metrics.WarmupComplaintRate*10, metrics.SpamPlacementRate),
			}
		case metrics.WarmupComplaintRate >= warmupComplaintWatchPct:
			if decision.State == models.WarmupHealthHealthy {
				decision = evaluationDecision{
					State:  models.WarmupHealthWatch,
					Reason: fmt.Sprintf("warmup user-complaint rate %.2f%% in watch band", metrics.WarmupComplaintRate),
					Score:  maxFloat(metrics.WarmupComplaintRate*10, metrics.SpamPlacementRate),
				}
			}
		}
	}

	// Evaluate spam placement rate (requires minimum 20 warmup sends in 7d)
	if metrics.SentLast7d < minSpamPlacementSample {
		return decision
	}

	switch {
	case metrics.SpamPlacementRate >= spamPlacementCatastrophicPct:
		until := now.Add(warmupCatastrophicBlock)
		return evaluationDecision{
			State:        models.WarmupHealthBlocked,
			BlockedUntil: &until,
			Reason:       fmt.Sprintf("catastrophic warmup spam placement %.1f%% over %d sends", metrics.SpamPlacementRate, metrics.SentLast7d),
			Score:        metrics.SpamPlacementRate,
		}
	case metrics.SpamPlacementRate >= spamPlacementBlockPct:
		until := now.Add(warmupBlockDuration)
		return evaluationDecision{
			State:        models.WarmupHealthBlocked,
			BlockedUntil: &until,
			Reason:       fmt.Sprintf("warmup spam placement %.1f%% exceeded block threshold", metrics.SpamPlacementRate),
			Score:        metrics.SpamPlacementRate,
		}
	case metrics.SpamPlacementRate >= spamPlacementQuarantinePct:
		until := now.Add(warmupQuarantineDuration)
		return evaluationDecision{
			State:        models.WarmupHealthQuarantined,
			BlockedUntil: &until,
			Reason:       fmt.Sprintf("warmup spam placement %.1f%% exceeded quarantine threshold", metrics.SpamPlacementRate),
			Score:        metrics.SpamPlacementRate,
		}
	case metrics.SpamPlacementRate >= spamPlacementThrottlePct:
		until := now.Add(warmupThrottleDuration)
		return evaluationDecision{
			State:        models.WarmupHealthThrottled,
			BlockedUntil: &until,
			Reason:       fmt.Sprintf("warmup spam placement %.1f%% in throttle band", metrics.SpamPlacementRate),
			Score:        metrics.SpamPlacementRate,
		}
	case metrics.SpamPlacementRate >= spamPlacementWatchPct:
		// Only upgrade to watch if not already at a worse state from complaint checks
		if decision.State == models.WarmupHealthHealthy {
			return evaluationDecision{
				State:  models.WarmupHealthWatch,
				Reason: fmt.Sprintf("warmup spam placement %.1f%% in watch band", metrics.SpamPlacementRate),
				Score:  metrics.SpamPlacementRate,
			}
		}
		return decision
	default:
		return decision
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// EvaluateAllParticipants runs a health evaluation sweep across all warmup pool participants.
// Returns the number evaluated and the number of state changes.
func (s *service) EvaluateAllParticipants(ctx context.Context) (int, int, *errx.Error) {
	accountIDs, err := s.repo.GetAllParticipantAccountIDs(ctx)
	if err != nil {
		log.Error().Err(err).Msg("warmup: health sweep could not list participants")
		return 0, 0, errx.InternalError()
	}

	evaluated := 0
	stateChanges := 0
	skipped := 0

	for _, accountID := range accountIDs {
		// Get current state before evaluation
		healthBefore, err := s.repo.GetParticipantHealth(ctx, accountID, "")
		if err != nil || healthBefore == nil {
			// Try both pool types
			for _, poolType := range []string{"premium", "free"} {
				healthBefore, err = s.repo.GetParticipantHealth(ctx, accountID, poolType)
				if err == nil && healthBefore != nil {
					break
				}
			}
		}

		var stateBefore models.WarmupHealthState
		if healthBefore != nil {
			stateBefore = healthBefore.HealthState
		}

		// Evaluate. evaluateAndPersistAnyPool logs the underlying cause; count
		// the skip here so the sweep can report how much of the pool it failed
		// to evaluate instead of quietly reporting only what worked.
		healthAfter, xerr := s.evaluateAndPersistAnyPool(ctx, accountID)
		if xerr != nil {
			skipped++
			continue
		}
		evaluated++

		if healthAfter != nil && healthAfter.HealthState != stateBefore {
			stateChanges++
		}
	}

	if skipped > 0 {
		log.Error().
			Int("skipped", skipped).
			Int("evaluated", evaluated).
			Int("participants", len(accountIDs)).
			Msg("warmup: health sweep could not evaluate every participant; those accounts keep their last known band")
	}

	return evaluated, stateChanges, nil
}

// GetPoolHealthSummary returns an aggregate health overview across all warmup pools
func (s *service) GetPoolHealthSummary(ctx context.Context) (*models.WarmupPoolHealthSummary, *errx.Error) {
	counts, avgScore, err := s.repo.GetPoolHealthCounts(ctx)
	if err != nil {
		return nil, errx.InternalError()
	}

	// Pool-wide spam-placement rate over the last 7 days. Previously this
	// summary field was always serialised as 0 because nothing populated it.
	since := s.now().UTC().Add(-7 * 24 * time.Hour)
	placementRate, prErr := s.repo.PoolSpamPlacementRate(ctx, since)
	if prErr != nil {
		return nil, errx.InternalError()
	}
	byProvider, bpErr := s.repo.PoolSpamPlacementsByProvider(ctx, since)
	if bpErr != nil {
		return nil, errx.InternalError()
	}

	total := 0
	blockedCount := 0
	atRiskCount := 0
	for state, count := range counts {
		total += count
		switch models.WarmupHealthState(state) {
		case models.WarmupHealthQuarantined, models.WarmupHealthBlocked:
			blockedCount += count
		case models.WarmupHealthWatch, models.WarmupHealthThrottled:
			atRiskCount += count
		}
	}

	return &models.WarmupPoolHealthSummary{
		TotalParticipants:       total,
		ByState:                 counts,
		AvgSpamScore:            avgScore,
		AvgSpamPlacement:        placementRate,
		SpamPlacementByProvider: byProvider,
		BlockedCount:            blockedCount,
		AtRiskCount:             atRiskCount,
	}, nil
}
