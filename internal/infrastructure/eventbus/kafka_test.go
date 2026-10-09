//go:build kafka

package eventbus

import (
	"context"
	"errors"
	"testing"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
)

func TestKafkaHandlerCountsIndependentRetriesUntilPreparationCanResolve(t *testing.T) {
	first, other := &ckf.Message{}, &ckf.Message{}
	var seen []int
	deliver := kafkaHandler(t.Context(), func(_ context.Context, msg Message) error {
		seen = append(seen, msg.Attempt)
		if !msg.Redelivers {
			t.Fatal("retry delivery was not marked")
		}
		if msg.Attempt < 5 {
			return errors.New("mailbox not loaded")
		}
		return nil
	})
	for attempt := 1; attempt <= 5; attempt++ {
		err := deliver(first)
		if (err == nil) != (attempt == 5) || seen[len(seen)-1] != attempt {
			t.Fatalf("attempt=%d seen=%v err=%v", attempt, seen, err)
		}
		if attempt == 3 {
			if err := deliver(other); err == nil || seen[len(seen)-1] != 1 {
				t.Fatal("another mailbox inherited retry state")
			}
		}
	}
	if err := deliver(first); err == nil || seen[len(seen)-1] != 1 {
		t.Fatal("successful delivery retained retry state")
	}
}

func TestKafkaSubscriptionReopensAfterFailureOrUnexpectedExit(t *testing.T) {
	for _, failure := range []error{errors.New("offset assignment lost"), nil} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		err := retrySubscription(ctx, func(context.Context) error {
			calls++
			if calls == 2 {
				cancel()
			}
			return failure
		})
		cancel()
		if !errors.Is(err, context.Canceled) || calls != 2 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	}
	calls := 0
	err := retrySubscription(t.Context(), func(context.Context) error { calls++; return ErrBusClosed })
	if !errors.Is(err, ErrBusClosed) || calls != 1 {
		t.Fatal("closed bus reopened")
	}
}

func TestKafkaSubscriptionRetiresClientsBeforeReopening(t *testing.T) {
	b := &KafkaBus{bootstrap: "127.0.0.1:1"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for i := 0; i < 3; i++ {
		if err := b.subscribeOnce(ctx, []string{"sync-test"}, "sync-test", 1, nil, func(context.Context, Message) error { return nil }); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		b.mu.Lock()
		retained := len(b.consumers)
		b.mu.Unlock()
		if retained != 0 {
			t.Fatalf("exited subscription retained %d clients", retained)
		}
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestKafkaBus_InterfaceSatisfaction is a compile-time check that KafkaBus
// satisfies the EventBus interface and that the constructors enforce their
// minimum required arguments. A live Kafka broker is intentionally not used
// here — that's covered by integration tests that aren't part of the unit
// suite.
func TestKafkaBus_InterfaceSatisfaction(t *testing.T) {
	var _ EventBus = (*KafkaBus)(nil)
}

func TestKafkaBus_NewKafkaRejectsEmptyBootstrap(t *testing.T) {
	_, err := NewKafka(KafkaConfig{})
	if err == nil {
		t.Fatal("expected error when bootstrap is empty")
	}
}

func TestKafkaBus_NewKafkaFromProducerName(t *testing.T) {
	// Passing nil here is fine because we never exercise Publish/Subscribe —
	// we only check the wrapper-level surface.
	bus := NewKafkaFromProducer(nil, KafkaConfig{Bootstrap: "unused:9092"})
	if bus.Name() != "kafka" {
		t.Fatalf("expected name 'kafka', got %q", bus.Name())
	}
	if bus.Producer() != nil {
		t.Fatal("expected nil producer to round-trip")
	}
}

func TestKafkaBus_SubscribeValidatesArgs(t *testing.T) {
	bus := NewKafkaFromProducer(nil, KafkaConfig{Bootstrap: "unused:9092"})
	defer bus.closeNoOp() // do not invoke real producer.Close on nil
	ctx := context.Background()
	cases := []struct {
		name    string
		topics  []string
		group   string
		handler Handler
	}{
		{"no topics", nil, "g", func(context.Context, Message) error { return nil }},
		{"no group", []string{"t"}, "", func(context.Context, Message) error { return nil }},
		{"no handler", []string{"t"}, "g", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := bus.Subscribe(ctx, tc.topics, tc.group, tc.handler); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestKafkaBus_PublishOnClosedBusFails(t *testing.T) {
	bus := NewKafkaFromProducer(nil, KafkaConfig{Bootstrap: "unused:9092"})
	bus.closeNoOp()
	err := bus.Publish(context.Background(), "t", "k", []byte("v"))
	if err == nil {
		t.Fatal("expected error publishing on closed bus")
	}
}

// TestKafkaBus_HandlerTimeoutEnv ensures the env override is parsed; the
// default is intentionally unchanged.
func TestKafkaBus_HandlerTimeoutEnv(t *testing.T) {
	t.Setenv("EVENTBUS_HANDLER_TIMEOUT", "5s")
	if got := handlerTimeout(); got.Seconds() != 5 {
		t.Fatalf("expected 5s, got %s", got)
	}
}

// closeNoOp marks the bus closed without invoking the (nil) underlying
// producer. Used only by tests that don't need a live broker.
func (b *KafkaBus) closeNoOp() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
}

// SubjectTranslation is shared across both backends and lives in bus.go;
// double-check it here so a future change to the rule fails loudly.
func TestSubject(t *testing.T) {
	cases := []struct{ in, out string }{
		{"w:abc", "w.abc"},
		{"jobs:worker-events", "jobs.worker-events"},
		{"already.dotted", "already.dotted"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := Subject(tc.in); got != tc.out {
			t.Errorf("Subject(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

// Sanity-check that the public kafka package's topic helpers still produce the
// shape NATSBus.subject() expects.
func TestKafkaTopicHelpersRoundTrip(t *testing.T) {
	w := kafka.GetWorkerTopic("11111111-1111-1111-1111-111111111111")
	if Subject(w) != "w.11111111-1111-1111-1111-111111111111" {
		t.Fatalf("worker topic translation drift: %q -> %q", w, Subject(w))
	}
	if Subject(kafka.TopicWorkerEvents) != "jobs.worker-events" {
		t.Fatalf("worker-events topic translation drift: %q", Subject(kafka.TopicWorkerEvents))
	}
}
