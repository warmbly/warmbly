package trial

import (
	"context"
	"math"
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
	ResetMonthlyAllowance(ctx context.Context, orgID uuid.UUID, allowance models.Micro, idempotencyKey string) error
}

type trialService struct {
	subRepo  repository.SubscriptionRepository
	planRepo repository.PlanRepository
	credits  CreditGranter
}

// NewService constructs the trial service. planRepo and credits are optional
// (nil-safe): when both are set, StartFreeTrialWithOrg seeds the free plan's
// monthly AI-credit allowance so trial orgs can try AI features.
func NewService(subRepo repository.SubscriptionRepository, planRepo repository.PlanRepository, credits CreditGranter) TrialService {
	return &trialService{
		subRepo:  subRepo,
		planRepo: planRepo,
		credits:  credits,
	}
}

// StartFreeTrialWithOrg gives the workspace a TrialDuration trial on the free
// plan, once per user: a workspace created after the user's trial is claimed
// has no subscription until it pays.
func (s *trialService) StartFreeTrialWithOrg(ctx context.Context, userID uuid.UUID, orgID uuid.UUID) error {
	existing, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	freePlanUUID, _ := uuid.Parse(FreePlanID)
	startedAt := time.Now()
	endsAt := startedAt.Add(TrialDuration)

	sub := &models.Subscription{
		ID:                 uuid.New(),
		UserID:             userID,
		OrganizationID:     orgID,
		PlanID:             freePlanUUID,
		StripeCustomerID:   "", // Will be set when user subscribes
		Status:             models.SubscriptionStatusIncomplete,
		FreeTrialStartedAt: &startedAt,
		FreeTrialEndsAt:    &endsAt,
	}

	// The claim and the row are one write: a failure keeps the trial, and a second workspace cannot also take it.
	created, err := s.subRepo.CreateTrial(ctx, sub)
	if err != nil || !created {
		return err
	}

	// Seed the free plan's monthly AI-credit allowance once, at trial start.
	// Trial orgs never hit invoice.paid, so this is their only grant until they
	// convert. Set-to-N with a per-org idempotency key so a re-trigger can't
	// double-grant. Best-effort: a credit failure must not fail trial creation.
	if s.credits != nil && s.planRepo != nil {
		if plan, perr := s.planRepo.GetByID(ctx, freePlanUUID); perr == nil && plan != nil && plan.Allowance() > 0 {
			_ = s.credits.ResetMonthlyAllowance(ctx, orgID, plan.Allowance(), "trial_grant:"+orgID.String())
		}
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
			// Rounded up, as the dashboard's trial bar does, so both show the same count.
			status.DaysRemaining = int(math.Ceil(time.Until(*sub.FreeTrialEndsAt).Hours() / 24))
		}
	}

	return status, nil
}
