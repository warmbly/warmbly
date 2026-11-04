package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// Older than anything else in the database, so each claim below takes these first.
var queueFixtureEpoch = time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC)

func TestLiveSendResultEffectsBatchClaimsDeliverEachEffectOnce(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := t.Context()
	f := newWarmupUsageFixture(t, pool)
	task := uuid.New()
	f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,scheduled_at) VALUES($1,'campaign',$2,'completed','',NOW())`, task, f.account)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tasks WHERE id=$1`, task) })
	insert := func(n, from int) {
		for i := range n {
			f.exec(`INSERT INTO send_result_effects(task_id,effect_key,organization_id,kind,payload,created_at) VALUES($1,$2,$3,'notification','{}',$4)`,
				task, fmt.Sprintf("k%d", from+i), f.org, queueFixtureEpoch.Add(time.Duration(from+i)*time.Second))
		}
	}
	mine := func() map[uuid.UUID]bool {
		rows, err := pool.Query(ctx, `SELECT id FROM send_result_effects WHERE task_id=$1`, task)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[uuid.UUID]bool{}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			out[id] = true
		}
		return out
	}

	// More than one batch, drained by two replicas at once.
	insert(2*sendResultEffectBatch+5, 0)
	ours := mine()
	repo := &advancedOutreachRepository{db: pool}
	var mu sync.Mutex
	delivered := map[uuid.UUID]int{}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := repo.DeliverSendResultEffects(ctx, func(_ context.Context, e SendResultEffect) error {
				if ours[e.ID] {
					mu.Lock()
					delivered[e.ID]++
					mu.Unlock()
				}
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(delivered) != len(ours) {
		t.Fatalf("delivered %d of %d effects", len(delivered), len(ours))
	}
	for id, n := range delivered {
		if n != 1 {
			t.Fatalf("effect %s delivered %d times", id, n)
		}
	}
	var undelivered int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM send_result_effects WHERE task_id=$1 AND delivered_at IS NULL`, task).Scan(&undelivered); err != nil || undelivered != 0 {
		t.Fatalf("%d effects left undelivered: %v", undelivered, err)
	}

	// A failure stops the pass and hands back what it never attempted, uncounted.
	f.exec(`DELETE FROM send_result_effects WHERE task_id=$1`, task)
	insert(4, 100)
	calls := 0
	err := repo.DeliverSendResultEffects(ctx, func(_ context.Context, e SendResultEffect) error {
		calls++
		if calls == 2 {
			return errors.New("notifier unavailable")
		}
		return nil
	})
	if err == nil {
		t.Fatal("a failed delivery was not reported")
	}
	rows, err := pool.Query(ctx, `SELECT delivered_at IS NOT NULL, attempts, locked_until IS NOT NULL FROM send_result_effects WHERE task_id=$1 ORDER BY created_at`, task)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type state struct {
		delivered bool
		attempts  int
		locked    bool
	}
	var got []state
	for rows.Next() {
		var s state
		if err := rows.Scan(&s.delivered, &s.attempts, &s.locked); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	want := []state{{true, 1, false}, {false, 1, true}, {false, 0, false}, {false, 0, false}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after a failed pass: %+v, want %+v", got, want)
	}
}

func TestLiveCRMClaimTakesDueAndLapsedJobsOnce(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := t.Context()
	f := newWarmupUsageFixture(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM crm_sync_jobs WHERE organization_id=$1`, f.org)
	})
	add := func(status string, at time.Time, lockedUntil *time.Time) uuid.UUID {
		id := uuid.New()
		f.exec(`INSERT INTO crm_sync_jobs(id,organization_id,provider,kind,status,next_attempt_at,locked_until) VALUES($1,$2,'pipedrive','log_event',$3,$4,$5)`,
			id, f.org, status, at, lockedUntil)
		return id
	}
	lapsed, live := queueFixtureEpoch, time.Now().Add(time.Hour)
	want := map[uuid.UUID]bool{}
	for i := range 6 {
		want[add("pending", queueFixtureEpoch.Add(time.Duration(i)*time.Second), nil)] = true
	}
	want[add("running", queueFixtureEpoch, &lapsed)] = true
	held := add("running", queueFixtureEpoch, &live)
	future := add("pending", time.Now().Add(time.Hour), nil)

	repo := &crmProviderRepository{db: pool}
	var mu sync.Mutex
	owner := map[uuid.UUID]int{}
	var wg sync.WaitGroup
	for d := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 4 {
				jobs, err := repo.ClaimJobs(ctx, models.CRMProvider("pipedrive"), 1, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				for _, j := range jobs {
					if prev, seen := owner[j.ID]; seen {
						t.Errorf("job %s claimed by drainer %d and %d", j.ID, prev, d)
					}
					owner[j.ID] = d
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for id := range want {
		if _, ok := owner[id]; !ok {
			t.Fatalf("due or lapsed job %s never claimed", id)
		}
	}
	if _, ok := owner[held]; ok {
		t.Fatal("a job under a live lease was claimed")
	}
	if _, ok := owner[future]; ok {
		t.Fatal("a job not yet due was claimed")
	}
}
