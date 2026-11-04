package tasksched

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// DueTaskLister claims pending tasks whose slot has arrived.
// repository.TaskRepository satisfies it.
type DueTaskLister interface {
	ClaimDueTaskIDs(ctx context.Context, limit int, lease time.Duration) ([]uuid.UUID, error)
	RecordDispatchAttempt(ctx context.Context, taskID uuid.UUID, failed bool, retryAt time.Time) error
	ReleaseDispatchClaims(ctx context.Context, taskIDs []uuid.UUID) error
}

const (
	dispatchRetryDelay = 5 * time.Minute
	// dispatchClaimLease is how long a replica that died mid-task holds it from the others.
	dispatchClaimLease      = 2 * time.Minute
	defaultLocalConcurrency = 8
)

type LocalOption func(*Local)

func WithPoolCapacity(maxConns int) LocalOption {
	return WithConcurrency(max(1, min(defaultLocalConcurrency, maxConns/3)))
}

func WithConcurrency(limit int) LocalOption {
	return func(l *Local) {
		if limit > 0 {
			l.slots = make(chan struct{}, limit)
		}
	}
}

// Local is the no-cloud Scheduler. Task rows already exist in Postgres (the
// caller inserts them before CreateTask), and Run polls for due rows and fires
// them in-process. Postgres is both the store and the clock.
type Local struct {
	repo     DueTaskLister
	interval time.Duration
	batch    int
	slots    chan struct{}
	inflight sync.Map // taskID string -> struct{}, dedups across overlapping ticks
}

// NewLocal builds a Local poller. interval<=0 defaults to 1s; batch<=0 to 200.
func NewLocal(repo DueTaskLister, interval time.Duration, batch int, options ...LocalOption) *Local {
	if interval <= 0 {
		interval = time.Second
	}
	if batch <= 0 {
		batch = 200
	}
	l := &Local{repo: repo, interval: interval, batch: batch, slots: make(chan struct{}, defaultLocalConcurrency)}
	for _, option := range options {
		option(l)
	}
	return l
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

// Run claims due tasks until ctx is cancelled, invoking handle(taskID) for
// each. A claim keeps other replicas off a task while it runs, but handle must
// still be idempotent: a lapsed lease, or a replica on an older release that
// does not claim, can fire the same row again, and the per-type handlers
// short-circuit on a non-pending status. The in-flight guard keeps a single
// node from re-dispatching a task it is already handling.
func (l *Local) Run(ctx context.Context, handle func(taskID string) error) {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	log.Info().Dur("interval", l.interval).Int("batch", l.batch).Int("concurrency", cap(l.slots)).Msg("tasksched: local dispatcher started")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.tick(ctx, handle)
		}
	}
}

func (l *Local) tick(ctx context.Context, handle func(taskID string) error) {
	free := cap(l.slots) - len(l.slots)
	if ctx.Err() != nil || free <= 0 {
		return
	}
	ids, err := l.repo.ClaimDueTaskIDs(ctx, min(l.batch, free), dispatchClaimLease)
	if err != nil {
		log.Error().Err(err).Msg("tasksched: claim due tasks")
		return
	}
	for i, id := range ids {
		if ctx.Err() != nil {
			l.release(ids[i:])
			return
		}
		select {
		case l.slots <- struct{}{}:
		default:
			l.release(ids[i:])
			return
		}
		key := id.String()
		// Still running here from an earlier claim; the new lease covers it.
		if _, busy := l.inflight.LoadOrStore(key, struct{}{}); busy {
			<-l.slots
			continue
		}
		go func(taskID string) {
			defer func() { <-l.slots }()
			defer l.inflight.Delete(taskID)
			if ctx.Err() != nil {
				l.release([]uuid.UUID{id})
				return
			}
			err := handle(taskID)
			if ctx.Err() != nil {
				l.release([]uuid.UUID{id})
				return
			}
			if saveErr := l.repo.RecordDispatchAttempt(ctx, id, err != nil, time.Now().Add(dispatchRetryDelay)); saveErr != nil {
				log.Error().Err(saveErr).Msg("tasksched: record dispatch retry")
			}
		}(key)
	}
}

// release hands claims back so another replica need not wait out the lease, even during shutdown.
func (l *Local) release(ids []uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.repo.ReleaseDispatchClaims(ctx, ids); err != nil {
		log.Warn().Err(err).Msg("tasksched: release dispatch claims")
	}
}
