// Package referral settles the referrals made while the referral program ran.
// No new referral is recorded; an invitee already attributed keeps its
// discount, and its referrer is still rewarded (and clawed back) through the
// Stripe webhooks, credited onto the referrer's Stripe customer balance.
// Reward grants and clawbacks are keyed on the Stripe event id so a retried
// webhook never double-grants.
package referral

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	DefaultCurrency  = "usd" // reward ledger currency
	MonthlyRewardCap = 50    // max rewarded conversions per referrer per 30 days
	clawbackWindow   = 30 * 24 * time.Hour
)

// AuditPublisher records org-scoped audit events. *audit.Service satisfies it.
// Used from webhook paths (no gin.Context) so a reward/clawback still flows
// through the AUDIT_CREATED realtime spine to the referrer's dashboard.
type AuditPublisher interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string)
}

// StripeBalancer applies a signed cents delta to a customer's Stripe balance
// (negative = credit the customer). The stripe service satisfies it; injected
// via WireBalancer to avoid an import cycle.
type StripeBalancer interface {
	ApplyCustomerCredit(ctx context.Context, customerID string, amountCents int64, currency, idempotencyKey string) (string, *errx.Error)
}

// Service is the referral program API. The Qualify/Reward/Clawback/Sync methods
// Service is driven by the Stripe webhooks, plus the checkout lookup of an
// attributed invitee's discount.
type Service interface {
	// InviteeDiscountCode returns the referral discount code an invitee org
	// should auto-apply at its first checkout, or "" if none applies.
	InviteeDiscountCode(ctx context.Context, inviteeOrgID uuid.UUID) string

	// WireBalancer attaches the Stripe customer-balance applier.
	WireBalancer(b StripeBalancer)

	// QualifyOnConversion marks an invitee's attribution qualified once they
	// reach a paid checkout.
	QualifyOnConversion(ctx context.Context, inviteeOrgID uuid.UUID)
	// RewardOnFirstInvoice grants the referrer credit for an invitee's first
	// paid invoice. planID is the invitee's plan; eventID keys idempotency.
	RewardOnFirstInvoice(ctx context.Context, inviteeOrgID, planID uuid.UUID, eventID string) *errx.Error
	// ClawbackForInvitee reverses a reward when the invitee refunds or cancels
	// inside the clawback window.
	ClawbackForInvitee(ctx context.Context, inviteeOrgID uuid.UUID, eventID, reason string)
	// SyncStripeBalance flushes any unpushed referral credit for an org onto its
	// Stripe customer balance (called when the referrer gains a customer).
	SyncStripeBalance(ctx context.Context, orgID uuid.UUID)
}

type service struct {
	repo     repository.ReferralRepository
	planRepo repository.PlanRepository
	subRepo  repository.SubscriptionRepository
	userRepo repository.UserRepository
	audit    AuditPublisher
	balancer StripeBalancer
}

// NewService builds the referral service. audit may be nil.
func NewService(
	repo repository.ReferralRepository,
	planRepo repository.PlanRepository,
	subRepo repository.SubscriptionRepository,
	userRepo repository.UserRepository,
	audit AuditPublisher,
) Service {
	return &service{repo: repo, planRepo: planRepo, subRepo: subRepo, userRepo: userRepo, audit: audit}
}

func (s *service) WireBalancer(b StripeBalancer) { s.balancer = b }

func (s *service) InviteeDiscountCode(ctx context.Context, inviteeOrgID uuid.UUID) string {
	attr, err := s.repo.GetAttributionByInviteeOrg(ctx, inviteeOrgID)
	if err != nil || attr == nil || attr.ReferralCodeID == nil {
		return ""
	}
	if attr.Status != models.ReferralStatusPending && attr.Status != models.ReferralStatusQualified {
		return ""
	}
	rc, err := s.repo.GetCodeByID(ctx, *attr.ReferralCodeID)
	if err != nil || rc == nil {
		return ""
	}
	return rc.Code
}

// --- webhook-driven reward lifecycle ---

func (s *service) QualifyOnConversion(ctx context.Context, inviteeOrgID uuid.UUID) {
	attr, err := s.repo.GetAttributionByInviteeOrg(ctx, inviteeOrgID)
	if err != nil || attr == nil {
		return
	}
	if attr.Status != models.ReferralStatusPending {
		return
	}
	if err := s.repo.MarkAttributionQualified(ctx, attr.ID); err != nil {
		errs.CaptureException(err)
	}
}

func (s *service) RewardOnFirstInvoice(ctx context.Context, inviteeOrgID, planID uuid.UUID, eventID string) *errx.Error {
	attr, err := s.repo.GetAttributionByInviteeOrg(ctx, inviteeOrgID)
	if err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to load referral attribution")
	}
	if attr == nil {
		return nil // not a referred org
	}
	if attr.Status != models.ReferralStatusPending && attr.Status != models.ReferralStatusQualified {
		return nil // already rewarded or void
	}

	// Self-referral guard at reward time: same person on both sides.
	if s.isSelfReferral(ctx, attr) {
		_ = s.repo.MarkAttributionVoid(ctx, attr.ID, "self_referral")
		return nil
	}

	// Per-referrer monthly cap on rewarded conversions. A count failure is
	// surfaced (not silently treated as under-cap) but doesn't block the reward.
	since := time.Now().Add(-clawbackWindow)
	if n, cerr := s.repo.CountRewardedByReferrerSince(ctx, attr.ReferrerOrgID, since); cerr != nil {
		errs.CaptureException(cerr)
	} else if n >= MonthlyRewardCap {
		_ = s.repo.MarkAttributionVoid(ctx, attr.ID, "monthly_cap")
		return nil
	}

	plan, perr := s.planRepo.GetByID(ctx, planID)
	if perr != nil || plan == nil {
		return nil // can't price the reward; leave attribution for a later retry
	}
	reward := plan.ReferralRewardCents()
	if reward <= 0 {
		_ = s.repo.MarkAttributionRewarded(ctx, attr.ID, 0, DefaultCurrency)
		return nil
	}

	// One atomic transaction: idempotency check + the pending|qualified ->
	// rewarded gate + the ledger credit. applied=false = already rewarded or a
	// replayed event, so nothing moved.
	applied, err := s.repo.ApplyReferralReward(ctx, attr, reward, DefaultCurrency, eventID)
	if err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to record referral reward")
	}
	if !applied {
		return nil
	}
	s.logCredit(ctx, attr, models.AuditActionCreate, reward)
	s.SyncStripeBalance(ctx, attr.ReferrerOrgID)
	return nil
}

func (s *service) ClawbackForInvitee(ctx context.Context, inviteeOrgID uuid.UUID, eventID, reason string) {
	attr, err := s.repo.GetAttributionByInviteeOrg(ctx, inviteeOrgID)
	if err != nil || attr == nil {
		return
	}
	if attr.Status != models.ReferralStatusRewarded {
		return // only a paid-out reward can be clawed back
	}
	// Vesting: rewards older than the clawback window are kept.
	if attr.RewardedAt != nil && time.Since(*attr.RewardedAt) > clawbackWindow {
		return
	}
	if attr.RewardCents <= 0 {
		_ = s.repo.MarkAttributionVoid(ctx, attr.ID, reason)
		return
	}

	// One atomic transaction: idempotency check + the rewarded -> void gate +
	// the ledger debit. The gate means a concurrent refund + cancel can't both
	// reverse the same reward.
	applied, err := s.repo.ApplyReferralClawback(ctx, attr, attr.RewardCents, attr.RewardCurrency, eventID+":clawback", reason)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	if !applied {
		return
	}
	s.logCredit(ctx, attr, models.AuditActionUpdate, -attr.RewardCents)
	s.SyncStripeBalance(ctx, attr.ReferrerOrgID)
}

// SyncStripeBalance mirrors the org's net referral credit onto its Stripe
// customer balance. Best-effort: the ledger is always authoritative, so a
// failed push is retried on the next event.
func (s *service) SyncStripeBalance(ctx context.Context, orgID uuid.UUID) {
	if s.balancer == nil {
		return
	}
	ledger, err := s.repo.GetLedger(ctx, orgID)
	if err != nil || ledger == nil {
		return
	}
	// Amount to add to the Stripe balance so it equals -balance_cents.
	delta := ledger.StripePushedCents - ledger.BalanceCents
	if delta == 0 {
		return
	}
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil || sub == nil || sub.StripeCustomerID == "" {
		return // no Stripe customer yet; flush when they subscribe
	}
	// The idempotency key is the TARGET ledger state, not the triggering event.
	// So if the watermark write below fails and a later (differently-triggered)
	// sync recomputes the same delta to reach the same target, Stripe dedups it
	// and the credit can never be applied twice. lifetime_earned is monotonic,
	// so a balance that returns to a prior value still yields a distinct key.
	key := fmt.Sprintf("refbal:%s:%d:%d", orgID, ledger.LifetimeEarnedCents, ledger.BalanceCents)
	if _, xerr := s.balancer.ApplyCustomerCredit(ctx, sub.StripeCustomerID, delta, ledger.Currency, key); xerr != nil {
		errs.CaptureException(xerr)
		return
	}
	if err := s.repo.SetStripePushed(ctx, orgID, ledger.BalanceCents); err != nil {
		errs.CaptureException(err)
	}
}

// --- helpers ---

func (s *service) isSelfReferral(ctx context.Context, attr *models.ReferralAttribution) bool {
	if attr.InviteeUserID == nil {
		return false
	}
	referrer, err := s.userRepo.GetUser(ctx, attr.ReferrerUserID)
	if err != nil || referrer == nil {
		return false
	}
	invitee, err := s.userRepo.GetUser(ctx, *attr.InviteeUserID)
	if err != nil || invitee == nil {
		return false
	}
	re := strings.ToLower(strings.TrimSpace(referrer.Email))
	ie := strings.ToLower(strings.TrimSpace(invitee.Email))
	return re != "" && re == ie
}

func (s *service) logCredit(ctx context.Context, attr *models.ReferralAttribution, action models.AuditAction, amountCents int64) {
	if s.audit == nil {
		return
	}
	id := attr.ID
	s.audit.LogAction(ctx, attr.ReferrerOrgID, attr.ReferrerUserID, action, models.AuditEntityReferralCredit, &id, "", "", nil, map[string]string{
		"amount_cents": strconv.FormatInt(amountCents, 10),
	})
}
