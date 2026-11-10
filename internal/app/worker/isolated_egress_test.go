package worker

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

var (
	isolatedPlanID = uuid.New()
	sharedPlanID   = uuid.New()
)

type egressSubs struct {
	repository.SubscriptionRepository
	sub *models.Subscription
}

func (r egressSubs) GetByOrganizationID(context.Context, uuid.UUID) (*models.Subscription, error) {
	return r.sub, nil
}

type egressPlans struct{ repository.PlanRepository }

func (egressPlans) GetByID(_ context.Context, id uuid.UUID) (*models.Plan, error) {
	if id == isolatedPlanID {
		return &models.Plan{ID: id, DedicatedWorkers: 1}, nil
	}
	return &models.Plan{ID: id}, nil
}

type egressWorkers struct {
	repository.WorkerRepository
	reserved  *models.DedicatedWorkerAssignment
	idle      *models.Worker
	mailboxes int
	created   []models.DedicatedWorkerAssignment
	released  []uuid.UUID
	// slot answers AgencyEgressSlot; slotLimit records the cap it was asked with.
	slot      bool
	slotLimit int
}

func (r *egressWorkers) AgencyEgressSlot(_ context.Context, _, _ uuid.UUID, limit int) (bool, error) {
	r.slotLimit = limit
	return r.slot, nil
}

func (r *egressWorkers) GetActiveDedicatedAssignment(context.Context, uuid.UUID) (*models.DedicatedWorkerAssignment, error) {
	return r.reserved, nil
}

func (r *egressWorkers) CountOrgMailboxes(context.Context, uuid.UUID) (int, error) {
	return r.mailboxes, nil
}

func (r *egressWorkers) GetIdleUnboundWorker(context.Context) (*models.Worker, error) {
	return r.idle, nil
}

func (r *egressWorkers) CreateDedicatedAssignmentIfNotExists(_ context.Context, a *models.DedicatedWorkerAssignment) (bool, error) {
	r.created = append(r.created, *a)
	return true, nil
}

func (r *egressWorkers) ReleaseDedicatedAssignmentByID(_ context.Context, id uuid.UUID) (bool, error) {
	r.released = append(r.released, id)
	return true, nil
}

func grantedSub(planID uuid.UUID, until *time.Time) *models.Subscription {
	now := time.Now()
	return &models.Subscription{ID: uuid.New(), PlanID: models.FreeTrialPlanID, Status: models.SubscriptionStatusIncomplete,
		ManagedAt: &now, ManagedUntil: until, ManagedPlanID: &planID}
}

func egressService(sub *models.Subscription, workers *egressWorkers) *workerAssignmentService {
	return &workerAssignmentService{workerRepo: workers, subRepo: egressSubs{sub: sub}, planRepo: egressPlans{}}
}

func TestSyncIsolatedEgressFollowsGrants(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "stripe")
	ctx, org := context.Background(), uuid.New()
	future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)

	idle := &models.Worker{ID: uuid.New()}
	w := &egressWorkers{idle: idle, mailboxes: 2}
	sub := grantedSub(isolatedPlanID, &future)
	if err := egressService(sub, w).SyncIsolatedEgress(ctx, org); err != nil {
		t.Fatal(err)
	}
	if len(w.created) != 1 || w.created[0].WorkerID != idle.ID || w.created[0].SubscriptionID != sub.ID {
		t.Fatalf("a grant onto an isolated plan must reserve an idle worker, got %+v", w.created)
	}

	empty := &egressWorkers{idle: idle}
	if err := egressService(grantedSub(isolatedPlanID, nil), empty).SyncIsolatedEgress(ctx, org); err != nil {
		t.Fatal(err)
	}
	if len(empty.created) != 0 {
		t.Fatal("a workspace with no mailboxes must not hold a worker out of the fleet")
	}

	none := &egressWorkers{mailboxes: 1}
	if err := egressService(grantedSub(isolatedPlanID, nil), none).SyncIsolatedEgress(ctx, org); err != nil {
		t.Fatalf("no idle worker is not an error: %v", err)
	}

	held := &models.DedicatedWorkerAssignment{ID: uuid.New(), WorkerID: uuid.New()}
	for name, s := range map[string]*models.Subscription{
		"lapsed grant":       grantedSub(isolatedPlanID, &past),
		"grant onto shared":  grantedSub(sharedPlanID, nil),
		"no grant, no plan":  {ID: uuid.New(), PlanID: isolatedPlanID, Status: models.SubscriptionStatusIncomplete},
		"canceled on Stripe": {ID: uuid.New(), PlanID: isolatedPlanID, StripeSubscriptionID: new("sub_1"), Status: models.SubscriptionStatusCanceled},
	} {
		w := &egressWorkers{reserved: held}
		if err := egressService(s, w).SyncIsolatedEgress(ctx, org); err != nil {
			t.Fatal(err)
		}
		if len(w.released) != 1 || w.released[0] != held.ID {
			t.Errorf("%s: must release the reservation it read, got %v", name, w.released)
		}
	}

	for name, s := range map[string]*models.Subscription{
		"grant in force":       grantedSub(isolatedPlanID, &future),
		"paying past due":      {ID: uuid.New(), PlanID: isolatedPlanID, StripeSubscriptionID: new("sub_1"), Status: models.SubscriptionStatusPastDue},
		"enterprise, isolated": {ID: uuid.New(), PlanID: isolatedPlanID, IsEnterprise: true},
	} {
		w := &egressWorkers{reserved: held}
		if err := egressService(s, w).SyncIsolatedEgress(ctx, org); err != nil {
			t.Fatal(err)
		}
		if len(w.released) != 0 || len(w.created) != 0 {
			t.Errorf("%s: an entitled reservation must be left alone", name)
		}
	}
}

func TestSyncIsolatedEgressNoBilling(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "none")
	held := &models.DedicatedWorkerAssignment{ID: uuid.New()}
	w := &egressWorkers{reserved: held}
	if err := egressService(grantedSub(sharedPlanID, nil), w).SyncIsolatedEgress(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if len(w.released) != 0 {
		t.Fatal("without billing no plan decides reservations, so none is released")
	}
}

func TestPlacementNeverReleases(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "stripe")
	held := &models.DedicatedWorkerAssignment{ID: uuid.New()}
	w := &egressWorkers{reserved: held}
	if err := egressService(grantedSub(sharedPlanID, nil), w).syncIsolatedEgress(context.Background(), uuid.New(), false); err != nil {
		t.Fatal(err)
	}
	if len(w.released) != 0 {
		t.Fatal("placing a mailbox may reserve but must never release")
	}
}

func TestAgencyGrantReservesOnlyWithinTheFairUseCap(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "stripe")
	ctx, org, agencyID := context.Background(), uuid.New(), uuid.New()
	agencySub := func() *models.Subscription {
		s := grantedSub(isolatedPlanID, nil)
		s.ManagedByAgencyID = &agencyID
		return s
	}
	idle := &models.Worker{ID: uuid.New()}

	within := &egressWorkers{idle: idle, mailboxes: 1, slot: true}
	if err := egressService(agencySub(), within).SyncIsolatedEgress(ctx, org); err != nil {
		t.Fatal(err)
	}
	if len(within.created) != 1 || within.slotLimit != config.AgencyIsolatedEgressMaxClients {
		t.Fatalf("a client inside the cap must reserve, asked with the cap: created %d, limit %d", len(within.created), within.slotLimit)
	}

	beyond := &egressWorkers{idle: idle, mailboxes: 1}
	if err := egressService(agencySub(), beyond).SyncIsolatedEgress(ctx, org); err != nil {
		t.Fatal(err)
	}
	if len(beyond.created) != 0 {
		t.Fatal("a client beyond the cap must not reserve a worker")
	}

	held := &models.DedicatedWorkerAssignment{ID: uuid.New(), WorkerID: uuid.New()}
	fell := &egressWorkers{reserved: held}
	if err := egressService(agencySub(), fell).SyncIsolatedEgress(ctx, org); err != nil {
		t.Fatal(err)
	}
	if len(fell.released) != 1 {
		t.Fatal("a client pushed past the cap must release its reservation")
	}

	operator := &egressWorkers{idle: idle, mailboxes: 1}
	if err := egressService(grantedSub(isolatedPlanID, nil), operator).SyncIsolatedEgress(ctx, org); err != nil {
		t.Fatal(err)
	}
	if len(operator.created) != 1 || operator.slotLimit != 0 {
		t.Fatal("an operator grant is not an agency grant and is not capped")
	}

	sh := &workerAssignmentService{workerRepo: &egressWorkers{}, subRepo: egressSubs{sub: agencySub()}, planRepo: egressPlans{}}
	if !sh.hasIsolatedEgress(ctx, org) {
		t.Fatal("a client beyond the cap keeps the placement preference")
	}
}
