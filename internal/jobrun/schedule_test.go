package jobrun

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// memStore mirrors the repository's scheduling SQL in memory.
type memStore struct {
	mu  sync.Mutex
	due map[string]time.Time
	// failClaims makes the next claims error; applyFailed decides whether the
	// write behind each one landed anyway.
	failClaims  int
	applyFailed bool
	requested   map[string]bool
	polls       int
	// onTake runs as a poll takes its requests, before they are delivered.
	onTake func()
}

func newMemStore() *memStore {
	return &memStore{due: map[string]time.Time{}, requested: map[string]bool{}}
}

func (m *memStore) Register(_ context.Context, name, _ string, _ time.Duration, next, earliest time.Time) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.due[name]
	if !ok {
		m.due[name] = next
		return next, nil
	}
	if next.Before(stored) {
		stored = next
	}
	if stored.Before(earliest) {
		stored = earliest
	}
	m.due[name] = stored
	return stored, nil
}

func (m *memStore) Claim(_ context.Context, name string, due, next time.Time) (bool, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.due[name]
	if m.failClaims > 0 {
		m.failClaims--
		if m.applyFailed && (!ok || stored.Equal(due)) {
			m.due[name] = next
		}
		return false, time.Time{}, errors.New("claim failed")
	}
	if !ok || stored.Equal(due) {
		m.due[name] = next
		return true, next, nil
	}
	return false, stored, nil
}

func (m *memStore) set(name string, due time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.due[name] = due.Truncate(time.Microsecond)
}

func (m *memStore) failNextClaim(applied bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failClaims, m.applyFailed = 1, applied
}

func (m *memStore) MarkStarted(context.Context, string, time.Time) error { return nil }
func (m *memStore) MarkFinished(context.Context, string, time.Time, time.Time, error) error {
	return nil
}
func (m *memStore) RequestRun(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requested[name] = true
	return true, nil
}
func (m *memStore) TakeRunRequests(_ context.Context, names []string) ([]string, error) {
	m.mu.Lock()
	m.polls++
	var taken []string
	for _, n := range names {
		if m.requested[n] {
			delete(m.requested, n)
			taken = append(taken, n)
		}
	}
	onTake := m.onTake
	m.mu.Unlock()
	if onTake != nil {
		onTake()
	}
	return taken, nil
}
func (m *memStore) List(context.Context) ([]models.ScheduledJobRun, error) {
	return nil, nil
}

func useStore(t *testing.T, s Store) {
	t.Helper()
	prev, prevSvc := current()
	Configure(s, "test")
	t.Cleanup(func() { Configure(prev, prevSvc) })
}

// startLoop runs a Loop that counts its runs and returns a stop function that
// waits for it to exit.
func startLoop(name string, interval time.Duration, runOnBoot bool, runs *atomic.Int32) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Loop(ctx, name, interval, runOnBoot, func(context.Context) error {
			runs.Add(1)
			return nil
		})
	}()
	return func() {
		cancel()
		<-done
	}
}

// Restarting more often than the interval used to push the first run back by
// a full interval every time, so the job never ran at all.
func TestLoopIsNotStarvedByRestartsShorterThanItsInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newMemStore()
		useStore(t, store)

		const name = "a"
		interval := time.Hour
		offset := phaseOffset(name, interval)
		restartEvery := 40 * time.Minute

		var runs atomic.Int32
		for i := 0; i < 6; i++ {
			stop := startLoop(name, interval, false, &runs)
			time.Sleep(restartEvery)
			stop()
		}
		// Four hours of restarts every 40 minutes hold four hourly slots.
		if got := runs.Load(); got < 3 {
			t.Fatalf("ran %d times across 6 restarts every %s at a %s interval (offset %s), want at least 3", got, restartEvery, interval, offset)
		}
	})
}

func TestLoopRunsAnOverdueJobRightAfterItsOffset(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newMemStore()
		useStore(t, store)

		const name = "overdue"
		interval := time.Hour
		store.set(name, time.Now().Add(-24*time.Hour))

		var runs atomic.Int32
		stop := startLoop(name, interval, false, &runs)
		defer stop()

		time.Sleep(phaseOffset(name, interval) + time.Millisecond)
		synctest.Wait()
		if runs.Load() != 1 {
			t.Fatalf("overdue job ran %d times right after its offset, want once", runs.Load())
		}
	})
}

func TestLoopWaitsOnlyTheRemainderOfAnInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newMemStore()
		useStore(t, store)

		const name = "remaining"
		interval := 24 * time.Hour
		remaining := 3 * time.Hour
		store.set(name, time.Now().Add(remaining))

		var runs atomic.Int32
		stop := startLoop(name, interval, false, &runs)
		defer stop()

		time.Sleep(remaining - time.Millisecond)
		synctest.Wait()
		if runs.Load() != 0 {
			t.Fatalf("ran before its stored due time")
		}
		time.Sleep(2 * time.Millisecond)
		synctest.Wait()
		if runs.Load() != 1 {
			t.Fatalf("ran %d times at its stored due time, want once without a fresh interval", runs.Load())
		}
	})
}

func TestLoopGivesANewJobItsFullFirstInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newMemStore()
		useStore(t, store)

		const name = "fresh"
		interval := time.Hour
		first := phaseOffset(name, interval) + interval

		var runs atomic.Int32
		stop := startLoop(name, interval, false, &runs)
		defer stop()

		time.Sleep(first - time.Millisecond)
		synctest.Wait()
		if runs.Load() != 0 {
			t.Fatalf("new job ran before its first interval")
		}
		time.Sleep(2 * time.Millisecond)
		synctest.Wait()
		if runs.Load() != 1 {
			t.Fatalf("new job ran %d times after its first interval, want once", runs.Load())
		}
	})
}

// Two processes hosting the same job share its slots instead of each running it.
func TestLoopRunsEachSlotOnceAcrossInstances(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newMemStore()
		useStore(t, store)

		const name = "shared"
		interval := time.Hour
		store.set(name, time.Now().Add(-time.Minute))

		var runs atomic.Int32
		stopA := startLoop(name, interval, false, &runs)
		stopB := startLoop(name, interval, false, &runs)
		// The overdue slot after the offset, then five hourly slots.
		time.Sleep(phaseOffset(name, interval) + 5*interval + interval/2)
		stopA()
		stopB()

		if got := runs.Load(); got != 6 {
			t.Fatalf("two instances ran %d times over six slots, want one run per slot", got)
		}
	})
}

// A claim whose outcome is unknown runs its slot once, and the next slot
// reconciles the store instead of running the earlier slot again.
func TestLoopRunsASlotOnceAfterAFailedClaim(t *testing.T) {
	for _, applied := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			store := newMemStore()
			useStore(t, store)

			const name = "flaky"
			interval := time.Hour
			store.set(name, time.Now().Add(time.Minute))
			store.failNextClaim(applied)

			var runs atomic.Int32
			stop := startLoop(name, interval, false, &runs)
			defer stop()

			time.Sleep(time.Minute + time.Millisecond)
			synctest.Wait()
			if runs.Load() != 1 {
				t.Fatalf("applied=%v: the slot with a failed claim ran %d times, want once", applied, runs.Load())
			}
			time.Sleep(3 * interval)
			synctest.Wait()
			if got := runs.Load(); got != 4 {
				t.Fatalf("applied=%v: ran %d times over four slots, want 4", applied, got)
			}
		})
	}
}

func TestNextDueKeepsTheCadenceAndDropsMissedSlots(t *testing.T) {
	due := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		at   time.Time
		want time.Time
	}{
		{due, due.Add(time.Hour)},
		{due.Add(10 * time.Minute), due.Add(time.Hour)},
		{due.Add(time.Hour), due.Add(2 * time.Hour)},
		{due.Add(150 * time.Minute), due.Add(3 * time.Hour)},
	}
	for _, c := range cases {
		if got := nextDue(due, time.Hour, c.at); !got.Equal(c.want) {
			t.Errorf("nextDue at %s = %s, want %s", c.at, got, c.want)
		}
	}
}

// A process asks for every loop's run requests in one query per poll, and only the requested loop runs.
func TestRunRequestsArePolledOncePerProcess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newMemStore()
		useStore(t, store)

		var wanted, other atomic.Int32
		stopWanted := startLoop("wanted", 24*time.Hour, false, &wanted)
		defer stopWanted()
		stopOther := startLoop("other", 24*time.Hour, false, &other)
		defer stopOther()

		if _, err := store.RequestRun(context.Background(), "wanted"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(requestPoll + time.Millisecond)
		synctest.Wait()

		if wanted.Load() != 1 || other.Load() != 0 {
			t.Fatalf("requested job ran %d times and the other %d, want 1 and 0", wanted.Load(), other.Load())
		}
		store.mu.Lock()
		polls := store.polls
		store.mu.Unlock()
		if polls != 1 {
			t.Fatalf("two loops polled for requests %d times in one interval, want 1", polls)
		}
	})
}

// A request taken just as its loop stopped here goes back to the store for the process that runs the loop now.
func TestRunRequestForAStoppedLoopIsHandedBack(t *testing.T) {
	store := newMemStore()
	useStore(t, store)
	ch := watchRequests("moved")
	store.requested["moved"] = true
	store.onTake = func() { unwatchRequests("moved", ch) }

	takeRequests(context.Background())

	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.requested["moved"] {
		t.Fatal("a run request taken for a loop that had stopped was lost")
	}
}
