// Package jobrun runs a background loop and records what it did, so the admin
// panel can list every scheduled job on the instance with its last run, its
// next run and its last error, and ask one to run now.
//
// Every service that hosts loops (backend, consumer) calls Configure once at
// boot with the store and its own name, then wraps each loop in Loop. Without
// a store the loops still run; nothing is recorded and "run now" has nowhere
// to land.
package jobrun

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// Store persists job runs. Implemented by repository.JobRunRepository.
type Store interface {
	// Register creates or refreshes the job's row at boot.
	Register(ctx context.Context, name, service string, interval time.Duration, nextRunAt time.Time) error
	MarkStarted(ctx context.Context, name string, at time.Time) error
	MarkFinished(ctx context.Context, name string, startedAt, finishedAt time.Time, runErr error, nextRunAt time.Time) error
	// RequestRun asks the owning loop to run at its next poll. False when no
	// job by that name has ever registered.
	RequestRun(ctx context.Context, name string) (bool, error)
	// TakeRunRequest clears a pending request and reports whether there was one.
	TakeRunRequest(ctx context.Context, name string) (bool, error)
	List(ctx context.Context) ([]models.ScheduledJobRun, error)
}

// requestPoll is how often a loop checks for a "run now" request.
const requestPoll = 15 * time.Second

var (
	mu      sync.RWMutex
	store   Store
	service string
)

// Configure sets the store every loop in this process records to and the
// name of this service. Safe to call once; later calls replace both.
func Configure(s Store, serviceName string) {
	mu.Lock()
	defer mu.Unlock()
	store = s
	service = serviceName
}

func current() (Store, string) {
	mu.RLock()
	defer mu.RUnlock()
	return store, service
}

// Loop runs fn every interval until ctx ends, records each run, and also runs
// fn when the panel requests it. runOnBoot runs fn once before the first tick,
// which is what most retention and reconcile loops want.
func Loop(ctx context.Context, name string, interval time.Duration, runOnBoot bool, fn func(ctx context.Context) error) {
	if interval <= 0 {
		interval = time.Minute
	}
	st, svc := current()
	if st != nil {
		next := time.Now().Add(interval)
		if runOnBoot {
			next = time.Now()
		}
		if err := st.Register(ctx, name, svc, interval, next); err != nil {
			log.Warn().Err(err).Str("job", name).Msg("jobrun: register failed")
		}
	}

	run := func() {
		Run(ctx, name, interval, fn)
	}

	if runOnBoot {
		run()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	poll := time.NewTicker(requestPoll)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		case <-poll.C:
			if st == nil {
				continue
			}
			requested, err := st.TakeRunRequest(ctx, name)
			if err != nil {
				log.Warn().Err(err).Str("job", name).Msg("jobrun: poll failed")
				continue
			}
			if requested {
				run()
			}
		}
	}
}

// Run executes fn once and records it. For loops that keep their own ticker.
func Run(ctx context.Context, name string, interval time.Duration, fn func(ctx context.Context) error) {
	st, _ := current()
	started := time.Now()
	if st != nil {
		if err := st.MarkStarted(ctx, name, started); err != nil {
			log.Warn().Err(err).Str("job", name).Msg("jobrun: mark started failed")
		}
	}
	runErr := safeRun(ctx, name, fn)
	if runErr != nil {
		errs.CaptureException(runErr)
		log.Warn().Err(runErr).Str("job", name).Msg("scheduled job failed")
	}
	if st != nil {
		finished := time.Now()
		if err := st.MarkFinished(ctx, name, started, finished, runErr, finished.Add(interval)); err != nil {
			log.Warn().Err(err).Str("job", name).Msg("jobrun: mark finished failed")
		}
	}
}

// safeRun turns a panic in a job into an error so one bad pass cannot take
// the service down.
func safeRun(ctx context.Context, name string, fn func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &panicError{job: name, value: r}
		}
	}()
	return fn(ctx)
}

type panicError struct {
	job   string
	value any
}

func (p *panicError) Error() string {
	return "job " + p.job + " panicked"
}
