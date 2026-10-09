//go:build kafka

package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type orderedConsumer struct {
	shutdownConsumer
	queue    []*ckf.Message
	stored   []ckf.Offset
	reads    int
	storeErr error
}

func (c *orderedConsumer) ReadMessage(time.Duration) (*ckf.Message, error) {
	c.reads++
	if len(c.queue) == 0 {
		return nil, errors.New("unexpected poll")
	}
	m := c.queue[0]
	c.queue = c.queue[1:]
	return m, nil
}
func (c *orderedConsumer) StoreMessage(m *ckf.Message) ([]ckf.TopicPartition, error) {
	c.stored = append(c.stored, m.TopicPartition.Offset)
	return nil, c.storeErr
}

func TestConsumerRetriesFailedRecordBeforeLaterOffsets(t *testing.T) {
	first := &ckf.Message{TopicPartition: ckf.TopicPartition{Partition: 0, Offset: 10}}
	second := &ckf.Message{TopicPartition: ckf.TopicPartition{Partition: 0, Offset: 11}}
	c := &orderedConsumer{queue: []*ckf.Message{first, second}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var handled []ckf.Offset
	err := (&Consumer{c: c}).Consume(ctx, func(m *ckf.Message) error {
		handled = append(handled, m.TopicPartition.Offset)
		if len(handled) == 1 {
			return errors.New("transient database failure")
		}
		if len(handled) == 2 && (c.reads != 1 || len(c.stored) != 0 || m != first) {
			t.Fatal("failed record was skipped or acked")
		}
		if m == second {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || len(handled) != 3 || handled[0] != 10 || handled[1] != 10 || handled[2] != 11 || len(c.stored) != 2 || c.stored[0] != 10 || c.stored[1] != 11 {
		t.Fatalf("err=%v handled=%v stored=%v", err, handled, c.stored)
	}
}

func TestConsumerFailureCancellationNeverStoresOffset(t *testing.T) {
	c := &orderedConsumer{queue: []*ckf.Message{{}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := (&Consumer{c: c}).Consume(ctx, func(*ckf.Message) error { cancel(); return errors.New("failed") })
	if !errors.Is(err, context.Canceled) || len(c.stored) != 0 || c.reads != 1 {
		t.Fatalf("err=%v stored=%v reads=%d", err, c.stored, c.reads)
	}
}

func TestConsumerOffsetStorageFailureStopsBeforeNextRecord(t *testing.T) {
	c := &orderedConsumer{queue: []*ckf.Message{{}, {}}, storeErr: errors.New("store unavailable")}
	err := (&Consumer{c: c}).Consume(context.Background(), func(*ckf.Message) error { return nil })
	if !errors.Is(err, c.storeErr) || c.reads != 1 {
		t.Fatalf("err=%v reads=%d", err, c.reads)
	}
}

func TestConsumerRetryBackoffCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := consumerBackoff(ctx, 5*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
