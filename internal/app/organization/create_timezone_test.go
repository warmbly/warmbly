package organization

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Only what Create touches is implemented; anything else panics.
type createRepo struct {
	repository.OrganizationRepository
	created *models.Organization
	owned   int
}

func (r *createRepo) GetUserOwnedOrganizationCount(context.Context, uuid.UUID) (int, error) {
	return r.owned, nil
}

func (r *createRepo) Create(_ context.Context, org *models.Organization) error {
	r.created = org
	return nil
}

func (r *createRepo) AddMember(context.Context, *models.OrganizationMember) error { return nil }

func (r *createRepo) CreateRole(context.Context, *models.OrganizationRole) error { return nil }

type createUsers struct {
	repository.UserRepository
}

func (createUsers) GetBanState(context.Context, uuid.UUID) (uint32, error) { return 0, nil }

func (createUsers) GetUser(_ context.Context, id uuid.UUID) (*models.User, error) {
	return &models.User{ID: id, MaxOrganizations: 5}, nil
}

// The zone a new workspace is created with is the one it keeps.
func TestCreateStoresTheWorkspaceTimezone(t *testing.T) {
	repo := &createRepo{}
	svc := &organizationService{orgRepo: repo, userRepo: createUsers{}}

	org, xerr := svc.Create(context.Background(), uuid.New(), "Acme", " America/New_York ")
	if xerr != nil {
		t.Fatalf("Create: %v", xerr)
	}
	if repo.created == nil || repo.created.Timezone != "America/New_York" || org.Timezone != "America/New_York" {
		t.Fatalf("stored %+v, returned %q; want America/New_York", repo.created, org.Timezone)
	}

	if _, xerr := svc.Create(context.Background(), uuid.New(), "Acme", "Mars/Olympus"); xerr == nil || xerr != errx.ErrTimezone {
		t.Fatalf("an unknown zone was accepted: %v", xerr)
	}
}

func TestWorkspaceLimitsApplyOnlyOnCloud(t *testing.T) {
	for _, mode := range []string{"self_hosted", "cloud"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("DEPLOYMENT_MODE", mode)
			repo := &createRepo{owned: 100}
			svc := &organizationService{orgRepo: repo, userRepo: createUsers{}}
			_, xerr := svc.Create(context.Background(), uuid.New(), "Acme", "UTC")
			if mode == "self_hosted" && (xerr != nil || repo.created == nil) {
				t.Fatalf("self-hosted cap was enforced: %v", xerr)
			}
			if mode == "cloud" && (xerr == nil || repo.created != nil) {
				t.Fatalf("hosted cap was not enforced: %v", xerr)
			}
		})
	}
}

type denyingWorkspaceThrottle struct{ calls int }

func (d *denyingWorkspaceThrottle) CheckAndIncrement(_ context.Context, _ uuid.UUID, res dailythrottle.Resource, _ int) *errx.Error {
	d.calls++
	if res != dailythrottle.ResourceOrg {
		panic("unexpected throttle resource")
	}
	return errx.New(errx.TooManyRequests, "daily workspace limit")
}

func TestWorkspaceDailyThrottleIsPreservedOnlyOnCloud(t *testing.T) {
	for _, mode := range []string{"self_hosted", "cloud"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("DEPLOYMENT_MODE", mode)
			repo, throttle := &createRepo{}, &denyingWorkspaceThrottle{}
			svc := &organizationService{orgRepo: repo, userRepo: createUsers{}, throttle: throttle}
			_, xerr := svc.Create(context.Background(), uuid.New(), "Acme", "UTC")
			if mode == "self_hosted" && (xerr != nil || throttle.calls != 0 || repo.created == nil) {
				t.Fatalf("self-host throttle ran: %v; calls %d", xerr, throttle.calls)
			}
			if mode == "cloud" && (xerr == nil || throttle.calls != 1 || repo.created != nil) {
				t.Fatal("hosted daily limit was bypassed")
			}
		})
	}
}
