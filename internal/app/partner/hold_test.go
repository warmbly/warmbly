package partner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type standingRepo struct {
	repository.PartnerRepository
	hasLinks   bool
	linkErr    error
	readErr    error
	orgs       map[uuid.UUID]*models.PartnerOrganization
	linkReads  int
	orgLookups int
}

func (r *standingRepo) HasPartnerOrganizations(context.Context) (bool, error) {
	r.linkReads++
	return r.hasLinks, r.linkErr
}

func (r *standingRepo) GetPartnerOrgByOrganization(_ context.Context, orgID uuid.UUID) (*models.PartnerOrganization, string, error) {
	r.orgLookups++
	if r.readErr != nil {
		return nil, "", r.readErr
	}
	return r.orgs[orgID], "Reseller", nil
}

// An instance without a single partner workspace never looks a workspace's standing up, so its warmup and writes cannot fail on it.
func TestStandingIsNotReadOnAnInstanceWithoutPartners(t *testing.T) {
	repo := &standingRepo{readErr: errors.New("connection refused")}
	s := NewService(Deps{Repo: repo})
	for range 3 {
		if paused, err := s.WarmupPaused(context.Background(), uuid.New()); paused || err != nil {
			t.Fatalf("warmup paused %v err %v, want open", paused, err)
		}
		if ref, xerr := s.ReadOnly(context.Background(), uuid.New()); ref != nil || xerr != nil {
			t.Fatalf("read-only %v err %v, want writable", ref, xerr)
		}
	}
	if repo.orgLookups != 0 || repo.linkReads != 1 {
		t.Fatalf("standing lookups %d, instance checks %d; want none and one cached", repo.orgLookups, repo.linkReads)
	}
}

// A failed read leaves a workspace with no known partner writable and warming, and keeps a known paused one paused.
func TestAFailedStandingReadHoldsOnlyAWorkspaceKnownToBePaused(t *testing.T) {
	paused := uuid.New()
	repo := &standingRepo{hasLinks: true, orgs: map[uuid.UUID]*models.PartnerOrganization{
		paused: {State: models.PartnerOrgSuspended, Mode: models.PartnerModeLive},
	}}
	now := time.Now()
	s := NewService(Deps{Repo: repo})
	s.now = func() time.Time { return now }
	if held, err := s.WarmupPaused(context.Background(), paused); !held || err != nil {
		t.Fatalf("suspended workspace: held %v err %v", held, err)
	}

	repo.readErr = errors.New("connection refused")
	now = now.Add(stateTTL + time.Second)
	if held, err := s.WarmupPaused(context.Background(), paused); !held || err != nil {
		t.Fatalf("suspended workspace after a failed read: held %v err %v, want still held", held, err)
	}
	if ref, xerr := s.ReadOnly(context.Background(), paused); ref == nil || xerr != nil {
		t.Fatalf("suspended workspace after a failed read: read-only %v err %v, want read-only", ref, xerr)
	}
	regular := uuid.New()
	if held, err := s.WarmupPaused(context.Background(), regular); held || err != nil {
		t.Fatalf("regular workspace: held %v err %v, want open", held, err)
	}
	if ref, xerr := s.ReadOnly(context.Background(), regular); ref != nil || xerr != nil {
		t.Fatalf("regular workspace: read-only %v err %v, want writable", ref, xerr)
	}
}
