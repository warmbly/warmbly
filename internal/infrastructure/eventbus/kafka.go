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

	// Kafka topics must exist before use, and a worker's command topic is
	// named after a node id issued at join time. See kafka_topics.go.
	topics topicEnsurer
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
		topics:    topicEnsurer{known: map[string]struct{}{}},
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
		topics:    topicEnsurer{known: map[string]struct{}{}},
	}
}

func (b *KafkaBus) Name() string { return "kafka" }

// Publish succeeds only after a native broker delivery report.
func (b *KafkaBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return ErrBusClosed
	}
	// A worker's command topic is named after its node id, so the first
	// publish to it is the first time anything knows the name.
	if err := b.ensureTopics(ctx, topic); err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(ctx, handlerTimeout())
	defer cancel()
	err := b.producer.ProduceConfirmed(pctx, topic, []byte(key), payload)
	if errors.Is(err, kafka.ErrClientClosed) {
		return ErrBusClosed
	}
	return err
}

// Subscribe creates a fresh consumer in the given group, subscribes to all
// topics, and blocks reading messages until ctx is cancelled or a fatal error
// occurs. Failed handlers never store offsets; exhausted retries reopen for replay.
func (b *KafkaBus) Subscribe(ctx context.Context, topics []string, group string, handler Handler) error {
	return b.SubscribeKeyed(ctx, topics, group, 1, nil, handler)
}

func (b *KafkaBus) SubscribeKeyed(ctx context.Context, topics []string, group string, lanes int, key KeyResolver, handler Handler) error {
	if len(topics) == 0 {
		return errors.New("eventbus kafka: at least one topic required")
	}
	if group == "" {
		return errors.New("eventbus kafka: consumer group required")
	}
	if handler == nil {
		return errors.New("eventbus kafka: handler required")
	}
	if lanes < 1 || lanes > 64 || (lanes > 1 && key == nil) {
		return errors.New("eventbus kafka: keyed subscription requires 1-64 lanes and a key resolver")
	}
	return retrySubscription(ctx, func(ctx context.Context) error {
		return b.subscribeOnce(ctx, topics, group, lanes, key, handler)
	})
}

func (b *KafkaBus) subscribeOnce(ctx context.Context, topics []string, group string, lanes int, key KeyResolver, handler Handler) error {

	// Subscribing to a topic that does not exist yet returns no messages and
	// no error, so a worker would sit silent rather than fail.
	if err := b.ensureTopics(ctx, topics...); err != nil {
		return err
	}

	cc := kafka.NewConsumer(b.bootstrap)
	if b.sasl != nil {
		cc.WithSASL(b.sasl)
	}
	cc.Set("group.id", group)
	cc.Set("auto.offset.reset", "earliest")
	// Offsets are stored once a message is handled and committed in the
	// background: a synchronous commit per message cost a broker round trip each.
	cc.Set("enable.auto.commit", true)
	cc.Set("enable.auto.offset.store", false)

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
		return ErrBusClosed
	}
	b.consumers = append(b.consumers, cons)
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		for i, c := range b.consumers {
			if c == cons {
				b.consumers = append(b.consumers[:i], b.consumers[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
		cons.Close()
	}()

	resolve := func(msg *ckf.Message) (string, error) {
		return resolveKey(ctx, key, kafkaEnvelope(msg))
	}
	deliver := kafkaHandler(ctx, handler)
	err = cons.ConsumeConcurrent(ctx, lanes, resolve, deliver)
	if errors.Is(err, kafka.ErrClientClosed) {
		return ErrBusClosed
	}
	if errors.Is(err, kafka.ErrAssignmentLost) {
		return errors.Join(ErrSubscriptionRebalanced, err)
	}
	return err
}

func kafkaHandler(ctx context.Context, handler Handler) func(*ckf.Message) error {
	var mu sync.Mutex
	attempts := map[*ckf.Message]int{}
	return func(msg *ckf.Message) error {
		mu.Lock()
		attempts[msg]++
		attempt := attempts[msg]
		mu.Unlock()
		envelope := kafkaEnvelope(msg)
		envelope.Attempt = attempt
		// A message already being handled finishes after a shutdown signal; only reading the next one stops.
		hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), handlerTimeout())
		defer cancel()
		if err := invokeHandler(hctx, handler, envelope); err != nil {
			log.Error().Err(err).Str("topic", envelope.Topic).Msg("eventbus kafka handler error")
			return err
		}
		mu.Lock()
		delete(attempts, msg)
		mu.Unlock()
		return nil
	}
}

func kafkaEnvelope(msg *ckf.Message) Message {
	topic := ""
	if msg.TopicPartition.Topic != nil {
		topic = *msg.TopicPartition.Topic
	}
	return Message{Topic: topic, Key: string(msg.Key), Payload: msg.Value, Attempt: 1, Redelivers: true}
}

// Close flushes the producer and closes every consumer that was opened via
// Subscribe.
func (b *KafkaBus) Close() error {
	// Marked closed before the client is cleared, so a Publish racing past the
	// closed check cannot open a replacement that outlives shutdown.
	b.topics.mu.Lock()
	b.topics.closed = true
	if b.topics.admin != nil {
		b.topics.admin.Close()
		b.topics.admin = nil
	}
	b.topics.mu.Unlock()

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
