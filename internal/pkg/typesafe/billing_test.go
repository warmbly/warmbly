package typesafe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const goodAnswer = `{"model":"jev-1.13.0","answers":{"kind":{"type":"choice","choice":"human_reply","confidence":0.9}},"usage":{"input_tokens":1300,"output_tokens":2}}`

func TestBillingCooldownAndRecovery(t *testing.T) {
	var calls atomic.Int64
	var status atomic.Int64
	status.Store(402)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if status.Load() != 200 {
			w.WriteHeader(int(status.Load()))
			_, _ = w.Write([]byte(`{"detail":"billing_error: no credits"}`))
			return
		}
		_, _ = w.Write([]byte(goodAnswer))
	}))
	defer srv.Close()
	c := testClient(t, srv)
	now := time.Now()
	c.billing.now = func() time.Time { return now }
	ctx := context.Background()
	if _, err := c.Ask(ctx, "x", testQuestions()); err == nil {
		t.Fatal("402 accepted")
	}
	for i := 0; i < 50; i++ {
		if resp, err := c.Ask(ctx, "x", testQuestions()); resp != nil || !errors.Is(err, ErrBillingCooldown) {
			t.Fatalf("paused call fabricated an answer or lost the pause: %v %v", resp, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("billing failures retried for each arrival")
	}
	now = now.Add(billingCooldown)
	if _, err := c.Ask(ctx, "x", testQuestions()); err == nil {
		t.Fatal("failed probe accepted")
	}
	if calls.Load() != 2 {
		t.Fatal("expired cooldown did not probe")
	}
	status.Store(200)
	now = now.Add(billingCooldown)
	if _, err := c.Ask(ctx, "x", testQuestions()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ask(ctx, "x", testQuestions()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 {
		t.Fatal("successful probe did not resume normal requests")
	}
}

func TestBillingRecoveryHasOneConcurrentProbe(t *testing.T) {
	g := newBillingGate("key")
	now := time.Now()
	g.now = func() time.Time { return now }
	g.finish(context.Background(), billingPermit{}, &APIError{Status: 402})
	now = now.Add(billingCooldown)
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := g.acquire(context.Background())
			if err == nil && p.owner != "" {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("%d recovery probes admitted", admitted.Load())
	}
}

func TestBillingLeaseAndStaleSuccess(t *testing.T) {
	g := newBillingGate("key")
	now := time.Now()
	g.now = func() time.Time { return now }
	g.finish(context.Background(), billingPermit{}, &APIError{Status: 402})
	now = now.Add(billingCooldown)
	old, err := g.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(billingProbeLease)
	current, err := g.acquire(context.Background())
	if err != nil {
		t.Fatal("abandoned probe did not expire", err)
	}
	g.finish(context.Background(), old, nil)
	if _, err := g.acquire(context.Background()); !errors.Is(err, ErrBillingCooldown) {
		t.Fatal("stale probe cleared a newer lease")
	}
	g.finish(context.Background(), current, errors.New("network unavailable"))
	if _, err := g.acquire(context.Background()); !errors.Is(err, ErrBillingCooldown) {
		t.Fatal("failed recovery probe released the billing gate")
	}
}

func TestNonBillingFailureDoesNotPause(t *testing.T) {
	g := newBillingGate("key")
	for _, status := range []int{400, 401, 422, 429, 500, 529} {
		g.finish(context.Background(), billingPermit{}, &APIError{Status: status})
		if _, err := g.acquire(context.Background()); err != nil {
			t.Fatalf("%d paused billing: %v", status, err)
		}
	}
}

func TestBillingRecoveryRetryCanSucceed(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(429)
			return
		}
		_, _ = w.Write([]byte(goodAnswer))
	}))
	defer srv.Close()
	c := testClient(t, srv)
	c.billing.until = time.Now().Add(-time.Second)
	if _, err := c.Ask(context.Background(), "x", testQuestions()); err != nil {
		t.Fatal(err)
	}
	if !c.billing.until.IsZero() {
		t.Fatal("successful retry left billing paused")
	}
}
