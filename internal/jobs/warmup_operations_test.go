package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/opsnotify"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

type warmupMonitorStore struct {
	loading, overdue []repository.MailboxLoadingIncident
	err              error
}

func (s *warmupMonitorStore) ListMailboxLoadingIncidents(context.Context, time.Time) ([]repository.MailboxLoadingIncident, error) {
	return s.loading, s.err
}
func (s *warmupMonitorStore) ListOverdueWarmupDispatches(context.Context, time.Time) ([]repository.MailboxLoadingIncident, error) {
	return s.overdue, s.err
}

type warmupMonitorNotifier struct {
	keys   []string
	fields []map[string]string
}

func (n *warmupMonitorNotifier) NotifyOperator(key, title, summary string, fields map[string]string) {
	n.keys = append(n.keys, key)
	n.fields = append(n.fields, fields)
}

func TestWarmupOperationsWorkerFleetAndDedup(t *testing.T) {
	now := time.Now()
	workerA, workerB, org := uuid.New(), uuid.New(), uuid.New()
	incident := func(worker *uuid.UUID) repository.MailboxLoadingIncident {
		return repository.MailboxLoadingIncident{AccountID: uuid.New(), OrganizationID: org, WorkerID: worker, FirstFailureAt: now.Add(-2 * time.Hour), LastFailureAt: now.Add(-time.Minute)}
	}
	for _, tc := range []struct {
		name    string
		rows    []repository.MailboxLoadingIncident
		scope   string
		workers string
	}{
		{"shared worker", []repository.MailboxLoadingIncident{incident(&workerA), incident(&workerA)}, workerA.String(), "1"},
		{"unassigned", []repository.MailboxLoadingIncident{incident(nil)}, "unassigned", "0"},
		{"fleet", []repository.MailboxLoadingIncident{incident(&workerA), incident(&workerA), incident(&workerA), incident(&workerA), incident(&workerA), incident(&workerB), incident(&workerB), incident(&workerB), incident(&workerB), incident(&workerB)}, "fleet", "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, notifier := &warmupMonitorStore{loading: tc.rows}, &warmupMonitorNotifier{}
			job := NewWarmupOperationsJob(store, store, nil, notifier)
			seen := make(map[string]bool)
			job.claim = func(_ context.Context, key string) (bool, error) {
				if seen[key] {
					return false, nil
				}
				seen[key] = true
				return true, nil
			}
			job.clock = func() time.Time { return now }
			reports := 0
			job.report = func(err error, _ ...errs.Option) {
				reports++
				if err.Error() != "Persistent warmup worker-loading failures" {
					t.Fatalf("unsanitized exception: %v", err)
				}
			}
			for range 2 {
				if err := job.Run(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if len(notifier.keys) != 1 || reports != 1 || notifier.keys[0] != opsnotify.EventWarmupLoading {
				t.Fatalf("alerts=%v reports=%d", notifier.keys, reports)
			}
			fields := notifier.fields[0]
			if fields["Scope"] != tc.scope || fields["Workers"] != tc.workers || fields["Workspaces"] != "1" {
				t.Fatalf("bad aggregate: %v", fields)
			}
			store.loading = nil
			if err := job.Run(context.Background()); err != nil || reports != 1 {
				t.Fatalf("recovered incidents still alert: %v", err)
			}
		})
	}
}

func TestWarmupOperationsOverdueAndCooldownFailure(t *testing.T) {
	now := time.Now()
	store := &warmupMonitorStore{overdue: []repository.MailboxLoadingIncident{{AccountID: uuid.New(), OrganizationID: uuid.New(), FirstFailureAt: now.Add(-2 * time.Hour), LastFailureAt: now}}}
	notifier := &warmupMonitorNotifier{}
	job := NewWarmupOperationsJob(store, store, nil, notifier)
	job.claim = func(context.Context, string) (bool, error) { return false, errors.New("cooldown unavailable") }
	job.report = func(error, ...errs.Option) { t.Fatal("must not emit undeduplicated errors") }
	if err := job.Run(context.Background()); err == nil || len(notifier.keys) != 0 {
		t.Fatal("cooldown outage must fail closed")
	}
	job.claim = func(context.Context, string) (bool, error) { return true, nil }
	job.report = func(err error, _ ...errs.Option) {
		if err.Error() != "Warmup task dispatch is overdue" {
			t.Fatalf("wrong evidence: %v", err)
		}
	}
	if err := job.Run(context.Background()); err != nil || len(notifier.keys) != 1 || notifier.keys[0] != opsnotify.EventWarmupDispatchOverdue {
		t.Fatalf("missing overdue alert: %v %v", notifier.keys, err)
	}
	store.err = errors.New("sensitive connection details")
	if err := job.Run(context.Background()); err == nil || err.Error() != "warmup loading monitor query failed" {
		t.Fatalf("query error was not sanitized: %v", err)
	}
}
