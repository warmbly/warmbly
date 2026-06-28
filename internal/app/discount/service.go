package discount

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// AuditLogger is the slice of the admin service the discount service needs to
// record management actions. adminService.LogAdminAction satisfies it.
type AuditLogger interface {
	LogAdminAction(ctx context.Context, adminID uuid.UUID, action, targetType string, targetID *uuid.UUID, details map[string]any, ipAddress, userAgent string)
}

// DiscountService owns discount-code management (admin) and validation +
// redemption recording (billing). It deliberately knows nothing about Stripe;
// the stripe service mints coupons and calls back here to record redemptions.
type DiscountService interface {
	// Admin management
	List(ctx context.Context, search *models.AdminDiscountSearch) (*models.AdminDiscountsResult, *errx.Error)
	Get(ctx context.Context, id uuid.UUID) (*models.DiscountCode, *errx.Error)
	Create(ctx context.Context, adminID uuid.UUID, req *models.CreateDiscountCodeRequest, ipAddress, userAgent string) (*models.DiscountCode, *errx.Error)
	Update(ctx context.Context, adminID, id uuid.UUID, req *models.UpdateDiscountCodeRequest, ipAddress, userAgent string) (*models.DiscountCode, *errx.Error)
	Delete(ctx context.Context, adminID, id uuid.UUID, ipAddress, userAgent string) *errx.Error
	ListRedemptions(ctx context.Context, codeID uuid.UUID, offset, limit int) (*models.AdminDiscountRedemptionsResult, *errx.Error)

	// ListOrganizationRedemptions returns an org's own redemption history for
	// the customer billing page.
	ListOrganizationRedemptions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.DiscountRedemption, *errx.Error)

	// Customer-facing validation (pre-checkout preview). Never errors on a
	// business-invalid code; returns Valid=false with a reason instead.
	Validate(ctx context.Context, orgID uuid.UUID, code string, planID *uuid.UUID) (*models.DiscountPreview, *errx.Error)

	// ValidateForCheckout strictly resolves a code for redemption against a
	// concrete plan. Returns errx.BadRequest when the code can't be applied.
	ValidateForCheckout(ctx context.Context, orgID uuid.UUID, code string, planID uuid.UUID) (*models.DiscountCode, *errx.Error)

	// Redemption recording, called by the stripe service. The reserve* methods
	// atomically claim the cap slot BEFORE any Stripe coupon is minted, and
	// return the redemption ID so the caller can attach Stripe refs on success
	// or release the slot via CancelRedemptionByID on failure.
	ReservePendingRedemption(ctx context.Context, code *models.DiscountCode, orgID uuid.UUID, redeemedBy, planID *uuid.UUID) (uuid.UUID, *errx.Error)
	ReserveAppliedRedemption(ctx context.Context, code *models.DiscountCode, orgID uuid.UUID, redeemedBy, planID, subscriptionID *uuid.UUID) (uuid.UUID, *errx.Error)
	AttachRedemptionStripe(ctx context.Context, redemptionID uuid.UUID, sessionID, couponID *string) *errx.Error
	CancelRedemptionByID(ctx context.Context, redemptionID uuid.UUID) *errx.Error
	MarkRedemptionApplied(ctx context.Context, sessionID string, subscriptionID *uuid.UUID) *errx.Error
	CancelRedemption(ctx context.Context, sessionID string) *errx.Error
}

type service struct {
	codeRepo repository.DiscountCodeRepository
	redRepo  repository.DiscountRedemptionRepository
	planRepo repository.PlanRepository
	audit    AuditLogger
}

// NewService builds the discount service. audit may be nil (management actions
// simply won't be audited in that case).
func NewService(
	codeRepo repository.DiscountCodeRepository,
	redRepo repository.DiscountRedemptionRepository,
	planRepo repository.PlanRepository,
	audit AuditLogger,
) DiscountService {
	return &service{codeRepo: codeRepo, redRepo: redRepo, planRepo: planRepo, audit: audit}
}

// NormalizeCode is the canonical on-disk form for a code (trimmed, uppercased).
func NormalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// --- Admin management ---

func (s *service) List(ctx context.Context, search *models.AdminDiscountSearch) (*models.AdminDiscountsResult, *errx.Error) {
	result, err := s.codeRepo.List(ctx, search)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list discount codes")
	}
	return result, nil
}

func (s *service) Get(ctx context.Context, id uuid.UUID) (*models.DiscountCode, *errx.Error) {
	code, err := s.codeRepo.GetByID(ctx, id)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get discount code")
	}
	if code == nil {
		return nil, errx.ErrNotFound
	}
	return code, nil
}

func (s *service) Create(ctx context.Context, adminID uuid.UUID, req *models.CreateDiscountCodeRequest, ipAddress, userAgent string) (*models.DiscountCode, *errx.Error) {
	code := NormalizeCode(req.Code)
	if code == "" {
		return nil, errx.New(errx.BadRequest, "code is required")
	}

	duration := req.Duration
	if duration == "" {
		duration = models.DiscountDurationOnce
	}
	if !req.Type.IsMoney() {
		// duration only applies to money discounts.
		duration = models.DiscountDurationOnce
	}

	dc := &models.DiscountCode{
		ID:                 uuid.New(),
		Code:               code,
		Description:        req.Description,
		Type:               req.Type,
		PercentOff:         req.PercentOff,
		AmountOff:          req.AmountOff,
		Currency:           normalizeCurrency(req.Currency),
		TrialExtensionDays: req.TrialExtensionDays,
		Duration:           duration,
		DurationInMonths:   req.DurationInMonths,
		MaxRedemptions:     req.MaxRedemptions,
		PerAccountLimit:    1,
		AppliesToAllPlans:  req.AppliesToAllPlans,
		PlanIDs:            req.PlanIDs,
		Status:             models.DiscountCodeStatusActive,
		StartsAt:           req.StartsAt,
		ExpiresAt:          req.ExpiresAt,
		CreatedBy:          &adminID,
	}
	if req.PerAccountLimit != nil {
		dc.PerAccountLimit = *req.PerAccountLimit
	}
	if req.Status != "" {
		dc.Status = req.Status
	}
	// A code restricted to no plans must not silently match everything.
	if !dc.AppliesToAllPlans && len(dc.PlanIDs) == 0 {
		return nil, errx.New(errx.BadRequest, "select at least one eligible plan, or mark the code valid for all plans")
	}
	if dc.AppliesToAllPlans {
		dc.PlanIDs = nil
	}

	if reason := validateShape(dc); reason != "" {
		return nil, errx.New(errx.BadRequest, reason)
	}

	existing, err := s.codeRepo.GetByCode(ctx, code)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to check existing code")
	}
	if existing != nil {
		return nil, errx.New(errx.Conflict, "a discount code with that name already exists")
	}

	if err := s.codeRepo.Create(ctx, dc); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to create discount code")
	}

	s.log(ctx, adminID, "create_discount_code", dc.ID, map[string]any{"code": dc.Code, "type": dc.Type}, ipAddress, userAgent)
	return s.Get(ctx, dc.ID)
}

func (s *service) Update(ctx context.Context, adminID, id uuid.UUID, req *models.UpdateDiscountCodeRequest, ipAddress, userAgent string) (*models.DiscountCode, *errx.Error) {
	dc, err := s.codeRepo.GetByID(ctx, id)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get discount code")
	}
	if dc == nil {
		return nil, errx.ErrNotFound
	}

	if req.Description != nil {
		dc.Description = *req.Description
	}
	if req.PercentOff != nil {
		dc.PercentOff = req.PercentOff
	}
	if req.AmountOff != nil {
		dc.AmountOff = req.AmountOff
	}
	if req.Currency != nil {
		dc.Currency = normalizeCurrency(req.Currency)
	}
	if req.TrialExtensionDays != nil {
		dc.TrialExtensionDays = req.TrialExtensionDays
	}
	if req.Duration != nil {
		dc.Duration = *req.Duration
	}
	if req.DurationInMonths != nil {
		dc.DurationInMonths = req.DurationInMonths
	}
	if req.MaxRedemptions != nil {
		dc.MaxRedemptions = req.MaxRedemptions
	}
	if req.PerAccountLimit != nil {
		dc.PerAccountLimit = *req.PerAccountLimit
	}
	if req.AppliesToAllPlans != nil {
		dc.AppliesToAllPlans = *req.AppliesToAllPlans
	}
	if req.PlanIDs != nil {
		dc.PlanIDs = *req.PlanIDs
	}
	if req.Status != nil {
		dc.Status = *req.Status
	}
	if req.StartsAt != nil {
		dc.StartsAt = req.StartsAt
	}
	if req.ExpiresAt != nil {
		dc.ExpiresAt = req.ExpiresAt
	}

	if !dc.Type.IsMoney() {
		dc.Duration = models.DiscountDurationOnce
	}
	if dc.AppliesToAllPlans {
		dc.PlanIDs = nil
	} else if len(dc.PlanIDs) == 0 {
		return nil, errx.New(errx.BadRequest, "select at least one eligible plan, or mark the code valid for all plans")
	}

	if reason := validateShape(dc); reason != "" {
		return nil, errx.New(errx.BadRequest, reason)
	}

	if err := s.codeRepo.Update(ctx, dc); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to update discount code")
	}

	s.log(ctx, adminID, "update_discount_code", dc.ID, map[string]any{"code": dc.Code}, ipAddress, userAgent)
	return s.Get(ctx, dc.ID)
}

func (s *service) Delete(ctx context.Context, adminID, id uuid.UUID, ipAddress, userAgent string) *errx.Error {
	dc, err := s.codeRepo.GetByID(ctx, id)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to get discount code")
	}
	if dc == nil {
		return errx.ErrNotFound
	}
	if err := s.codeRepo.Delete(ctx, id); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to delete discount code")
	}
	s.log(ctx, adminID, "delete_discount_code", id, map[string]any{"code": dc.Code}, ipAddress, userAgent)
	return nil
}

func (s *service) ListRedemptions(ctx context.Context, codeID uuid.UUID, offset, limit int) (*models.AdminDiscountRedemptionsResult, *errx.Error) {
	result, err := s.redRepo.ListByCode(ctx, codeID, offset, limit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list redemptions")
	}
	return result, nil
}

func (s *service) ListOrganizationRedemptions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.DiscountRedemption, *errx.Error) {
	rows, err := s.redRepo.ListByOrganization(ctx, orgID, limit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list redemptions")
	}
	return rows, nil
}

// --- Customer-facing validation ---

func (s *service) Validate(ctx context.Context, orgID uuid.UUID, code string, planID *uuid.UUID) (*models.DiscountPreview, *errx.Error) {
	dc, reason, xerr := s.resolve(ctx, orgID, code, planID)
	if xerr != nil {
		return nil, xerr
	}
	if reason != "" {
		return &models.DiscountPreview{Valid: false, Reason: reason}, nil
	}

	preview := &models.DiscountPreview{
		Valid:              true,
		Code:               dc.Code,
		Type:               dc.Type,
		PercentOff:         dc.PercentOff,
		AmountOff:          dc.AmountOff,
		Currency:           dc.Currency,
		TrialExtensionDays: dc.TrialExtensionDays,
		Duration:           dc.Duration,
		DurationInMonths:   dc.DurationInMonths,
	}

	// Plan-specific money computation, best-effort.
	if planID != nil && dc.Type.IsMoney() {
		if plan, err := s.planRepo.GetByID(ctx, *planID); err == nil && plan != nil {
			original := float64(plan.Price)
			discounted := applyMoneyDiscount(original, dc)
			savings := original - discounted
			preview.OriginalAmount = &original
			preview.DiscountedAmount = &discounted
			preview.SavingsAmount = &savings
		}
	}

	return preview, nil
}

func (s *service) ValidateForCheckout(ctx context.Context, orgID uuid.UUID, code string, planID uuid.UUID) (*models.DiscountCode, *errx.Error) {
	dc, reason, xerr := s.resolve(ctx, orgID, code, &planID)
	if xerr != nil {
		return nil, xerr
	}
	if reason != "" {
		return nil, errx.New(errx.BadRequest, reason)
	}
	return dc, nil
}

// resolve returns (code, invalidReason, internalErr). A non-empty reason means
// the code is business-invalid (not redeemable); code may still be returned for
// context. planID is optional; when nil, plan-eligibility is not enforced.
func (s *service) resolve(ctx context.Context, orgID uuid.UUID, code string, planID *uuid.UUID) (*models.DiscountCode, string, *errx.Error) {
	normalized := NormalizeCode(code)
	if normalized == "" {
		return nil, "Enter a discount code.", nil
	}

	dc, err := s.codeRepo.GetByCode(ctx, normalized)
	if err != nil {
		sentry.CaptureException(err)
		return nil, "", errx.New(errx.Internal, "failed to validate discount code")
	}
	if dc == nil {
		return nil, "That discount code doesn't exist.", nil
	}

	if dc.Status != models.DiscountCodeStatusActive {
		return dc, "This discount code is no longer active.", nil
	}

	now := time.Now()
	if dc.StartsAt != nil && now.Before(*dc.StartsAt) {
		return dc, "This discount code isn't active yet.", nil
	}
	if dc.ExpiresAt != nil && now.After(*dc.ExpiresAt) {
		return dc, "This discount code has expired.", nil
	}

	if dc.MaxRedemptions != nil && dc.TimesRedeemed >= *dc.MaxRedemptions {
		return dc, "This discount code has reached its redemption limit.", nil
	}

	if !dc.AppliesToAllPlans && planID != nil {
		eligible := false
		for _, p := range dc.PlanIDs {
			if p == *planID {
				eligible = true
				break
			}
		}
		if !eligible {
			return dc, "This discount code isn't valid for the selected plan.", nil
		}
	}

	orgCount, err := s.redRepo.CountActiveByCodeAndOrg(ctx, dc.ID, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, "", errx.New(errx.Internal, "failed to validate discount code")
	}
	if dc.PerAccountLimit > 0 && orgCount >= dc.PerAccountLimit {
		return dc, "You've already used this discount code.", nil
	}

	return dc, "", nil
}

// --- Redemption recording ---

func (s *service) ReservePendingRedemption(ctx context.Context, code *models.DiscountCode, orgID uuid.UUID, redeemedBy, planID *uuid.UUID) (uuid.UUID, *errx.Error) {
	return s.reserve(ctx, code, orgID, redeemedBy, planID, nil, models.DiscountRedemptionStatusPending)
}

func (s *service) ReserveAppliedRedemption(ctx context.Context, code *models.DiscountCode, orgID uuid.UUID, redeemedBy, planID, subscriptionID *uuid.UUID) (uuid.UUID, *errx.Error) {
	return s.reserve(ctx, code, orgID, redeemedBy, planID, subscriptionID, models.DiscountRedemptionStatusApplied)
}

// reserve builds and atomically inserts a redemption snapshot (without Stripe
// refs, which are attached later) and returns its ID.
func (s *service) reserve(ctx context.Context, code *models.DiscountCode, orgID uuid.UUID, redeemedBy, planID, subscriptionID *uuid.UUID, status models.DiscountRedemptionStatus) (uuid.UUID, *errx.Error) {
	red := &models.DiscountRedemption{
		ID:                 uuid.New(),
		DiscountCodeID:     code.ID,
		OrganizationID:     orgID,
		RedeemedBy:         redeemedBy,
		PlanID:             planID,
		SubscriptionID:     subscriptionID,
		Type:               code.Type,
		PercentOff:         code.PercentOff,
		AmountOff:          code.AmountOff,
		Currency:           code.Currency,
		TrialExtensionDays: code.TrialExtensionDays,
		Status:             status,
	}
	err := s.redRepo.ReserveRedemption(ctx, red, code.MaxRedemptions, code.PerAccountLimit)
	switch {
	case errors.Is(err, repository.ErrDiscountExhausted):
		return uuid.Nil, errx.New(errx.BadRequest, "This discount code has reached its redemption limit.")
	case errors.Is(err, repository.ErrDiscountAlreadyRedeemed):
		return uuid.Nil, errx.New(errx.BadRequest, "You've already used this discount code.")
	case err != nil:
		sentry.CaptureException(err)
		return uuid.Nil, errx.New(errx.Internal, "failed to record discount redemption")
	}
	return red.ID, nil
}

func (s *service) AttachRedemptionStripe(ctx context.Context, redemptionID uuid.UUID, sessionID, couponID *string) *errx.Error {
	if err := s.redRepo.AttachStripeRefs(ctx, redemptionID, sessionID, couponID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to attach redemption references")
	}
	return nil
}

func (s *service) CancelRedemptionByID(ctx context.Context, redemptionID uuid.UUID) *errx.Error {
	if err := s.redRepo.CancelByID(ctx, redemptionID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to cancel redemption")
	}
	return nil
}

func (s *service) MarkRedemptionApplied(ctx context.Context, sessionID string, subscriptionID *uuid.UUID) *errx.Error {
	if err := s.redRepo.MarkAppliedBySession(ctx, sessionID, subscriptionID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to mark redemption applied")
	}
	return nil
}

func (s *service) CancelRedemption(ctx context.Context, sessionID string) *errx.Error {
	if err := s.redRepo.CancelBySession(ctx, sessionID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to cancel redemption")
	}
	return nil
}

// --- helpers ---

func (s *service) log(ctx context.Context, adminID uuid.UUID, action string, targetID uuid.UUID, details map[string]any, ip, ua string) {
	if s.audit == nil {
		return
	}
	tid := targetID
	s.audit.LogAdminAction(ctx, adminID, action, "discount_code", &tid, details, ip, ua)
}

// validateShape mirrors the DB CHECK so invalid combos surface as clean 400s
// instead of a constraint violation. Returns "" when valid.
func validateShape(dc *models.DiscountCode) string {
	switch dc.Type {
	case models.DiscountTypePercent:
		if dc.PercentOff == nil || *dc.PercentOff < 1 || *dc.PercentOff > 100 {
			return "Percentage discounts require a percent_off between 1 and 100."
		}
		if dc.AmountOff != nil || dc.TrialExtensionDays != nil {
			return "A percentage discount can't also set an amount or trial days."
		}
	case models.DiscountTypeFixed:
		if dc.AmountOff == nil || *dc.AmountOff <= 0 {
			return "Fixed discounts require an amount_off greater than 0."
		}
		if dc.Currency == nil || len(*dc.Currency) != 3 {
			return "Fixed discounts require a 3-letter currency code."
		}
		if dc.PercentOff != nil || dc.TrialExtensionDays != nil {
			return "A fixed discount can't also set a percent or trial days."
		}
	case models.DiscountTypeTrialExtension:
		if dc.TrialExtensionDays == nil || *dc.TrialExtensionDays <= 0 {
			return "Trial-extension discounts require trial_extension_days greater than 0."
		}
		if dc.PercentOff != nil || dc.AmountOff != nil {
			return "A trial-extension discount can't also set a price discount."
		}
	default:
		return "Unknown discount type."
	}

	if dc.Type.IsMoney() && dc.Duration == models.DiscountDurationRepeating {
		if dc.DurationInMonths == nil || *dc.DurationInMonths <= 0 {
			return "A repeating discount requires duration_in_months greater than 0."
		}
	}
	if dc.PerAccountLimit < 1 {
		return "Per-account limit must be at least 1."
	}
	return ""
}

// applyMoneyDiscount computes the discounted price for a single period given a
// money discount. Trial-extension codes return the original unchanged.
func applyMoneyDiscount(original float64, dc *models.DiscountCode) float64 {
	switch dc.Type {
	case models.DiscountTypePercent:
		if dc.PercentOff != nil {
			discounted := original * (1 - float64(*dc.PercentOff)/100)
			if discounted < 0 {
				return 0
			}
			return discounted
		}
	case models.DiscountTypeFixed:
		if dc.AmountOff != nil {
			discounted := original - *dc.AmountOff
			if discounted < 0 {
				return 0
			}
			return discounted
		}
	}
	return original
}

func normalizeCurrency(c *string) *string {
	if c == nil {
		return nil
	}
	v := strings.ToLower(strings.TrimSpace(*c))
	if v == "" {
		return nil
	}
	return &v
}
