package worker

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

var (
	ErrNoAvailableWorkers  = errors.New("no available workers")
	ErrNoDedicatedWorkers  = errors.New("no dedicated workers available")
	ErrOrgAlreadyAssigned  = errors.New("organization already has a dedicated worker assigned")
)

type WorkerAssignmentService interface {
	// AssignWorkerToEmail assigns an appropriate worker to an email account
	// based on the organization's subscription status (free tier vs paid)
	AssignWorkerToEmail(ctx context.Context, emailAccountID, orgID uuid.UUID) (*uuid.UUID, error)

	// SelectSharedWorker selects the least loaded shared worker for the given tier
	SelectSharedWorker(ctx context.Context, freeTier bool) (*models.Worker, error)

	// Dedicated worker management
	AssignDedicatedWorker(ctx context.Context, orgID, subscriptionID uuid.UUID) error
	ReleaseDedicatedWorker(ctx context.Context, orgID uuid.UUID) error
	GetDedicatedWorker(ctx context.Context, orgID uuid.UUID) (*models.Worker, error)

	// Migration operations
	MigrateOrgToPremiumWorkers(ctx context.Context, orgID uuid.UUID) error
	MigrateOrgToFreeWorkers(ctx context.Context, orgID uuid.UUID) error
	MigrateOrgToDedicated(ctx context.Context, orgID uuid.UUID, subscriptionID uuid.UUID) error
	MigrateOrgToShared(ctx context.Context, orgID uuid.UUID) error
	MigrateEmailsFromWorker(ctx context.Context, workerID uuid.UUID, targetFreeTier bool) error
}

type workerAssignmentService struct {
	workerRepo repository.WorkerRepository
	subRepo    repository.SubscriptionRepository
	planRepo   repository.PlanRepository
}

func NewAssignmentService(
	workerRepo repository.WorkerRepository,
	subRepo repository.SubscriptionRepository,
	planRepo repository.PlanRepository,
) WorkerAssignmentService {
	return &workerAssignmentService{
		workerRepo: workerRepo,
		subRepo:    subRepo,
		planRepo:   planRepo,
	}
}

// AssignWorkerToEmail assigns an appropriate worker to an email account
func (s *workerAssignmentService) AssignWorkerToEmail(ctx context.Context, emailAccountID, orgID uuid.UUID) (*uuid.UUID, error) {
	// 1. Check organization's subscription status
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return nil, err
	}

	// 2. Determine if free tier or paid
	isPaidOrg := sub != nil && sub.HasPaidSubscription()

	// 3. Check if paid org has dedicated worker plan
	if isPaidOrg {
		plan, err := s.planRepo.GetByID(ctx, sub.PlanID)
		if err != nil {
			return nil, err
		}
		if plan != nil && plan.DedicatedWorkers > 0 {
			// Check if org has a dedicated worker
			dedicatedWorker, err := s.workerRepo.GetDedicatedWorkerByUserID(ctx, orgID)
			if err != nil {
				return nil, err
			}
			if dedicatedWorker != nil {
				// Assign to dedicated worker
				if err := s.workerRepo.UpdateEmailAccountWorker(ctx, emailAccountID, dedicatedWorker.ID); err != nil {
					return nil, err
				}
				if err := s.workerRepo.IncrementAccountCount(ctx, dedicatedWorker.ID); err != nil {
					return nil, err
				}
				return &dedicatedWorker.ID, nil
			}
		}
	}

	// 4. Assign to shared worker (strict tier separation)
	freeTier := !isPaidOrg // Free trial = free workers, Paid = premium workers
	worker, err := s.SelectSharedWorker(ctx, freeTier)
	if err != nil {
		return nil, err
	}

	// 5. Update database
	if err := s.workerRepo.UpdateEmailAccountWorker(ctx, emailAccountID, worker.ID); err != nil {
		return nil, err
	}

	// 6. Update worker account count
	if err := s.workerRepo.IncrementAccountCount(ctx, worker.ID); err != nil {
		return nil, err
	}

	// 7. Update warmup pool type
	poolType := "free"
	if !freeTier {
		poolType = "premium"
	}
	if err := s.workerRepo.UpdateEmailAccountWarmupPoolType(ctx, emailAccountID, poolType); err != nil {
		// Log but don't fail
	}

	return &worker.ID, nil
}

// SelectSharedWorker selects the least loaded shared worker for the given tier
func (s *workerAssignmentService) SelectSharedWorker(ctx context.Context, freeTier bool) (*models.Worker, error) {
	// Get all workers for the tier, sorted by account_count ASC
	workers, err := s.workerRepo.GetSharedWorkersByTier(ctx, freeTier)
	if err != nil {
		return nil, err
	}

	if len(workers) == 0 {
		return nil, ErrNoAvailableWorkers
	}

	// Return the least loaded worker (first one since sorted ASC)
	return &workers[0], nil
}

// AssignDedicatedWorker assigns a dedicated worker to an organization
func (s *workerAssignmentService) AssignDedicatedWorker(ctx context.Context, orgID, subscriptionID uuid.UUID) error {
	// Check if org already has a dedicated worker
	existing, err := s.workerRepo.GetActiveDedicatedAssignment(ctx, orgID)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrOrgAlreadyAssigned
	}

	// Find an available dedicated worker
	worker, err := s.workerRepo.GetAvailableDedicatedWorker(ctx)
	if err != nil {
		return err
	}
	if worker == nil {
		return ErrNoDedicatedWorkers
	}

	// Create assignment
	assignment := &models.DedicatedWorkerAssignment{
		ID:             uuid.New(),
		WorkerID:       worker.ID,
		UserID:         orgID,
		SubscriptionID: subscriptionID,
		AssignedAt:     time.Now(),
	}

	return s.workerRepo.CreateDedicatedAssignment(ctx, assignment)
}

// ReleaseDedicatedWorker releases a dedicated worker assignment
func (s *workerAssignmentService) ReleaseDedicatedWorker(ctx context.Context, orgID uuid.UUID) error {
	return s.workerRepo.ReleaseDedicatedAssignment(ctx, orgID)
}

// GetDedicatedWorker gets the dedicated worker for an organization
func (s *workerAssignmentService) GetDedicatedWorker(ctx context.Context, orgID uuid.UUID) (*models.Worker, error) {
	return s.workerRepo.GetDedicatedWorkerByUserID(ctx, orgID)
}

// MigrateOrgToPremiumWorkers migrates all org's emails from free to premium workers
// Called when a trial org subscribes to a paid plan
func (s *workerAssignmentService) MigrateOrgToPremiumWorkers(ctx context.Context, orgID uuid.UUID) error {
	accountIDs, err := s.workerRepo.GetEmailAccountsByOrganizationID(ctx, orgID)
	if err != nil {
		return err
	}

	for _, accountID := range accountIDs {
		info, err := s.workerRepo.GetEmailAccountWorkerInfo(ctx, accountID)
		if err != nil || info == nil {
			continue
		}

		// Skip if no worker assigned or already on premium
		if info.WorkerID == nil || (info.FreeTier != nil && !*info.FreeTier) {
			continue
		}

		// Select new premium worker
		newWorker, err := s.SelectSharedWorker(ctx, false)
		if err != nil {
			continue
		}

		// Migrate
		if err := s.migrateEmailToWorker(ctx, accountID, *info.WorkerID, newWorker.ID); err != nil {
			continue
		}

		// Update warmup pool type
		s.workerRepo.UpdateEmailAccountWarmupPoolType(ctx, accountID, "premium")
	}

	return nil
}

// MigrateOrgToFreeWorkers migrates all org's emails to free tier workers
// Called when a paid subscription is cancelled/expired
func (s *workerAssignmentService) MigrateOrgToFreeWorkers(ctx context.Context, orgID uuid.UUID) error {
	accountIDs, err := s.workerRepo.GetEmailAccountsByOrganizationID(ctx, orgID)
	if err != nil {
		return err
	}

	for _, accountID := range accountIDs {
		info, err := s.workerRepo.GetEmailAccountWorkerInfo(ctx, accountID)
		if err != nil || info == nil {
			continue
		}

		// Skip if no worker assigned or already on free tier
		if info.WorkerID == nil || (info.FreeTier != nil && *info.FreeTier) {
			continue
		}

		// Select new free tier worker
		newWorker, err := s.SelectSharedWorker(ctx, true)
		if err != nil {
			continue
		}

		// Migrate
		if err := s.migrateEmailToWorker(ctx, accountID, *info.WorkerID, newWorker.ID); err != nil {
			continue
		}

		// Update warmup pool type
		s.workerRepo.UpdateEmailAccountWarmupPoolType(ctx, accountID, "free")
	}

	return nil
}

// MigrateOrgToDedicated migrates org's emails to their dedicated worker
func (s *workerAssignmentService) MigrateOrgToDedicated(ctx context.Context, orgID uuid.UUID, subscriptionID uuid.UUID) error {
	// First, assign a dedicated worker to the org
	if err := s.AssignDedicatedWorker(ctx, orgID, subscriptionID); err != nil {
		if !errors.Is(err, ErrOrgAlreadyAssigned) {
			return err
		}
	}

	// Get the dedicated worker
	dedicatedWorker, err := s.workerRepo.GetDedicatedWorkerByUserID(ctx, orgID)
	if err != nil {
		return err
	}
	if dedicatedWorker == nil {
		return ErrNoDedicatedWorkers
	}

	accountIDs, err := s.workerRepo.GetEmailAccountsByOrganizationID(ctx, orgID)
	if err != nil {
		return err
	}

	for _, accountID := range accountIDs {
		info, err := s.workerRepo.GetEmailAccountWorkerInfo(ctx, accountID)
		if err != nil || info == nil {
			continue
		}

		// Skip if no worker assigned or already on dedicated worker
		if info.WorkerID == nil || *info.WorkerID == dedicatedWorker.ID {
			continue
		}

		// Migrate to dedicated worker
		if err := s.migrateEmailToWorker(ctx, accountID, *info.WorkerID, dedicatedWorker.ID); err != nil {
			continue
		}

		// Update warmup pool type
		s.workerRepo.UpdateEmailAccountWarmupPoolType(ctx, accountID, "premium")
	}

	return nil
}

// MigrateOrgToShared migrates org's emails from dedicated to shared workers
func (s *workerAssignmentService) MigrateOrgToShared(ctx context.Context, orgID uuid.UUID) error {
	accountIDs, err := s.workerRepo.GetEmailAccountsByOrganizationID(ctx, orgID)
	if err != nil {
		return err
	}

	for _, accountID := range accountIDs {
		info, err := s.workerRepo.GetEmailAccountWorkerInfo(ctx, accountID)
		if err != nil || info == nil {
			continue
		}

		if info.WorkerID == nil {
			continue
		}

		// Select new shared premium worker
		newWorker, err := s.SelectSharedWorker(ctx, false)
		if err != nil {
			continue
		}

		// Migrate
		if err := s.migrateEmailToWorker(ctx, accountID, *info.WorkerID, newWorker.ID); err != nil {
			continue
		}
	}

	// Release the dedicated worker
	if err := s.ReleaseDedicatedWorker(ctx, orgID); err != nil {
		// Log but don't fail
	}

	return nil
}

// MigrateEmailsFromWorker migrates all emails from a worker to other workers
func (s *workerAssignmentService) MigrateEmailsFromWorker(ctx context.Context, workerID uuid.UUID, targetFreeTier bool) error {
	// Get all email accounts on this worker
	accountIDs, err := s.workerRepo.GetEmailAccountsByWorkerID(ctx, workerID)
	if err != nil {
		return err
	}

	for _, accountID := range accountIDs {
		// Select new worker
		newWorker, err := s.SelectSharedWorker(ctx, targetFreeTier)
		if err != nil {
			continue
		}

		// Migrate
		if err := s.migrateEmailToWorker(ctx, accountID, workerID, newWorker.ID); err != nil {
			continue
		}
	}

	return nil
}

// migrateEmailToWorker handles the actual migration of an email account
func (s *workerAssignmentService) migrateEmailToWorker(ctx context.Context, emailAccountID, oldWorkerID, newWorkerID uuid.UUID) error {
	// 1. Update database
	if err := s.workerRepo.UpdateEmailAccountWorker(ctx, emailAccountID, newWorkerID); err != nil {
		return err
	}

	// 2. Decrement old worker count
	if err := s.workerRepo.DecrementAccountCount(ctx, oldWorkerID); err != nil {
		// Log but don't fail
	}

	// 3. Increment new worker count
	if err := s.workerRepo.IncrementAccountCount(ctx, newWorkerID); err != nil {
		// Log but don't fail
	}

	return nil
}
