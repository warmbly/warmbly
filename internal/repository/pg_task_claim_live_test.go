package repository

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLiveConcurrentDispatchersNeverClaimTheSameTask(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := t.Context()
	f := newWarmupUsageFixture(t, pool)
	base := time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC)
	ids := make([]uuid.UUID, 40)
	mine := map[uuid.UUID]bool{}
	for i := range ids {
		ids[i] = uuid.New()
		mine[ids[i]] = true
		f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,scheduled_at) VALUES($1,'warmup',$2,'pending','',$3)`, ids[i], f.account, base.Add(time.Duration(i)*time.Second))
	}
	t.Cleanup(func() { _, _ = pool.Exec(t.Context(), `DELETE FROM tasks WHERE id = ANY($1)`, ids) })

	repo := &taskRepository{db: pool}
	var mu sync.Mutex
	owner := map[uuid.UUID]int{}
	var foreign []uuid.UUID
	claims := map[uuid.UUID]time.Time{}
	var wg sync.WaitGroup
	for d := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 40 {
				got, claim, err := repo.ClaimDueTaskIDs(ctx, 5, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				for _, id := range got {
					claims[id] = claim
					if !mine[id] {
						foreign = append(foreign, id)
						continue
					}
					if prev, seen := owner[id]; seen {
						t.Errorf("task %s claimed by dispatcher %d and %d", id, prev, d)
					}
					owner[id] = d
				}
				done := len(owner) == len(ids)
				mu.Unlock()
				if done || len(got) == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()
	// Other tests' due tasks share the table; hand theirs back untouched.
	for _, id := range foreign {
		if err := repo.ReleaseDispatchClaims(ctx, []uuid.UUID{id}, claims[id]); err != nil {
			t.Fatal(err)
		}
	}
	if len(owner) != len(ids) {
		t.Fatalf("two dispatchers claimed %d of %d due tasks", len(owner), len(ids))
	}

	claimable := func() map[uuid.UUID]bool {
		t.Helper()
		got, claim, err := repo.ClaimDueTaskIDs(ctx, 100000, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		out := map[uuid.UUID]bool{}
		var theirs []uuid.UUID
		for _, id := range got {
			if mine[id] {
				out[id] = true
			} else {
				theirs = append(theirs, id)
			}
		}
		if err := repo.ReleaseDispatchClaims(ctx, theirs, claim); err != nil {
			t.Fatal(err)
		}
		for id := range out {
			claims[id] = claim
		}
		return out
	}

	// Every task is leased: a third dispatcher finds none of them.
	if got := claimable(); len(got) != 0 {
		t.Fatalf("leased tasks claimed again: %v", got)
	}

	// A lapsed lease, as left by a replica that died mid-task, is claimed again; a live one is not.
	lapsed := ids[0]
	stale := claims[lapsed]
	f.exec(`UPDATE tasks SET dispatch_claimed_until = NOW() - INTERVAL '1 second' WHERE id = $1`, lapsed)
	if got := claimable(); len(got) != 1 || !got[lapsed] {
		t.Fatalf("claimed %v, want only the task whose lease lapsed", got)
	}

	// The replica whose lease lapsed cannot clear the claim another replica took since.
	if err := repo.ReleaseDispatchClaims(ctx, []uuid.UUID{lapsed}, stale); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordDispatchAttempt(ctx, lapsed, stale, false, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	var held *time.Time
	if err := pool.QueryRow(ctx, `SELECT dispatch_claimed_until FROM tasks WHERE id = $1`, lapsed).Scan(&held); err != nil || held == nil {
		t.Fatalf("a stale claim released the current one: %v, %v", held, err)
	}

	// Recording an attempt releases the claim, and so does handing a claim back.
	if err := repo.RecordDispatchAttempt(ctx, ids[1], claims[ids[1]], false, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	var claimed *time.Time
	if err := pool.QueryRow(ctx, `SELECT dispatch_claimed_until FROM tasks WHERE id = $1`, ids[1]).Scan(&claimed); err != nil || claimed != nil {
		t.Fatalf("recorded attempt kept its claim: %v, %v", claimed, err)
	}
	for _, id := range ids[2:] {
		if err := repo.ReleaseDispatchClaims(ctx, []uuid.UUID{id}, claims[id]); err != nil {
			t.Fatal(err)
		}
	}
	if got := claimable(); len(got) != len(ids)-1 || got[lapsed] {
		t.Fatalf("after release %d of %d tasks claimable, want all but the one still leased", len(got), len(ids)-1)
	}
}
