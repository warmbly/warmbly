package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/models"
)

func TestMonitoringCoalescesCacheAndPreservesObservationTimes(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC).Unix())
	var calls atomic.Int64
	start, release := make(chan struct{}), make(chan struct{})
	s := New([]Source{{ID: "test", Permission: models.AdminPermViewAnalytics, Collect: func(_ context.Context, at time.Time) (models.MonitoringSource, error) {
		if calls.Add(1) == 1 {
			close(start)
			<-release
		}
		evidence := at.Add(-time.Hour)
		n := int64(0)
		return models.MonitoringSource{Metrics: []models.MonitoringMetric{{ID: "zero", Count: &n, ObservedAt: &at, EvidenceAt: &evidence, Availability: models.MonitoringFresh, Condition: models.MonitoringNoEvidence}}}, nil
	}}})
	s.now = func() time.Time { return time.Unix(clock.Load(), 0).UTC() }
	var wg sync.WaitGroup
	results := make(chan models.MonitoringSnapshot, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Snapshot(t.Context(), models.AdminPermViewAnalytics) }()
	}
	<-start
	close(release)
	wg.Wait()
	close(results)
	if calls.Load() != 1 {
		t.Fatalf("coalesced calls=%d", calls.Load())
	}
	var first models.MonitoringSnapshot
	for result := range results {
		first = result
		if result.Coverage != "complete" || result.Sources[0].Metrics[0].Count == nil || *result.Sources[0].Metrics[0].Count != 0 {
			t.Fatal(result)
		}
	}
	clock.Add(30)
	next := s.Snapshot(t.Context(), models.AdminPermViewAnalytics)
	if !next.CheckedAt.Equal(first.CheckedAt) || !next.Sources[0].ObservedAt.Equal(*first.Sources[0].ObservedAt) || *next.Sources[0].Metrics[0].EvidenceAgeSeconds != 3630 || calls.Load() != 1 {
		t.Fatal("cache refreshed evidence", next)
	}
}

func TestMonitoringPreservesSourceObservationAfterRefreshStart(t *testing.T) {
	start := time.Date(2026, 10, 10, 16, 0, 59, 0, time.UTC)
	observed, checked := start.Add(650*time.Millisecond), start.Add(700*time.Millisecond)
	s := New([]Source{{ID: "workers", Permission: models.AdminPermViewWorkers, Collect: func(_ context.Context, _ time.Time) (models.MonitoringSource, error) {
		return models.MonitoringSource{ObservedAt: &observed, CheckedAt: checked, Metrics: []models.MonitoringMetric{{ID: "workers_missing_heartbeat", ObservedAt: &observed, Availability: models.MonitoringFresh}}}, nil
	}}})
	s.now = func() time.Time { return start }
	out := s.Snapshot(t.Context(), models.AdminPermViewWorkers)
	if !out.CheckedAt.Equal(start) || !out.Sources[0].CheckedAt.Equal(start) || !out.Sources[0].ObservedAt.Equal(observed) || !out.Sources[0].Metrics[0].ObservedAt.Equal(observed) {
		t.Fatalf("source observation overwritten by snapshot start: %+v", out)
	}
}

func TestMonitoringFailedRefreshRetainsOriginalEvidenceThenExpires(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	fail := false
	s := New([]Source{{ID: "test", Permission: models.AdminPermViewAnalytics, Collect: func(_ context.Context, at time.Time) (models.MonitoringSource, error) {
		if fail {
			return models.MonitoringSource{}, errors.New("postgres://sensitive-user:secret@private.host/customer@example.test")
		}
		n := int64(2)
		evidence := at.Add(-time.Hour)
		return models.MonitoringSource{Metrics: []models.MonitoringMetric{{ID: "backlog", Count: &n, EvidenceAt: &evidence, ObservedAt: &at, Availability: models.MonitoringFresh, Condition: models.MonitoringBacklog}}}, nil
	}}})
	s.now = func() time.Time { return now }
	first := s.Snapshot(t.Context(), models.AdminPermViewAnalytics)
	fail = true
	now = now.Add(2 * time.Minute)
	next := s.Snapshot(t.Context(), models.AdminPermViewAnalytics)
	source := next.Sources[0]
	if source.Availability != models.MonitoringStale || source.Reason != "query_failed" || !source.ObservedAt.Equal(*first.Sources[0].ObservedAt) || *source.Metrics[0].EvidenceAgeSeconds != 3720 || source.Metrics[0].Availability != models.MonitoringStale {
		t.Fatal(next)
	}
	if first.Sources[0].Metrics[0].Availability != models.MonitoringFresh {
		t.Fatal("previous response was mutated")
	}
	now = now.Add(4 * time.Minute)
	expired := s.Snapshot(t.Context(), models.AdminPermViewAnalytics)
	if expired.Sources[0].Availability != models.MonitoringUnavailable || expired.Sources[0].ObservedAt != nil || len(expired.Sources[0].Metrics) != 0 {
		t.Fatal(expired)
	}
	raw, _ := json.Marshal(expired)
	if strings.Contains(string(raw), "sensitive-user") || strings.Contains(string(raw), "customer@") || strings.Contains(string(raw), "private.host") {
		t.Fatal("raw dependency error leaked")
	}
}

func TestMonitoringPermissionsNilPanicAndErrorReasons(t *testing.T) {
	n := int64(123)
	s := New([]Source{
		{ID: "mailboxes", Permission: models.AdminPermViewUsers, Collect: func(context.Context, time.Time) (models.MonitoringSource, error) {
			return models.MonitoringSource{Metrics: []models.MonitoringMetric{{ID: "private", ScopeID: "private-worker-id", Count: &n, Availability: models.MonitoringFresh}}}, nil
		}},
		{ID: "nil", Permission: models.AdminPermViewAnalytics},
		{ID: "panic", Permission: models.AdminPermViewAnalytics, Collect: func(context.Context, time.Time) (models.MonitoringSource, error) { panic("secret") }},
		{ID: "denied", Permission: models.AdminPermViewAnalytics, Collect: func(context.Context, time.Time) (models.MonitoringSource, error) {
			return models.MonitoringSource{}, &pgconn.PgError{Code: "42501", Message: "secret"}
		}},
	})
	out := s.Snapshot(t.Context(), models.AdminPermViewAnalytics)
	for i, reason := range []string{"permission_denied", "dependency_missing", "collection_failed", "permission_denied"} {
		if out.Sources[i].Reason != reason || out.Sources[i].Availability != models.MonitoringUnavailable || len(out.Sources[i].Metrics) != 0 {
			t.Fatal(out.Sources[i])
		}
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "private-worker") || strings.Contains(string(raw), "123") || strings.Contains(string(raw), "secret") {
		t.Fatal("restricted evidence leaked")
	}
	admin := s.Snapshot(t.Context(), models.AdminPermViewAnalytics|models.AdminPermViewUsers)
	if *admin.Sources[0].Metrics[0].Count != 123 {
		t.Fatal("permission projection contaminated cache")
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, &pgconn.PgError{Code: "57014"}} {
		if ErrorReason(err) != "timeout" {
			t.Fatal(err)
		}
	}
	var absent *Service
	if absent.Snapshot(t.Context(), models.AdminPermViewAnalytics).Coverage != "unavailable" {
		t.Fatal("nil service is healthy")
	}
}

func TestMonitoringCallerCancellationAndPartialCoverage(t *testing.T) {
	s := New([]Source{{ID: "partial", Permission: models.AdminPermViewAnalytics, Collect: func(ctx context.Context, _ time.Time) (models.MonitoringSource, error) {
		<-ctx.Done()
		return models.MonitoringSource{}, ctx.Err()
	}}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start := time.Now()
	out := s.Snapshot(ctx, models.AdminPermViewAnalytics)
	if time.Since(start) > time.Second || out.Coverage != "unavailable" || out.Sources[0].Reason != "timeout" {
		t.Fatal(out)
	}
	s.mu.Lock()
	ready := s.refreshing
	s.mu.Unlock()
	<-ready
	out = s.Snapshot(t.Context(), models.AdminPermViewAnalytics)
	if out.Sources[0].ObservedAt != nil {
		t.Fatal("timeout observation became fresh")
	}
	partial := New([]Source{{ID: "partial", Permission: models.AdminPermViewAnalytics, Collect: func(context.Context, time.Time) (models.MonitoringSource, error) {
		return models.MonitoringSource{Metrics: []models.MonitoringMetric{{ID: "absent", Availability: models.MonitoringUnavailable, Count: nil, Reason: "no_recent_evidence"}}}, nil
	}}})
	if out := partial.Snapshot(t.Context(), models.AdminPermViewAnalytics); out.Coverage == "complete" || out.Sources[0].Coverage != "partial" {
		t.Fatal(out)
	}
}

func TestMonitoringMetricJSONKeepsUnknownMeasurementsNull(t *testing.T) {
	raw, err := json.Marshal(models.MonitoringMetric{ID: "unknown", Availability: models.MonitoringUnavailable})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"count", "affected_mailboxes", "affected_organizations", "evidence_at", "evidence_age_seconds"} {
		if !strings.Contains(string(raw), `"`+field+`":null`) {
			t.Fatal(string(raw))
		}
	}
}
