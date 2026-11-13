package campaign

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type countingThrottle struct{ calls int }

func (t *countingThrottle) CheckAndIncrement(context.Context, uuid.UUID, dailythrottle.Resource, int) *errx.Error {
	t.calls++
	return errx.New(errx.TooManyRequests, "daily limit reached")
}

type createOnlyRepo struct {
	repository.CampaignRepository
	created int
}

func (r *createOnlyRepo) Create(_ context.Context, _ string, _ *uuid.UUID, data *models.CreateCampaign) (*models.Campaign, *errx.Error) {
	r.created++
	return &models.Campaign{ID: uuid.New(), Name: data.Name, Status: "draft"}, nil
}

func TestMigrationCreateSkipsOnlyTheDailyThrottle(t *testing.T) {
	throttle, repo := &countingThrottle{}, &createOnlyRepo{}
	svc := &campaignService{campaignRepository: repo, throttle: throttle}
	org := uuid.New()

	if _, xerr := svc.Create(context.Background(), uuid.NewString(), &org, &models.CreateCampaign{Name: "Request path"}); xerr == nil {
		t.Fatal("a request-path create passed a throttle that refuses")
	}
	if throttle.calls != 1 || repo.created != 0 {
		t.Fatalf("throttle calls=%d created=%d; want 1, 0", throttle.calls, repo.created)
	}

	if _, xerr := svc.Create(WithMigrationCreate(context.Background()), uuid.NewString(), &org, &models.CreateCampaign{Name: "Migrated"}); xerr != nil {
		t.Fatalf("migration create: %v", xerr)
	}
	if throttle.calls != 1 || repo.created != 1 {
		t.Fatalf("throttle calls=%d created=%d; want 1, 1", throttle.calls, repo.created)
	}

	// Validation still applies to a migration's campaigns.
	if _, xerr := svc.Create(WithMigrationCreate(context.Background()), uuid.NewString(), &org, &models.CreateCampaign{Name: "x"}); xerr == nil {
		t.Fatal("a migration create skipped name validation")
	}
}
