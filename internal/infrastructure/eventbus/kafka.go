//go:build kafka

// This file is compiled only with the `kafka` build tag. The default build is
// pure-Go (NATS + JSON), so it never links confluent-kafka-go / librdkafka and
// needs no CGO. Build with `-tags kafka` to include the Kafka backend.
package eventbus

import (
	"context"
	"errors"
	"fmt"
	"sync"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
)

// KafkaBus is the EventBus backed by Confluent / Apache Kafka. It wraps the
// existing internal/infrastructure/kafka producer + consumer rather than
// reimplementing them, so existing callers (and the schema-registry / Avro
// wiring they depend on) keep working unchanged.
//
// A single KafkaBus owns one shared Producer. Each Subscribe call creates a
// fresh Consumer dedicated to that subscription; this matches Kafka's
// consumer-group model where one process can join multiple groups
// independently. All consumers are closed when Close is called.
type KafkaBus struct {
	producer *kafka.Producer

	bootstrap string
	sasl      *kafka.SASLConfig

	mu        sync.Mutex
	consumers []*kafka.Consumer
	closed    bool
}

// NewKafka constructs a KafkaBus and opens the shared producer connection.
func NewKafka(cfg KafkaConfig) (*KafkaBus, error) {
	if cfg.Bootstrap == "" {
		return nil, errors.New("eventbus kafka: bootstrap servers required")
	}
	pc := kafka.NewProducer(cfg.Bootstrap)
	if cfg.SASL != nil {
		pc.WithSASL(cfg.SASL)
	}
	prod, err := pc.Connect()
	if err != nil {
		return nil, fmt.Errorf("eventbus kafka: connect producer: %w", err)
	}
	return &KafkaBus{
		producer:  prod,
		bootstrap: cfg.Bootstrap,
		sasl:      cfg.SASL,
	}, nil
}

// NewKafkaFromProducer wraps an already-constructed kafka.Producer. Useful
// during the migration period where producer / Avro wiring lives in main and
// the bus should reuse the same connection.
//
// The returned KafkaBus does not own the producer's lifecycle: Close will
// still flush + close it, so callers must not double-close.
func NewKafkaFromProducer(p *kafka.Producer, cfg KafkaConfig) *KafkaBus {
	return &KafkaBus{
		producer:  p,
		bootstrap: cfg.Bootstrap,
		sasl:      cfg.SASL,
	}
}

func (b *KafkaBus) Name() string { return "kafka" }

// Publish writes to the underlying producer. The context is checked for
// cancellation before the call but not used to bound the produce itself: the
// confluent-kafka-go producer is asynchronous and Produce returns immediately
// after enqueuing.
func (b *KafkaBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return errors.New("eventbus kafka: bus closed")
	}
	return b.producer.Produce(topic, []byte(key), payload)
}

// Subscribe creates a fresh consumer in the given group, subscribes to all
// topics, and blocks reading messages until ctx is cancelled or a fatal error
// occurs. Handler errors are logged but do not abort the loop; the message is
// still committed to match the existing kafka.Consumer.Consume behaviour.
func (b *KafkaBus) Subscribe(ctx context.Context, topics []string, group string, handler Handler) error {
	if len(topics) == 0 {
		return errors.New("eventbus kafka: at least one topic required")
	}
	if group == "" {
		return errors.New("eventbus kafka: consumer group required")
	}
	if handler == nil {
		return errors.New("eventbus kafka: handler required")
	}

	cc := kafka.NewConsumer(b.bootstrap)
	if b.sasl != nil {
		cc.WithSASL(b.sasl)
	}
	cc.Set("group.id", group)
	cc.Set("auto.offset.reset", "earliest")

	cons, err := cc.Connect()
	if err != nil {
		return fmt.Errorf("eventbus kafka: connect consumer: %w", err)
	}
	if err := cons.SubscribeTopics(topics); err != nil {
		cons.Close()
		return fmt.Errorf("eventbus kafka: subscribe: %w", err)
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cons.Close()
		return errors.New("eventbus kafka: bus closed")
	}
	b.consumers = append(b.consumers, cons)
	b.mu.Unlock()

	return cons.Consume(ctx, func(msg *ckf.Message) error {
		topic := ""
		if msg.TopicPartition.Topic != nil {
			topic = *msg.TopicPartition.Topic
		}
		hctx, cancel := context.WithTimeout(ctx, handlerTimeout())
		defer cancel()
		if err := handler(hctx, Message{
			Topic:   topic,
			Key:     string(msg.Key),
			Payload: msg.Value,
		}); err != nil {
			log.Error().Err(err).Str("topic", topic).Msg("eventbus kafka handler error")
			return err
		}
		return nil
	})
}

// Close flushes the producer and closes every consumer that was opened via
// Subscribe.
func (b *KafkaBus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	consumers := b.consumers
	b.consumers = nil
	b.mu.Unlock()

	for _, c := range consumers {
		c.Close()
	}
	if b.producer != nil {
		b.producer.Close()
	}
	return nil
}

// Producer exposes the underlying *kafka.Producer for legacy callers that
// need the Avrov2 serializer attached or other Kafka-specific knobs. New code
// should not depend on this; it exists to keep the migration incremental.
func (b *KafkaBus) Producer() *kafka.Producer { return b.producer }

// Compile-time interface check.
var _ EventBus = (*KafkaBus)(nil)
