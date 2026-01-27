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
	// StartFreeTrial creates a new free trial subscription for a user
	StartFreeTrial(ctx context.Context, userID uuid.UUID) error

	// StartFreeTrialWithOrg creates a new free trial subscription linked to an organization
	StartFreeTrialWithOrg(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID) error

	// GetTrialStatus returns the current trial status for a user
	GetTrialStatus(ctx context.Context, userID uuid.UUID) (*TrialStatus, *errx.Error)
}

type trialService struct {
	subRepo repository.SubscriptionRepository
}

func NewService(subRepo repository.SubscriptionRepository) TrialService {
	return &trialService{
		subRepo: subRepo,
	}
}

// StartFreeTrial creates a new free trial subscription for a user
func (s *trialService) StartFreeTrial(ctx context.Context, userID uuid.UUID) error {
	return s.StartFreeTrialWithOrg(ctx, userID, nil)
}

// StartFreeTrialWithOrg creates a new free trial subscription linked to an organization
func (s *trialService) StartFreeTrialWithOrg(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID) error {
	// Check if user already has a subscription
	existing, err := s.subRepo.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if existing != nil {
		// User already has a subscription
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

	return s.subRepo.Create(ctx, sub)
}

// GetTrialStatus returns the current trial status for a user
func (s *trialService) GetTrialStatus(ctx context.Context, userID uuid.UUID) (*TrialStatus, *errx.Error) {
	sub, err := s.subRepo.GetByUserID(ctx, userID)
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
