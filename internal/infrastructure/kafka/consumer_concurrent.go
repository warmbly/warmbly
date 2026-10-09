//go:build kafka

package kafka

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog/log"
)

const laneBuffer = 32

type partitionKey struct {
	topic     string
	partition int32
}

type laneResult struct {
	msg *ckf.Message
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

func messageLane(key string, lanes int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(lanes))
}

// ConsumeConcurrent preserves key order and stores only contiguous successful offsets.
func (cons *Consumer) ConsumeConcurrent(ctx context.Context, lanes int, key func(*ckf.Message) (string, error), handler func(msg *ckf.Message) error) error {
	if lanes < 1 || lanes > 64 {
		return errors.New("kafka: concurrency must be between 1 and 64")
	}
	if lanes <= 1 {
		return cons.Consume(ctx, handler)
	}
	results := make(chan laneResult, lanes*laneBuffer)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	epoch := cons.assignmentEpoch.Load()
	queues := make([]chan *ckf.Message, lanes)
	var wg sync.WaitGroup
	for i := range queues {
		queues[i] = make(chan *ckf.Message, laneBuffer)
		wg.Add(1)
		go func(queue <-chan *ckf.Message) {
			defer wg.Done()
			var failed bool
			for msg := range queue {
				if failed || runCtx.Err() != nil {
					results <- laneResult{msg: msg, err: errLaneStopped}
					continue
				}
				err := cons.handleWithRetry(runCtx, msg, handler)
				failed = err != nil
				results <- laneResult{msg: msg, err: err}
			}
		}(queues[i])
	}

	partitions := map[partitionKey]*partitionOffsets{}
	pending := 0
	var stopErr error
	settle := func(r laneResult) {
		if r.err != nil {
			if stopErr == nil && !errors.Is(r.err, errLaneStopped) {
				stopErr = r.err
			}
			return
		}
		if errors.Is(stopErr, ErrClientClosed) || errors.Is(stopErr, ErrAssignmentLost) {
			return
		}
		// Contiguous successes are stored even after another lane failed.
		p := partitions[messagePartition(r.msg)]
		before := len(p.pending)
		cons.storeSettled(partitions, r.msg, epoch, &stopErr)
		pending -= before - len(p.pending)
	}
	shutdown := func() error {
		cancel()
		for _, q := range queues {
			close(q)
		}
		go func() { wg.Wait(); close(results) }()
		for r := range results {
			settle(r)
		}
		return stopErr
	}

	for stopErr == nil {
		select {
		case r := <-results:
			settle(r)
			continue
		case <-ctx.Done():
			stopErr = ctx.Err()
			continue
		default:
		}
		if pending >= lanes*(laneBuffer+1) {
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
		pending++
		queue := queues[messageLane(laneKey, lanes)]
	dispatch:
		for {
			select {
			case queue <- msg:
				break dispatch
			case r := <-results:
				settle(r)
				if stopErr != nil {
					break dispatch
				}
			case <-ctx.Done():
				stopErr = ctx.Err()
				break dispatch
			}
		}
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
