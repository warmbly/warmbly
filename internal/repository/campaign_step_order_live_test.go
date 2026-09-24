package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Issue #669: Step performance listed a wizard campaign as Step 3, Step 2,
// Step 1. The wizard writes every step in one transaction, so created_at
// carries no order; the rows follow the canvas and number the email steps
// the way the canvas's "Email N" does.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCampaignStepStatsFollowTheCanvas -v

func TestLiveCampaignStepStatsFollowTheCanvas(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()

	// created_at runs against position, an action step sits between two
	// emails, position 4 was deleted, and the last two share a created_at.
	step1, action, step2, step3, unnamed := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, s := range []struct {
		id       uuid.UUID
		name     string
		kind     string
		position int
		ageSecs  int
	}{
		{step1, "Step 1", "email", 1, 0},
		{action, "Tag lead", "action", 2, 3},
		{step2, "Step 2", "email", 3, 1},
		{step3, "Step 3", "email", 5, 2},
		{unnamed, "", "email", 6, 2},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
		          body_plain, body_html, wait_after, position, kind, action, created_at)
		      VALUES ($1, $2, $3, $4, 'Hi', 'Hello', '<p>Hello</p>', 0, $5, $6, '{"type":"add_tag"}'::jsonb,
		              date_trunc('second', NOW()) - make_interval(secs => $7))`,
			s.id, f.campaign, f.org, s.name, s.position, s.kind, float64(s.ageSecs)); err != nil {
			t.Fatalf("sequence %q: %v", s.name, err)
		}
	}

	repo := &analyticsRepository{DB: handle}
	stats, xerr := repo.GetSequenceStats(ctx, f.campaign)
	if xerr != nil {
		t.Fatalf("GetSequenceStats: %v", xerr)
	}

	want := []struct {
		id       uuid.UUID
		name     string
		position int
	}{
		{step1, "Step 1", 1},
		{step2, "Step 2", 2},
		{step3, "Step 3", 3},
		{unnamed, "", 4},
	}
	if len(stats) != len(want) {
		t.Fatalf("got %d steps, want %d email steps (the action step is not one)", len(stats), len(want))
	}
	for i, w := range want {
		if got := stats[i]; got.SequenceID != w.id || got.Position != w.position {
			t.Errorf("row %d = %q position %d, want %q position %d", i, got.Name, got.Position, w.name, w.position)
		}
	}
}
