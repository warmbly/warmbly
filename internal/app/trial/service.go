package trial

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// TrialDuration is the default trial period
	TrialDuration = 14 * 24 * time.Hour // 2 weeks

	// FreePlanID is the default free trial plan ID
	FreePlanID = "00000000-0000-0000-0000-000000000001"
)

// TrialStatus represents the current trial status for a user
type TrialStatus struct {
	IsInTrial     bool       `json:"is_in_trial"`
	TrialEndsAt   *time.Time `json:"trial_ends_at,omitempty"`
	DaysRemaining int        `json:"days_remaining"`
	IsExpired     bool       `json:"is_expired"`
	IsSubscribed  bool       `json:"is_subscribed"`
}

type TrialService interface {
	// StartFreeTrialWithOrg creates a new free trial subscription linked to an organization
	StartFreeTrialWithOrg(ctx context.Context, userID uuid.UUID, orgID uuid.UUID) error

	// GetTrialStatus returns the current trial status for an organization
	GetTrialStatus(ctx context.Context, orgID uuid.UUID) (*TrialStatus, *errx.Error)
}

// CreditGranter is the slice of the credit service the trial flow uses to seed
// the org's monthly AI-credit allowance once, when the free trial starts.
type CreditGranter interface {
	ResetMonthlyAllowance(ctx context.Context, orgID uuid.UUID, allowance int, idempotencyKey string) error
}

type trialService struct {
	subRepo  repository.SubscriptionRepository
	userRepo repository.UserRepository
	planRepo repository.PlanRepository
	credits  CreditGranter
}

// NewService constructs the trial service. planRepo and credits are optional
// (nil-safe): when both are set, StartFreeTrialWithOrg seeds the free plan's
// monthly AI-credit allowance so trial orgs can try AI features.
func NewService(subRepo repository.SubscriptionRepository, userRepo repository.UserRepository, planRepo repository.PlanRepository, credits CreditGranter) TrialService {
	return &trialService{
		subRepo:  subRepo,
		userRepo: userRepo,
		planRepo: planRepo,
		credits:  credits,
	}
}

// StartFreeTrialWithOrg creates a new free trial subscription linked to an organization
func (s *trialService) StartFreeTrialWithOrg(ctx context.Context, userID uuid.UUID, orgID uuid.UUID) error {
	// Check if user already used their free trial
	user, err := s.userRepo.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if user != nil && user.FreeTrialUsed {
		return nil // No-op: user already used their free trial
	}

	// Check if org already has a subscription
	existing, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	// Create new subscription with free trial
	now := time.Now()
	trialEnds := now.Add(TrialDuration)
	freePlanUUID, _ := uuid.Parse(FreePlanID)

	sub := &models.Subscription{
		ID:                 uuid.New(),
		UserID:             userID,
		OrganizationID:     orgID,
		PlanID:             freePlanUUID,
		StripeCustomerID:   "", // Will be set when user subscribes
		Status:             models.SubscriptionStatusTrialing,
		FreeTrialStartedAt: &now,
		FreeTrialEndsAt:    &trialEnds,
	}

	if err := s.subRepo.Create(ctx, sub); err != nil {
		return err
	}

	// Seed the free plan's monthly AI-credit allowance once, at trial start.
	// Trial orgs never hit invoice.paid, so this is their only grant until they
	// convert. Set-to-N with a per-org idempotency key so a re-trigger can't
	// double-grant. Best-effort: a credit failure must not fail trial creation.
	if s.credits != nil && s.planRepo != nil {
		if plan, perr := s.planRepo.GetByID(ctx, freePlanUUID); perr == nil && plan != nil && plan.MonthlyCredits > 0 {
			_ = s.credits.ResetMonthlyAllowance(ctx, orgID, plan.MonthlyCredits, "trial_grant:"+orgID.String())
		}
	}

	// Mark the user's free trial as used
	if err := s.userRepo.SetFreeTrialUsed(ctx, userID); err != nil {
		// Log but don't fail - the subscription was created successfully
		return nil
	}

	return nil
}

// GetTrialStatus returns the current trial status for an organization
func (s *trialService) GetTrialStatus(ctx context.Context, orgID uuid.UUID) (*TrialStatus, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to get subscription")
	}

	status := &TrialStatus{
		IsInTrial:     false,
		DaysRemaining: 0,
		IsExpired:     false,
		IsSubscribed:  false,
	}

	if sub == nil {
		return status, nil
	}

	// Check if user has a paid subscription
	status.IsSubscribed = sub.HasPaidSubscription()

	// If subscribed, trial status doesn't matter
	if status.IsSubscribed {
		return status, nil
	}

	// Check trial status
	if sub.FreeTrialEndsAt != nil {
		status.TrialEndsAt = sub.FreeTrialEndsAt
		status.IsInTrial = sub.IsInFreeTrial()
		status.IsExpired = sub.IsFreeTrialExpired()

		if status.IsInTrial {
			remaining := time.Until(*sub.FreeTrialEndsAt)
			status.DaysRemaining = int(remaining.Hours() / 24)
			if remaining > 0 && status.DaysRemaining == 0 {
				status.DaysRemaining = 1 // At least 1 day remaining if not expired
			}
		}
	}

	return status, nil
}
