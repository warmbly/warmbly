// Package referral implements the customer referral program: a user shares a
// code, an invitee who signs up with it gets 10% off their first 3 months, and
// when the invitee converts to a paid plan the referrer earns account credit
// equal to one month-equivalent of the invitee's plan price. The credit is
// recorded in a dollar ledger and mirrored onto the referrer's Stripe customer
// balance so it nets off their invoices.
//
// The referral code IS a discount_codes row, so the invitee discount reuses the
// existing checkout/coupon path with no new Stripe mechanics. Reward grants and
// clawbacks are driven by Stripe webhooks and keyed on the Stripe event id so a
// retried webhook never double-grants.
package referral

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Product defaults for the referral program.
const (
	InviteePercentOff = 10    // invitee discount, percent off
	InviteeMonths     = 3     // invitee discount, months it repeats
	DefaultCurrency   = "usd" // reward ledger currency
	MonthlyRewardCap  = 50    // max rewarded conversions per referrer per 30 days
	clawbackWindow    = 30 * 24 * time.Hour
	defaultShareBase  = "https://app.warmbly.com"
	codeAlphabet      = "ABCDEFGHJKMNPQRSTVWXYZ23456789" // no ambiguous chars
	codeLength        = 8
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
// are driven by Stripe webhooks; the rest serve the dashboard + signup flow.
type Service interface {
	// EnsureCode idempotently mints the caller's canonical referral code (and
	// its backing 10%/3-month discount code) and returns it.
	EnsureCode(ctx context.Context, ownerUserID, ownerOrgID uuid.UUID) (*models.ReferralCode, *errx.Error)
	// Summary returns the dashboard payload (code, link, earnings, counts),
	// minting the code if needed.
	Summary(ctx context.Context, ownerUserID, ownerOrgID uuid.UUID) (*models.ReferralSummary, *errx.Error)
	ListAttributions(ctx context.Context, referrerOrgID uuid.UUID, limit, offset int) ([]models.ReferralAttribution, *errx.Error)
	ListEarnings(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]models.ReferralEarningsTransaction, *errx.Error)

	// AttributeSignup links a new invitee org to the referrer behind `code`.
	// Best-effort: a bad/self code never blocks signup.
	AttributeSignup(ctx context.Context, code string, inviteeOrgID, inviteeUserID uuid.UUID) *errx.Error

	// InviteeDiscountCode returns the referral discount code an invitee org
	// should auto-apply at its first checkout, or "" if none applies.
	InviteeDiscountCode(ctx context.Context, inviteeOrgID uuid.UUID) string

	// WireBalancer attaches the Stripe customer-balance applier.
	WireBalancer(b StripeBalancer)

	// --- webhook-driven (called by the stripe service) ---

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
	repo         repository.ReferralRepository
	discountRepo repository.DiscountCodeRepository
	planRepo     repository.PlanRepository
	subRepo      repository.SubscriptionRepository
	userRepo     repository.UserRepository
	audit        AuditPublisher
	balancer     StripeBalancer
	shareBase    string
}

// NewService builds the referral service. audit may be nil. shareBase is the
// public base URL for share links (falls back to the app default when empty).
func NewService(
	repo repository.ReferralRepository,
	discountRepo repository.DiscountCodeRepository,
	planRepo repository.PlanRepository,
	subRepo repository.SubscriptionRepository,
	userRepo repository.UserRepository,
	audit AuditPublisher,
	shareBase string,
) Service {
	if strings.TrimSpace(shareBase) == "" {
		shareBase = defaultShareBase
	}
	return &service{
		repo:         repo,
		discountRepo: discountRepo,
		planRepo:     planRepo,
		subRepo:      subRepo,
		userRepo:     userRepo,
		audit:        audit,
		shareBase:    strings.TrimRight(shareBase, "/"),
	}
}

func (s *service) WireBalancer(b StripeBalancer) { s.balancer = b }

// --- code minting ---

func genCode() string {
	b := make([]byte, codeLength)
	_, _ = rand.Read(b)
	out := make([]byte, codeLength)
	for i, v := range b {
		out[i] = codeAlphabet[int(v)%len(codeAlphabet)]
	}
	return string(out)
}

func (s *service) EnsureCode(ctx context.Context, ownerUserID, ownerOrgID uuid.UUID) (*models.ReferralCode, *errx.Error) {
	existing, err := s.repo.GetCodeByOwner(ctx, ownerUserID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load referral code")
	}
	if existing != nil {
		return existing, nil
	}

	// Mint a fresh, collision-free code and its backing discount row.
	percent := InviteePercentOff
	months := InviteeMonths
	for attempt := 0; attempt < 6; attempt++ {
		code := genCode()

		taken, err := s.repo.GetCodeByCode(ctx, code)
		if err != nil {
			sentry.CaptureException(err)
			return nil, errx.New(errx.Internal, "failed to mint referral code")
		}
		if taken != nil {
			continue
		}
		clash, err := s.discountRepo.GetByCode(ctx, code)
		if err != nil {
			sentry.CaptureException(err)
			return nil, errx.New(errx.Internal, "failed to mint referral code")
		}
		if clash != nil {
			continue
		}

		dc := &models.DiscountCode{
			ID:                uuid.New(),
			Code:              code,
			Description:       "Referral reward: 10% off for 3 months",
			Type:              models.DiscountTypePercent,
			PercentOff:        &percent,
			Duration:          models.DiscountDurationRepeating,
			DurationInMonths:  &months,
			PerAccountLimit:   1,
			AppliesToAllPlans: true,
			Status:            models.DiscountCodeStatusActive,
		}
		if err := s.discountRepo.Create(ctx, dc); err != nil {
			// Most likely a unique-code race; try a different code.
			continue
		}

		rc := &models.ReferralCode{
			ID:             uuid.New(),
			OwnerUserID:    ownerUserID,
			OwnerOrgID:     ownerOrgID,
			Code:           code,
			DiscountCodeID: &dc.ID,
		}
		if err := s.repo.CreateCode(ctx, rc); err != nil {
			// Owner raced to a code on another request; return whatever stuck.
			if again, gerr := s.repo.GetCodeByOwner(ctx, ownerUserID); gerr == nil && again != nil {
				return again, nil
			}
			sentry.CaptureException(err)
			return nil, errx.New(errx.Internal, "failed to mint referral code")
		}
		return rc, nil
	}
	return nil, errx.New(errx.Internal, "failed to mint a unique referral code")
}

func (s *service) shareURL(code string) string {
	return s.shareBase + "/register?ref=" + code
}

func (s *service) Summary(ctx context.Context, ownerUserID, ownerOrgID uuid.UUID) (*models.ReferralSummary, *errx.Error) {
	code, xerr := s.EnsureCode(ctx, ownerUserID, ownerOrgID)
	if xerr != nil {
		return nil, xerr
	}

	ledger, err := s.repo.GetLedger(ctx, ownerOrgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load referral earnings")
	}
	total, pending, qualified, rewarded, err := s.repo.AttributionStats(ctx, ownerOrgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load referral stats")
	}

	sum := &models.ReferralSummary{
		Code:              code.Code,
		ShareURL:          s.shareURL(code.Code),
		Currency:          DefaultCurrency,
		InviteePercentOff: InviteePercentOff,
		InviteeMonths:     InviteeMonths,
		TotalReferred:     total,
		Pending:           pending,
		Qualified:         qualified,
		Rewarded:          rewarded,
	}
	if ledger != nil {
		sum.BalanceCents = ledger.BalanceCents
		sum.LifetimeEarnedCents = ledger.LifetimeEarnedCents
		sum.Currency = ledger.Currency
	}
	return sum, nil
}

func (s *service) ListAttributions(ctx context.Context, referrerOrgID uuid.UUID, limit, offset int) ([]models.ReferralAttribution, *errx.Error) {
	rows, err := s.repo.ListAttributionsByReferrer(ctx, referrerOrgID, limit, offset)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list referrals")
	}
	return rows, nil
}

func (s *service) ListEarnings(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]models.ReferralEarningsTransaction, *errx.Error) {
	rows, err := s.repo.ListEarnings(ctx, orgID, limit, offset)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list referral earnings")
	}
	return rows, nil
}

// --- signup attribution ---

func (s *service) AttributeSignup(ctx context.Context, code string, inviteeOrgID, inviteeUserID uuid.UUID) *errx.Error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil
	}
	rc, err := s.repo.GetCodeByCode(ctx, code)
	if err != nil {
		sentry.CaptureException(err)
		return nil // never block signup on a lookup failure
	}
	if rc == nil {
		return nil // unknown code: ignore silently
	}
	// Self-referral guard: a user can't refer their own new org/account.
	if rc.OwnerUserID == inviteeUserID || rc.OwnerOrgID == inviteeOrgID {
		return nil
	}

	attr := &models.ReferralAttribution{
		ID:             uuid.New(),
		ReferralCodeID: &rc.ID,
		ReferrerUserID: rc.OwnerUserID,
		ReferrerOrgID:  rc.OwnerOrgID,
		InviteeOrgID:   inviteeOrgID,
		InviteeUserID:  &inviteeUserID,
		Status:         models.ReferralStatusPending,
	}
	if err := s.repo.CreateAttribution(ctx, attr); err != nil {
		// UNIQUE(invitee_org_id): the org was already referred. Ignore.
		return nil
	}
	return nil
}

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
		sentry.CaptureException(err)
	}
}

func (s *service) RewardOnFirstInvoice(ctx context.Context, inviteeOrgID, planID uuid.UUID, eventID string) *errx.Error {
	attr, err := s.repo.GetAttributionByInviteeOrg(ctx, inviteeOrgID)
	if err != nil {
		sentry.CaptureException(err)
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
		sentry.CaptureException(cerr)
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
		sentry.CaptureException(err)
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
		sentry.CaptureException(err)
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
		sentry.CaptureException(xerr)
		return
	}
	if err := s.repo.SetStripePushed(ctx, orgID, ledger.BalanceCents); err != nil {
		sentry.CaptureException(err)
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
