//go:build kafka

package kafka

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog/log"
)

var ErrClientClosed = errors.New("kafka: client closed")
var ErrAssignmentLost = errors.New("kafka: assignment lost before offset storage")

type consumerClient interface {
	ReadMessage(time.Duration) (*ckf.Message, error)
	StoreMessage(*ckf.Message) ([]ckf.TopicPartition, error)
	SubscribeTopics([]string, ckf.RebalanceCb) error
	Close() error
}

type Consumer struct {
	c               consumerClient
	mu              sync.Mutex
	closed          bool
	retryLimit      int
	assignmentEpoch atomic.Uint64
}

type ConsumerConfig struct {
	config map[string]ckf.ConfigValue
}

func NewConsumer(servers string) *ConsumerConfig {
	return &ConsumerConfig{
		config: map[string]ckf.ConfigValue{
			"bootstrap.servers": servers,
		},
	}
}

func (conf *ConsumerConfig) WithSASL(saslConfig *SASLConfig) {
	for key, value := range saslConfig.Generate() {
		conf.Set(key, value)
	}
}

func (conf *ConsumerConfig) Set(key string, value ckf.ConfigValue) {
	conf.config[key] = value
}

func (conf *ConsumerConfig) Connect() (*Consumer, error) {
	cm := &ckf.ConfigMap{}
	for k, v := range conf.config {
		if err := cm.SetKey(k, v); err != nil {
			return nil, err
		}
	}
	consumer, err := ckf.NewConsumer(cm)
	if err != nil {
		return nil, err
	}

	return &Consumer{
		c: consumer,
	}, nil
}

func (cons *Consumer) Close() {
	cons.mu.Lock()
	if cons.closed {
		cons.mu.Unlock()
		return
	}
	cons.closed = true
	cons.mu.Unlock()
	if err := cons.c.Close(); err != nil {
		log.Warn().Err(err).Msg("kafka: closing consumer failed")
	}
}

func (cons *Consumer) SubscribeTopics(topics []string) error {
	cons.mu.Lock()
	defer cons.mu.Unlock()
	if cons.closed {
		return ErrClientClosed
	}
	return cons.c.SubscribeTopics(topics, func(_ *ckf.Consumer, event ckf.Event) error {
		if _, revoked := event.(ckf.RevokedPartitions); revoked {
			cons.assignmentEpoch.Add(1)
		}
		// Let the native client perform its usual assignment protocol.
		return nil
	})
}

// Polling and offset storage must finish before the native client is destroyed.
func (cons *Consumer) readMessage() (*ckf.Message, error) {
	cons.mu.Lock()
	defer cons.mu.Unlock()
	if cons.closed {
		return nil, ErrClientClosed
	}
	return cons.c.ReadMessage(100 * time.Millisecond)
}

func (cons *Consumer) storeMessage(msg *ckf.Message) error {
	return cons.storeMessageAt(msg, cons.assignmentEpoch.Load())
}

func (cons *Consumer) storeMessageAt(msg *ckf.Message, epoch uint64) error {
	cons.mu.Lock()
	defer cons.mu.Unlock()
	if cons.closed {
		return ErrClientClosed
	}
	if cons.assignmentEpoch.Load() != epoch {
		return ErrAssignmentLost
	}
	if native, ok := cons.c.(interface{ AssignmentLost() bool }); ok && native.AssignmentLost() {
		return ErrAssignmentLost
	}
	_, err := cons.c.StoreMessage(msg)
	return err
}

func (cons *Consumer) Consume(ctx context.Context, handler func(msg *ckf.Message) error) error {
	epoch := cons.assignmentEpoch.Load()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			msg, err := cons.readMessage()
			if cons.assignmentEpoch.Load() != epoch {
				return ErrAssignmentLost
			}
			if err != nil {
				if kafkaErr, ok := err.(ckf.Error); ok {
					if kafkaErr.Code() == ckf.ErrTimedOut {
						continue
					}
					if kafkaErr.IsFatal() || kafkaErr.Code() == ckf.ErrMaxPollExceeded {
						return fmt.Errorf("kafka: consumer must reopen: %w", err)
					}
					// Log transient Kafka errors and retry after a brief delay
					log.Warn().Str("code", fmt.Sprintf("%d", kafkaErr.Code())).Err(kafkaErr).Msg("kafka consumer error")
					if err := consumerBackoff(ctx, time.Second); err != nil {
						return err
					}
					continue
				}
				return fmt.Errorf("error reading message: %w", err)
			}

			if err := cons.handleWithRetry(ctx, msg, handler); err != nil {
				return err
			}

			// Needs enable.auto.offset.store=false; the background commit sends it.
			if err := cons.storeMessageAt(msg, epoch); err != nil {
				if errors.Is(err, ErrClientClosed) {
					return err
				}
				return fmt.Errorf("kafka: storing a handled offset failed: %w", err)
			}
		}
	}
}

func consumerBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (cons *Consumer) handleWithRetry(ctx context.Context, msg *ckf.Message, handler func(msg *ckf.Message) error) error {
	backoff := 100 * time.Millisecond
	limit := cons.retryLimit
	if limit <= 0 {
		limit = 6
	}
	for attempt := 1; ; attempt++ {
		err := handler(msg)
		if err == nil {
			return nil
		}
		log.Error().Err(err).Msg("kafka message handler error; retrying before advancing offsets")
		if attempt >= limit {
			return fmt.Errorf("kafka: handler retry budget exhausted before offset storage: %w", err)
		}
		if err := consumerBackoff(ctx, backoff); err != nil {
			return err
		}
		cons.mu.Lock()
		closed := cons.closed
		cons.mu.Unlock()
		if closed {
			return ErrClientClosed
		}
		backoff = min(2*backoff, 5*time.Second)
	}
}
