package campaign

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestReadCacheGetOrComputeCoalesces proves that many concurrent callers hitting
// a cold cache for the same key share a single computation instead of each
// starting their own planner walk. It releases the shared compute only after
// every caller has actually parked on the flight (via onWait), so it exercises
// coalescing rather than the fast cache path.
func TestReadCacheGetOrComputeCoalesces(t *testing.T) {
	c := newReadCache[int](time.Minute)

	const callers = 32
	var calls int32
	release := make(chan struct{})
	waiting := make(chan struct{}, callers)
	c.onWait = func() { waiting <- struct{}{} }

	compute := func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		// Hold the flight open so every caller is parked on it at once.
		<-release
		return 42, nil
	}

	var wg sync.WaitGroup
	results := make([]int, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = c.getOrCompute(context.Background(), "k", compute)
		}(i)
	}

	// Release only once every caller has reached the wait point, so the result
	// is not yet cached and every caller genuinely coalesces onto the flight.
	for i := 0; i < callers; i++ {
		<-waiting
	}
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly one computation, got %d", got)
	}
	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatalf("caller %d returned error: %v", i, errs[i])
		}
		if results[i] != 42 {
			t.Fatalf("caller %d got %d, want 42", i, results[i])
		}
	}
}

// TestReadCacheGetOrComputeCachesResult proves a successful result is cached so
// a later caller is served without recomputing, and that a different key is
// computed independently.
func TestReadCacheGetOrComputeCachesResult(t *testing.T) {
	c := newReadCache[int](time.Minute)

	var calls int32
	compute := func(v int) func(context.Context) (int, error) {
		return func(context.Context) (int, error) {
			atomic.AddInt32(&calls, 1)
			return v, nil
		}
	}

	if v, err := c.getOrCompute(context.Background(), "a", compute(1)); err != nil || v != 1 {
		t.Fatalf("first call: v=%d err=%v", v, err)
	}
	if v, err := c.getOrCompute(context.Background(), "a", compute(99)); err != nil || v != 1 {
		t.Fatalf("second call should be cached: v=%d err=%v", v, err)
	}
	if v, err := c.getOrCompute(context.Background(), "b", compute(2)); err != nil || v != 2 {
		t.Fatalf("different key: v=%d err=%v", v, err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 computations (one per key), got %d", got)
	}
}

// TestReadCacheGetOrComputeDoesNotCacheErrors proves an error is not cached, so
// the next caller retries rather than being served a failure.
func TestReadCacheGetOrComputeDoesNotCacheErrors(t *testing.T) {
	c := newReadCache[int](time.Minute)
	sentinel := errors.New("boom")

	if _, err := c.getOrCompute(context.Background(), "k", func(context.Context) (int, error) { return 0, sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	v, err := c.getOrCompute(context.Background(), "k", func(context.Context) (int, error) { return 7, nil })
	if err != nil || v != 7 {
		t.Fatalf("retry after error should recompute: v=%d err=%v", v, err)
	}
}

// TestReadCacheGetOrComputeRecoversPanic proves a panic in compute is turned
// into an error and releases every waiter, not only the leader. It parks both a
// leader and a waiter on the flight (via onWait) before the compute panics.
func TestReadCacheGetOrComputeRecoversPanic(t *testing.T) {
	c := newReadCache[int](time.Minute)

	const callers = 2
	release := make(chan struct{})
	waiting := make(chan struct{}, callers)
	c.onWait = func() { waiting <- struct{}{} }

	compute := func(context.Context) (int, error) {
		// Hold until both callers have joined the flight, so the test proves
		// the panic releases the waiter and not only the leader.
		<-release
		panic("kaboom")
	}

	var wg sync.WaitGroup
	results := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, results[idx] = c.getOrCompute(context.Background(), "k", compute)
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	for i := 0; i < callers; i++ {
		<-waiting
	}
	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("getOrCompute deadlocked after compute panicked")
	}
	for i, err := range results {
		if err == nil {
			t.Errorf("caller %d: expected an error from a panicking compute", i)
		}
	}
}

// TestReadCacheGetOrComputeCanceledWaiterDoesNotAffectFlight proves a waiter
// whose own context is canceled returns promptly with its ctx.Err() while the
// shared walk carries on, caches its result, and runs exactly once.
func TestReadCacheGetOrComputeCanceledWaiterDoesNotAffectFlight(t *testing.T) {
	c := newReadCache[int](time.Minute)

	var calls int32
	release := make(chan struct{})
	started := make(chan struct{})
	compute := func(ctx context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		close(started)
		<-release
		// The shared walk's context must survive a waiter's cancellation.
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 7, nil
	}

	// The leader starts the flight and blocks inside compute.
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		v, err := c.getOrCompute(context.Background(), "k", compute)
		if err != nil || v != 7 {
			t.Errorf("leader: v=%d err=%v", v, err)
		}
	}()
	<-started

	// A waiter joins the live flight, then has its own context canceled. It must
	// return promptly with context.Canceled without starting a second compute.
	ctx, cancel := context.WithCancel(context.Background())
	waiterErr := make(chan error, 1)
	go func() {
		_, err := c.getOrCompute(ctx, "k", compute)
		waiterErr <- err
	}()
	cancel()

	select {
	case err := <-waiterErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter: want context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled waiter did not return promptly while the flight was in progress")
	}

	// The flight still runs; release it and confirm it completed and cached.
	close(release)
	<-leaderDone
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly one computation, got %d", got)
	}
	if v, ok := c.get("k"); !ok || v != 7 {
		t.Fatalf("flight result should be cached for other callers: v=%d ok=%v", v, ok)
	}
}

// A read that began before a forget does not store what it read.
func TestReadCacheForgetOutranksAnEarlierRead(t *testing.T) {
	c := newReadCache[int](time.Minute)
	gen := c.generation("org")
	c.forget("org")
	c.putAt("org", 1, gen)
	if _, ok := c.get("org"); ok {
		t.Fatal("a read from before the forget was stored")
	}
	c.putAt("org", 2, c.generation("org"))
	if v, ok := c.get("org"); !ok || v != 2 {
		t.Fatalf("got %d %v, want the read taken after the forget", v, ok)
	}
}
