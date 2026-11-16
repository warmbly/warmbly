package campaign

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// readCacheComputeTimeout bounds a shared walk so a wedged planner read cannot
// hold the flight (and every waiter) open forever. It is deliberately
// independent of any caller's context: one viewer disconnecting never cancels
// the walk nor turns its result into a cancellation error for the rest.
const readCacheComputeTimeout = 30 * time.Second

// readCache holds a derived read for a few seconds. Both the send plan and
// the workspace capacity walk every mailbox (and the plan every lead) through
// the scheduler's gates, and both are polled by every open dashboard tab, so
// two viewers of one campaign share one computation instead of doubling it.
// The clock is the only input a tick would change, and it does not change
// inside the window.
type readCache[T any] struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]cacheEntry[T]
	// gens counts forgets per key, so a read that started before one cannot
	// store what it read after it.
	gens map[string]uint64
	// flights coalesces a cold or expired key: the first miss computes while
	// every concurrent miss for the same key waits on its result, so a large
	// campaign is planned once per version rather than once per viewer.
	flights map[string]*cacheFlight[T]
	// onWait, when set, is called by a caller immediately before it blocks on a
	// flight. Tests use it to synchronize at the wait point; nil in production.
	onWait func()
}

type cacheEntry[T any] struct {
	v   T
	exp time.Time
}

// cacheFlight is one in-flight computation; waiters read v/err only after done
// is closed, which happens-before makes the unlocked writes safe. The compute
// runs in its own goroutine on a bounded, caller-independent context, so a
// waiter leaving never cancels or affects it.
type cacheFlight[T any] struct {
	done chan struct{}
	v    T
	err  error
}

func newReadCache[T any](ttl time.Duration) *readCache[T] {
	return &readCache[T]{
		ttl:     ttl,
		m:       map[string]cacheEntry[T]{},
		flights: map[string]*cacheFlight[T]{},
	}
}

// get returns the cached value while it is fresh.
func (c *readCache[T]) get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.exp) {
		var zero T
		return zero, false
	}
	return e.v, true
}

// forget drops key, so the next read computes it again.
func (c *readCache[T]) forget(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
	if c.gens == nil {
		c.gens = map[string]uint64{}
	}
	c.gens[key]++
}

// generation is key's forget count, taken before a read that putAt stores.
func (c *readCache[T]) generation(key string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gens[key]
}

// putAt stores v unless key was forgotten since gen was taken.
func (c *readCache[T]) putAt(key string, v T, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gens[key] != gen {
		return
	}
	c.putLocked(key, v)
}

// putLocked stores v and sweeps expired entries so a long-lived process does
// not keep every campaign it ever planned. Caller holds c.mu.
func (c *readCache[T]) putLocked(key string, v T) {
	now := time.Now()
	for k, e := range c.m {
		if now.After(e.exp) {
			delete(c.m, k)
		}
	}
	c.m[key] = cacheEntry[T]{v: v, exp: now.Add(c.ttl)}
}

// getOrCompute returns the fresh cached value, or runs compute exactly once for
// a cold/expired key while concurrent callers for that key wait on the same
// result. A successful result is cached under the TTL; an error is not cached,
// so the next caller retries. Keying stays the caller's responsibility, so an
// edit or start/stop that changes the key is still answered by a fresh walk.
//
// compute runs in its own goroutine on a bounded context derived from the
// leader's ctx with cancellation stripped, so the shared walk is never tied to
// any one caller: a caller whose own ctx is canceled returns its ctx.Err()
// promptly while the walk carries on and still caches its result for the rest.
func (c *readCache[T]) getOrCompute(ctx context.Context, key string, compute func(context.Context) (T, error)) (T, error) {
	c.mu.Lock()
	if e, ok := c.m[key]; ok && !time.Now().After(e.exp) {
		v := e.v
		c.mu.Unlock()
		return v, nil
	}
	f, ok := c.flights[key]
	if !ok {
		f = &cacheFlight[T]{done: make(chan struct{})}
		c.flights[key] = f
		go c.runFlight(ctx, key, f, compute)
	}
	c.mu.Unlock()

	if c.onWait != nil {
		c.onWait()
	}
	select {
	case <-f.done:
		return f.v, f.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// runFlight computes the value for one flight on a context that outlives every
// caller, records it, caches a success, and releases the waiters.
func (c *readCache[T]) runFlight(callerCtx context.Context, key string, f *cacheFlight[T], compute func(context.Context) (T, error)) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(callerCtx), readCacheComputeTimeout)
	defer cancel()

	// safeCompute turns a panic into an error so waiters are always released.
	v, err := c.safeCompute(ctx, compute)

	c.mu.Lock()
	if c.flights[key] == f {
		delete(c.flights, key)
	}
	if err == nil {
		c.putLocked(key, v)
	}
	c.mu.Unlock()

	f.v, f.err = v, err
	close(f.done)
}

func (c *readCache[T]) safeCompute(ctx context.Context, compute func(context.Context) (T, error)) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			var zero T
			v, err = zero, fmt.Errorf("read cache: compute panicked: %v", r)
		}
	}()
	return compute(ctx)
}
