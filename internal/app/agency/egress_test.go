package agency

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

type fakeEgress struct{ synced []uuid.UUID }

func (f *fakeEgress) SyncIsolatedEgress(_ context.Context, orgID uuid.UUID) error {
	f.synced = append(f.synced, orgID)
	return nil
}

type sweepRepo struct {
	*fakeRepo
	clients []models.AgencyClient
	stale   []uuid.UUID
}

func (r *sweepRepo) Get(context.Context, uuid.UUID) (*models.Agency, error) { return r.agency, nil }
func (r *sweepRepo) ListClientOrganizations(context.Context, uuid.UUID) ([]models.AgencyClient, error) {
	return r.clients, nil
}
func (r *sweepRepo) ListMembers(context.Context, uuid.UUID) ([]models.AgencyMember, error) {
	return nil, nil
}
func (r *sweepRepo) ClearStaleGrants(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return r.stale, nil
}

func TestGrantSyncFollowsWithIsolatedEgress(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	ctx, org := context.Background(), uuid.New()

	repo := &fakeRepo{grants: map[uuid.UUID]uuid.UUID{}}
	egress := &fakeEgress{}
	s := newTestService(repo, intp(500_000), true, now)
	s.WireIsolatedEgress(egress)

	pro := activeAgency(now)
	plan := models.AgencyProPlanID
	pro.PlanID = &plan
	if err := s.syncGrant(ctx, pro, org); err != nil {
		t.Fatal(err)
	}
	if repo.grants[org] != models.AgencyProPlanID || !slices.Equal(egress.synced, []uuid.UUID{org}) {
		t.Fatalf("granting must be followed by an egress sync for that workspace, got %v", egress.synced)
	}

	end := now.Add(-models.AgencyGrantGrace - time.Hour)
	pro.CurrentPeriodEnd = &end
	if err := s.syncGrant(ctx, pro, org); err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.grants[org]; ok || len(egress.synced) != 2 {
		t.Fatal("a lapsed grant must be cleared before the egress sync, so the sync sees no entitlement")
	}

	selfHosted := &fakeEgress{}
	sh := newTestService(&fakeRepo{grants: map[uuid.UUID]uuid.UUID{}}, nil, false, now)
	sh.WireIsolatedEgress(selfHosted)
	if err := sh.syncGrant(ctx, activeAgency(now), org); err != nil {
		t.Fatal(err)
	}
	if len(selfHosted.synced) != 0 {
		t.Fatal("a self-host has no plans, so grants never move reservations")
	}
}

func TestSyncAgencyReleasesDetachedWorkspaces(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	client, gone := uuid.New(), uuid.New()
	repo := &sweepRepo{
		fakeRepo: &fakeRepo{agency: activeAgency(now), grants: map[uuid.UUID]uuid.UUID{}},
		clients:  []models.AgencyClient{{OrganizationID: client}},
		stale:    []uuid.UUID{gone},
	}
	egress := &fakeEgress{}
	s := newTestService(repo.fakeRepo, intp(150_000), true, now)
	s.repo = repo
	s.WireIsolatedEgress(egress)
	if err := s.SyncAgency(context.Background(), repo.agency.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(egress.synced, []uuid.UUID{client, gone}) {
		t.Fatalf("every client and every workspace that lost its grant must be synced, got %v", egress.synced)
	}
}
