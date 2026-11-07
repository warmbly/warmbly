package eventbus

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type migrationTestBus struct {
	name      string
	published atomic.Int32
	subscribe func(context.Context, Handler) error
	pubErr    error
}

func (b *migrationTestBus) Name() string { return b.name }
func (b *migrationTestBus) Close() error { return nil }
func (b *migrationTestBus) Publish(context.Context, string, string, []byte) error {
	b.published.Add(1)
	return b.pubErr
}
func (b *migrationTestBus) Subscribe(ctx context.Context, _ []string, _ string, h Handler) error {
	return b.subscribe(ctx, h)
}

func TestMigrationPublishNeverFallsBackAfterAmbiguousFailure(t *testing.T) {
	errTimeout := errors.New("acknowledgement timeout")
	primary := &migrationTestBus{name: "nats", pubErr: errTimeout}
	other := &migrationTestBus{name: "kafka"}
	b := &migrationBus{primary: primary, other: other}
	if err := b.Publish(t.Context(), "commands", "mailbox", []byte("send")); !errors.Is(err, errTimeout) {
		t.Fatalf("publish error: %v", err)
	}
	if primary.published.Load() != 1 || other.published.Load() != 0 {
		t.Fatal("ambiguous publish was repeated on another broker")
	}
}

func TestMigrationReadsBothAndCancelsTogether(t *testing.T) {
	started := make(chan struct{}, 2)
	stopped := make(chan struct{}, 2)
	errBroker := errors.New("broker stopped")
	first := &migrationTestBus{name: "kafka", subscribe: func(ctx context.Context, _ Handler) error {
		started <- struct{}{}
		<-ctx.Done()
		stopped <- struct{}{}
		return ctx.Err()
	}}
	second := &migrationTestBus{name: "nats", subscribe: func(_ context.Context, _ Handler) error {
		started <- struct{}{}
		<-started
		<-started
		stopped <- struct{}{}
		return errBroker
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	b := &migrationBus{primary: first, other: second}
	if err := b.Subscribe(ctx, []string{"commands"}, "group", func(context.Context, Message) error { return nil }); !errors.Is(err, errBroker) {
		t.Fatalf("expected subscription failure, got %v", err)
	}
	if len(stopped) != 2 {
		t.Fatal("both subscriptions must stop before returning")
	}
}

func TestMigrationHoldQueuesUntilOldTransportDrains(t *testing.T) {
	oldBus, newBus := newTestNATSBus(t), newTestNATSBus(t)
	old := &namedMigrationBus{EventBus: oldBus, name: "kafka"}
	ctx, cancel := context.WithCancel(t.Context())
	seen := make(chan string, 2)
	handler := func(_ context.Context, msg Message) error { seen <- string(msg.Payload); return nil }
	b := &migrationBus{primary: newBus, other: old, hold: "nats"}
	done := make(chan error, 1)
	go func() { done <- b.Subscribe(ctx, []string{"commands"}, "migration", handler) }()
	if err := newBus.Publish(t.Context(), "commands", "mailbox", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := oldBus.Publish(t.Context(), "commands", "mailbox", []byte("old")); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-seen:
		if value != "old" {
			t.Fatalf("new command overtook old command: %q", value)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("old queue did not drain")
	}
	cancel()
	<-done
	b.hold = ""
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	go func() { done <- b.Subscribe(ctx, []string{"commands"}, "migration", handler) }()
	select {
	case value := <-seen:
		if value != "new" {
			t.Fatalf("held command changed: %q", value)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("held queue did not resume")
	}
	cancel()
	<-done
}

type namedMigrationBus struct {
	EventBus
	name string
}

func (b *namedMigrationBus) Name() string { return b.name }

func TestMigrationRejectsHoldWithoutDualReaders(t *testing.T) {
	t.Setenv("EVENTBUS_READ_BOTH", "false")
	t.Setenv("EVENTBUS_HOLD_READS", "nats")
	if _, err := FromEnv("", nil); err == nil {
		t.Fatal("hold without a drain reader accepted")
	}
}
