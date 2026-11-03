package repository

import (
	"context"
	"sync"
	"testing"
)

// MarkCompleted is what lets the campaign task and the reconciler both find a
// campaign finished and announce campaign.completed once between them.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCampaignMarkCompleted -v
func TestLiveCampaignMarkCompletedClosesOnce(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewCampaignRepostory(handle)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `UPDATE campaigns SET status = 'active' WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("activate: %v", err)
	}

	const racers = 8
	var wg sync.WaitGroup
	closed := make([]bool, racers)
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ok, err := repo.MarkCompleted(ctx, f.campaign)
			if err != nil {
				t.Errorf("racer %d: %v", i, err)
				return
			}
			closed[i] = ok
		}(i)
	}
	close(start)
	wg.Wait()

	n := 0
	for _, c := range closed {
		if c {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d of %d racers reported closing the campaign, want 1", n, racers)
	}
	if ok, err := repo.MarkCompleted(ctx, f.campaign); err != nil || ok {
		t.Errorf("a later completer reported closing it again: ok=%v err=%v", ok, err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM campaigns WHERE id = $1`, f.campaign).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Errorf("status = %q, want completed", status)
	}
}
