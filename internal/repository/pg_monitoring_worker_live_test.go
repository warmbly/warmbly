package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/app/monitoring"
	"github.com/warmbly/warmbly/internal/models"
)

type monitoringWorkerTraceKey struct{}

type monitoringWorkerTrace struct {
	queries    atomic.Int64
	afterFirst func()
}

func (tr *monitoringWorkerTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, monitoringWorkerTraceKey{}, strings.Contains(data.SQL, "FROM fleet_nodes"))
}

func (tr *monitoringWorkerTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if workerQuery, _ := ctx.Value(monitoringWorkerTraceKey{}).(bool); workerQuery && tr.queries.Add(1) == 1 && tr.afterFirst != nil {
		tr.afterFirst()
	}
}

func monitoringTracedWorkerPool(t *testing.T, pool *pgxpool.Pool, trace *monitoringWorkerTrace) *pgxpool.Pool {
	t.Helper()
	cfg := pool.Config().Copy()
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.ConnConfig.Tracer = trace
	ro, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ro.Close)
	return ro
}

func monitoringWorkerNode(t *testing.T, pool *pgxpool.Pool, role string, active bool, seen *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(t.Context(), `INSERT INTO fleet_nodes(id,role,name,active,last_seen_at) VALUES($1,$2,'monitoring fixture',$3,$4)`, id, role, active, seen); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM workers WHERE id=$1`, id); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, id); err != nil {
			t.Error(err)
		}
	})
	if _, err := pool.Exec(t.Context(), `INSERT INTO workers(id) VALUES($1)`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func monitoringWorkerClock(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	var at time.Time
	if err := pool.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

func TestLiveMonitoringWorkerHeartbeatAfterRefreshStart(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 283)
	at := monitoringWorkerClock(t, pool).Add(-time.Second)
	id := monitoringWorkerNode(t, pool, "worker", true, nil)
	fleet := NewFleetNodeRepository(handle)
	if err := fleet.UpsertOnHeartbeat(t.Context(), models.NodeHeartbeat{NodeID: id, Role: models.NodeRoleWorker}); err != nil {
		t.Fatal(err)
	}
	got := collectMonitoring(t, NewMonitoringRepository(monitoringReadOnlyPool(t, pool)), "workers", at)
	m := measuredMonitoring(t, got, "workers_missing_heartbeat")
	if *m.Count != 0 || len(got.Metrics) != 1 || *got.ExpectedScopes != 0 || *got.MeasuredScopes != 0 {
		t.Fatalf("in-collection native heartbeat classified missing/future: %+v", got)
	}
	if got.ObservedAt == nil || !got.ObservedAt.After(at) || !m.ObservedAt.Equal(*got.ObservedAt) {
		t.Fatalf("observation was relabelled with refresh start: %+v", got)
	}
}

func TestLiveMonitoringWorkerHeartbeatSingleSnapshot(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 283)
	old := monitoringWorkerClock(t, pool).Add(-time.Minute)
	id := monitoringWorkerNode(t, pool, "worker", true, &old)
	at := monitoringWorkerClock(t, pool)
	trace := &monitoringWorkerTrace{afterFirst: func() {
		if err := NewFleetNodeRepository(handle).UpsertOnHeartbeat(t.Context(), models.NodeHeartbeat{NodeID: id, Role: models.NodeRoleWorker}); err != nil {
			t.Fatal(err)
		}
	}}
	got := collectMonitoring(t, NewMonitoringRepository(monitoringTracedWorkerPool(t, pool, trace)), "workers", at)
	m := measuredMonitoring(t, got, "workers_missing_heartbeat")
	if *m.Count != 0 || len(got.Metrics) != 1 || *got.ExpectedScopes != 0 || *got.MeasuredScopes != 0 || trace.queries.Load() != 1 {
		t.Fatalf("aggregate/details observed different heartbeats (queries=%d): %+v", trace.queries.Load(), got)
	}
	var written time.Time
	if err := pool.QueryRow(t.Context(), `SELECT last_seen_at FROM fleet_nodes WHERE id=$1`, id).Scan(&written); err != nil || !written.After(at) {
		t.Fatalf("concurrent native heartbeat was not written after refresh start: %v %v", written, err)
	}
}

func TestLiveMonitoringWorkerHeartbeatStatesAndClockBoundary(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 283)
	at := monitoringWorkerClock(t, pool)
	stale, fresh, future := at.Add(-6*time.Minute), at.Add(-time.Minute), at.Add(time.Hour)
	for _, tc := range []struct {
		name, role string
		active     bool
		seen       *time.Time
		count      int64
		condition  models.MonitoringCondition
	}{
		{"stale", "worker", true, &stale, 1, models.MonitoringTelemetryStale},
		{"null", "worker", true, nil, 1, models.MonitoringTelemetryStale},
		{"future supplied through native mirror", "worker", true, &future, 1, models.MonitoringUnknown},
		{"fresh supplied through native mirror", "worker", true, &fresh, 0, models.MonitoringNoEvidence},
		{"disabled", "worker", false, nil, 0, models.MonitoringNoEvidence},
		{"nonworker", "consumer", true, nil, 0, models.MonitoringNoEvidence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := tc.seen
			if strings.Contains(tc.name, "native mirror") {
				seen = nil
			}
			id := monitoringWorkerNode(t, pool, tc.role, tc.active, seen)
			if seen == nil && tc.seen != nil {
				if err := NewFleetNodeRepository(handle).TouchLastSeen(t.Context(), id, *tc.seen); err != nil {
					t.Fatal(err)
				}
			}
			f := newWarmupUsageFixture(t, pool)
			f.exec(`UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, id, f.account)
			r := NewMonitoringRepository(monitoringReadOnlyPool(t, pool))
			snapshot := monitoring.New([]monitoring.Source{r.workerSource()}).Snapshot(t.Context(), models.AdminPermViewWorkers)
			got := snapshot.Sources[0]
			m := measuredMonitoring(t, got, "workers_missing_heartbeat")
			if got.Availability != models.MonitoringFresh || got.Coverage != "complete" || got.Reason != "" || *m.Count != tc.count || *got.ExpectedScopes != tc.count || *got.MeasuredScopes != tc.count || len(got.Metrics) != 1+int(tc.count) {
				t.Fatalf("incorrect worker coverage: %+v", got)
			}
			if *m.AffectedMailboxes != tc.count || *m.AffectedOrganizations != tc.count || *m.UnknownOrganizationRows != 0 || *m.ThresholdSeconds != 300 || !m.ObservedAt.Equal(*got.ObservedAt) {
				t.Fatalf("incorrect worker aggregate: %+v", m)
			}
			if m.Condition != tc.condition || (tc.count > 0 && m.Severity != "warning") {
				t.Fatalf("worker aggregate misclassified heartbeat evidence: %+v", m)
			}
			if tc.count == 0 {
				if m.Condition != models.MonitoringNoEvidence || m.EvidenceAt != nil || m.LatestEvidenceAt != nil {
					t.Fatalf("empty observation fabricated evidence: %+v", m)
				}
				return
			}
			v := got.Metrics[1]
			if v.ScopeID != id.String() || v.Condition != tc.condition || v.Severity != "warning" || *v.Count != 1 || *v.AffectedMailboxes != 1 || *v.AffectedOrganizations != 1 || *v.UnknownOrganizationRows != 0 || !v.ObservedAt.Equal(*got.ObservedAt) {
				t.Fatalf("incorrect worker scope: %+v", v)
			}
			if tc.seen == nil {
				if v.EvidenceAt != nil || m.EvidenceAt != nil || m.LatestEvidenceAt != nil {
					t.Fatal("NULL heartbeat was assigned an evidence clock")
				}
			} else if !v.EvidenceAt.Equal(*tc.seen) || !m.EvidenceAt.Equal(*tc.seen) || !m.LatestEvidenceAt.Equal(*tc.seen) {
				t.Fatal("stored heartbeat clock changed", got)
			}
			if tc.condition == models.MonitoringUnknown && (!v.EvidenceAt.After(*v.ObservedAt) || v.EvidenceAgeSeconds != nil) {
				t.Fatal("future timestamp was hidden or assigned a positive age", v)
			}
		})
	}
	t.Run("exact native predicate boundaries", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			seen    time.Time
			missing bool
		}{
			{"at stale cutoff", at.Add(-5 * time.Minute), true},
			{"one microsecond inside liveness", at.Add(-5*time.Minute + time.Microsecond), false},
			{"one microsecond outside liveness", at.Add(-5*time.Minute - time.Microsecond), true},
			{"at observation", at, false},
			{"one microsecond future", at.Add(time.Microsecond), true},
		} {
			var missing bool
			if err := pool.QueryRow(t.Context(), `SELECT (`+workerHeartbeatMissingPredicate+`) FROM (VALUES($1::timestamptz)) AS observation(at) CROSS JOIN (VALUES($2::timestamptz)) AS w(last_seen_at)`, at, tc.seen).Scan(&missing); err != nil || missing != tc.missing {
				t.Fatalf("%s: missing=%v want=%v err=%v", tc.name, missing, tc.missing, err)
			}
		}
	})
}

func TestLiveMonitoringWorkerHeartbeatAggregateClockUncertainty(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 283)
	at := monitoringWorkerClock(t, pool)
	future, stale := at.Add(time.Hour), at.Add(-6*time.Minute)
	for _, tc := range []struct {
		name      string
		future    int
		stale     bool
		missing   bool
		condition models.MonitoringCondition
	}{
		{"future only", 1, false, false, models.MonitoringUnknown},
		{"future only beyond sample limit", 21, false, false, models.MonitoringUnknown},
		{"future and stale", 1, true, false, models.MonitoringTelemetryStale},
		{"future and NULL", 1, false, true, models.MonitoringTelemetryStale},
		{"future and stale and NULL", 1, true, true, models.MonitoringTelemetryStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := make([]uuid.UUID, tc.future)
			conditions := make(map[string]models.MonitoringCondition)
			for i := range ids {
				ids[i] = monitoringWorkerNode(t, pool, "worker", true, &future)
				conditions[ids[i].String()] = models.MonitoringUnknown
			}
			if tc.stale {
				id := monitoringWorkerNode(t, pool, "worker", true, &stale)
				conditions[id.String()] = models.MonitoringTelemetryStale
			}
			if tc.missing {
				id := monitoringWorkerNode(t, pool, "worker", true, nil)
				conditions[id.String()] = models.MonitoringTelemetryStale
			}
			f := newWarmupUsageFixture(t, pool)
			f.exec(`UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, ids[0], f.account)
			trace := &monitoringWorkerTrace{}
			r := NewMonitoringRepository(monitoringTracedWorkerPool(t, pool, trace))
			got := monitoring.New([]monitoring.Source{r.workerSource()}).Snapshot(t.Context(), models.AdminPermViewWorkers).Sources[0]
			m := measuredMonitoring(t, got, "workers_missing_heartbeat")
			total := int64(len(conditions))
			coverage, reason := "complete", ""
			if total > 20 {
				coverage, reason = "partial", "sample_limit"
			}
			if got.Availability != models.MonitoringFresh || got.Coverage != coverage || got.Reason != reason || *got.ExpectedScopes != total || *got.MeasuredScopes != min(total, 20) || int64(len(got.Metrics)-1) != min(total, 20) || trace.queries.Load() != 1 {
				t.Fatalf("aggregate clock classification changed source coverage: %+v", got)
			}
			if m.Condition != tc.condition || m.Severity != "warning" || *m.Count != total || *m.AffectedMailboxes != 1 || *m.AffectedOrganizations != 1 || *m.UnknownOrganizationRows != 0 || *m.ThresholdSeconds != 300 || !m.ObservedAt.Equal(*got.ObservedAt) {
				t.Fatalf("aggregate clock classification changed evidence or misclassified it: %+v", m)
			}
			for _, v := range got.Metrics[1:] {
				if v.Condition != conditions[v.ScopeID] || v.Severity != "warning" || *v.Count != 1 || !v.ObservedAt.Equal(*got.ObservedAt) {
					t.Fatalf("scoped clock condition disagrees with aggregate: %+v", v)
				}
			}
		})
	}
}

func TestLiveMonitoringWorkerHeartbeatSampleAndAssignments(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 283)
	for _, total := range []int64{0, 20, 21} {
		t.Run(fmt.Sprint(total), func(t *testing.T) {
			ids := make([]uuid.UUID, total)
			for i := range ids {
				ids[i] = monitoringWorkerNode(t, pool, "worker", true, nil)
			}
			slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
			if total > 0 {
				f, other := newWarmupUsageFixture(t, pool), newWarmupUsageFixture(t, pool)
				f.exec(`UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, ids[0], f.account)
				other.exec(`UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, ids[len(ids)-1], other.account)
				copyID := uuid.New()
				f.exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider) SELECT $1,user_id,organization_id,$2,name,'','','smtp_imap' FROM email_accounts WHERE id=$3`, copyID, uuid.NewString()+"@example.test", f.account)
				f.exec(`UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, ids[0], copyID)
				t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id=$1`, copyID) })
			}
			r := NewMonitoringRepository(monitoringReadOnlyPool(t, pool))
			got := monitoring.New([]monitoring.Source{r.workerSource()}).Snapshot(t.Context(), models.AdminPermViewWorkers).Sources[0]
			m := measuredMonitoring(t, got, "workers_missing_heartbeat")
			if *m.Count != total || *got.ExpectedScopes != total || *got.MeasuredScopes != min(total, 20) || len(got.Metrics) != int(min(total, 20))+1 {
				t.Fatalf("bounded scope counts disagree: %+v", got)
			}
			if total > 20 {
				if got.Coverage != "partial" || got.Reason != "sample_limit" {
					t.Fatal("truncated scope sample reported complete", got)
				}
			} else if got.Coverage != "complete" || got.Reason != "" {
				t.Fatal("untruncated worker source not complete", got)
			}
			mailboxes, organizations := int64(0), int64(0)
			if total > 0 {
				mailboxes, organizations = 3, 2
			}
			if *m.AffectedMailboxes != mailboxes || *m.AffectedOrganizations != organizations || *m.UnknownOrganizationRows != 0 {
				t.Fatal("aggregate assignments lost outside bounded sample", m)
			}
			for i, v := range got.Metrics[1:] {
				if !v.ObservedAt.Equal(*m.ObservedAt) || v.EvidenceAt != nil || (i > 0 && got.Metrics[i].ScopeID >= v.ScopeID) {
					t.Fatal("sample clocks or deterministic NULL-first ordering disagree", got)
				}
				want := int64(0)
				if v.ScopeID == ids[0].String() {
					want = 2
				} else if v.ScopeID == ids[len(ids)-1].String() {
					want = 1
				}
				if *v.AffectedMailboxes != want || *v.AffectedOrganizations != min(want, 1) || *v.UnknownOrganizationRows != 0 {
					t.Fatal("sampled worker assignments disagree with aggregate", v)
				}
			}
			if total == 21 && got.Metrics[len(got.Metrics)-1].ScopeID == ids[len(ids)-1].String() {
				t.Fatal("bounded sample unexpectedly includes the excluded worker", got)
			}
		})
	}
}

func TestLiveMonitoringWorkerHeartbeatConcurrentRead(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 283)
	f := newWarmupUsageFixture(t, pool)
	ids := make([]uuid.UUID, 25)
	for i := range ids {
		ids[i] = monitoringWorkerNode(t, pool, "worker", true, nil)
		if i == 0 {
			f.exec(`UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, ids[i], f.account)
		} else {
			mailbox := uuid.New()
			f.exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,worker_id) SELECT $1,user_id,organization_id,$2,name,'','','smtp_imap',$3 FROM email_accounts WHERE id=$4`, mailbox, uuid.NewString()+"@example.test", ids[i], f.account)
			t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id=$1`, mailbox) })
		}
	}
	trace := &monitoringWorkerTrace{}
	r := NewMonitoringRepository(monitoringTracedWorkerPool(t, pool, trace))
	stop, started, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	writerCtx, cancel := context.WithCancel(context.Background())
	var writes atomic.Int64
	go func() {
		fresh := false
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			fresh = !fresh
			_, err := pool.Exec(writerCtx, `UPDATE fleet_nodes SET last_seen_at=CASE WHEN $2 THEN clock_timestamp() ELSE NULL END WHERE id=ANY($1::uuid[])`, ids, fresh)
			if writes.Add(1) == 1 {
				close(started)
			}
			if err != nil {
				done <- err
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	<-started
	for i := 0; i < 100; i++ {
		got := collectMonitoring(t, r, "workers", monitoringWorkerClock(t, pool).Add(-time.Second))
		m := measuredMonitoring(t, got, "workers_missing_heartbeat")
		if (*m.Count != 0 && *m.Count != 25) || *got.ExpectedScopes != *m.Count || *got.MeasuredScopes != min(*m.Count, 20) || int64(len(got.Metrics)-1) != *got.MeasuredScopes {
			t.Fatal("concurrent committed heartbeat batch split the source observation", got)
		}
		wantOrgs := min(*m.Count, 1)
		if *m.AffectedMailboxes != *m.Count || *m.AffectedOrganizations != wantOrgs || *m.UnknownOrganizationRows != 0 {
			t.Fatal("concurrent worker and mailbox counts disagreed", m)
		}
		for _, v := range got.Metrics[1:] {
			if v.EvidenceAt != nil || !v.ObservedAt.Equal(*m.ObservedAt) || *v.AffectedMailboxes != 1 || *v.AffectedOrganizations != 1 {
				t.Fatal("native current heartbeat misclassified future during collection", v)
			}
		}
	}
	if trace.queries.Load() != 100 || writes.Load() < 2 {
		t.Fatalf("missing concurrent/single-query proof: queries=%d writes=%d", trace.queries.Load(), writes.Load())
	}
}

func TestLiveMonitoringWorkerHeartbeatPermissionAndTimeout(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 283)
	r := NewMonitoringRepository(monitoringReadOnlyPool(t, pool))
	denied := monitoring.New([]monitoring.Source{r.workerSource()}).Snapshot(t.Context(), 0).Sources[0]
	if denied.Availability != models.MonitoringUnavailable || denied.Reason != "permission_denied" || len(denied.Metrics) != 0 || denied.ObservedAt != nil || denied.ExpectedScopes != nil || denied.MeasuredScopes != nil {
		t.Fatal("permission denied worker evidence leaked", denied)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(t.Context(), `LOCK TABLE fleet_nodes IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	got := monitoring.New([]monitoring.Source{r.workerSource()}).Snapshot(t.Context(), models.AdminPermViewWorkers).Sources[0]
	if got.Availability != models.MonitoringUnavailable || got.Reason != "timeout" || got.Coverage != "unavailable" || len(got.Metrics) != 0 || got.ObservedAt != nil || got.ExpectedScopes != nil || time.Since(start) > 4*time.Second {
		t.Fatalf("worker read did not preserve bounded unavailable semantics (%s): %+v", time.Since(start), got)
	}
}
