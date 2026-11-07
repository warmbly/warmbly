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
	"errors"
	"hash/fnv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// Store persists job runs. Implemented by repository.JobRunRepository.
type Store interface {
	// Register creates or refreshes the job's row at boot and returns when it
	// is due: a stored due time clamped to [earliest, nextRunAt], else nextRunAt.
	Register(ctx context.Context, name, service string, interval time.Duration, nextRunAt, earliest time.Time) (time.Time, error)
	// Claim advances the stored due time from due to next. False with the
	// stored due time when another instance already took the slot.
	Claim(ctx context.Context, name string, due, next time.Time) (bool, time.Time, error)
	MarkStarted(ctx context.Context, name string, at time.Time) error
	MarkFinished(ctx context.Context, name string, startedAt, finishedAt time.Time, runErr error) error
	// RequestRun asks the owning loop to run at its next poll. False when no
	// job by that name has ever registered.
	RequestRun(ctx context.Context, name string) (bool, error)
	// TakeRunRequests clears the pending requests among names and returns the jobs that had one.
	TakeRunRequests(ctx context.Context, names []string) ([]string, error)
	List(ctx context.Context) ([]models.ScheduledJobRun, error)
}

// requestPoll is how often a process checks for "run now" requests, in one query for all its loops.
const requestPoll = 15 * time.Second

// phaseWindow is how far apart loops that share an interval are pulled. Every
// loop registers in the same instant at boot, so without an offset the nine
// hourly jobs keep firing together for the life of the process: seven backend
// jobs opened a database connection in one second and the server refused all
// of them at once. The offset is derived from the job's name, so it is the
// same on every restart and a job keeps its slot rather than shuffling.
const phaseWindow = 45 * time.Second

// phaseOffset is the job's fixed place inside the window, never past its own
// interval so a frequent job is not held longer than its period.
func phaseOffset(name string, interval time.Duration) time.Duration {
	window := phaseWindow
	if interval < window {
		window = interval
	}
	ms := window.Milliseconds()
	if ms <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return time.Duration(int64(h.Sum32())%ms) * time.Millisecond
}

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
//
// The due time is persisted, so a restart resumes the schedule instead of
// starting a fresh interval, and an overdue job runs right after its offset.
func Loop(ctx context.Context, name string, interval time.Duration, runOnBoot bool, fn func(ctx context.Context) error) {
	if interval <= 0 {
		interval = time.Minute
	}
	offset := phaseOffset(name, interval)

	// The offset counts toward the first run whether or not it is at boot,
	// so every job sharing an interval stays in its own slot.
	earliest := time.Now().Add(offset).Truncate(time.Microsecond)
	due := earliest.Add(interval)
	if runOnBoot {
		due = earliest
	}

	st, svc := current()
	if st != nil {
		stored, err := st.Register(ctx, name, svc, interval, due, earliest)
		if err != nil {
			log.Warn().Err(err).Str("job", name).Msg("jobrun: register failed")
		} else if !stored.IsZero() {
			due = stored
		}
	}

	timer := time.NewTimer(time.Until(due))
	defer timer.Stop()
	requests := watchRequests(name)
	defer unwatchRequests(name, requests)
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			next := nextDue(due, interval, time.Now())
			claimed := true
			if st != nil {
				won, stored, err := st.Claim(ctx, name, due, next)
				switch {
				case ctx.Err() != nil:
					return
				case err != nil:
					log.Warn().Err(err).Str("job", name).Msg("jobrun: claim failed")
				case !won:
					claimed, next = false, stored
				}
			}
			due = next
			if claimed {
				Run(ctx, name, fn)
			}
			timer.Reset(time.Until(due))
		case <-requests:
			Run(ctx, name, fn)
		}
	}
}

var (
	watchMu    sync.Mutex
	watchers   = map[string][]chan struct{}{}
	pollerStop chan struct{}
)

// watchRequests registers a loop for run requests, starting the process's poller with the first one.
func watchRequests(name string) chan struct{} {
	ch := make(chan struct{}, 1)
	watchMu.Lock()
	defer watchMu.Unlock()
	watchers[name] = append(watchers[name], ch)
	if pollerStop == nil {
		pollerStop = make(chan struct{})
		go pollRequests(pollerStop)
	}
	return ch
}

// unwatchRequests removes a loop; the poller stops with the last one.
func unwatchRequests(name string, ch chan struct{}) {
	watchMu.Lock()
	defer watchMu.Unlock()
	list := watchers[name]
	for i, c := range list {
		if c == ch {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(watchers, name)
	} else {
		watchers[name] = list
	}
	if len(watchers) == 0 && pollerStop != nil {
		close(pollerStop)
		pollerStop = nil
	}
}

// pollRequests takes every pending request for this process's loops in one query per poll.
func pollRequests(stop chan struct{}) {
	ticker := time.NewTicker(requestPoll)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			takeRequests(context.Background())
		}
	}
}

func takeRequests(ctx context.Context) {
	st, _ := current()
	if st == nil {
		return
	}
	watchMu.Lock()
	names := make([]string, 0, len(watchers))
	for name := range watchers {
		names = append(names, name)
	}
	watchMu.Unlock()
	if len(names) == 0 {
		return
	}
	pollCtx, cancel := context.WithTimeout(ctx, requestPoll)
	defer cancel()
	taken, err := st.TakeRunRequests(pollCtx, names)
	if err != nil {
		log.Warn().Err(err).Msg("jobrun: run request poll failed")
		return
	}
	var orphaned []string
	watchMu.Lock()
	for _, name := range taken {
		if len(watchers[name]) == 0 {
			orphaned = append(orphaned, name)
		}
		for _, ch := range watchers[name] {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
	watchMu.Unlock()
	// The loop stopped here after the poll began; the request goes back for whichever process runs it now.
	for _, name := range orphaned {
		handCtx, cancelHand := context.WithTimeout(ctx, requestPoll)
		_, err := st.RequestRun(handCtx, name)
		cancelHand()
		if err != nil {
			log.Warn().Err(err).Str("job", name).Msg("jobrun: could not hand a run request back")
		}
	}
}

// nextDue is the first slot after at on due's cadence; slots missed while a
// run overran are dropped, as a ticker drops ticks.
func nextDue(due time.Time, interval time.Duration, at time.Time) time.Time {
	next := due.Add(interval)
	if !next.After(at) {
		next = due.Add((at.Sub(due)/interval + 1) * interval)
	}
	return next.Truncate(time.Microsecond)
}

// Run executes fn once and records it without moving the schedule. For runs
// outside the loop's own cadence, such as a wake.
func Run(ctx context.Context, name string, fn func(ctx context.Context) error) {
	st, _ := current()
	started := time.Now()
	if st != nil {
		if err := st.MarkStarted(ctx, name, started); err != nil {
			log.Warn().Err(err).Str("job", name).Msg("jobrun: mark started failed")
		}
	}
	runErr := safeRun(ctx, name, fn)
	if runErr != nil && !IsQuiet(runErr) {
		errs.CaptureException(runErr)
		log.Warn().Err(runErr).Str("job", name).Msg("scheduled job failed")
	}
	if st != nil {
		if err := st.MarkFinished(ctx, name, started, time.Now(), runErr); err != nil {
			log.Warn().Err(err).Str("job", name).Msg("jobrun: mark finished failed")
		}
	}
}

// Quiet marks an expected or already reported condition: the panel records it, but it is not logged or captured.
func Quiet(err error) error {
	if err == nil {
		return nil
	}
	return quietError{err}
}

// IsQuiet reports whether err was marked with Quiet.
func IsQuiet(err error) bool {
	var quiet quietError
	return errors.As(err, &quiet)
}

type quietError struct{ error }

func (q quietError) Unwrap() error { return q.error }

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
