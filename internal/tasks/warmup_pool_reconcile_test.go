package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/feature"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Stubs embed their interface, so any call this pass is not supposed to make
// panics instead of quietly passing.

type poolReconcileWarmupRepo struct {
	repository.WarmupRepository
	members []repository.WarmupParticipantMembership
	listErr error
}

func (r *poolReconcileWarmupRepo) ListParticipantMemberships(_ context.Context) ([]repository.WarmupParticipantMembership, error) {
	return r.members, r.listErr
}

type poolReconcileRisk struct {
	repository.OrgRiskRepository
	states map[uuid.UUID]models.OrgRiskState
	calls  int
}

func (r *poolReconcileRisk) GetOrgRiskStates(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]models.OrgRiskState, error) {
	r.calls++
	out := map[uuid.UUID]models.OrgRiskState{}
	for _, id := range ids {
		if st, ok := r.states[id]; ok {
			out[id] = st
		}
	}
	return out, nil
}

type poolReconcileGate struct {
	feature.FeatureGateService
	entitled map[uuid.UUID]bool
	fail     bool
	calls    int
}

func (g *poolReconcileGate) CanUseWarmup(_ context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	g.calls++
	if g.fail {
		return false, errx.InternalError()
	}
	return g.entitled[orgID], nil
}

type poolReconcileWarmup struct {
	warmupapp.Service
	moved   map[uuid.UUID]string
	removed map[uuid.UUID]bool
	// inPool is where each mailbox currently sits, so MovePoolMembership can
	// report honestly whether it had anything to move.
	inPool map[uuid.UUID]string
}

func (w *poolReconcileWarmup) MovePoolMembership(_ context.Context, accountID uuid.UUID, poolType string) (bool, *errx.Error) {
	if w.inPool[accountID] == poolType {
		return false, nil
	}
	w.inPool[accountID] = poolType
	w.moved[accountID] = poolType
	return true, nil
}

func (w *poolReconcileWarmup) RemoveFromAllPools(_ context.Context, accountID uuid.UUID) *errx.Error {
	w.removed[accountID] = true
	delete(w.inPool, accountID)
	return nil
}

type poolReconcileFixture struct {
	svc      *tasksService
	gate     *poolReconcileGate
	warmup   *poolReconcileWarmup
	risk     *poolReconcileRisk
	accounts map[uuid.UUID]*repository.WarmupParticipantMembership
	orgPaid  uuid.UUID
	orgFree  uuid.UUID
}

func newPoolReconcileFixture() *poolReconcileFixture {
	f := &poolReconcileFixture{
		orgPaid:  uuid.New(),
		orgFree:  uuid.New(),
		accounts: map[uuid.UUID]*repository.WarmupParticipantMembership{},
		risk:     &poolReconcileRisk{states: map[uuid.UUID]models.OrgRiskState{}},
	}
	f.gate = &poolReconcileGate{entitled: map[uuid.UUID]bool{}}
	f.warmup = &poolReconcileWarmup{
		moved:   map[uuid.UUID]string{},
		removed: map[uuid.UUID]bool{},
		inPool:  map[uuid.UUID]string{},
	}
	f.gate.entitled[f.orgPaid] = true
	f.gate.entitled[f.orgFree] = false
	return f
}

// mailbox registers an account and the pool its participant row is currently in.
func (f *poolReconcileFixture) mailbox(orgID uuid.UUID, poolType, sittingIn string) uuid.UUID {
	id := uuid.New()
	org := orgID
	f.accounts[id] = &repository.WarmupParticipantMembership{
		EmailAccountID: id,
		OrganizationID: &org,
		Status:         "active",
		WarmupPoolType: poolType,
	}
	f.warmup.inPool[id] = sittingIn
	return id
}

func (f *poolReconcileFixture) run(t *testing.T) (int, int) {
	t.Helper()
	members := make([]repository.WarmupParticipantMembership, 0, len(f.warmup.inPool))
	for id, pool := range f.warmup.inPool {
		m := *f.accounts[id]
		m.PoolTypes = []string{pool}
		members = append(members, m)
	}
	f.svc = &tasksService{
		warmupRepo:   &poolReconcileWarmupRepo{members: members},
		featureGate:  f.gate,
		warmupHealth: f.warmup,
		orgRiskRepo:  f.risk,
	}
	moved, removed, err := f.svc.ReconcileWarmupPoolMembership(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return moved, removed
}

// The headline defect: a downgraded mailbox keeps its premium row, so paying
// customers go on being handed a mailbox with no paid entitlement as a warmup
// partner. Nothing revisits it, because a mailbox that stopped warming is not a
// schedule candidate.
func TestPoolReconcileEvictsAMailboxThatLostItsEntitlement(t *testing.T) {
	f := newPoolReconcileFixture()
	downgraded := f.mailbox(f.orgFree, "free", "premium")

	_, removed := f.run(t)

	if removed != 1 {
		t.Fatalf("removed %d mailboxes, want 1", removed)
	}
	if !f.warmup.removed[downgraded] {
		t.Fatal("the downgraded mailbox is still in a warmup pool")
	}
	if _, still := f.warmup.inPool[downgraded]; still {
		t.Fatal("membership survived the eviction")
	}
}

// An entitled mailbox whose tier moved is corrected rather than evicted.
func TestPoolReconcileMovesAnEntitledMailboxToItsOwnPool(t *testing.T) {
	f := newPoolReconcileFixture()
	upgraded := f.mailbox(f.orgPaid, "premium", "free")

	moved, removed := f.run(t)

	if moved != 1 || removed != 0 {
		t.Fatalf("moved %d removed %d, want 1 and 0", moved, removed)
	}
	if f.warmup.moved[upgraded] != "premium" {
		t.Fatalf("moved to %q, want premium", f.warmup.moved[upgraded])
	}
}

// Steady state costs nothing: a mailbox already in the right pool is left alone.
func TestPoolReconcileLeavesASettledMailboxAlone(t *testing.T) {
	f := newPoolReconcileFixture()
	f.mailbox(f.orgPaid, "premium", "premium")

	moved, removed := f.run(t)

	if moved != 0 || removed != 0 {
		t.Fatalf("moved %d removed %d, want 0 and 0", moved, removed)
	}
}

// An unreadable subscription is not evidence that a mailbox lost warmup. If a
// blip could evict, one bad minute would empty the pool.
func TestPoolReconcileLeavesMembershipAloneWhenEntitlementCannotBeRead(t *testing.T) {
	f := newPoolReconcileFixture()
	mailbox := f.mailbox(f.orgPaid, "premium", "free")
	f.gate.fail = true

	moved, removed := f.run(t)

	if moved != 0 || removed != 0 {
		t.Fatalf("moved %d removed %d, want the mailbox untouched", moved, removed)
	}
	if f.warmup.removed[mailbox] {
		t.Fatal("evicted a mailbox on an entitlement lookup failure")
	}
}

// A participant whose mailbox is suspended or has no workspace has nothing left
// to check an entitlement against, so it leaves the shared pool. A deleted
// mailbox takes its participant rows with it (ON DELETE CASCADE).
func TestPoolReconcileEvictsMailboxesItCannotVouchFor(t *testing.T) {
	for _, c := range []struct {
		name   string
		adjust func(m *repository.WarmupParticipantMembership)
	}{
		{"inactive", func(m *repository.WarmupParticipantMembership) { m.Status = "inactive" }},
		{"no workspace", func(m *repository.WarmupParticipantMembership) { m.OrganizationID = nil }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newPoolReconcileFixture()
			id := f.mailbox(f.orgPaid, "premium", "premium")
			c.adjust(f.accounts[id])

			_, removed := f.run(t)

			if removed != 1 || !f.warmup.removed[id] {
				t.Fatalf("removed %d, want the mailbox out of the pool", removed)
			}
		})
	}
}

// One subscription lookup per workspace, not per mailbox.
func TestPoolReconcileAsksAboutEachOrganizationOnce(t *testing.T) {
	f := newPoolReconcileFixture()
	for i := 0; i < 5; i++ {
		f.mailbox(f.orgPaid, "premium", "premium")
	}

	f.run(t)

	if f.gate.calls != 1 {
		t.Fatalf("asked about the same organization %d times, want 1", f.gate.calls)
	}
}

// A listing failure is reported, not swallowed into a silent no-op pass.
func TestPoolReconcileReportsAListingFailure(t *testing.T) {
	svc := &tasksService{
		warmupRepo:   &poolReconcileWarmupRepo{listErr: errors.New("connection reset by peer")},
		warmupHealth: &poolReconcileWarmup{},
	}

	if _, _, err := svc.ReconcileWarmupPoolMembership(context.Background()); err == nil {
		t.Fatal("expected the listing failure to surface")
	}
}
