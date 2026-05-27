package worker

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// defaultMailboxWeight is the weight applied when AssignWorkerToEmail
// can't or won't fetch placement hints (e.g. the email account row was
// just deleted, or the lookup fails). 1.0 lines up with cold_smtp, which
// is the conservative assumption: better to over-account for the
// placement than to silently under-count and let a worker over-commit.
const defaultMailboxWeight = 1.0

var (
	ErrNoAvailableWorkers = errors.New("no available workers")
	ErrNoDedicatedWorkers = errors.New("no dedicated workers available")
	ErrOrgAlreadyAssigned = errors.New("organization already has a dedicated worker assigned")
)

type WorkerAssignmentService interface {
	// AssignWorkerToEmail assigns an appropriate worker to an email account
	// based on the organization's subscription status (free tier vs paid)
	AssignWorkerToEmail(ctx context.Context, emailAccountID, orgID uuid.UUID) (*uuid.UUID, error)

	// SelectSharedWorker selects the least loaded shared worker for the given tier
	SelectSharedWorker(ctx context.Context, freeTier bool) (*models.Worker, error)

	// SelectSharedWorkerForBand selects the least-loaded shared worker whose
	// risk_pool matches the mailbox's risk band. Falls back to the clean
	// pool if no worker exists in the target pool, and to any worker of the
	// tier as a last resort.
	SelectSharedWorkerForBand(ctx context.Context, freeTier bool, band models.EmailRiskBand) (*models.Worker, error)

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

	// 3. Compute mailbox weight once - reused for whichever worker we
	// land on (dedicated or shared). 0 is a sentinel that means
	// "couldn't look it up, use default" and is handled by
	// resolveMailboxWeight below.
	weight := s.resolveMailboxWeight(ctx, emailAccountID)

	// 4. Check if paid org has dedicated worker plan
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
				// Best-effort load_score bump. Non-fatal: dedicated workers
				// don't use load_score for placement (one customer per
				// worker), but keeping the column accurate makes the
				// capacity view useful for ops dashboards.
				_ = s.workerRepo.AddLoadScore(ctx, dedicatedWorker.ID, weight)
				return &dedicatedWorker.ID, nil
			}
		}
	}

	// 5. Assign to shared worker (strict tier separation)
	freeTier := !isPaidOrg // Free trial = free workers, Paid = premium workers
	worker, err := s.selectSharedWorkerForWeight(ctx, freeTier, weight)
	if err != nil {
		return nil, err
	}

	// 6. Update database
	if err := s.workerRepo.UpdateEmailAccountWorker(ctx, emailAccountID, worker.ID); err != nil {
		return nil, err
	}

	// 7. Update worker account count
	if err := s.workerRepo.IncrementAccountCount(ctx, worker.ID); err != nil {
		return nil, err
	}

	// 8. Bump load_score so the next selection sees this worker as
	// proportionally more loaded. Non-fatal so a transient DB hiccup
	// doesn't strand the mailbox; the next capacity-view refresh
	// will correct any drift.
	if err := s.workerRepo.AddLoadScore(ctx, worker.ID, weight); err != nil {
		// log but don't fail
	}

	// 9. Update warmup pool type
	poolType := "free"
	if !freeTier {
		poolType = "premium"
	}
	if err := s.workerRepo.UpdateEmailAccountWarmupPoolType(ctx, emailAccountID, poolType); err != nil {
		// Log but don't fail
	}

	return &worker.ID, nil
}

// resolveMailboxWeight asks the repository for the mailbox's provider +
// warmup flag, then turns that into a load weight via MailboxWeight.
// Any error or missing row falls back to defaultMailboxWeight so a
// transient DB blip doesn't break placement.
func (s *workerAssignmentService) resolveMailboxWeight(ctx context.Context, emailAccountID uuid.UUID) float64 {
	hint, err := s.workerRepo.GetEmailAccountPlacementHint(ctx, emailAccountID)
	if err != nil || hint == nil {
		return defaultMailboxWeight
	}
	return MailboxWeight(hint.Provider, hint.IsWarmup)
}

// UnassignWorkerFromEmail removes the worker assignment for an email
// account and refunds the load_score by the mailbox's weight. Decrement
// is best-effort and clamped at zero by the repository so a duplicate
// unassign never makes the score go negative.
func (s *workerAssignmentService) UnassignWorkerFromEmail(ctx context.Context, emailAccountID uuid.UUID) error {
	info, err := s.workerRepo.GetEmailAccountWorkerInfo(ctx, emailAccountID)
	if err != nil || info == nil || info.WorkerID == nil {
		return err
	}
	weight := s.resolveMailboxWeight(ctx, emailAccountID)

	if err := s.workerRepo.ClearEmailAccountWorker(ctx, emailAccountID); err != nil {
		return err
	}
	if err := s.workerRepo.DecrementAccountCount(ctx, *info.WorkerID); err != nil {
		// log but don't fail
	}
	if err := s.workerRepo.AddLoadScore(ctx, *info.WorkerID, -weight); err != nil {
		// log but don't fail
	}
	return nil
}

// selectSharedWorkerForWeight picks the least-utilised worker that still
// has at least `weight` headroom. Falls back to the legacy
// account-count path if the capacity view returns nothing (test
// environments, fresh deployments before the first health sample lands).
func (s *workerAssignmentService) selectSharedWorkerForWeight(ctx context.Context, freeTier bool, weight float64) (*models.Worker, error) {
	rows, err := s.workerRepo.ListCapacityCandidates(ctx, freeTier, nil)
	if err != nil {
		// Falling back here means a broken capacity view doesn't take
		// the whole onboarding flow down. The legacy path uses
		// account_count, which is always up to date.
		return s.selectSharedWorkerLegacy(ctx, freeTier)
	}
	if len(rows) == 0 {
		return s.selectSharedWorkerLegacy(ctx, freeTier)
	}

	type scored struct {
		WorkerID    uuid.UUID
		Utilization float64
		Headroom    float64
	}
	candidates := make([]scored, 0, len(rows))
	for _, row := range rows {
		cap := ComputeCapacity(WorkerCapacityRow{
			WorkerID:         row.WorkerID,
			WorkerType:       row.WorkerType,
			FreeTier:         row.FreeTier,
			EgressKind:       row.EgressKind,
			HealthState:      row.HealthState,
			LoadScore:        row.LoadScore,
			BaseCapacity:     row.BaseCapacity,
			HealthMultiplier: row.HealthMultiplier,
			AgeMultiplier:    row.AgeMultiplier,
			SendsAttempted1h: row.SendsAttempted1h,
			SendsSucceeded1h: row.SendsSucceeded1h,
			BouncesHard1h:    row.BouncesHard1h,
			BouncesSoft1h:    row.BouncesSoft1h,
			Complaints1h:     row.Complaints1h,
			AuthErrors1h:     row.AuthErrors1h,
		})
		headroom := cap.Effective - cap.Load
		if headroom < weight {
			continue
		}
		candidates = append(candidates, scored{
			WorkerID:    row.WorkerID,
			Utilization: cap.Utilization,
			Headroom:    headroom,
		})
	}
	if len(candidates) == 0 {
		// Every healthy worker is full. The legacy path will at least
		// pick the least-loaded one and the operator can react to the
		// alert this throws via the saturated load_score values.
		return s.selectSharedWorkerLegacy(ctx, freeTier)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Utilization < candidates[j].Utilization
	})
	best := candidates[0]
	worker, err := s.workerRepo.GetByID(ctx, best.WorkerID)
	if err != nil {
		return nil, err
	}
	if worker == nil {
		return nil, ErrNoAvailableWorkers
	}
	return worker, nil
}

// SelectSharedWorker selects the least-utilised healthy shared worker for
// the given tier. Capacity-aware:
//
//   - candidates come from worker_capacity_view (filtered to
//     health_state IN ('healthy', 'watch'))
//   - workers without enough headroom for one cold-SMTP-equivalent
//     mailbox are filtered out
//   - sorted ASC by utilization ratio (Load / Effective) so the
//     least-loaded healthy worker wins
//
// Falls back to selectSharedWorkerLegacy (sort by account_count ASC) if
// the capacity view returns nothing.
func (s *workerAssignmentService) SelectSharedWorker(ctx context.Context, freeTier bool) (*models.Worker, error) {
	return s.selectSharedWorkerForWeight(ctx, freeTier, defaultMailboxWeight)
}

// selectSharedWorkerLegacy is the pre-capacity-view selection path. Kept
// as a separate function so the fallback in selectSharedWorkerForWeight
// is explicit and the migration to capacity-aware placement can be
// reversed without rewriting the call site.
func (s *workerAssignmentService) selectSharedWorkerLegacy(ctx context.Context, freeTier bool) (*models.Worker, error) {
	workers, err := s.workerRepo.GetSharedWorkersByTier(ctx, freeTier)
	if err != nil {
		return nil, err
	}
	if len(workers) == 0 {
		return nil, ErrNoAvailableWorkers
	}
	return &workers[0], nil
}

// SelectSharedWorkerForBand picks the least-loaded shared worker whose
// risk_pool matches band.MatchingRiskPool(). Fallback chain:
//
//  1. Worker in the matching pool of the right tier
//  2. Worker in the clean pool of the right tier (better to land risky
//     mailboxes on clean workers than to refuse, but log + audit this
//     since it dilutes the clean pool — operator should provision a
//     risky/quarantine worker)
//  3. Any worker of the right tier (existing SelectSharedWorker behavior)
//
// Step 3 maintains backwards compatibility with installations that don't
// run risk pools yet — they leave everything in risk_pool='clean' and
// behavior is unchanged.
func (s *workerAssignmentService) SelectSharedWorkerForBand(ctx context.Context, freeTier bool, band models.EmailRiskBand) (*models.Worker, error) {
	target := band.MatchingRiskPool()

	workers, err := s.workerRepo.GetSharedWorkersByTierAndPool(ctx, freeTier, target)
	if err != nil {
		return nil, err
	}
	if len(workers) > 0 {
		return &workers[0], nil
	}

	// Step 2: fall back to the clean pool. Only kicks in for risky/quarantine
	// bands when no matching-pool worker exists.
	if target != models.WorkerRiskPoolClean {
		workers, err = s.workerRepo.GetSharedWorkersByTierAndPool(ctx, freeTier, models.WorkerRiskPoolClean)
		if err != nil {
			return nil, err
		}
		if len(workers) > 0 {
			return &workers[0], nil
		}
	}

	// Step 3: last-resort, any tier worker. Same as legacy SelectSharedWorker.
	return s.SelectSharedWorker(ctx, freeTier)
}

// AssignDedicatedWorker assigns a dedicated worker to an organization
func (s *workerAssignmentService) AssignDedicatedWorker(ctx context.Context, orgID, subscriptionID uuid.UUID) error {
	// Use atomic insert with conflict check to prevent race conditions.
	// Two concurrent requests could both pass the "check if exists" step and
	// both attempt to insert, causing duplicate assignments.
	worker, err := s.workerRepo.GetAvailableDedicatedWorker(ctx)
	if err != nil {
		return err
	}
	if worker == nil {
		return ErrNoDedicatedWorkers
	}

	assignment := &models.DedicatedWorkerAssignment{
		ID:             uuid.New(),
		WorkerID:       worker.ID,
		UserID:         orgID,
		SubscriptionID: subscriptionID,
		AssignedAt:     time.Now(),
	}

	created, err := s.workerRepo.CreateDedicatedAssignmentIfNotExists(ctx, assignment)
	if err != nil {
		return err
	}
	if !created {
		return ErrOrgAlreadyAssigned
	}
	return nil
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
