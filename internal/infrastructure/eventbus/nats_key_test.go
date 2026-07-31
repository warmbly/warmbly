package eventbus

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// TestNATSBus_KeyIsNotADedupID guards the rule that Publish's key is a
// partition/affinity hint, not a unique event id. The worker keys every event
// from one mailbox by that mailbox's ID; if the key reaches JetStream as
// Nats-Msg-Id, the dedup window swallows every event after the first.
func TestNATSBus_KeyIsNotADedupID(t *testing.T) {
	bus := newTestNATSBus(t)
	topic := "jobs:worker-events"
	const n = 20
	const key = "11111111-1111-1111-1111-111111111111"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu   sync.Mutex
		seen []string
	)
	go func() {
		_ = bus.Subscribe(ctx, []string{topic}, "key-dedup", func(_ context.Context, m Message) error {
			mu.Lock()
			seen = append(seen, string(m.Payload))
			mu.Unlock()
			return nil
		})
	}()
	time.Sleep(300 * time.Millisecond)

	for i := 0; i < n; i++ {
		if err := bus.Publish(ctx, topic, key, []byte(fmt.Sprintf("new-email-%d", i))); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(seen)
		mu.Unlock()
		if got == n {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != n {
		t.Fatalf("published %d events with the same key, handler saw %d (dedup dropped %d)", n, len(seen), n-len(seen))
	}
}

// TestNATSBus_PublishSetsNoMsgID is the direct guard: dedup in JetStream is
// stream-wide, so a stray Nats-Msg-Id also collides across topics (a mailbox's
// AddEmail command against that same mailbox's first result event).
func TestNATSBus_PublishSetsNoMsgID(t *testing.T) {
	bus := newTestNATSBus(t)
	topic := "jobs:msgid-header"
	const key = "mailbox-1"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan nats.Header, 1)
	go func() {
		_ = bus.Subscribe(ctx, []string{topic}, "msgid-header", func(_ context.Context, m Message) error {
			return nil
		})
	}()
	time.Sleep(300 * time.Millisecond)

	// Read the raw stream message so we assert on what was persisted, not on
	// what Subscribe chose to expose.
	sub, err := bus.nc.SubscribeSync(bus.subject(topic))
	if err != nil {
		t.Fatalf("raw subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	if err := bus.Publish(ctx, topic, key, []byte("payload")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	msg, err := sub.NextMsg(5 * time.Second)
	if err != nil {
		t.Fatalf("next msg: %v", err)
	}
	close(got)

	if id := msg.Header.Get(nats.MsgIdHdr); id != "" {
		t.Fatalf("Publish set %s=%q; the key is a partition hint and must not drive JetStream dedup", nats.MsgIdHdr, id)
	}
	if k := msg.Header.Get("Warmbly-Key"); k != key {
		t.Fatalf("Warmbly-Key = %q, want %q", k, key)
	}
}

// TestNATSBus_HandlerPanicDoesNotKillSubscriber pins the resilience rule: a
// handler panic (a malformed payload from another service) must be contained
// and nak'd, not crash the whole subscriber process and stop every org's
// event processing.
func TestNATSBus_HandlerPanicDoesNotKillSubscriber(t *testing.T) {
	bus := newTestNATSBus(t)
	topic := "jobs:panic-guard"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu       sync.Mutex
		attempts int
	)
	survived := make(chan struct{})
	go func() {
		_ = bus.Subscribe(ctx, []string{topic}, "panic-guard", func(_ context.Context, m Message) error {
			mu.Lock()
			attempts++
			n := attempts
			mu.Unlock()
			if n == 1 {
				var p *Message
				_ = p.Topic // deliberate nil deref
			}
			select {
			case <-survived:
			default:
				close(survived)
			}
			return nil
		})
	}()
	time.Sleep(300 * time.Millisecond)

	if err := bus.Publish(ctx, topic, "k", []byte("payload")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-survived:
	case <-time.After(15 * time.Second):
		t.Fatal("subscriber did not survive the handler panic and redeliver")
	}

	mu.Lock()
	defer mu.Unlock()
	if attempts < 2 {
		t.Fatalf("expected the panicking message to be redelivered, got %d attempts", attempts)
	}
}
