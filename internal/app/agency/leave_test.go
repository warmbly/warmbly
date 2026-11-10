package agency

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type leaveRepo struct {
	*fakeRepo
	removed   []uuid.UUID
	members   []uuid.UUID
	unmirrors []uuid.UUID
}

func (r *leaveRepo) RemoveClient(_ context.Context, _, orgID uuid.UUID) (bool, error) {
	if r.client == nil || r.client.OrganizationID != orgID {
		return false, nil
	}
	r.removed = append(r.removed, orgID)
	r.client = nil
	return true, nil
}

func (r *leaveRepo) AgencyMembersOf(context.Context, uuid.UUID, uuid.UUID) ([]uuid.UUID, error) {
	return r.members, nil
}

func (r *leaveRepo) UnmirrorStaff(_ context.Context, _, _, userID uuid.UUID) (bool, error) {
	r.unmirrors = append(r.unmirrors, userID)
	return true, nil
}

type ownedOrgs struct {
	fakeOrgs
	owner uuid.UUID
}

func (o *ownedOrgs) Get(_ context.Context, orgID uuid.UUID) (*models.Organization, *errx.Error) {
	return &models.Organization{ID: orgID, OwnerUserID: o.owner}, nil
}

func leaveFixture(t *testing.T, agencyOwner, orgOwner uuid.UUID) (*Service, *leaveRepo, *fakeEgress, uuid.UUID) {
	t.Helper()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	org := uuid.New()
	a := activeAgency(now)
	a.OwnerUserID = agencyOwner
	repo := &leaveRepo{
		fakeRepo: &fakeRepo{agency: a, client: &models.AgencyClient{OrganizationID: org}, grants: map[uuid.UUID]uuid.UUID{org: models.AgencyPlanID}},
		members:  []uuid.UUID{uuid.New(), uuid.New()},
	}
	egress := &fakeEgress{}
	s := newTestService(repo.fakeRepo, intp(1_000), true, now)
	s.repo, s.orgs = repo, &ownedOrgs{owner: orgOwner}
	s.WireIsolatedEgress(egress)
	return s, repo, egress, org
}

func TestLeaveAgencyReleasesTheWorkspace(t *testing.T) {
	owner := uuid.New()
	s, repo, egress, org := leaveFixture(t, uuid.New(), owner)
	ref, xerr := s.LeaveAgency(context.Background(), org, owner)
	if xerr != nil || ref == nil || ref.ID != repo.agency.ID {
		t.Fatalf("the workspace owner must be able to leave: %v", xerr)
	}
	if !slices.Equal(repo.removed, []uuid.UUID{org}) {
		t.Fatal("the client link must be removed")
	}
	if _, ok := repo.grants[org]; ok {
		t.Fatal("the agency's grant must be cleared")
	}
	if !slices.Equal(egress.synced, []uuid.UUID{org}) {
		t.Fatal("the reserved worker must follow the grant")
	}
	if !slices.Equal(repo.unmirrors, repo.members) {
		t.Fatal("every membership the agency added must be taken back")
	}

	// Leaving again is a no-op, not an error.
	if ref, xerr := s.LeaveAgency(context.Background(), org, owner); xerr != nil || ref != nil || len(repo.removed) != 1 {
		t.Fatalf("a second leave must change nothing: ref %v, err %v", ref, xerr)
	}
}

func TestLeaveAgencyRefusals(t *testing.T) {
	owner := uuid.New()
	s, repo, _, org := leaveFixture(t, uuid.New(), owner)
	if _, xerr := s.LeaveAgency(context.Background(), org, uuid.New()); xerr == nil || xerr.Identifier != "agency_leave_not_owner" {
		t.Fatalf("only the workspace owner may leave, got %v", xerr)
	}

	both := uuid.New()
	s2, repo2, _, org2 := leaveFixture(t, both, both)
	if _, xerr := s2.LeaveAgency(context.Background(), org2, both); xerr == nil || xerr.Identifier != "agency_leave_agency_owner" {
		t.Fatalf("the agency owner detaches from the agency side, got %v", xerr)
	}
	if len(repo.removed)+len(repo2.removed) != 0 {
		t.Fatal("a refused leave must not detach")
	}
}

func TestAgencyOfReportsWhetherTheWorkspaceCanLeave(t *testing.T) {
	agencyOwner := uuid.New()
	s, repo, _, org := leaveFixture(t, agencyOwner, uuid.New())
	repo.client.OwnerUserID = uuid.New()
	if ref := s.AgencyOf(context.Background(), org); ref == nil || !ref.CanLeave {
		t.Fatal("a workspace someone else owns can leave")
	}
	repo.client.OwnerUserID = agencyOwner
	if ref := s.AgencyOf(context.Background(), org); ref == nil || ref.CanLeave {
		t.Fatal("a workspace the agency owner owns cannot leave from its side")
	}
}
