package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLiveJobRegistrationDoesNotRetireUnregisteredRows(t *testing.T) {
	handle, pool := liveContactDB(t)
	repo := NewJobRunRepository(handle)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	monitor := NewMonitoringRepository(monitoringReadOnlyPool(t, pool))
	before := collectMonitoring(t, monitor, "jobs", now)
	oldName, currentName := uuid.NewString(), uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM scheduled_job_runs WHERE name IN ($1,$2)`, oldName, currentName)
	})
	oldAt := now.Add(-30 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO scheduled_job_runs(name,service,interval_seconds,last_status,last_finished_at,next_run_at,updated_at) VALUES($1,'test',300,'ok',$2,$2,$2)`, oldName, oldAt); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Register(ctx, currentName, "test", time.Minute, now.Add(time.Minute), now); err != nil {
		t.Fatal(err)
	}
	after := collectMonitoring(t, monitor, "jobs", now)
	for _, id := range []string{"job_overdue", "job_ok"} {
		got, baseline := measuredMonitoring(t, after, id), measuredMonitoring(t, before, id)
		if got.Count == nil || baseline.Count == nil || *got.Count != *baseline.Count+1 || got.EvidenceAt == nil || got.EvidenceAt.After(oldAt) {
			t.Fatal("historical wrapper evidence was hidden or refreshed", id, got)
		}
	}
	rows, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Name != oldName {
			continue
		}
		if row.LastStatus != "ok" || row.LastFinishedAt == nil || !row.LastFinishedAt.Equal(oldAt) || row.NextRunAt == nil || !row.NextRunAt.Equal(oldAt) || !row.UpdatedAt.Equal(oldAt) {
			t.Fatal("unregistered job history was changed", row)
		}
		return
	}
	t.Fatal("registration of a new loop hid the old row without retirement authority")
}

// A restart keeps the stored due time inside [earliest, next]; a claim takes
// each slot once; a finished run leaves the schedule to the claim.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveJobRunSchedule -v
func TestLiveJobRunScheduleSurvivesRestarts(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	repo := NewJobRunRepository(handle)
	name := "live_test_" + uuid.NewString()
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM scheduled_job_runs WHERE name = $1`, name) })

	at := func(d time.Duration) time.Time {
		return time.Now().Add(d).Truncate(time.Microsecond)
	}
	register := func(next, earliest time.Time) time.Time {
		t.Helper()
		due, err := repo.Register(ctx, name, "test", time.Hour, next, earliest)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		return due
	}

	first := at(time.Hour)
	if due := register(first, at(0)); !due.Equal(first) {
		t.Fatalf("new job due %s, want %s", due, first)
	}
	if due := register(at(2*time.Hour), at(time.Minute)); !due.Equal(first) {
		t.Fatalf("restart moved a not-yet-due job to %s, want %s", due, first)
	}

	shorter := at(30 * time.Minute)
	if due := register(shorter, at(time.Minute)); !due.Equal(shorter) {
		t.Fatalf("a shorter interval left the job due %s, want %s", due, shorter)
	}

	if _, err := pool.Exec(ctx, `UPDATE scheduled_job_runs SET next_run_at = now() - interval '3 days' WHERE name = $1`, name); err != nil {
		t.Fatal(err)
	}
	earliest := at(20 * time.Second)
	due := register(at(time.Hour), earliest)
	if !due.Equal(earliest) {
		t.Fatalf("overdue job due %s after restart, want %s", due, earliest)
	}

	next := due.Add(time.Hour)
	won, got, err := repo.Claim(ctx, name, due, next)
	if err != nil || !won || !got.Equal(next) {
		t.Fatalf("first Claim = %v %s %v, want the slot", won, got, err)
	}
	won, got, err = repo.Claim(ctx, name, due, next.Add(time.Hour))
	if err != nil || won || !got.Equal(next) {
		t.Fatalf("second Claim of the same slot = %v %s %v, want lost with %s", won, got, err, next)
	}

	if err := repo.MarkStarted(ctx, name, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkFinished(ctx, name, time.Now(), time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var stored time.Time
	var status string
	if err := pool.QueryRow(ctx, `SELECT next_run_at, last_status FROM scheduled_job_runs WHERE name = $1`, name).Scan(&stored, &status); err != nil {
		t.Fatal(err)
	}
	if !stored.Equal(next) || status != "ok" {
		t.Fatalf("after a run the row is due %s with status %q, want %s and ok", stored, status, next)
	}
}
