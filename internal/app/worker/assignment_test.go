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
	lastEmailWorkerAssigned uuid.UUID
	lastEmailPoolTypeSet    string
	incrementedWorkerCounts map[uuid.UUID]int
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
