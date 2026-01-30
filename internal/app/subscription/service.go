package subscription

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type SubscriptionService interface {
	// Get subscription for an organization
	Get(ctx context.Context, orgID uuid.UUID) (*models.Subscription, *errx.Error)
	GetWithLimits(ctx context.Context, orgID uuid.UUID) (*models.SubscriptionWithLimits, *errx.Error)

	// Check if subscription is active
	IsActive(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)

	// Get realtime limits for an organization
	GetRealtimeLimits(ctx context.Context, orgID uuid.UUID) (*models.RealtimeRateLimits, *errx.Error)

	// List available plans
	ListPlans(ctx context.Context, publicOnly bool) ([]*models.Plan, *errx.Error)
	GetPlan(ctx context.Context, planID uuid.UUID) (*models.Plan, *errx.Error)
}

type subscriptionService struct {
	subRepo  repository.SubscriptionRepository
	planRepo repository.PlanRepository
}

func NewService(subRepo repository.SubscriptionRepository, planRepo repository.PlanRepository) SubscriptionService {
	return &subscriptionService{
		subRepo:  subRepo,
		planRepo: planRepo,
	}
}

func (s *subscriptionService) Get(ctx context.Context, orgID uuid.UUID) (*models.Subscription, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to get subscription")
	}
	if sub == nil {
		return nil, errx.New(errx.NotFound, "no subscription found")
	}

	// Load plan
	plan, _ := s.planRepo.GetByID(ctx, sub.PlanID)
	sub.Plan = plan

	return sub, nil
}

func (s *subscriptionService) GetWithLimits(ctx context.Context, orgID uuid.UUID) (*models.SubscriptionWithLimits, *errx.Error) {
	sub, err := s.subRepo.GetWithLimits(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to get subscription")
	}
	if sub == nil {
		return nil, errx.New(errx.NotFound, "no subscription found")
	}
	return sub, nil
}

func (s *subscriptionService) IsActive(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return false, errx.New(errx.Internal, "failed to get subscription")
	}
	if sub == nil {
		return false, nil
	}
	return sub.Status.IsActive(), nil
}

func (s *subscriptionService) GetRealtimeLimits(ctx context.Context, orgID uuid.UUID) (*models.RealtimeRateLimits, *errx.Error) {
	sub, err := s.subRepo.GetWithLimits(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to get subscription limits")
	}

	// Return defaults if no subscription
	if sub == nil || sub.RateLimits == nil {
		return &models.RealtimeRateLimits{
			LimitWSMessagePM: 120,
			LimitWSJoinPM:    30,
			LimitWSEventPM:   60,
			MaxConnections:   10,
		}, nil
	}

	return sub.RateLimits, nil
}

func (s *subscriptionService) ListPlans(ctx context.Context, publicOnly bool) ([]*models.Plan, *errx.Error) {
	plans, err := s.planRepo.List(ctx, publicOnly)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list plans")
	}
	return plans, nil
}

func (s *subscriptionService) GetPlan(ctx context.Context, planID uuid.UUID) (*models.Plan, *errx.Error) {
	plan, err := s.planRepo.GetByID(ctx, planID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to get plan")
	}
	if plan == nil {
		return nil, errx.New(errx.NotFound, "plan not found")
	}
	return plan, nil
}
