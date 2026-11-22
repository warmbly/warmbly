//go:build kafka

package kafka

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog/log"
)

// Read-ahead bounds. A slow key holds back only its own messages, so these cap memory, not throughput.
const (
	readAheadPerHandler = 256
	minReadAhead        = 4096
	maxReadAheadBytes   = 64 << 20
	maxUncommitted      = 100_000
)

func readAheadLimit(handlers int) int { return max(handlers*readAheadPerHandler, minReadAhead) }

type partitionKey struct {
	topic     string
	partition int32
}

type keyedMessage struct {
	msg  *ckf.Message
	key  string
	size int
}

type laneResult struct {
	keyedMessage
	err error
}

// partitionOffsets stores an offset only once every earlier read offset in its partition succeeded.
type partitionOffsets struct {
	pending []*ckf.Message
	done    map[ckf.Offset]bool
}

func (p *partitionOffsets) complete(offset ckf.Offset) *ckf.Message {
	p.done[offset] = true
	var last *ckf.Message
	for len(p.pending) > 0 && p.done[p.pending[0].TopicPartition.Offset] {
		last = p.pending[0]
		delete(p.done, last.TopicPartition.Offset)
		p.pending[0] = nil
		p.pending = p.pending[1:]
	}
	return last
}

func messagePartition(msg *ckf.Message) partitionKey {
	k := partitionKey{partition: msg.TopicPartition.Partition}
	if msg.TopicPartition.Topic != nil {
		k.topic = *msg.TopicPartition.Topic
	}
	return k
}

// keyScheduler runs each key's messages in read order, one at a time, and hands the next ready key to any free handler.
type keyScheduler struct {
	waiting map[string][]keyedMessage
	busy    map[string]bool
	ready   []string
}

func newKeyScheduler() *keyScheduler {
	return &keyScheduler{waiting: map[string][]keyedMessage{}, busy: map[string]bool{}}
}

func (s *keyScheduler) push(m keyedMessage) {
	s.waiting[m.key] = append(s.waiting[m.key], m)
	if !s.busy[m.key] && len(s.waiting[m.key]) == 1 {
		s.ready = append(s.ready, m.key)
	}
}

func (s *keyScheduler) next() (keyedMessage, bool) {
	if len(s.ready) == 0 {
		return keyedMessage{}, false
	}
	key := s.ready[0]
	s.ready[0] = ""
	s.ready = s.ready[1:]
	queue := s.waiting[key]
	m := queue[0]
	queue[0] = keyedMessage{}
	if len(queue) == 1 {
		delete(s.waiting, key)
	} else {
		s.waiting[key] = queue[1:]
	}
	s.busy[key] = true
	return m, true
}

func (s *keyScheduler) finish(key string) {
	delete(s.busy, key)
	if len(s.waiting[key]) > 0 {
		s.ready = append(s.ready, key)
	}
}

// ConsumeConcurrent preserves key order and stores only contiguous successful offsets.
func (cons *Consumer) ConsumeConcurrent(ctx context.Context, lanes int, key func(*ckf.Message) (string, error), handler func(msg *ckf.Message) error) error {
	if lanes < 1 || lanes > 64 {
		return errors.New("kafka: concurrency must be between 1 and 64")
	}
	if lanes <= 1 {
		return cons.Consume(ctx, handler)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	epoch := cons.assignmentEpoch.Load()
	work := make(chan keyedMessage, lanes)
	results := make(chan laneResult, lanes)
	var wg sync.WaitGroup
	for range lanes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range work {
				if runCtx.Err() != nil {
					results <- laneResult{keyedMessage: m, err: errLaneStopped}
					continue
				}
				results <- laneResult{keyedMessage: m, err: cons.handleWithRetry(runCtx, m.msg, handler)}
			}
		}()
	}

	sched := newKeyScheduler()
	partitions := map[partitionKey]*partitionOffsets{}
	limit := readAheadLimit(lanes)
	inFlight, buffered, bufferedBytes, uncommitted := 0, 0, 0, 0
	var stopErr error
	settle := func(r laneResult) {
		inFlight--
		buffered--
		bufferedBytes -= r.size
		sched.finish(r.key)
		if r.err != nil {
			if stopErr == nil && !errors.Is(r.err, errLaneStopped) {
				stopErr = r.err
			}
			return
		}
		// Only the offset is needed from here on.
		r.msg.Value, r.msg.Key, r.msg.Headers = nil, nil, nil
		if errors.Is(stopErr, ErrClientClosed) || errors.Is(stopErr, ErrAssignmentLost) {
			return
		}
		// Contiguous successes are stored even after another key failed.
		p := partitions[messagePartition(r.msg)]
		before := len(p.pending)
		cons.storeSettled(partitions, r.msg, epoch, &stopErr)
		uncommitted -= before - len(p.pending)
	}
	dispatch := func() {
		for stopErr == nil && inFlight < lanes {
			m, ok := sched.next()
			if !ok {
				return
			}
			inFlight++
			work <- m
		}
	}
	shutdown := func() error {
		cancel()
		close(work)
		go func() { wg.Wait(); close(results) }()
		for r := range results {
			settle(r)
		}
		return stopErr
	}

	for stopErr == nil {
		dispatch()
		select {
		case r := <-results:
			settle(r)
			continue
		case <-ctx.Done():
			stopErr = ctx.Err()
			continue
		default:
		}
		if buffered >= limit || bufferedBytes >= maxReadAheadBytes || uncommitted >= maxUncommitted {
			select {
			case r := <-results:
				settle(r)
			case <-ctx.Done():
				stopErr = ctx.Err()
			}
			continue
		}
		msg, err := cons.readMessage()
		if cons.assignmentEpoch.Load() != epoch {
			stopErr = ErrAssignmentLost
			continue
		}
		if err != nil {
			var kafkaErr ckf.Error
			if errors.As(err, &kafkaErr) {
				if kafkaErr.Code() == ckf.ErrTimedOut {
					continue
				}
				if kafkaErr.IsFatal() || kafkaErr.Code() == ckf.ErrMaxPollExceeded {
					stopErr = fmt.Errorf("kafka: consumer must reopen: %w", err)
					continue
				}
				log.Warn().Str("code", fmt.Sprintf("%d", kafkaErr.Code())).Err(kafkaErr).Msg("kafka consumer error")
				if err := consumerBackoff(ctx, time.Second); err != nil {
					stopErr = err
				}
				continue
			}
			stopErr = fmt.Errorf("error reading message: %w", err)
			continue
		}
		laneKey := string(msg.Key)
		if key != nil {
			laneKey, err = key(msg)
			if err != nil {
				stopErr = fmt.Errorf("kafka: resolving handler key: %w", err)
				continue
			}
		}
		pk := messagePartition(msg)
		p := partitions[pk]
		if p == nil {
			p = &partitionOffsets{done: map[ckf.Offset]bool{}}
			partitions[pk] = p
		}
		p.pending = append(p.pending, msg)
		uncommitted++
		size := len(msg.Value) + len(msg.Key)
		buffered++
		bufferedBytes += size
		sched.push(keyedMessage{msg: msg, key: laneKey, size: size})
	}
	return shutdown()
}

var errLaneStopped = errors.New("kafka: lane stopped after an earlier failure")

func (cons *Consumer) storeSettled(partitions map[partitionKey]*partitionOffsets, msg *ckf.Message, epoch uint64, stopErr *error) {
	p := partitions[messagePartition(msg)]
	if p == nil {
		return
	}
	last := p.complete(msg.TopicPartition.Offset)
	if last == nil {
		return
	}
	if err := cons.storeMessageAt(last, epoch); err != nil && *stopErr == nil {
		if errors.Is(err, ErrClientClosed) || errors.Is(err, ErrAssignmentLost) {
			*stopErr = err
			return
		}
		*stopErr = fmt.Errorf("kafka: storing a handled offset failed: %w", err)
	}
}
