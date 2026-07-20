// Package tasksched abstracts delayed task scheduling so the platform can run
// with no cloud account. A task's schedule already lives in the Postgres
// `tasks` table (status + scheduled_at); the Scheduler only decides how a due
// task gets fired.
//
//   - Local  (default, TASKS_PROVIDER=local): an in-process poller reads due
//     rows from Postgres and dispatches them directly. No external queue, no
//     webhook, no GCP.
//   - gtasks.Client (TASKS_PROVIDER=gcloud): Google Cloud Tasks POSTs a webhook
//     back at scheduled_at. The historical behavior, still available.
package tasksched

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// Scheduler enqueues a task to fire at scheduleTime and can cancel it. It is
// satisfied structurally by both *gtasks.Client and *Local, so the app services
// depend only on this interface.
type Scheduler interface {
	// CreateTask registers taskData to fire at scheduleTime and returns an
	// opaque handle used by DeleteTask. The caller has already written the
	// `tasks` row; the local provider treats this as a no-op enqueue.
	CreateTask(ctx context.Context, taskData *proto.ProcessTask, scheduleTime time.Time) (string, error)

	// DeleteTask cancels a previously created task by its handle. Cancellation
	// is best-effort: callers flip the DB row's status first, so the local
	// provider treats this as a no-op.
	DeleteTask(ctx context.Context, name string) error
}
