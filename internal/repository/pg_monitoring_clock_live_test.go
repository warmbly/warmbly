package repository

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

type monitoringClockTrace struct {
	queries atomic.Int64
}

type monitoringClockTraceKey struct{}

func (tr *monitoringClockTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, monitoringClockTraceKey{}, strings.Contains(data.SQL, "SELECT '"))
}

func (tr *monitoringClockTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if measured, _ := ctx.Value(monitoringClockTraceKey{}).(bool); measured {
		tr.queries.Add(1)
	}
}

func monitoringClockPool(t *testing.T, pool *pgxpool.Pool, trace *monitoringClockTrace, scope monitoringScope) *pgxpool.Pool {
	t.Helper()
	cfg := pool.Config().Copy()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "3000"
	cfg.ConnConfig.RuntimeParams["timezone"] = "America/Los_Angeles"
	cfg.ConnConfig.Tracer = trace
	scope.isolate(cfg)
	ro, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ro.Close)
	var readOnly bool
	if err := ro.QueryRow(t.Context(), `SELECT current_setting('transaction_read_only')::boolean`).Scan(&readOnly); err != nil || !readOnly {
		t.Fatal("collection connection is not read-only", err)
	}
	return ro
}

func requireMonitoringClock(t *testing.T, source models.MonitoringSource, before, after time.Time) {
	t.Helper()
	if source.ObservedAt == nil || source.ObservedAt.Before(before) || source.ObservedAt.After(after) {
		t.Fatalf("source clock outside database collection: observed=%v before=%v after=%v", source.ObservedAt, before, after)
	}
	for _, metric := range source.Metrics {
		if metric.ObservedAt == nil || !metric.ObservedAt.Equal(*source.ObservedAt) {
			t.Fatalf("metric %s has a different source clock: %v", metric.ID, metric.ObservedAt)
		}
		if metric.WindowEnd != nil && (!metric.WindowEnd.Equal(*source.ObservedAt) || metric.WindowStart == nil) {
			t.Fatalf("metric %s window has a different source clock", metric.ID)
		}
	}
}

func TestLiveMonitoringDatabaseClockAfterRequestStart(t *testing.T) {
	for _, source := range []string{"sync", "worker_samples"} {
		t.Run(source, func(t *testing.T) {
			_, pool := liveContactDB(t)
			requireSchemaVersion(t, pool, 287)
			f := newWarmupUsageFixture(t, pool)
			worker := monitoringWorkerNode(t, pool, "worker", true, nil)
			f.exec(`UPDATE email_accounts SET status='active',worker_id=$2 WHERE id=$1`, f.account, worker)
			requested := monitoringWorkerClock(t, pool)
			var receipt time.Time
			metric := "sync_future_clock"
			if source == "sync" {
				if err := pool.QueryRow(t.Context(), `INSERT INTO email_sync_state(email_id,user_id,last_synced_at) VALUES($1,$2,clock_timestamp()) RETURNING last_synced_at`, f.account, f.user).Scan(&receipt); err != nil {
					t.Fatal(err)
				}
			} else {
				metric = "worker_samples_missing"
				if err := pool.QueryRow(t.Context(), `INSERT INTO worker_health_samples(worker_id,observed_at,auth_errors) VALUES($1,clock_timestamp(),3) RETURNING observed_at`, worker).Scan(&receipt); err != nil {
					t.Fatal(err)
				}
			}
			if !receipt.After(requested) {
				t.Fatal("fixture receipt did not follow request start")
			}
			trace := &monitoringClockTrace{}
			ro := monitoringClockPool(t, pool, trace, monitoringScope{nodes: []uuid.UUID{worker}, orgs: []uuid.UUID{f.org}})
			before := monitoringWorkerClock(t, pool)
			got := collectMonitoring(t, NewMonitoringRepository(ro), source, requested)
			after := monitoringWorkerClock(t, pool)
			m := measuredMonitoring(t, got, metric)
			t.Logf("request=%s database_receipt=%s collection_before=%s warning_count=%d observation=%v", requested.Format(time.RFC3339Nano), receipt.Format(time.RFC3339Nano), before.Format(time.RFC3339Nano), *m.Count, m.ObservedAt)
			if *m.Count != 0 {
				t.Fatalf("normal database receipt after request start was classified future/missing: %s=%d", metric, *m.Count)
			}
			requireMonitoringClock(t, got, before, after)
			if trace.queries.Load() != 1 || ro.Stat().AcquiredConns() != 0 {
				t.Fatal("source did not use one released connection and statement", trace.queries.Load())
			}
			if source == "worker_samples" {
				window := measuredMonitoring(t, got, "worker_sample_auth_errors_1h")
				if *window.Count != 3 || window.WindowStart == nil || window.WindowEnd.Sub(*window.WindowStart) != time.Hour {
					t.Fatal("new receipt was excluded from the measured window", window)
				}
			}
		})
	}
}

func TestLiveMonitoringDatabaseClockWarningSemantics(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 287)
	f := newWarmupUsageFixture(t, pool)
	worker := monitoringWorkerNode(t, pool, "worker", true, nil)
	f.exec(`UPDATE email_accounts SET status='active',worker_id=$2 WHERE id=$1`, f.account, worker)
	now := monitoringWorkerClock(t, pool)
	stale, future, expired := now.Add(-time.Hour), now.Add(time.Hour), now.Add(-time.Second)
	r := NewMonitoringRepository(monitoringClockPool(t, pool, &monitoringClockTrace{}, monitoringScope{nodes: []uuid.UUID{worker}, orgs: []uuid.UUID{f.org}}))
	for _, tc := range []struct {
		name, provider, metric string
		last, hold             *time.Time
		missing                bool
		condition              models.MonitoringCondition
	}{
		{"future smtp", "smtp_imap", "sync_future_clock", &future, nil, false, models.MonitoringUnknown},
		{"future gmail", "gmail", "sync_future_clock", &future, nil, false, models.MonitoringUnknown},
		{"future outlook", "outlook", "sync_future_clock", &future, nil, false, models.MonitoringUnknown},
		{"stale", "smtp_imap", "sync_stale", &stale, nil, false, models.MonitoringTelemetryStale},
		{"null completion", "smtp_imap", "sync_stale", nil, nil, false, models.MonitoringTelemetryStale},
		{"missing state", "smtp_imap", "sync_missing", nil, nil, true, models.MonitoringUnknown},
		{"held stale", "smtp_imap", "sync_held_stale", &stale, &future, false, models.MonitoringBudget},
		{"expired hold", "smtp_imap", "sync_stale", &stale, &expired, false, models.MonitoringTelemetryStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.exec(`UPDATE email_accounts SET provider=$2 WHERE id=$1`, f.account, tc.provider)
			f.exec(`DELETE FROM email_sync_state WHERE email_id=$1`, f.account)
			if !tc.missing {
				f.exec(`INSERT INTO email_sync_state(email_id,user_id,last_synced_at,throttled_until,throttle_reason) VALUES($1,$2,$3,$4,'org_daily')`, f.account, f.user, tc.last, tc.hold)
			}
			before := monitoringWorkerClock(t, pool)
			got := collectMonitoring(t, r, "sync", now.Add(24*time.Hour))
			requireMonitoringClock(t, got, before, monitoringWorkerClock(t, pool))
			m := measuredMonitoring(t, got, tc.metric)
			if *m.Count != 1 || m.Condition != tc.condition || m.Severity != "warning" && tc.metric != "sync_held_stale" {
				t.Fatal("real warning was hidden or reclassified", m)
			}
			if tc.metric == "sync_future_clock" && (m.LatestEvidenceAt == nil || !m.LatestEvidenceAt.Equal(future)) {
				t.Fatal("future evidence clock was changed", m)
			}
			if tc.metric == "sync_held_stale" {
				if *measuredMonitoring(t, got, "sync_stale").Count != 0 || *measuredMonitoring(t, got, "sync_hold_org_daily").Count != 1 {
					t.Fatal("active hold did not retain its separate evidence", got)
				}
			}
			if tc.name == "expired hold" && *measuredMonitoring(t, got, "sync_hold_org_daily").Count != 0 {
				t.Fatal("expired hold stayed active", got)
			}
		})
	}
	for _, tc := range []struct {
		name string
		at   *time.Time
	}{
		{"future receipt", &future}, {"stale receipt", &stale}, {"missing receipt", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.exec(`DELETE FROM worker_health_samples WHERE worker_id=$1`, worker)
			if tc.at != nil {
				f.exec(`INSERT INTO worker_health_samples(worker_id,observed_at) VALUES($1,$2)`, worker, tc.at)
			}
			got := collectMonitoring(t, r, "worker_samples", now.Add(24*time.Hour))
			m := measuredMonitoring(t, got, "worker_samples_missing")
			if *m.Count != 1 || m.Condition != models.MonitoringTelemetryStale || m.Severity != "warning" {
				t.Fatal("future/stale/missing worker receipt was hidden", m)
			}
		})
	}
	f.exec(`UPDATE email_accounts SET auth_state='passing',auth_checked_at=$2,send_recovery_hold=true,send_recovery_reason='unknown' WHERE id=$1`, f.account, future)
	domain := collectMonitoring(t, r, "domain_auth", now)
	if *measuredMonitoring(t, domain, "domain_future_clock").Count != 1 || *measuredMonitoring(t, domain, "domain_passing_fresh").Count != 0 {
		t.Fatal("provider future observation became passing evidence", domain)
	}
	if *measuredMonitoring(t, collectMonitoring(t, r, "send_safety", now), "holds_unknown").Count != 1 {
		t.Fatal("unknown-send protection disappeared")
	}
}

func TestLiveMonitoringDatabaseClockPredicateBoundaries(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 287)
	f := newWarmupUsageFixture(t, pool)
	worker := monitoringWorkerNode(t, pool, "worker", true, nil)
	f.exec(`UPDATE email_accounts SET status='active',worker_id=$2 WHERE id=$1`, f.account, worker)
	at := monitoringWorkerClock(t, pool)
	for _, tc := range []struct {
		name, section, metric string
		offset                time.Duration
		want                  int64
	}{
		{"sync just past", "sync", "sync_future_clock", -time.Microsecond, 0},
		{"sync at reference", "sync", "sync_future_clock", 0, 0},
		{"sync one microsecond future", "sync", "sync_future_clock", time.Microsecond, 1},
		{"sync exactly stale cutoff", "sync", "sync_stale", -20 * time.Minute, 0},
		{"sync just outside cutoff", "sync", "sync_stale", -20*time.Minute - time.Microsecond, 1},
		{"sync just inside cutoff", "sync", "sync_stale", -20*time.Minute + time.Microsecond, 0},
		{"hold exactly reference", "sync", "sync_hold_org_daily", 0, 0},
		{"hold just expired", "sync", "sync_hold_org_daily", -time.Microsecond, 0},
		{"hold one microsecond active", "sync", "sync_hold_org_daily", time.Microsecond, 1},
		{"sample exactly cutoff", "worker_samples", "worker_samples_missing", -20 * time.Minute, 0},
		{"sample just outside cutoff", "worker_samples", "worker_samples_missing", -20*time.Minute - time.Microsecond, 1},
		{"sample just inside cutoff", "worker_samples", "worker_samples_missing", -20*time.Minute + time.Microsecond, 0},
		{"sample at reference", "worker_samples", "worker_samples_missing", 0, 0},
		{"sample one microsecond future", "worker_samples", "worker_samples_missing", time.Microsecond, 1},
		{"window lower inclusive", "worker_samples", "worker_sample_auth_errors_1h", -time.Hour, 7},
		{"window just outside", "worker_samples", "worker_sample_auth_errors_1h", -time.Hour - time.Microsecond, -1},
		{"window just inside", "worker_samples", "worker_sample_auth_errors_1h", -time.Hour + time.Microsecond, 7},
		{"window upper inclusive", "worker_samples", "worker_sample_auth_errors_1h", 0, 7},
		{"window just future", "worker_samples", "worker_sample_auth_errors_1h", time.Microsecond, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stamp := at.Add(tc.offset)
			f.exec(`DELETE FROM email_sync_state WHERE email_id=$1`, f.account)
			f.exec(`INSERT INTO email_sync_state(email_id,user_id,last_synced_at,throttled_until,throttle_reason) VALUES($1,$2,$3,$3,'org_daily')`, f.account, f.user, stamp)
			if tc.metric == "sync_stale" {
				f.exec(`UPDATE email_sync_state SET throttled_until=NULL WHERE email_id=$1`, f.account)
			}
			f.exec(`DELETE FROM worker_health_samples WHERE worker_id=$1`, worker)
			f.exec(`INSERT INTO worker_health_samples(worker_id,observed_at,auth_errors) VALUES($1,$2,7)`, worker, stamp)
			var q monitoringQuery
			for _, candidate := range monitoringSections[tc.section].queries {
				if candidate.id == tc.metric {
					q = candidate
				}
			}
			if q.id == "" {
				t.Fatal("missing production predicate")
			}
			scope := f.account
			switch {
			case tc.section == "sync":
				q.query += " AND e.id=$2"
			case tc.metric == "worker_samples_missing":
				q.query += " AND n.id=$2"
				scope = worker
			default:
				q.query += " AND h.worker_id=$2"
				scope = worker
			}
			// Fix only the test clock to exercise exact native predicate boundaries.
			query := strings.Replace(monitoringStatement(monitoringSection{queries: []monitoringQuery{q}}), "clock_timestamp()", "$1::timestamptz", 1)
			var id string
			var observed time.Time
			var m models.MonitoringMetric
			if err := pool.QueryRow(t.Context(), query, at, scope).Scan(&id, &observed, &m.Count, &m.AffectedMailboxes, &m.AffectedOrganizations, &m.UnknownOrganizationRows, &m.EvidenceAt, &m.LatestEvidenceAt, &m.NextEligibleAt); err != nil {
				t.Fatal(err)
			}
			if id != q.id || !observed.Equal(at) || tc.want == -1 && m.Count != nil || tc.want >= 0 && (m.Count == nil || *m.Count != tc.want) {
				t.Fatalf("native boundary changed: observed=%v metric=%+v want=%d", observed, m, tc.want)
			}
		})
	}
}

func TestLiveMonitoringDatabaseClockSingleSnapshot(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 287)
	f := newWarmupUsageFixture(t, pool)
	worker := monitoringWorkerNode(t, pool, "worker", true, nil)
	f.exec(`UPDATE email_accounts SET status='active',worker_id=$2 WHERE id=$1`, f.account, worker)
	old := monitoringWorkerClock(t, pool).Add(-time.Hour)
	f.exec(`INSERT INTO email_sync_state(email_id,user_id,last_synced_at) VALUES($1,$2,$3)`, f.account, f.user, old)
	trace := &monitoringClockTrace{}
	ro := monitoringClockPool(t, pool, trace, monitoringScope{nodes: []uuid.UUID{worker}, orgs: []uuid.UUID{f.org}})
	var pid int
	if err := ro.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	writer, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Release()
	key := int64(uuid.New().ID())
	if _, err := writer.Exec(t.Context(), `SELECT pg_advisory_lock($1)`, key); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = writer.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key) }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	section := monitoringSections["sync"]
	section.queries = append([]monitoringQuery(nil), section.queries...)
	section.queries[0].query += " AND (SELECT pg_advisory_xact_lock(" + strconv.FormatInt(key, 10) + ")::text)=''"
	result := make(chan struct {
		source models.MonitoringSource
		err    error
	}, 1)
	before := monitoringWorkerClock(t, pool)
	go func() {
		source, err := NewMonitoringRepository(ro).collect(ctx, before, "sync", section)
		result <- struct {
			source models.MonitoringSource
			err    error
		}{source, err}
	}()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		if err := writer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND NOT granted)`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("native statement did not reach the snapshot barrier", ctx.Err())
		case <-tick.C:
		}
	}
	if _, err := writer.Exec(ctx, `UPDATE email_sync_state SET last_synced_at=clock_timestamp() WHERE email_id=$1`, f.account); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(ctx, `SELECT pg_advisory_unlock($1)`, key); err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil {
		t.Fatal(got.err)
	}
	requireMonitoringClock(t, got.source, before, monitoringWorkerClock(t, pool))
	stale := measuredMonitoring(t, got.source, "sync_stale")
	rows := measuredMonitoring(t, got.source, "sync_state_rows")
	if *stale.Count != 1 || rows.LatestEvidenceAt == nil || !rows.LatestEvidenceAt.Equal(old) || trace.queries.Load() != 1 || ro.Stat().AcquiredConns() != 0 {
		t.Fatal("concurrent commit split the native statement snapshot", got.source, trace.queries.Load())
	}
	if *measuredMonitoring(t, collectMonitoring(t, NewMonitoringRepository(ro), "sync", before), "sync_stale").Count != 0 {
		t.Fatal("later statement did not observe the committed completion")
	}
}
