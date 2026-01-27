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
	ErrNoAvailableWorkers   = errors.New("no available workers")
	ErrNoDedicatedWorkers   = errors.New("no dedicated workers available")
	ErrUserAlreadyAssigned  = errors.New("user already has a dedicated worker assigned")
)

type WorkerAssignmentService interface {
	// AssignWorkerToEmail assigns an appropriate worker to an email account
	// based on the user's subscription status (free tier vs paid)
	AssignWorkerToEmail(ctx context.Context, emailAccountID, userID uuid.UUID) (*uuid.UUID, error)

	// SelectSharedWorker selects the least loaded shared worker for the given tier
	SelectSharedWorker(ctx context.Context, freeTier bool) (*models.Worker, error)

	// Dedicated worker management
	AssignDedicatedWorker(ctx context.Context, userID, subscriptionID uuid.UUID) error
	ReleaseDedicatedWorker(ctx context.Context, userID uuid.UUID) error
	GetDedicatedWorker(ctx context.Context, userID uuid.UUID) (*models.Worker, error)

	// Migration operations
	MigrateUserToPremiumWorkers(ctx context.Context, userID uuid.UUID) error
	MigrateUserToFreeWorkers(ctx context.Context, userID uuid.UUID) error
	MigrateUserToDedicated(ctx context.Context, userID uuid.UUID, subscriptionID uuid.UUID) error
	MigrateUserToShared(ctx context.Context, userID uuid.UUID) error
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
func (s *workerAssignmentService) AssignWorkerToEmail(ctx context.Context, emailAccountID, userID uuid.UUID) (*uuid.UUID, error) {
	// 1. Check user's subscription status
	sub, err := s.subRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 2. Determine if free tier or paid
	isPaidUser := sub != nil && sub.HasPaidSubscription()

	// 3. Check if paid user has dedicated worker plan
	if isPaidUser {
		plan, err := s.planRepo.GetByID(ctx, sub.PlanID)
		if err != nil {
			return nil, err
		}
		if plan != nil && plan.DedicatedWorkers > 0 {
			// Check if user has a dedicated worker
			dedicatedWorker, err := s.workerRepo.GetDedicatedWorkerByUserID(ctx, userID)
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
	freeTier := !isPaidUser // Free trial = free workers, Paid = premium workers
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

// AssignDedicatedWorker assigns a dedicated worker to a user
func (s *workerAssignmentService) AssignDedicatedWorker(ctx context.Context, userID, subscriptionID uuid.UUID) error {
	// Check if user already has a dedicated worker
	existing, err := s.workerRepo.GetActiveDedicatedAssignment(ctx, userID)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrUserAlreadyAssigned
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
		UserID:         userID,
		SubscriptionID: subscriptionID,
		AssignedAt:     time.Now(),
	}

	return s.workerRepo.CreateDedicatedAssignment(ctx, assignment)
}

// ReleaseDedicatedWorker releases a dedicated worker assignment
func (s *workerAssignmentService) ReleaseDedicatedWorker(ctx context.Context, userID uuid.UUID) error {
	return s.workerRepo.ReleaseDedicatedAssignment(ctx, userID)
}

// GetDedicatedWorker gets the dedicated worker for a user
func (s *workerAssignmentService) GetDedicatedWorker(ctx context.Context, userID uuid.UUID) (*models.Worker, error) {
	return s.workerRepo.GetDedicatedWorkerByUserID(ctx, userID)
}

// MigrateUserToPremiumWorkers migrates all user's emails from free to premium workers
// Called when a trial user subscribes to a paid plan
func (s *workerAssignmentService) MigrateUserToPremiumWorkers(ctx context.Context, userID uuid.UUID) error {
	// Get all email account IDs for the user
	accountIDs, err := s.workerRepo.GetEmailAccountsByUserID(ctx, userID)
	if err != nil {
		return err
	}

	for _, accountID := range accountIDs {
		// Get current worker info via workerRepo
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

// MigrateUserToFreeWorkers migrates all user's emails to free tier workers
// Called when a paid subscription is cancelled/expired
func (s *workerAssignmentService) MigrateUserToFreeWorkers(ctx context.Context, userID uuid.UUID) error {
	// Get all email account IDs for the user
	accountIDs, err := s.workerRepo.GetEmailAccountsByUserID(ctx, userID)
	if err != nil {
		return err
	}

	for _, accountID := range accountIDs {
		// Get current worker info via workerRepo
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

// MigrateUserToDedicated migrates user's emails to their dedicated worker
func (s *workerAssignmentService) MigrateUserToDedicated(ctx context.Context, userID uuid.UUID, subscriptionID uuid.UUID) error {
	// First, assign a dedicated worker to the user
	if err := s.AssignDedicatedWorker(ctx, userID, subscriptionID); err != nil {
		if !errors.Is(err, ErrUserAlreadyAssigned) {
			return err
		}
	}

	// Get the dedicated worker
	dedicatedWorker, err := s.workerRepo.GetDedicatedWorkerByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if dedicatedWorker == nil {
		return ErrNoDedicatedWorkers
	}

	// Get all email account IDs for the user
	accountIDs, err := s.workerRepo.GetEmailAccountsByUserID(ctx, userID)
	if err != nil {
		return err
	}

	for _, accountID := range accountIDs {
		// Get current worker info via workerRepo
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

// MigrateUserToShared migrates user's emails from dedicated to shared workers
func (s *workerAssignmentService) MigrateUserToShared(ctx context.Context, userID uuid.UUID) error {
	// Get all email account IDs for the user
	accountIDs, err := s.workerRepo.GetEmailAccountsByUserID(ctx, userID)
	if err != nil {
		return err
	}

	for _, accountID := range accountIDs {
		// Get current worker info via workerRepo
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
	if err := s.ReleaseDedicatedWorker(ctx, userID); err != nil {
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

	// TODO: 4. Notify workers via Kafka (remove from old, add to new)
	// s.notifyWorkerRemoveEmail(ctx, oldWorkerID, emailAccountID)
	// s.notifyWorkerAddEmail(ctx, newWorkerID, emailAccount)

	return nil
}
