package typesafe

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func liveBillingRedis(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("TYPESAFE_TEST_REDIS_URL")
	if url == "" {
		t.Skip("set TYPESAFE_TEST_REDIS_URL for shared billing-gate checks")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	return client
}

func TestLiveSharedBillingGate(t *testing.T) {
	r := liveBillingRedis(t)
	key := uuid.NewString()
	a, b := NewClient(key, WithBillingRedis(r)), NewClient(key, WithBillingRedis(r))
	ctx := context.Background()
	t.Cleanup(func() { _ = r.Del(ctx, a.billing.key).Err() })
	a.billing.finish(ctx, billingPermit{shared: true}, &APIError{Status: http.StatusPaymentRequired})
	if _, err := b.billing.acquire(ctx); !errors.Is(err, ErrBillingCooldown) {
		t.Fatal("another process ignored the billing pause", err)
	}
	other := NewClient(uuid.NewString(), WithBillingRedis(r))
	if _, err := other.billing.acquire(ctx); err != nil {
		t.Fatal("a different API key was paused", err)
	}
	if err := r.HSet(ctx, a.billing.key, "until", 1).Err(); err != nil {
		t.Fatal(err)
	}
	var admitted atomic.Int64
	var permits sync.Map
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p, err := b.billing.acquire(ctx); err == nil {
				admitted.Add(1)
				permits.Store("probe", p)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("%d shared recovery probes", admitted.Load())
	}
	p, _ := permits.Load("probe")
	b.billing.finish(ctx, p.(billingPermit), nil)
	if exists, err := r.Exists(ctx, a.billing.key).Result(); err != nil || exists != 0 {
		t.Fatal("successful probe did not clear the shared pause", err)
	}
	if _, err := b.billing.acquire(ctx); err != nil {
		t.Fatal("recovered key stayed paused", err)
	}
}

func TestLiveSharedRecoveryOverridesLocalPause(t *testing.T) {
	r := liveBillingRedis(t)
	key := uuid.NewString()
	a, b := NewClient(key, WithBillingRedis(r)), NewClient(key, WithBillingRedis(r))
	ctx := context.Background()
	t.Cleanup(func() { _ = r.Del(ctx, a.billing.key).Err() })
	localNow := time.Now()
	a.billing.now = func() time.Time { return localNow }
	a.billing.finish(ctx, billingPermit{shared: true}, &APIError{Status: 402})
	if err := r.HSet(ctx, a.billing.key, "until", 1).Err(); err != nil {
		t.Fatal(err)
	}
	probe, err := b.billing.acquire(ctx)
	if err != nil || probe.owner == "" {
		t.Fatal("another process did not obtain the recovery probe", err)
	}
	b.billing.finish(ctx, probe, nil)
	if !a.billing.until.After(localNow) {
		t.Fatal("precondition: the triggering process has no stale local pause")
	}
	if _, err := a.billing.acquire(ctx); err != nil {
		t.Fatal("local state hid a successful shared recovery", err)
	}
}

func TestLiveSharedBillingLeaseAndCancelledPublication(t *testing.T) {
	r := liveBillingRedis(t)
	g := newBillingGate(uuid.NewString())
	g.shared = r
	ctx := context.Background()
	t.Cleanup(func() { _ = r.Del(ctx, g.key).Err() })
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	g.finish(cancelled, billingPermit{shared: true}, &APIError{Status: 402})
	if exists, err := r.Exists(ctx, g.key).Result(); err != nil || exists != 1 {
		t.Fatal("cancelled request failed to publish billing pause", err)
	}
	g.mu.Lock()
	g.until = time.Time{}
	g.mu.Unlock()
	_ = r.HSet(ctx, g.key, "until", 1).Err()
	old, err := g.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.HSet(ctx, g.key, "until", 1).Err()
	current, err := g.acquire(ctx)
	if err != nil {
		t.Fatal("expired probe was not replaced", err)
	}
	g.finish(ctx, old, nil)
	if exists, err := r.Exists(ctx, g.key).Result(); err != nil || exists != 1 {
		t.Fatal("stale probe cleared a new lease", err)
	}
	g.finish(ctx, current, errors.New("temporary provider failure"))
	if _, err := g.acquire(ctx); !errors.Is(err, ErrBillingCooldown) {
		t.Fatal("failed probe did not defer recovery", err)
	}
	if ttl := r.PTTL(ctx, g.key).Val(); ttl <= 0 || ttl > billingStateTTL {
		t.Fatalf("unbounded shared state retention: %v", ttl)
	}
}

func TestLiveBillingRedisUnavailableFallsBackLocally(t *testing.T) {
	r := liveBillingRedis(t)
	_ = r.Close()
	g := newBillingGate("key")
	g.shared = r
	ctx := context.Background()
	if _, err := g.acquire(ctx); err != nil {
		t.Fatal("Redis failure disabled healthy classification", err)
	}
	g.finish(ctx, billingPermit{}, &APIError{Status: 402})
	if _, err := g.acquire(ctx); !errors.Is(err, ErrBillingCooldown) {
		t.Fatal("Redis failure lost the local billing pause", err)
	}
}
