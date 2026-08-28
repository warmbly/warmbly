package repository

import (
	"context"
	"sync"
	"testing"
	"time"
)

// CreateLogOnce backs the per-send content warning, which runs on EVERY
// recipient of a low-scoring step at once. A check-then-insert would let
// concurrent sends both find nothing and both write.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCampaignLogOnce -v
func TestLiveCampaignLogOnceIsAtomicUnderConcurrency(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewCampaignLogRepository(handle)
	ctx := context.Background()

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM campaign_logs WHERE campaign_id = $1`, f.campaign); err != nil {
			t.Errorf("cleanup logs: %v", err)
		}
	})

	const racers = 12
	var wg sync.WaitGroup
	wrote := make([]bool, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ok, err := repo.CreateLogOnce(ctx, &CampaignLogEntry{
				CampaignID: f.campaign,
				EventType:  "content_warning",
				Message:    "step scores badly",
				Metadata:   map[string]interface{}{"sequence_id": "step-1"},
			}, "sequence_id", "step-1", time.Now().Add(-time.Hour))
			if err != nil {
				t.Errorf("racer %d: %v", i, err)
				return
			}
			wrote[i] = ok
		}(i)
	}
	close(start)
	wg.Wait()

	writes := 0
	for _, w := range wrote {
		if w {
			writes++
		}
	}
	if writes != 1 {
		t.Errorf("%d of %d racers reported writing, want exactly 1", writes, racers)
	}

	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM campaign_logs WHERE campaign_id = $1 AND event_type = 'content_warning'`,
		f.campaign).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Errorf("%d feed entries written, want 1", rows)
	}
}

// A different step is a different warning, and an old one no longer suppresses.
func TestLiveCampaignLogOnceScopesByKeyAndWindow(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewCampaignLogRepository(handle)
	ctx := context.Background()

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM campaign_logs WHERE campaign_id = $1`, f.campaign); err != nil {
			t.Errorf("cleanup logs: %v", err)
		}
	})

	entry := func(step string) *CampaignLogEntry {
		return &CampaignLogEntry{
			CampaignID: f.campaign,
			EventType:  "content_warning",
			Message:    "step scores badly",
			Metadata:   map[string]interface{}{"sequence_id": step},
		}
	}
	since := time.Now().Add(-time.Hour)

	if ok, err := repo.CreateLogOnce(ctx, entry("step-1"), "sequence_id", "step-1", since); err != nil || !ok {
		t.Fatalf("first write: ok=%v err=%v", ok, err)
	}
	if ok, err := repo.CreateLogOnce(ctx, entry("step-1"), "sequence_id", "step-1", since); err != nil || ok {
		t.Errorf("second write for the same step: ok=%v err=%v, want suppressed", ok, err)
	}
	if ok, err := repo.CreateLogOnce(ctx, entry("step-2"), "sequence_id", "step-2", since); err != nil || !ok {
		t.Errorf("a different step: ok=%v err=%v, want written", ok, err)
	}
	// A window that starts after the existing row must not see it, so the
	// warning returns once the day rolls over.
	if ok, err := repo.CreateLogOnce(ctx, entry("step-1"), "sequence_id", "step-1", time.Now().Add(time.Hour)); err != nil || !ok {
		t.Errorf("after the window: ok=%v err=%v, want written again", ok, err)
	}
}
