//go:build kafka

package kafka

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type concurrentConsumer struct {
	shutdownConsumer
	mu     sync.Mutex
	queue  []*ckf.Message
	stored []*ckf.Message
	reads  atomic.Int64
	acked  chan struct{}
	onRead func()
}

func (c *concurrentConsumer) ReadMessage(time.Duration) (*ckf.Message, error) {
	if c.onRead != nil {
		c.onRead()
	}
	c.mu.Lock()
	if len(c.queue) > 0 {
		msg := c.queue[0]
		c.queue = c.queue[1:]
		c.reads.Add(1)
		c.mu.Unlock()
		return msg, nil
	}
	c.mu.Unlock()
	time.Sleep(time.Millisecond)
	return nil, ckf.NewError(ckf.ErrTimedOut, "idle", false)
}

func (c *concurrentConsumer) StoreMessage(msg *ckf.Message) ([]ckf.TopicPartition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stored = append(c.stored, msg)
	if c.acked != nil {
		select {
		case c.acked <- struct{}{}:
		default:
		}
	}
	return nil, nil
}

func concurrentMessage(offset int, partition int32, mailbox string) *ckf.Message {
	topic := "commands"
	return &ckf.Message{
		TopicPartition: ckf.TopicPartition{Topic: &topic, Partition: partition, Offset: ckf.Offset(offset)},
		Key:            []byte("legacy-task-key"),
		Value:          []byte(mailbox),
	}
}

func mailboxKey(msg *ckf.Message) (string, error) { return string(msg.Value), nil }

func awaitConcurrent(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for concurrent handler")
	}
}

func TestConcurrentConsumerOrdersMailboxAndFencesOffsets(t *testing.T) {
	if messageLane("a", 2) == messageLane("b", 2) {
		t.Fatal("test requires separate lanes")
	}
	c := &concurrentConsumer{queue: []*ckf.Message{
		concurrentMessage(10, 0, "a"), concurrentMessage(12, 0, "b"), concurrentMessage(18, 0, "a"),
	}, acked: make(chan struct{}, 5)}
	cons := &Consumer{c: c}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	release := make(chan struct{})
	started, independent, later := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- cons.ConsumeConcurrent(ctx, 2, mailboxKey, func(msg *ckf.Message) error {
			switch msg.TopicPartition.Offset {
			case 10:
				close(started)
				<-release
			case 12:
				close(independent)
			case 18:
				close(later)
			}
			return nil
		})
	}()
	awaitConcurrent(t, started)
	awaitConcurrent(t, independent)
	select {
	case <-later:
		t.Fatal("same mailbox ran before earlier handler completed")
	default:
	}
	c.mu.Lock()
	count := len(c.stored)
	c.mu.Unlock()
	if count != 0 {
		t.Fatal("stored an offset beyond an unfinished command")
	}
	close(release)
	awaitConcurrent(t, later)
	for {
		awaitConcurrent(t, c.acked)
		c.mu.Lock()
		last := c.stored[len(c.stored)-1].TopicPartition.Offset
		c.mu.Unlock()
		if last == 18 {
			break
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestConcurrentConsumerFailureKeepsPartitionReplayAndStopsLane(t *testing.T) {
	c := &concurrentConsumer{queue: []*ckf.Message{
		concurrentMessage(0, 0, "a"), concurrentMessage(1, 0, "b"),
		concurrentMessage(2, 0, "a"), concurrentMessage(3, 1, "b"),
	}}
	cons := &Consumer{c: c, retryLimit: 1}
	independent := make(chan struct{})
	failure := errors.New("provider command failed")
	var later atomic.Bool
	err := cons.ConsumeConcurrent(t.Context(), 2, mailboxKey, func(msg *ckf.Message) error {
		if msg.TopicPartition.Offset == 0 {
			<-independent
			return failure
		}
		if msg.TopicPartition.Offset == 1 {
			close(independent)
		}
		if msg.TopicPartition.Offset == 2 {
			later.Store(true)
		}
		return nil
	})
	if !errors.Is(err, failure) || later.Load() {
		t.Fatalf("err=%v later=%v", err, later.Load())
	}
	for _, msg := range c.stored {
		if msg.TopicPartition.Partition == 0 {
			t.Fatal("advanced past failed partition offset")
		}
	}
}

func TestConcurrentConsumerBoundsReadAheadBehindSlowMailbox(t *testing.T) {
	c := &concurrentConsumer{acked: make(chan struct{}, 100)}
	c.queue = append(c.queue, concurrentMessage(0, 0, "a"))
	for i := 1; i < 200; i++ {
		c.queue = append(c.queue, concurrentMessage(i, 0, "b"))
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	release := make(chan struct{})
	done := make(chan error, 1)
	var handled atomic.Int64
	go func() {
		done <- (&Consumer{c: c}).ConsumeConcurrent(ctx, 2, mailboxKey, func(msg *ckf.Message) error {
			if msg.TopicPartition.Offset == 0 {
				<-release
			}
			handled.Add(1)
			return nil
		})
	}()
	deadline := time.After(3 * time.Second)
	for handled.Load() < 2*(laneBuffer+1)-1 {
		select {
		case <-deadline:
			t.Fatal("did not process independent messages")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if read := c.reads.Load(); read > 2*(laneBuffer+1) {
		t.Fatalf("unbounded read-ahead: %d", read)
	}
	cancel()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestConcurrentConsumerRejectsOffsetsFromRevokedAssignment(t *testing.T) {
	c := &concurrentConsumer{queue: []*ckf.Message{concurrentMessage(0, 0, "a")}}
	cons := &Consumer{c: c}
	released := make(chan struct{})
	var revoked atomic.Bool
	c.onRead = func() {
		if revoked.Load() {
			cons.assignmentEpoch.Add(1)
			close(released)
			c.onRead = nil
		}
	}
	err := cons.ConsumeConcurrent(t.Context(), 2, mailboxKey, func(*ckf.Message) error {
		revoked.Store(true)
		<-released
		return nil
	})
	if !errors.Is(err, ErrAssignmentLost) || len(c.stored) != 0 {
		t.Fatalf("err=%v stored=%v", err, c.stored)
	}
}

func TestPartitionOffsetsTracksReadOrderNotNumericAdjacency(t *testing.T) {
	p := &partitionOffsets{pending: []*ckf.Message{concurrentMessage(4, 0, "a"), concurrentMessage(9, 0, "b")}, done: map[ckf.Offset]bool{}}
	if msg := p.complete(9); msg != nil {
		t.Fatal("completed past unfinished predecessor")
	}
	if msg := p.complete(4); msg == nil || msg.TopicPartition.Offset != 9 || len(p.pending) != 0 {
		t.Fatalf("last=%v pending=%v", msg, p.pending)
	}
}
