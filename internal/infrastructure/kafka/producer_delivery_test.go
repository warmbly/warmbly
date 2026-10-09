//go:build kafka

package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type queuedPublication struct {
	msg    *ckf.Message
	report chan ckf.Event
}

type deliveryProducer struct {
	queued chan queuedPublication
	err    error
}

func (p *deliveryProducer) Produce(msg *ckf.Message, report chan ckf.Event) error {
	if p.err != nil {
		return p.err
	}
	p.queued <- queuedPublication{msg, report}
	return nil
}

func (*deliveryProducer) Flush(int) int { return 0 }
func (*deliveryProducer) Close()        {}

func confirmedPublication(t *testing.T, p *Producer, ctx context.Context) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.ProduceConfirmed(ctx, "jobs", []byte("mailbox"), []byte("result")) }()
	return done
}

func waitDelivery(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("publication did not settle")
		return nil
	}
}

func TestProducerConfirmedPublicationWaitsForIndependentDeliveryReports(t *testing.T) {
	p := &deliveryProducer{queued: make(chan queuedPublication, 2)}
	pr := &Producer{p: p}
	first := confirmedPublication(t, pr, t.Context())
	a := <-p.queued
	second := confirmedPublication(t, pr, t.Context())
	b := <-p.queued
	select {
	case err := <-first:
		t.Fatalf("local admission was treated as delivered: %v", err)
	default:
	}
	b.report <- b.msg
	if err := waitDelivery(t, second); err != nil {
		t.Fatal(err)
	}
	failed := errors.New("broker rejected result")
	a.msg.TopicPartition.Error = failed
	a.report <- a.msg
	if err := waitDelivery(t, first); !errors.Is(err, failed) {
		t.Fatalf("delivery failure was lost: %v", err)
	}
	if NewProducer("broker").config["acks"] != "all" {
		t.Fatal("producer did not request broker acknowledgement")
	}
}

func TestProducerConfirmedPublicationCancellationAllowsLateReport(t *testing.T) {
	p := &deliveryProducer{queued: make(chan queuedPublication, 1)}
	ctx, cancel := context.WithCancel(t.Context())
	done := confirmedPublication(t, &Producer{p: p}, ctx)
	a := <-p.queued
	cancel()
	if err := waitDelivery(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled publication was acknowledged", err)
	}
	select {
	case a.report <- a.msg:
	default:
		t.Fatal("late delivery report blocks native producer")
	}
	if err := (&Producer{p: p}).ProduceConfirmed(ctx, "jobs", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("already canceled context was published", err)
	}
	if len(p.queued) != 0 {
		t.Fatal("canceled publication entered native queue")
	}
}

func TestProducerConfirmedPublicationRejectsEnqueueFailureOrClosedClient(t *testing.T) {
	failed := errors.New("local queue full")
	for _, pr := range []*Producer{{p: &deliveryProducer{err: failed}}, {closed: true}} {
		want := failed
		if pr.closed {
			want = ErrClientClosed
		}
		if err := pr.ProduceConfirmed(t.Context(), "jobs", nil, nil); !errors.Is(err, want) {
			t.Fatalf("want %v, got %v", want, err)
		}
	}
}

func TestProducerConfirmedPublicationReportsNativeTimeout(t *testing.T) {
	conf := NewProducer("127.0.0.1:1")
	conf.Set("message.timeout.ms", 100)
	conf.Set("socket.timeout.ms", 100)
	conf.Set("log_level", 0)
	pr, err := conf.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	err = pr.ProduceConfirmed(ctx, "jobs", nil, []byte("result"))
	var native ckf.Error
	if !errors.As(err, &native) || native.Code() != ckf.ErrMsgTimedOut {
		t.Fatalf("native undelivered result treated as success: %v", err)
	}
}
