package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

func TestLiveCloudWorkspaceApprovalsAreSerialized(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	repo := NewPoolLinkRepository(f.pool)
	const count = 12
	var wg sync.WaitGroup
	errs := make(chan error, count)
	ids := make(chan uuid.UUID, count)
	for i := range count {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			org := uuid.New()
			inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: f.org, RemoteOrganizationID: &org, CreatedBy: &f.user, Name: "Workspace"}
			err := repo.CreateInstance(ctx, inst, fmt.Sprintf("fixture-%s-%d", f.org, i))
			if err == nil {
				ids <- inst.ID
			}
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	close(ids)
	wins, refused := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrCloudWorkspaceLinked):
			refused++
		default:
			t.Fatalf("approval failed unexpectedly: %v", err)
		}
	}
	if wins != 1 || refused != count-1 {
		t.Fatalf("approved %d, rejected %d", wins, refused)
	}
	for id := range ids {
		if err := repo.RevokeInstance(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	legacy := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: f.org, CreatedBy: &f.user, Name: "Legacy"}
	if err := repo.CreateInstance(ctx, legacy, "legacy-fixture-"+f.org.String()); err != nil {
		t.Fatal(err)
	}
	org := uuid.New()
	scoped := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: f.org, RemoteOrganizationID: &org, CreatedBy: &f.user, Name: "Scoped"}
	if err := repo.CreateInstance(ctx, scoped, "scoped-fixture-"+f.org.String()); !errors.Is(err, ErrCloudWorkspaceLinked) {
		t.Fatalf("legacy link didn't reserve its workspace: %v", err)
	}
}

func TestLiveWorkspaceLinksPreserveLegacyEnrollmentOwnership(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	enc, err := encrypt.NewEncrypter(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCloudLinkRepository(f.pool, enc)
	legacy := &models.CloudLink{InstanceID: uuid.New(), CloudURL: "https://cloud.test", Token: "legacy-fixture"}
	scoped := &models.CloudLink{InstanceID: uuid.New(), OrganizationID: &f.org, CloudURL: "https://cloud.test", Token: "scoped-fixture"}
	t.Cleanup(func() {
		for _, l := range []*models.CloudLink{legacy, scoped} {
			_ = repo.Delete(ctx, l.InstanceID)
		}
	})
	for _, l := range []*models.CloudLink{legacy, scoped} {
		if err := repo.Put(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.Enroll(ctx, f.sender, uuid.New(), legacy.InstanceID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Enroll(ctx, f.recipient, uuid.New(), scoped.InstanceID, false); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, &f.org)
	if err != nil || got == nil || got.InstanceID != scoped.InstanceID || got.Token != "scoped-fixture" {
		t.Fatalf("workspace resolution = %v, %v", got, err)
	}
	other := uuid.New()
	got, err = repo.Get(ctx, &other)
	if err != nil || got == nil || got.InstanceID != legacy.InstanceID {
		t.Fatalf("legacy fallback = %v, %v", got, err)
	}
	if got, err := repo.GetForRedirect(ctx, other, "unused.test"); err != nil || got != nil {
		t.Fatalf("new redirect used legacy link = %v, %v", got, err)
	}
	if err := NewOrganizationRepository(f.pool).Delete(ctx, f.org); !errors.Is(err, ErrOrganizationCloudLinked) {
		t.Fatalf("linked workspace deletion = %v", err)
	}
	d := &models.ScheduledDeletion{ResourceType: models.DeletionResourceOrganization, ResourceID: f.org}
	if err := NewDangerZoneRepository(f.pool).CreatePending(ctx, d); !errors.Is(err, ErrOrganizationCloudLinked) {
		t.Fatalf("linked workspace deletion schedule = %v", err)
	}
	d.ResourceType, d.ResourceID = models.DeletionResourceUser, f.user
	if err := NewDangerZoneRepository(f.pool).CreatePending(ctx, d); !errors.Is(err, ErrOrganizationCloudLinked) {
		t.Fatalf("linked owner deletion schedule = %v", err)
	}
	if err := repo.Delete(ctx, scoped.InstanceID); err != nil {
		t.Fatal(err)
	}
	if m, err := repo.GetByAccount(ctx, f.sender); err != nil || m == nil || m.InstanceID != legacy.InstanceID {
		t.Fatalf("legacy enrollment removed = %v, %v", m, err)
	}
	if m, err := repo.GetByAccount(ctx, f.recipient); err != nil || m != nil {
		t.Fatalf("scoped enrollment not removed = %v, %v", m, err)
	}
}
