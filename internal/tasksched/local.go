package tasksched

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// DueTaskLister returns the ids of pending tasks whose scheduled_at has passed.
// repository.TaskRepository satisfies it.
type DueTaskLister interface {
	ListDuePendingTaskIDs(ctx context.Context, limit int) ([]uuid.UUID, error)
}

// Local is the no-cloud Scheduler. Task rows already exist in Postgres (the
// caller inserts them before CreateTask), and Run polls for due rows and fires
// them in-process. Postgres is both the store and the clock.
type Local struct {
	repo     DueTaskLister
	interval time.Duration
	batch    int
	inflight sync.Map // taskID string -> struct{}, dedups across overlapping ticks
}

// NewLocal builds a Local poller. interval<=0 defaults to 1s; batch<=0 to 200.
func NewLocal(repo DueTaskLister, interval time.Duration, batch int) *Local {
	if interval <= 0 {
		interval = time.Second
	}
	if batch <= 0 {
		batch = 200
	}
	return &Local{repo: repo, interval: interval, batch: batch}
}

// CreateTask is a no-op enqueue: the row already exists with status=pending and
// scheduled_at, which the poller picks up at its slot. The returned handle is a
// marker; the local provider never needs to look a task up by it.
func (l *Local) CreateTask(_ context.Context, taskData *proto.ProcessTask, _ time.Time) (string, error) {
	return "local:" + taskData.TaskId, nil
}

// DeleteTask is a no-op: cancellation flips the DB row's status first, and the
// poller only fires rows that are still pending.
func (l *Local) DeleteTask(_ context.Context, _ string) error { return nil }

// Run polls for due tasks until ctx is cancelled, invoking handle(taskID) for
// each. handle must be idempotent: rows stay pending until handle flips their
// status, and the per-type handlers short-circuit on a non-pending status, so a
// row re-selected before handle finished is a no-op. The in-flight guard keeps
// a single node from re-dispatching a task it is already handling.
func (l *Local) Run(ctx context.Context, handle func(taskID string)) {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	log.Info().Dur("interval", l.interval).Int("batch", l.batch).Msg("tasksched: local dispatcher started")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.tick(ctx, handle)
		}
	}
}

func (l *Local) tick(ctx context.Context, handle func(taskID string)) {
	ids, err := l.repo.ListDuePendingTaskIDs(ctx, l.batch)
	if err != nil {
		log.Error().Err(err).Msg("tasksched: list due tasks")
		return
	}
	for _, id := range ids {
		key := id.String()
		if _, busy := l.inflight.LoadOrStore(key, struct{}{}); busy {
			continue
		}
		go func(taskID string) {
			defer l.inflight.Delete(taskID)
			handle(taskID)
		}(key)
	}
}
