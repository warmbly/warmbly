package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Run against the dev stack:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCampaignWarmupGraduation -v
func TestLiveCampaignWarmupGraduation(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 370)
	f := newSharedOrgFixture(t, pool)
	repo := NewCampaignRepostory(handle)
	ctx := context.Background()

	plain, xerr := repo.Create(ctx, f.owner.String(), &f.org, &models.CreateCampaign{Name: "Graduation default"})
	if xerr != nil {
		t.Fatalf("create: %v", xerr)
	}
	if !plain.WarmupGraduation {
		t.Fatal("a new campaign must default to the reply-earned ceiling")
	}

	off := false
	start, inc, ceiling, on := 10, 5, 30, true
	scheduled, xerr := repo.Create(ctx, f.owner.String(), &f.org, &models.CreateCampaign{
		Name: "Graduation off", WarmupGraduation: &off, RampEnabled: &on, RampStart: &start, RampIncrement: &inc, RampCeiling: &ceiling,
	})
	if xerr != nil {
		t.Fatalf("create off: %v", xerr)
	}
	if scheduled.WarmupGraduation || !scheduled.RampEnabled {
		t.Fatalf("create returned graduation=%v ramp=%v, want false and true", scheduled.WarmupGraduation, scheduled.RampEnabled)
	}
	byID, err := repo.GetByID(ctx, scheduled.ID)
	if err != nil || byID.WarmupGraduation {
		t.Fatalf("GetByID: %v, graduation=%v", err, byID != nil && byID.WarmupGraduation)
	}
	got, err := repo.Get(ctx, f.org.String(), scheduled.ID.String())
	if err != nil || got.WarmupGraduation || got.RampCeiling != 30 {
		t.Fatalf("Get: %v, %+v", err, got)
	}

	// The first sending day sends ramp_start, then it adds the increment.
	if err := repo.AdvanceRampLevel(ctx, scheduled.ID); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if c, _ := repo.GetByID(ctx, scheduled.ID); c.RampLevel != 10 {
		t.Fatalf("day 1 level = %d, want 10", c.RampLevel)
	}
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET ramp_level_date = CURRENT_DATE - 1 WHERE id = $1`, scheduled.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AdvanceRampLevel(ctx, scheduled.ID); err != nil {
		t.Fatal(err)
	}
	if c, _ := repo.GetByID(ctx, scheduled.ID); c.RampLevel != 15 {
		t.Fatalf("day 2 level = %d, want 15", c.RampLevel)
	}

	dup, err := repo.Duplicate(ctx, DuplicateCampaignInput{SourceID: scheduled.ID, NewID: uuid.New(), UserID: f.owner, Name: "Graduation off (copy)"})
	if err != nil || dup.WarmupGraduation {
		t.Fatalf("duplicate: %v, graduation=%v", err, dup != nil && dup.WarmupGraduation)
	}

	updated, xerr := repo.Update(ctx, f.org.String(), plain.ID.String(), &models.UpdateCampaign{WarmupGraduation: &off})
	if xerr != nil || updated.WarmupGraduation {
		t.Fatalf("update: %v, %+v", xerr, updated)
	}
}
