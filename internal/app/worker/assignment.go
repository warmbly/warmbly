package worker

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// defaultMailboxWeight is the weight applied when placement can't fetch the
// mailbox's provider (row just deleted, or the lookup failed). 1.0 lines up
// with a raw SMTP mailbox, which is the conservative assumption: better to
// over-account for the placement than to let a worker over-commit.
const defaultMailboxWeight = 1.0

var (
	ErrNoAvailableWorkers = errors.New("no available workers")
	ErrNoIdleWorkers      = errors.New("no idle worker available to reserve")
	ErrOrgAlreadyReserved = errors.New("organization already has a reserved worker")
)

// WorkerAssignmentService places mailboxes onto workers and keeps them there.
//
// There is one kind of worker. Placement is a score over live facts (capacity,
// health, incumbency, sign-in geography, tenant blast radius, per-provider
// crowding), never a lookup of matching labels, because the worker is not the
// sending identity: it authenticates to the customer's mailbox provider, which
// delivers from its own outbound pool.
type WorkerAssignmentService interface {
	// AssignWorkerToEmail gives a newly connected mailbox a home.
	AssignWorkerToEmail(ctx context.Context, emailAccountID, orgID uuid.UUID) (*uuid.UUID, error)

	// UnassignWorkerFromEmail releases a mailbox from its worker and refunds
	// the worker's load score.
	UnassignWorkerFromEmail(ctx context.Context, emailAccountID uuid.UUID) error

	// IsWorkerLive reports whether a worker can still receive commands. A
	// mailbox on a worker that fails this cannot send until it is re-placed.
	IsWorkerLive(ctx context.Context, workerID uuid.UUID) (bool, error)

	// SelectWorkerFor scores the fleet for one mailbox without committing to
	// anything. The rotation loop uses it to ask "is there somewhere better?"
	// before deciding a move is worth its provider-trust cost.
	SelectWorkerFor(ctx context.Context, req PlacementLookup) (*PlacementResult, error)

	// MoveMailbox re-places a mailbox onto a specific worker, keeping the
	// account counts and load scores on both sides consistent.
	MoveMailbox(ctx context.Context, emailAccountID uuid.UUID, from *uuid.UUID, to uuid.UUID) error

	// Isolated egress: the entitlement that reserves a worker for one
	// organization so its mailboxes sign in from an address nobody else uses.
	ReserveIsolatedWorker(ctx context.Context, orgID, subscriptionID uuid.UUID) error
	ReleaseIsolatedWorker(ctx context.Context, orgID uuid.UUID) error
	GetIsolatedWorker(ctx context.Context, orgID uuid.UUID) (*models.Worker, error)

	// MigrateEmailsFromWorker drains every mailbox off a worker.
	MigrateEmailsFromWorker(ctx context.Context, workerID uuid.UUID) error

	// SelectValidationWorker returns any live worker to run a one-shot
	// credential handshake on. Nothing is placed, so no scoring applies: the
	// worker only dials the mailbox once and reports back.
	SelectValidationWorker(ctx context.Context) (*models.Worker, error)
}

// PlacementLookup asks "where should this mailbox live?".
type PlacementLookup struct {
	EmailAccountID uuid.UUID
	OrgID          uuid.UUID
	// CurrentWorkerID is the incumbent, if any. Present means the caller is
	// considering a move and the incumbent should get its stickiness bonus.
	CurrentWorkerID *uuid.UUID
	// ExcludeWorkerID drops one worker from consideration entirely. Set when
	// draining: without it the drained worker is still the incumbent, wins on
	// stickiness, and the drain is a silent no-op.
	ExcludeWorkerID *uuid.UUID
	// Region is where the mailbox signs in from today. Supplied on a
	// re-placement so a move keeps the sign-in geography its provider has
	// already seen; empty on first placement, which scores neutral.
	Region string
}

// PlacementResult is the chosen worker plus the score it and the incumbent
// earned, so a caller can decide whether the difference justifies a migration
// and log why.
type PlacementResult struct {
	Worker         *models.Worker
	Score          float64
	IncumbentScore float64
	// IncumbentEligible is false when the current worker is too unhealthy to
	// host the mailbox at all, in which case IncumbentScore is meaningless.
	// Being over capacity does not clear it; that shows up in IncumbentScore.
	IncumbentEligible bool
	// Mandated is set when the worker was chosen by an entitlement rather than
	// by scoring, which today means an isolated-egress reservation. Callers
	// must not weigh it against the incumbent's score: the reserved worker
	// usually scores LOWER, because the incumbent carries the stickiness
	// bonus, so comparing them would refuse the move forever and the
	// organization would never converge onto the worker it is paying for.
	Mandated bool
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

// AssignWorkerToEmail places a mailbox for the first time.
func (s *workerAssignmentService) AssignWorkerToEmail(ctx context.Context, emailAccountID, orgID uuid.UUID) (*uuid.UUID, error) {
	res, err := s.SelectWorkerFor(ctx, PlacementLookup{EmailAccountID: emailAccountID, OrgID: orgID})
	if err != nil {
		return nil, err
	}
	if res == nil || res.Worker == nil {
		return nil, ErrNoAvailableWorkers
	}

	if err := s.workerRepo.UpdateEmailAccountWorker(ctx, emailAccountID, res.Worker.ID); err != nil {
		return nil, err
	}
	if err := s.workerRepo.IncrementAccountCount(ctx, res.Worker.ID); err != nil {
		// Non-fatal: the next capacity-view refresh corrects any drift, and
		// stranding a freshly connected mailbox would be much worse.
		log.Warn().Err(err).Str("worker_id", res.Worker.ID.String()).Msg("placement: increment account count failed")
	}
	if err := s.workerRepo.AddLoadScore(ctx, res.Worker.ID, s.resolveMailboxWeight(ctx, emailAccountID)); err != nil {
		log.Warn().Err(err).Str("worker_id", res.Worker.ID.String()).Msg("placement: load score bump failed")
	}

	// Warmup pool membership still follows the subscription. It used to be a
	// by-product of tier placement; with tiers gone it has to be set
	// explicitly, or every new mailbox keeps the column default and paid
	// customers silently warm in the free pool.
	if err := s.workerRepo.UpdateEmailAccountWarmupPoolType(ctx, emailAccountID, s.warmupPoolFor(ctx, orgID)); err != nil {
		log.Warn().Err(err).Str("account_id", emailAccountID.String()).Msg("placement: warmup pool assignment failed")
	}
	return &res.Worker.ID, nil
}

// SelectWorkerFor scores every eligible worker and returns the best, plus what
// the incumbent scored so the caller can weigh a move against staying put.
func (s *workerAssignmentService) SelectWorkerFor(ctx context.Context, lookup PlacementLookup) (*PlacementResult, error) {
	hint, _ := s.workerRepo.GetEmailAccountPlacementHint(ctx, lookup.EmailAccountID)
	provider := ""
	weight := defaultMailboxWeight
	if hint != nil {
		provider = hint.Provider
		weight = MailboxWeight(hint.Provider, hint.IsWarmup)
	}

	orgTotal, err := s.workerRepo.CountOrgMailboxes(ctx, lookup.OrgID)
	if err != nil {
		// Blast radius just loses its denominator; placement still works.
		orgTotal = 0
	}

	req := PlacementRequest{
		Weight:            weight,
		Region:            lookup.Region,
		CurrentWorkerID:   lookup.CurrentWorkerID,
		OrgMailboxesTotal: orgTotal,
		IsolatedEgress:    s.hasIsolatedEgress(ctx, lookup.OrgID),
	}

	rows, err := s.workerRepo.ListPlacementCandidates(ctx, lookup.OrgID, provider, nil)
	if err != nil || len(rows) == 0 {
		// A broken or unpopulated capacity view must not take onboarding down.
		// Fall back to the least-loaded live worker.
		return s.selectFallback(ctx, req, lookup.ExcludeWorkerID)
	}

	candidates := make([]PlacementCandidate, 0, len(rows))
	for _, row := range rows {
		if lookup.ExcludeWorkerID != nil && row.WorkerID == *lookup.ExcludeWorkerID {
			continue
		}
		candidates = append(candidates, PlacementCandidate{
			WorkerID: row.WorkerID,
			Region:   row.Region,
			Health:   row.HealthState,
			Capacity: ComputeCapacity(WorkerCapacityRow{
				WorkerID:         row.WorkerID,
				Region:           row.Region,
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
			}),
			TotalMailboxes:        row.TotalMailboxes,
			OrgMailboxesHere:      row.OrgMailboxesHere,
			ProviderMailboxesHere: row.ProviderMailboxesHere,
		})
	}

	// The reserved worker for an isolated-egress org outranks the score: the
	// customer is paying for that specific address. It still has to be
	// eligible - a reserved worker that is down is not a reason to strand -
	// and it still has to have room. Capacity used to bound this branch
	// through Eligible; now that Eligible is health-only, the check is
	// explicit, because this path skips SelectPlacement and would otherwise
	// pile a whole organization onto one box without ever paying the
	// over-target cost. An over-target reserved worker falls through to
	// scoring, where weightIsolation still prefers it: the entitlement is a
	// strong preference, not a pin.
	if req.IsolatedEgress {
		if reserved, rerr := s.workerRepo.GetDedicatedWorkerByOrgID(ctx, lookup.OrgID); rerr == nil && reserved != nil {
			for _, c := range candidates {
				if c.WorkerID == reserved.ID && c.Eligible(req) && !c.OverTarget(req) {
					res, err := s.buildResult(ctx, c, req, candidates)
					if res != nil {
						res.Mandated = true
					}
					return res, err
				}
			}
		}
	}

	best := SelectPlacement(candidates, req)
	if best == nil {
		return s.selectFallback(ctx, req, lookup.ExcludeWorkerID)
	}
	return s.buildResult(ctx, *best, req, candidates)
}

func (s *workerAssignmentService) buildResult(
	ctx context.Context,
	chosen PlacementCandidate,
	req PlacementRequest,
	all []PlacementCandidate,
) (*PlacementResult, error) {
	worker, err := s.workerRepo.GetByID(ctx, chosen.WorkerID)
	if err != nil {
		return nil, err
	}
	if worker == nil {
		return nil, ErrNoAvailableWorkers
	}
	res := &PlacementResult{Worker: worker, Score: chosen.Score(req)}
	if req.CurrentWorkerID != nil {
		for _, c := range all {
			if c.WorkerID != *req.CurrentWorkerID {
				continue
			}
			res.IncumbentEligible = c.Eligible(req)
			res.IncumbentScore = c.Score(req)
			break
		}
	}
	return res, nil
}

// selectFallback is the no-capacity-view path: least-loaded live worker.
//
// It still refuses an unhealthy one. Without that, draining a quarantined
// worker could empty the candidate set, fall through here, and place the
// mailboxes onto another blocked machine, which is the opposite of what the
// drain was for.
func (s *workerAssignmentService) selectFallback(ctx context.Context, req PlacementRequest, exclude *uuid.UUID) (*PlacementResult, error) {
	workers, err := s.workerRepo.ListPlaceableWorkers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range workers {
		w := workers[i]
		if exclude != nil && w.ID == *exclude {
			continue
		}
		switch w.HealthState {
		case models.WorkerHealthHealthy, models.WorkerHealthWatch:
			return &PlacementResult{Worker: &w}, nil
		}
	}
	return nil, ErrNoAvailableWorkers
}

// warmupPoolFor resolves which warmup pool a mailbox joins. Unrelated to
// placement: it is a property of the organization's subscription, and workers
// no longer carry a tier to infer it from. Anything unknown answers "free",
// which is the conservative direction - a paid mailbox in the free pool warms
// more slowly, where the reverse would put unproven mail in front of paying
// customers.
func (s *workerAssignmentService) warmupPoolFor(ctx context.Context, orgID uuid.UUID) string {
	// Billing first: with it disabled there is no free/paid split to enforce,
	// so every org gets the premium pool and the subscription is irrelevant.
	// Checking the repository before this made a self-host install with no
	// subscription repo wired fall through to "free" and warm every mailbox in
	// the wrong pool. Mirrors feature.gate's self-host unlock.
	if config.BillingProvider() == "none" {
		return "premium"
	}
	if s.subRepo == nil {
		return "free"
	}
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil || sub == nil || !sub.HasPaidSubscription() {
		return "free"
	}
	return "premium"
}

// hasIsolatedEgress reports whether the org's plan entitles it to a worker
// nobody else sends from. Any lookup failure answers false: the entitlement
// only ever adds preference, so losing it degrades to ordinary placement.
func (s *workerAssignmentService) hasIsolatedEgress(ctx context.Context, orgID uuid.UUID) bool {
	if s.subRepo == nil || s.planRepo == nil {
		return false
	}
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil || sub == nil {
		return false
	}
	plan, err := s.planRepo.GetByID(ctx, sub.EffectivePlanID())
	if err != nil || plan == nil {
		return false
	}
	return plan.IsolatedEgress()
}

// resolveMailboxWeight turns the mailbox's provider + warmup flag into a load
// weight. Any error falls back to the conservative default.
func (s *workerAssignmentService) resolveMailboxWeight(ctx context.Context, emailAccountID uuid.UUID) float64 {
	hint, err := s.workerRepo.GetEmailAccountPlacementHint(ctx, emailAccountID)
	if err != nil || hint == nil {
		return defaultMailboxWeight
	}
	return MailboxWeight(hint.Provider, hint.IsWarmup)
}

func (s *workerAssignmentService) IsWorkerLive(ctx context.Context, workerID uuid.UUID) (bool, error) {
	return s.workerRepo.IsWorkerLive(ctx, workerID)
}

// UnassignWorkerFromEmail removes the worker assignment and refunds the load
// score. Decrement is clamped at zero by the repository, so a duplicate
// unassign never makes the score negative.
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
		log.Warn().Err(err).Msg("unassign: decrement account count failed")
	}
	if err := s.workerRepo.AddLoadScore(ctx, *info.WorkerID, -weight); err != nil {
		log.Warn().Err(err).Msg("unassign: load score refund failed")
	}
	return nil
}

// MoveMailbox re-places a mailbox, keeping both workers' counters consistent.
// The weight is resolved once and applied symmetrically so a move is
// load-neutral across the fleet.
func (s *workerAssignmentService) MoveMailbox(ctx context.Context, emailAccountID uuid.UUID, from *uuid.UUID, to uuid.UUID) error {
	if from != nil && *from == to {
		return nil
	}
	weight := s.resolveMailboxWeight(ctx, emailAccountID)

	if err := s.workerRepo.UpdateEmailAccountWorker(ctx, emailAccountID, to); err != nil {
		return err
	}
	if from != nil {
		if err := s.workerRepo.DecrementAccountCount(ctx, *from); err != nil {
			log.Warn().Err(err).Msg("move: decrement source account count failed")
		}
		if err := s.workerRepo.AddLoadScore(ctx, *from, -weight); err != nil {
			log.Warn().Err(err).Msg("move: source load refund failed")
		}
	}
	if err := s.workerRepo.IncrementAccountCount(ctx, to); err != nil {
		log.Warn().Err(err).Msg("move: increment target account count failed")
	}
	if err := s.workerRepo.AddLoadScore(ctx, to, weight); err != nil {
		log.Warn().Err(err).Msg("move: target load bump failed")
	}
	return nil
}

// ReserveIsolatedWorker binds an idle worker to an organization so its
// mailboxes sign in from an address no other tenant uses.
//
// The binding is a strong placement preference, not a hard pin: the worker
// itself carries no category, so if it dies the org's mailboxes place normally
// instead of stranding, and the rotation loop pulls them back onto a
// replacement once one exists.
func (s *workerAssignmentService) ReserveIsolatedWorker(ctx context.Context, orgID, subscriptionID uuid.UUID) error {
	worker, err := s.workerRepo.GetIdleUnboundWorker(ctx)
	if err != nil {
		return err
	}
	if worker == nil {
		return ErrNoIdleWorkers
	}

	assignment := &models.DedicatedWorkerAssignment{
		ID:             uuid.New(),
		WorkerID:       worker.ID,
		OrganizationID: orgID,
		SubscriptionID: subscriptionID,
		AssignedAt:     time.Now(),
	}

	created, err := s.workerRepo.CreateDedicatedAssignmentIfNotExists(ctx, assignment)
	if err != nil {
		return err
	}
	if !created {
		// Lost the race; the org is bound by the winner. Nothing to undo,
		// because reserving no longer mutates the worker row.
		return ErrOrgAlreadyReserved
	}
	log.Info().
		Str("worker_id", worker.ID.String()).
		Str("org_id", orgID.String()).
		Msg("placement: reserved worker for isolated egress")
	return nil
}

func (s *workerAssignmentService) ReleaseIsolatedWorker(ctx context.Context, orgID uuid.UUID) error {
	return s.workerRepo.ReleaseDedicatedAssignment(ctx, orgID)
}

func (s *workerAssignmentService) GetIsolatedWorker(ctx context.Context, orgID uuid.UUID) (*models.Worker, error) {
	return s.workerRepo.GetDedicatedWorkerByOrgID(ctx, orgID)
}

// MigrateEmailsFromWorker drains a worker. Each mailbox is re-scored rather
// than dumped onto one destination, so draining a big worker spreads its load
// instead of moving the hotspot.
func (s *workerAssignmentService) MigrateEmailsFromWorker(ctx context.Context, workerID uuid.UUID) error {
	accountIDs, err := s.workerRepo.GetEmailAccountsByWorkerID(ctx, workerID)
	if err != nil {
		return err
	}

	for _, accountID := range accountIDs {
		info, err := s.workerRepo.GetEmailAccountWorkerInfo(ctx, accountID)
		if err != nil || info == nil {
			continue
		}
		state, err := s.workerRepo.GetMailboxPlacementState(ctx, accountID)
		if err != nil || state == nil || state.OrganizationID == nil {
			continue
		}
		res, err := s.SelectWorkerFor(ctx, PlacementLookup{
			EmailAccountID:  accountID,
			OrgID:           *state.OrganizationID,
			Region:          state.WorkerRegion,
			ExcludeWorkerID: &workerID,
		})
		if err != nil || res == nil || res.Worker == nil || res.Worker.ID == workerID {
			continue
		}
		if err := s.MoveMailbox(ctx, accountID, &workerID, res.Worker.ID); err != nil {
			log.Warn().Err(err).Str("account_id", accountID.String()).Msg("drain: move failed")
		}
	}

	return nil
}

// SelectValidationWorker returns the least loaded live worker. Used by the
// connect and reconnect flows to test credentials before anything is stored.
func (s *workerAssignmentService) SelectValidationWorker(ctx context.Context) (*models.Worker, error) {
	workers, err := s.workerRepo.ListPlaceableWorkers(ctx)
	if err != nil {
		return nil, err
	}
	if len(workers) == 0 {
		return nil, ErrNoAvailableWorkers
	}
	return &workers[0], nil
}
