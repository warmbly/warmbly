package eventbus

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscriptionTimeout_RaisesNeverLowers(t *testing.T) {
	t.Setenv("EVENTBUS_HANDLER_TIMEOUT", "30s")
	base := context.Background()
	if got := subscriptionTimeout(base); got != 30*time.Second {
		t.Fatalf("default budget = %s, want 30s", got)
	}
	if got := subscriptionTimeout(WithHandlerTimeout(base, 195*time.Second)); got != 195*time.Second {
		t.Fatalf("raised budget = %s, want 195s", got)
	}
	if got := subscriptionTimeout(WithHandlerTimeout(base, 5*time.Second)); got != 30*time.Second {
		t.Fatalf("a shorter budget must not lower EVENTBUS_HANDLER_TIMEOUT, got %s", got)
	}
}

// subscribeInBackground runs SubscribeKeyed until the test ends.
func subscribeInBackground(t *testing.T, ctx context.Context, bus *NATSBus, topic, group string, lanes int, key KeyResolver, h Handler) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = bus.SubscribeKeyed(ctx, []string{topic}, group, lanes, key, h)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func waitForConsumer(t *testing.T, bus *NATSBus, durable string) time.Duration {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := bus.js.Stream(t.Context(), bus.stream); err == nil {
			if c, err := s.Consumer(t.Context(), durable); err == nil {
				if info, err := c.Info(t.Context()); err == nil {
					return info.Config.AckWait
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("consumer %s never appeared", durable)
	return 0
}

func TestNATSBus_HandlerTimeoutSetsBudgetAndAckWait(t *testing.T) {
	t.Setenv("EVENTBUS_HANDLER_TIMEOUT", "30s")
	bus := newTestNATSBus(t)
	topic := "w.budget"

	deadlines := make(chan time.Duration, 1)
	ctx := WithHandlerTimeout(t.Context(), 3*time.Minute)
	subscribeInBackground(t, ctx, bus, topic, "budget", 1, nil, func(ctx context.Context, _ Message) error {
		d, _ := ctx.Deadline()
		deadlines <- time.Until(d)
		return nil
	})

	if got, want := waitForConsumer(t, bus, bus.durable("budget", []string{topic})), 3*time.Minute+natsAckGrace; got != want {
		t.Fatalf("ack wait = %s, want %s", got, want)
	}
	if err := bus.Publish(t.Context(), topic, "k", []byte("x")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case left := <-deadlines:
		if left < 2*time.Minute || left > 3*time.Minute {
			t.Fatalf("handler deadline %s away, want about 3m", left)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler never ran")
	}
}

func payloadKey(_ context.Context, msg Message) (string, error) {
	return string(msg.Payload[:1]), nil
}

func TestNATSBus_KeyedLanesKeepKeyOrderAndRunKeysConcurrently(t *testing.T) {
	bus := newTestNATSBus(t)
	topic := "w.lanes"

	releaseA := make(chan struct{})
	var mu sync.Mutex
	var order []string
	done := make(chan struct{})
	subscribeInBackground(t, t.Context(), bus, topic, "lanes", 4, payloadKey, func(_ context.Context, msg Message) error {
		p := string(msg.Payload)
		if p == "a1" {
			// Held until another key is handled, which only a concurrent lane can do.
			select {
			case <-releaseA:
			case <-time.After(5 * time.Second):
			}
		}
		mu.Lock()
		order = append(order, p)
		n := len(order)
		mu.Unlock()
		if p == "b1" {
			close(releaseA)
		}
		if n == 4 {
			close(done)
		}
		return nil
	})
	waitForConsumer(t, bus, bus.durable("lanes", []string{topic}))
	for _, p := range []string{"a1", "a2", "a3", "b1"} {
		if err := bus.Publish(t.Context(), topic, "", []byte(p)); err != nil {
			t.Fatalf("publish %s: %v", p, err)
		}
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("not every message was handled")
	}
	mu.Lock()
	defer mu.Unlock()
	if order[0] != "b1" {
		t.Fatalf("b1 waited behind another key's lane: %v", order)
	}
	if got := order[1:]; got[0] != "a1" || got[1] != "a2" || got[2] != "a3" {
		t.Fatalf("one key's messages ran out of order: %v", order)
	}
}

// A slow key takes one lane and no more: with two lanes, every other key keeps
// moving through the second while the first is held.
func TestNATSBus_KeyedLanesNeverQueueOneKeyBehindAnother(t *testing.T) {
	bus := newTestNATSBus(t)
	topic := "w.blocking"

	releaseA := make(chan struct{})
	handled := make(chan string, 8)
	subscribeInBackground(t, t.Context(), bus, topic, "blocking", 2, payloadKey, func(_ context.Context, msg Message) error {
		p := string(msg.Payload)
		if p == "a1" {
			select {
			case <-releaseA:
			case <-time.After(5 * time.Second):
			}
		}
		handled <- p
		return nil
	})
	waitForConsumer(t, bus, bus.durable("blocking", []string{topic}))
	for _, p := range []string{"a1", "b1", "c1", "d1", "e1"} {
		if err := bus.Publish(t.Context(), topic, "", []byte(p)); err != nil {
			t.Fatalf("publish %s: %v", p, err)
		}
	}
	others := map[string]bool{}
	for len(others) < 4 {
		select {
		case p := <-handled:
			if p == "a1" {
				t.Fatalf("a1 finished before the other keys: %v", others)
			}
			others[p] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("keys waited behind a held one: handled %v", others)
		}
	}
	close(releaseA)
	select {
	case p := <-handled:
		if p != "a1" {
			t.Fatalf("handled %s, want a1", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a1 never finished")
	}
}

// A message queued behind a slow one on its lane outlives its ack wait; the
// in-progress heartbeat must keep JetStream from handing it out again.
func TestNATSBus_KeyedLanesKeepQueuedMessagesAlive(t *testing.T) {
	grace := natsAckGrace
	natsAckGrace = 200 * time.Millisecond
	t.Cleanup(func() { natsAckGrace = grace })
	t.Setenv("EVENTBUS_HANDLER_TIMEOUT", "300ms")

	bus := newTestNATSBus(t)
	topic := "w.heartbeat"

	var mu sync.Mutex
	seen := map[string][]int{}
	subscribeInBackground(t, t.Context(), bus, topic, "heartbeat", 2, payloadKey, func(_ context.Context, msg Message) error {
		mu.Lock()
		seen[string(msg.Payload)] = append(seen[string(msg.Payload)], msg.Attempt)
		mu.Unlock()
		if string(msg.Payload) == "a1" {
			// Well past the 500ms ack wait, ignoring the handler deadline.
			time.Sleep(1500 * time.Millisecond)
		}
		return nil
	})
	waitForConsumer(t, bus, bus.durable("heartbeat", []string{topic}))
	for _, p := range []string{"a1", "a2"} {
		if err := bus.Publish(t.Context(), topic, "", []byte(p)); err != nil {
			t.Fatalf("publish %s: %v", p, err)
		}
	}
	time.Sleep(3 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	for _, p := range []string{"a1", "a2"} {
		if got := seen[p]; len(got) != 1 || got[0] != 1 {
			t.Fatalf("%s handled with attempts %v, want exactly one first delivery", p, got)
		}
	}
}

func TestNATSBus_SubscribeKeyedValidatesLanes(t *testing.T) {
	bus := newTestNATSBus(t)
	noop := func(context.Context, Message) error { return nil }
	if err := bus.SubscribeKeyed(t.Context(), []string{"w.x"}, "g", 0, payloadKey, noop); err == nil {
		t.Fatal("zero lanes accepted")
	}
	if err := bus.SubscribeKeyed(t.Context(), []string{"w.x"}, "g", 65, payloadKey, noop); err == nil {
		t.Fatal("65 lanes accepted")
	}
	if err := bus.SubscribeKeyed(t.Context(), []string{"w.x"}, "g", 2, nil, noop); err == nil {
		t.Fatal("several lanes without a key resolver accepted")
	}
}

func TestNATSBus_KeyedLanesKeepSaturatedMessagesAlive(t *testing.T) {
	grace := natsAckGrace
	natsAckGrace = 200 * time.Millisecond
	t.Cleanup(func() { natsAckGrace = grace })
	t.Setenv("EVENTBUS_HANDLER_TIMEOUT", "300ms")

	bus := newTestNATSBus(t)
	topic := "w.saturated"
	const messages = 64
	var mu sync.Mutex
	var order []string
	seen := map[string][]int{}
	allHandled := make(chan struct{})
	subscribeInBackground(t, t.Context(), bus, topic, "saturated", 8, payloadKey, func(_ context.Context, msg Message) error {
		p := string(msg.Payload)
		mu.Lock()
		order = append(order, p)
		seen[p] = append(seen[p], msg.Attempt)
		if len(seen) == messages && len(seen[p]) == 1 {
			close(allHandled)
		}
		mu.Unlock()
		time.Sleep(150 * time.Millisecond)
		return nil
	})
	ackWait := waitForConsumer(t, bus, bus.durable("saturated", []string{topic}))
	for i := 0; i < messages; i++ {
		p := fmt.Sprintf("a%02d", i)
		if err := bus.Publish(t.Context(), topic, "a", []byte(p)); err != nil {
			t.Fatalf("publish %s: %v", p, err)
		}
	}
	select {
	case <-allHandled:
	case <-time.After(20 * time.Second):
		mu.Lock()
		redelivered := 0
		for _, attempts := range seen {
			redelivered += len(attempts) - 1
		}
		handled, distinct := len(order), len(seen)
		mu.Unlock()
		t.Fatalf("not every saturated-lane message was handled: %d distinct of %d, %d handlings, %d redeliveries", distinct, messages, handled, redelivered)
	}
	// Give already-prefetched redeliveries time to reach the handler too.
	time.Sleep(2 * ackWait)
	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < messages; i++ {
		p := fmt.Sprintf("a%02d", i)
		if got := seen[p]; len(got) != 1 || got[0] != 1 {
			t.Errorf("%s handled with attempts %v, want exactly one first delivery", p, got)
		}
		if order[i] != p {
			t.Errorf("message %d = %s, want %s", i, order[i], p)
		}
	}
	if len(order) != messages {
		t.Errorf("handled %d messages, want %d", len(order), messages)
	}
}

// A delivery waiting for room behind a full lane is heartbeated too, so a slow
// handler ahead of it does not let JetStream hand it out a second time.
func TestNATSBus_KeyedLanesKeepMessagesWaitingForRoomAlive(t *testing.T) {
	grace := natsAckGrace
	natsAckGrace = 200 * time.Millisecond
	t.Cleanup(func() { natsAckGrace = grace })
	t.Setenv("EVENTBUS_HANDLER_TIMEOUT", "300ms")

	bus := newTestNATSBus(t)
	topic := "w.room"
	// Two lanes, since one runs without the pool; one key fills both lanes' room.
	const messages = 2*natsLaneBuffer + 4
	var mu sync.Mutex
	seen := map[string][]int{}
	subscribeInBackground(t, t.Context(), bus, topic, "room", 2, payloadKey, func(_ context.Context, msg Message) error {
		p := string(msg.Payload)
		mu.Lock()
		seen[p] = append(seen[p], msg.Attempt)
		mu.Unlock()
		if p == "a00" {
			// Well past the 500ms ack wait while the lane's room is full.
			time.Sleep(1500 * time.Millisecond)
		}
		return nil
	})
	ackWait := waitForConsumer(t, bus, bus.durable("room", []string{topic}))
	for i := 0; i < messages; i++ {
		if err := bus.Publish(t.Context(), topic, "a", fmt.Appendf(nil, "a%02d", i)); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n == messages || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(2 * ackWait)
	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < messages; i++ {
		p := fmt.Sprintf("a%02d", i)
		if got := seen[p]; len(got) != 1 || got[0] != 1 {
			t.Errorf("%s handled with attempts %v, want exactly one first delivery", p, got)
		}
	}
}

// Closing the bus ends a pull waiting for room behind handlers that have not
// finished, and hands back what is queued instead of starting it.
func TestNATSBus_CloseDoesNotWaitForLaneRoom(t *testing.T) {
	bus := newTestNATSBus(t)
	topic := "w.close"
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var started atomic.Int32
	subscribeInBackground(t, t.Context(), bus, topic, "close", 2, payloadKey, func(context.Context, Message) error {
		started.Add(1)
		<-release
		return nil
	})
	t.Cleanup(unblock)
	waitForConsumer(t, bus, bus.durable("close", []string{topic}))
	for i := 0; i < 2*natsLaneBuffer+8; i++ {
		if err := bus.Publish(t.Context(), topic, "k", fmt.Appendf(nil, "%c%02d", 'a'+i%2, i)); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for started.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	// Let the pull fill the lanes' room and wait for more.
	time.Sleep(500 * time.Millisecond)

	closed := make(chan struct{})
	go func() {
		_ = bus.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close waited for room in the lanes")
	}
	before := started.Load()
	unblock()
	time.Sleep(500 * time.Millisecond)
	if after := started.Load(); after != before {
		t.Fatalf("%d queued deliveries started after Close", after-before)
	}
}
