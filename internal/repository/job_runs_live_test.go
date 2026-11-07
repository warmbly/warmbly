package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A restart keeps the stored due time inside [earliest, next]; a claim takes
// each slot once; a finished run leaves the schedule to the claim.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveJobRunSchedule -v
func TestLiveJobRunScheduleSurvivesRestarts(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 286)
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

func TestLiveJobRunRetirementPreservesHistoryAndReactivatesOnlyOnRegistration(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 286)
	ctx := context.Background()
	repo := NewJobRunRepository(handle)
	name := "retired_test_" + uuid.NewString()
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM scheduled_job_runs WHERE name = $1`, name) })
	due := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	next := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	if _, err := repo.Register(ctx, name, "test", time.Hour, due, due); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE scheduled_job_runs SET retired_at=now(), last_status='error', last_error='historical failure', run_count=1850, error_count=7, run_requested_at=$2 WHERE name=$1`, name, due); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(j)::text FROM scheduled_job_runs j WHERE name=$1`, name).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if found, err := repo.RequestRun(ctx, name); err != nil || found {
		t.Fatalf("retired RequestRun found=%v err=%v", found, err)
	}
	if taken, err := repo.TakeRunRequests(ctx, []string{name}); err != nil || len(taken) != 0 {
		t.Fatalf("retired TakeRunRequests taken=%v err=%v", taken, err)
	}
	if won, got, err := repo.Claim(ctx, name, due, next); err != nil || won || !got.Equal(next) {
		t.Fatalf("retired Claim won=%v due=%s err=%v", won, got, err)
	}
	if err := repo.MarkStarted(ctx, name, time.Now()); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(j)::text FROM scheduled_job_runs j WHERE name=$1`, name).Scan(&after); err != nil || after != before {
		t.Fatalf("retired run request/claim changed historical record: err=%v", err)
	}
	jobs, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, job := range jobs {
		if job.Name == name {
			found = true
			if job.RetiredAt == nil || job.LastStatus != "error" || job.LastError != "historical failure" || job.RunCount != 1850 || job.ErrorCount != 7 || job.NextRunAt == nil || !job.NextRunAt.Equal(due) {
				t.Fatalf("retired history not exposed accurately: %+v", job)
			}
		}
	}
	if !found {
		t.Fatal("retired history omitted from job list")
	}
	earliest := time.Now().Add(time.Minute).Truncate(time.Microsecond)
	got, err := repo.Register(ctx, name, "test", time.Hour, next, earliest)
	if err != nil || !got.Equal(earliest) {
		t.Fatalf("reactivated Register due=%s err=%v", got, err)
	}
	var retiredAt, requestedAt *time.Time
	var status, lastError string
	var runs, errors int64
	if err := pool.QueryRow(ctx, `SELECT retired_at,run_requested_at,last_status,last_error,run_count,error_count FROM scheduled_job_runs WHERE name=$1`, name).Scan(&retiredAt, &requestedAt, &status, &lastError, &runs, &errors); err != nil {
		t.Fatal(err)
	}
	if retiredAt != nil || requestedAt != nil || status != "error" || lastError != "historical failure" || runs != 1850 || errors != 7 {
		t.Fatal("registration lost history, kept a retired request, or failed to reactivate")
	}
	if requested, err := repo.RequestRun(ctx, name); err != nil || !requested {
		t.Fatalf("reactivated RequestRun requested=%v err=%v", requested, err)
	}
	if won, _, err := repo.Claim(ctx, name, got, next); err != nil || !won {
		t.Fatalf("reactivated Claim won=%v err=%v", won, err)
	}
}

func TestLiveJobRunMonitoringExcludesOnlyExplicitRetirements(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 286)
	ctx := context.Background()
	names := []string{"active_job_" + uuid.NewString(), "retired_job_" + uuid.NewString()}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM scheduled_job_runs WHERE name=ANY($1)`, names) })
	for i, name := range names {
		var retiredAt *time.Time
		if i == 1 {
			at := time.Now()
			retiredAt = &at
		}
		if _, err := pool.Exec(ctx, `INSERT INTO scheduled_job_runs(name,service,interval_seconds,retired_at) VALUES($1,'test',300,$2)`, name, retiredAt); err != nil {
			t.Fatal(err)
		}
	}
	observedAt := time.Now().Truncate(time.Microsecond)
	for _, tc := range []struct {
		id     string
		status string
		finish time.Time
		due    time.Time
	}{
		{"job_error_recent", "error", observedAt.Add(-time.Minute), observedAt.Add(time.Hour)},
		{"job_error_old", "error", observedAt.Add(-2 * time.Hour), observedAt.Add(time.Hour)},
		{"job_running", "running", observedAt.Add(-time.Hour), observedAt.Add(time.Hour)},
		{"job_future", "idle", observedAt.Add(-time.Hour), observedAt.Add(time.Hour)},
		{"job_overdue", "idle", observedAt.Add(-time.Hour), observedAt.Add(-time.Hour)},
		{"job_ok", "ok", observedAt.Add(-24 * time.Hour), observedAt.Add(time.Hour)},
	} {
		t.Run(tc.id, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `UPDATE scheduled_job_runs SET last_status=$2,last_finished_at=$3,next_run_at=$4 WHERE name=ANY($1)`, names, tc.status, tc.finish, tc.due); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, query := range monitoringSections["jobs"].queries {
				if query.id != tc.id {
					continue
				}
				found = true
				var count int64
				var ignored [6]any
				statement := strings.ReplaceAll(query.query, "$1", "$1::timestamptz") + ` AND j.name=ANY($2) AND $1::timestamptz IS NOT NULL`
				if err := handle.QueryRow(ctx, statement, observedAt, names).Scan(&count, &ignored[0], &ignored[1], &ignored[2], &ignored[3], &ignored[4], &ignored[5]); err != nil || count != 1 {
					t.Fatalf("active/retired metric count=%d want1 err=%v", count, err)
				}
			}
			if !found {
				t.Fatalf("missing production monitoring query %s", tc.id)
			}
		})
	}
}

// One poll takes the requests of every named job and leaves the rest pending.
func TestLiveTakeRunRequestsTakesOnlyNamedRequestsOnce(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	repo := NewJobRunRepository(handle)
	asked, idle, elsewhere := "live_take_"+uuid.NewString(), "live_take_"+uuid.NewString(), "live_take_"+uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM scheduled_job_runs WHERE name = ANY($1)`, []string{asked, idle, elsewhere})
	})
	now := time.Now().Truncate(time.Microsecond)
	for _, name := range []string{asked, idle, elsewhere} {
		if _, err := repo.Register(ctx, name, "test", time.Hour, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{asked, elsewhere} {
		if found, err := repo.RequestRun(ctx, name); err != nil || !found {
			t.Fatalf("RequestRun %s found=%v err=%v", name, found, err)
		}
	}

	taken, err := repo.TakeRunRequests(ctx, []string{asked, idle})
	if err != nil || len(taken) != 1 || taken[0] != asked {
		t.Fatalf("taken %v err %v, want only %s", taken, err, asked)
	}
	if again, err := repo.TakeRunRequests(ctx, []string{asked, idle}); err != nil || len(again) != 0 {
		t.Fatalf("a request was taken twice: %v err %v", again, err)
	}
	if other, err := repo.TakeRunRequests(ctx, []string{elsewhere}); err != nil || len(other) != 1 {
		t.Fatalf("another process's request was not left pending: %v err %v", other, err)
	}
}
