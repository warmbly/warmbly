// Contract tests for plan-aware worker assignment.
//
// These lock the rule that's at the heart of multi-tenant deliverability:
// who lands on which worker is a function of the org's subscription, not of
// the request shape or who happens to be online. If this contract ever
// regresses, free-tier orgs could leak onto premium workers (or vice versa)
// and tank the IPs of paying customers.
//
// Test style: hand-rolled stub repos that embed the interface as a nil
// field so only the methods AssignWorkerToEmail actually touches need
// real bodies — anything else panics, which is the desired loud failure.

package worker

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// stubs

type stubWorkerRepo struct {
	repository.WorkerRepository // embed for the panic-on-unused-method behavior

	dedicatedForOrg         *models.Worker
	sharedFree              []models.Worker
	sharedPremium           []models.Worker
	capacityFree            []repository.WorkerCapacityRowDB
	capacityPremium         []repository.WorkerCapacityRowDB
	workersByID             map[uuid.UUID]models.Worker
	placementHint           *repository.EmailAccountPlacementHint
	lastEmailWorkerAssigned uuid.UUID
	lastEmailPoolTypeSet    string
	incrementedWorkerCounts map[uuid.UUID]int
	loadScoreDeltas         map[uuid.UUID]float64
}

func (r *stubWorkerRepo) GetDedicatedWorkerByUserID(_ context.Context, _ uuid.UUID) (*models.Worker, error) {
	return r.dedicatedForOrg, nil
}
func (r *stubWorkerRepo) GetSharedWorkersByTier(_ context.Context, freeTier bool) ([]models.Worker, error) {
	if freeTier {
		return r.sharedFree, nil
	}
	return r.sharedPremium, nil
}
func (r *stubWorkerRepo) UpdateEmailAccountWorker(_ context.Context, emailID, workerID uuid.UUID) error {
	r.lastEmailWorkerAssigned = workerID
	return nil
}
func (r *stubWorkerRepo) IncrementAccountCount(_ context.Context, workerID uuid.UUID) error {
	if r.incrementedWorkerCounts == nil {
		r.incrementedWorkerCounts = map[uuid.UUID]int{}
	}
	r.incrementedWorkerCounts[workerID]++
	return nil
}
func (r *stubWorkerRepo) UpdateEmailAccountWarmupPoolType(_ context.Context, _ uuid.UUID, pool string) error {
	r.lastEmailPoolTypeSet = pool
	return nil
}

// Capacity-aware methods. Default behaviour: empty capacity view so the
// assignment service falls back to the legacy account-count path. Tests
// that want to exercise the capacity-aware path populate
// capacityFree/capacityPremium + workersByID.
func (r *stubWorkerRepo) ListCapacityCandidates(_ context.Context, freeTier bool, _ []models.WorkerHealthState) ([]repository.WorkerCapacityRowDB, error) {
	if freeTier {
		return r.capacityFree, nil
	}
	return r.capacityPremium, nil
}

func (r *stubWorkerRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Worker, error) {
	w, ok := r.workersByID[id]
	if !ok {
		return nil, nil
	}
	return &w, nil
}

func (r *stubWorkerRepo) GetEmailAccountPlacementHint(_ context.Context, _ uuid.UUID) (*repository.EmailAccountPlacementHint, error) {
	return r.placementHint, nil
}

func (r *stubWorkerRepo) AddLoadScore(_ context.Context, workerID uuid.UUID, delta float64) error {
	if r.loadScoreDeltas == nil {
		r.loadScoreDeltas = map[uuid.UUID]float64{}
	}
	r.loadScoreDeltas[workerID] += delta
	return nil
}

type stubSubRepo struct {
	repository.SubscriptionRepository
	sub *models.Subscription
}

func (r *stubSubRepo) GetByOrganizationID(_ context.Context, _ uuid.UUID) (*models.Subscription, error) {
	return r.sub, nil
}

type stubPlanRepo struct {
	repository.PlanRepository
	plan *models.Plan
}

func (r *stubPlanRepo) GetByID(_ context.Context, _ uuid.UUID) (*models.Plan, error) {
	return r.plan, nil
}

// tests

func newWorker(id uuid.UUID, freeTier bool, wtype models.WorkerType) models.Worker {
	return models.Worker{ID: id, FreeTier: freeTier, WorkerType: wtype, Active: true}
}

func TestAssign_FreeOrg_LandsOnFreeSharedWorker(t *testing.T) {
	freeWorker := newWorker(uuid.New(), true, models.WorkerTypeShared)
	premiumWorker := newWorker(uuid.New(), false, models.WorkerTypeShared)

	wr := &stubWorkerRepo{
		sharedFree:    []models.Worker{freeWorker},
		sharedPremium: []models.Worker{premiumWorker},
	}
	svc := NewAssignmentService(wr, &stubSubRepo{sub: nil}, &stubPlanRepo{})

	emailID := uuid.New()
	got, err := svc.AssignWorkerToEmail(context.Background(), emailID, uuid.New())
	if err != nil {
		t.Fatalf("AssignWorkerToEmail: %v", err)
	}
	if *got != freeWorker.ID {
		t.Errorf("free org should land on free worker, got %s (free=%s)", got, freeWorker.ID)
	}
	if wr.lastEmailPoolTypeSet != "free" {
		t.Errorf("free org should join the free warmup pool, got %q", wr.lastEmailPoolTypeSet)
	}
}

func TestAssign_PaidOrg_LandsOnPremiumSharedWorker(t *testing.T) {
	freeWorker := newWorker(uuid.New(), true, models.WorkerTypeShared)
	premiumWorker := newWorker(uuid.New(), false, models.WorkerTypeShared)

	wr := &stubWorkerRepo{
		sharedFree:    []models.Worker{freeWorker},
		sharedPremium: []models.Worker{premiumWorker},
	}
	// Subscription is active but plan has no dedicated workers.
	sub := paidSub()
	plan := &models.Plan{ID: sub.PlanID, DedicatedWorkers: 0}
	svc := NewAssignmentService(wr, &stubSubRepo{sub: sub}, &stubPlanRepo{plan: plan})

	got, err := svc.AssignWorkerToEmail(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("AssignWorkerToEmail: %v", err)
	}
	if *got != premiumWorker.ID {
		t.Errorf("paid org should land on premium worker, got %s (premium=%s)", got, premiumWorker.ID)
	}
	if wr.lastEmailPoolTypeSet != "premium" {
		t.Errorf("paid org should join the premium warmup pool, got %q", wr.lastEmailPoolTypeSet)
	}
}

func TestAssign_PaidOrgWithDedicatedPlan_LandsOnDedicatedWorker(t *testing.T) {
	dedicated := newWorker(uuid.New(), false, models.WorkerTypeDedicated)
	premium := newWorker(uuid.New(), false, models.WorkerTypeShared)

	wr := &stubWorkerRepo{
		dedicatedForOrg: &dedicated,
		sharedPremium:   []models.Worker{premium},
	}
	sub := paidSub()
	plan := &models.Plan{ID: sub.PlanID, DedicatedWorkers: 1}
	svc := NewAssignmentService(wr, &stubSubRepo{sub: sub}, &stubPlanRepo{plan: plan})

	got, err := svc.AssignWorkerToEmail(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("AssignWorkerToEmail: %v", err)
	}
	if *got != dedicated.ID {
		t.Errorf("paid org with dedicated plan + assignment should land on the dedicated worker, got %s", got)
	}
}

func TestAssign_PaidOrgWithDedicatedPlanButNoAssignment_FallsBackToPremium(t *testing.T) {
	premium := newWorker(uuid.New(), false, models.WorkerTypeShared)
	wr := &stubWorkerRepo{
		dedicatedForOrg: nil, // org has the plan but no worker assigned yet
		sharedPremium:   []models.Worker{premium},
	}
	sub := paidSub()
	plan := &models.Plan{ID: sub.PlanID, DedicatedWorkers: 1}
	svc := NewAssignmentService(wr, &stubSubRepo{sub: sub}, &stubPlanRepo{plan: plan})

	got, err := svc.AssignWorkerToEmail(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("AssignWorkerToEmail: %v", err)
	}
	if *got != premium.ID {
		t.Errorf("org with dedicated plan but no assignment should fall back to premium, got %s", got)
	}
}

func TestSelectSharedWorker_NoWorkers_Errors(t *testing.T) {
	svc := NewAssignmentService(&stubWorkerRepo{}, &stubSubRepo{}, &stubPlanRepo{})
	if _, err := svc.SelectSharedWorker(context.Background(), true); err != ErrNoAvailableWorkers {
		t.Fatalf("expected ErrNoAvailableWorkers, got %v", err)
	}
}

// paidSub returns a minimal subscription that HasPaidSubscription() will
// return true for: status == "active" AND StripeSubscriptionID set.
func paidSub() *models.Subscription {
	sid := "sub_test_" + uuid.NewString()
	return &models.Subscription{
		ID:                   uuid.New(),
		PlanID:               uuid.New(),
		Status:               models.SubscriptionStatusActive,
		StripeSubscriptionID: &sid,
	}
}

// capacity-aware selection tests
//
// These exercise the new path: ListCapacityCandidates returns rows,
// the service computes utilization, sorts ASC, and lands the mailbox on
// the least-utilised worker that still has headroom for the weight.

func capacityRow(id uuid.UUID, base, load float64) repository.WorkerCapacityRowDB {
	return repository.WorkerCapacityRowDB{
		WorkerID:         id,
		WorkerType:       models.WorkerTypeShared,
		FreeTier:         true,
		EgressKind:       models.WorkerEgressColdSMTP,
		HealthState:      models.WorkerHealthHealthy,
		LoadScore:        load,
		BaseCapacity:     base,
		HealthMultiplier: 1.0,
		AgeMultiplier:    1.0,
	}
}

func TestSelectSharedWorker_CapacityAware_LeastUtilizedWins(t *testing.T) {
	hot := uuid.New()  // 14/16 utilised
	cold := uuid.New() // 2/16 utilised

	wr := &stubWorkerRepo{
		capacityFree: []repository.WorkerCapacityRowDB{
			capacityRow(hot, 16, 14),
			capacityRow(cold, 16, 2),
		},
		workersByID: map[uuid.UUID]models.Worker{
			hot:  {ID: hot, FreeTier: true, WorkerType: models.WorkerTypeShared, Active: true},
			cold: {ID: cold, FreeTier: true, WorkerType: models.WorkerTypeShared, Active: true},
		},
	}
	svc := NewAssignmentService(wr, &stubSubRepo{}, &stubPlanRepo{})
	got, err := svc.SelectSharedWorker(context.Background(), true)
	if err != nil {
		t.Fatalf("SelectSharedWorker: %v", err)
	}
	if got.ID != cold {
		t.Errorf("least-utilized should win: got %s, want %s", got.ID, cold)
	}
}

func TestSelectSharedWorker_CapacityAware_FiltersOutSaturated(t *testing.T) {
	// Saturated worker (load == base) has zero headroom. Filtered out;
	// the only remaining candidate wins.
	saturated := uuid.New()
	headroom := uuid.New()

	wr := &stubWorkerRepo{
		capacityFree: []repository.WorkerCapacityRowDB{
			capacityRow(saturated, 16, 16),
			capacityRow(headroom, 16, 4),
		},
		workersByID: map[uuid.UUID]models.Worker{
			saturated: {ID: saturated, FreeTier: true, WorkerType: models.WorkerTypeShared, Active: true},
			headroom:  {ID: headroom, FreeTier: true, WorkerType: models.WorkerTypeShared, Active: true},
		},
	}
	svc := NewAssignmentService(wr, &stubSubRepo{}, &stubPlanRepo{})
	got, err := svc.SelectSharedWorker(context.Background(), true)
	if err != nil {
		t.Fatalf("SelectSharedWorker: %v", err)
	}
	if got.ID != headroom {
		t.Errorf("saturated worker should be filtered, got %s, want %s", got.ID, headroom)
	}
}

func TestSelectSharedWorker_CapacityAware_FallsBackWhenAllSaturated(t *testing.T) {
	// Every worker is full; the legacy account_count path catches us so
	// we don't fail the assignment outright. Falls back through
	// selectSharedWorkerLegacy -> GetSharedWorkersByTier.
	a := newWorker(uuid.New(), true, models.WorkerTypeShared)
	wr := &stubWorkerRepo{
		capacityFree: []repository.WorkerCapacityRowDB{
			capacityRow(a.ID, 16, 16),
		},
		workersByID: map[uuid.UUID]models.Worker{a.ID: a},
		sharedFree:  []models.Worker{a},
	}
	svc := NewAssignmentService(wr, &stubSubRepo{}, &stubPlanRepo{})
	got, err := svc.SelectSharedWorker(context.Background(), true)
	if err != nil {
		t.Fatalf("SelectSharedWorker: %v", err)
	}
	if got.ID != a.ID {
		t.Errorf("fallback should still return the only worker, got %s", got.ID)
	}
}

func TestAssign_UpdatesLoadScoreByMailboxWeight(t *testing.T) {
	// AssignWorkerToEmail must bump load_score by MailboxWeight. With an
	// OAuth provider the bump is 0.05; with cold SMTP it's 1.0; with
	// warmup it's 0.4.
	freeWorker := newWorker(uuid.New(), true, models.WorkerTypeShared)
	wr := &stubWorkerRepo{
		sharedFree:    []models.Worker{freeWorker},
		placementHint: &repository.EmailAccountPlacementHint{Provider: "gmail-api", IsWarmup: false},
	}
	svc := NewAssignmentService(wr, &stubSubRepo{sub: nil}, &stubPlanRepo{})

	if _, err := svc.AssignWorkerToEmail(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("AssignWorkerToEmail: %v", err)
	}
	if got := wr.loadScoreDeltas[freeWorker.ID]; got != 0.05 {
		t.Errorf("load_score should be bumped by 0.05 for gmail-api, got %v", got)
	}
}
