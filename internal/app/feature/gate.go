package feature

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// FreeTierDailyEmailLimit is the daily campaign email limit for free trial users
	FreeTierDailyEmailLimit = 20

	// UnlimitedEmails indicates unlimited daily emails
	UnlimitedEmails = -1
)

type FeatureGateService interface {
	// CanSendCampaignEmail checks if an organization can send campaign emails
	CanSendCampaignEmail(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)

	// CanUseWarmup checks if an organization can use the warmup feature.
	// Free-trial orgs may use warmup during their 14-day window.
	CanUseWarmup(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)

	// CanUseUnibox checks if an organization can use the unibox feature.
	// Free-trial orgs may use unibox during their 14-day window.
	CanUseUnibox(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)

	// CanAddInbox returns whether the org may connect another email account.
	// Free-trial orgs are capped at FreeTrialInboxLimit connected inboxes.
	// Paid orgs are not gated here (plan limits govern sending volume).
	CanAddInbox(ctx context.Context, orgID uuid.UUID, currentCount int) (bool, *errx.Error)

	// GetDailyEmailLimit returns the daily email limit for an organization
	// Returns -1 for unlimited, 0 for blocked
	GetDailyEmailLimit(ctx context.Context, orgID uuid.UUID) (int, *errx.Error)

	// GetSubscriptionStatus returns subscription info for feature checks
	GetSubscriptionStatus(ctx context.Context, orgID uuid.UUID) (*SubscriptionStatus, *errx.Error)

	// IsPaidOrganization checks if the organization has an active paid subscription
	IsPaidOrganization(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
}

// SubscriptionStatus contains status info for feature gating
type SubscriptionStatus struct {
	HasSubscription    bool         `json:"has_subscription"`
	IsInFreeTrial      bool         `json:"is_in_free_trial"`
	IsFreeTrialExpired bool         `json:"is_free_trial_expired"`
	IsPaidSubscriber   bool         `json:"is_paid_subscriber"`
	DailyEmailLimit    int          `json:"daily_email_limit"`
	Plan               *models.Plan `json:"plan,omitempty"`
}

type featureGateService struct {
	subRepo  repository.SubscriptionRepository
	planRepo repository.PlanRepository
}

func NewService(subRepo repository.SubscriptionRepository, planRepo repository.PlanRepository) FeatureGateService {
	return &featureGateService{
		subRepo:  subRepo,
		planRepo: planRepo,
	}
}

// CanSendCampaignEmail checks if an organization can send campaign emails
func (s *featureGateService) CanSendCampaignEmail(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return false, errx.New(errx.Internal, "failed to get subscription")
	}

	// No subscription = blocked
	if sub == nil {
		return false, nil
	}

	// Active paid subscription = allowed
	if sub.HasPaidSubscription() {
		return true, nil
	}

	// In free trial = allowed (with limit)
	if sub.IsInFreeTrial() {
		return true, nil
	}

	// Trial expired, no paid subscription = blocked
	return false, nil
}

// CanUseWarmup checks if an organization can use the warmup feature.
// Free-trial users get warmup access for the 14-day window via the
// `free` pool; once the trial expires they must upgrade.
func (s *featureGateService) CanUseWarmup(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return false, errx.New(errx.Internal, "failed to get subscription")
	}
	if sub == nil {
		return false, nil
	}
	return sub.CanUseWarmup(), nil
}

// CanUseUnibox checks if an organization can use the unibox feature.
// Same trial allowance as warmup.
func (s *featureGateService) CanUseUnibox(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return false, errx.New(errx.Internal, "failed to get subscription")
	}
	if sub == nil {
		return false, nil
	}
	return sub.CanUseUnibox(), nil
}

// CanAddInbox enforces the free-trial inbox cap. Paid orgs are never
// blocked here. Trial orgs may connect up to models.FreeTrialInboxLimit
// inboxes; once that cap is reached we refuse so the warmup pool is not
// seeded with throwaway trial accounts.
func (s *featureGateService) CanAddInbox(ctx context.Context, orgID uuid.UUID, currentCount int) (bool, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return false, errx.New(errx.Internal, "failed to get subscription")
	}
	if sub == nil {
		return false, nil
	}
	if sub.HasPaidSubscription() {
		return true, nil
	}
	if sub.IsInFreeTrial() {
		return currentCount < models.FreeTrialInboxLimit, nil
	}
	return false, nil
}

// GetDailyEmailLimit returns the daily email limit for an organization
func (s *featureGateService) GetDailyEmailLimit(ctx context.Context, orgID uuid.UUID) (int, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return 0, errx.New(errx.Internal, "failed to get subscription")
	}

	if sub == nil {
		return 0, nil // No subscription = no emails
	}

	// Free trial users = 20 emails/day
	if sub.IsInFreeTrial() && !sub.HasPaidSubscription() {
		return FreeTierDailyEmailLimit, nil
	}

	// Paid users = plan limit or unlimited
	if sub.HasPaidSubscription() {
		plan, err := s.planRepo.GetByID(ctx, sub.PlanID)
		if err != nil || plan == nil {
			return UnlimitedEmails, nil // Default to unlimited if plan not found
		}
		if plan.DailyCampaignLimit != nil {
			return *plan.DailyCampaignLimit, nil
		}
		return UnlimitedEmails, nil // -1 = unlimited
	}

	// Trial expired = no emails
	return 0, nil
}

// GetSubscriptionStatus returns subscription info for feature checks
func (s *featureGateService) GetSubscriptionStatus(ctx context.Context, orgID uuid.UUID) (*SubscriptionStatus, *errx.Error) {
	status := &SubscriptionStatus{
		HasSubscription:    false,
		IsInFreeTrial:      false,
		IsFreeTrialExpired: false,
		IsPaidSubscriber:   false,
		DailyEmailLimit:    0,
	}

	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to get subscription")
	}

	if sub == nil {
		return status, nil
	}

	status.HasSubscription = true
	status.IsInFreeTrial = sub.IsInFreeTrial()
	status.IsFreeTrialExpired = sub.IsFreeTrialExpired()
	status.IsPaidSubscriber = sub.HasPaidSubscription()

	// Load plan
	plan, _ := s.planRepo.GetByID(ctx, sub.PlanID)
	status.Plan = plan

	// Calculate daily limit
	if status.IsInFreeTrial && !status.IsPaidSubscriber {
		status.DailyEmailLimit = FreeTierDailyEmailLimit
	} else if status.IsPaidSubscriber {
		if plan != nil && plan.DailyCampaignLimit != nil {
			status.DailyEmailLimit = *plan.DailyCampaignLimit
		} else {
			status.DailyEmailLimit = UnlimitedEmails
		}
	}

	return status, nil
}

// IsPaidOrganization checks if the organization has an active paid subscription
func (s *featureGateService) IsPaidOrganization(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return false, errx.New(errx.Internal, "failed to get subscription")
	}

	if sub == nil {
		return false, nil
	}

	return sub.HasPaidSubscription(), nil
}
