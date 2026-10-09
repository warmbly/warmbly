//go:build kafka

package kafka

import (
	"context"
	"fmt"
	"sync"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog/log"
)

type producerClient interface {
	Produce(*ckf.Message, chan ckf.Event) error
	Flush(int) int
	Close()
}

type Producer struct {
	p             producerClient
	Avrov2        *Avrov2
	mu            sync.Mutex
	closed        bool
	eventsDrained <-chan struct{}
}

type ProducerConfig struct {
	config map[string]ckf.ConfigValue
}

func NewProducer(servers string) *ProducerConfig {
	return &ProducerConfig{
		config: map[string]ckf.ConfigValue{
			"bootstrap.servers": servers,
			"acks":              "all",
		},
	}
}

func (conf *ProducerConfig) WithSASL(saslConfig *SASLConfig) {
	for key, value := range saslConfig.Generate() {
		conf.Set(key, value)
	}
}

func (conf *ProducerConfig) Set(key string, value ckf.ConfigValue) {
	conf.config[key] = value
}

func (conf *ProducerConfig) Connect() (*Producer, error) {
	cm := &ckf.ConfigMap{}
	for k, v := range conf.config {
		if err := cm.SetKey(k, v); err != nil {
			return nil, err
		}
	}
	p, err := ckf.NewProducer(cm)
	if err != nil {
		return nil, err
	}

	eventsDrained := make(chan struct{})
	go func() {
		defer close(eventsDrained)
		for event := range p.Events() {
			switch event := event.(type) {
			case *ckf.Message:
				if event != nil && event.TopicPartition.Error != nil {
					log.Warn().Err(event.TopicPartition.Error).Msg("kafka background delivery failed")
				}
			case ckf.Error:
				log.Debug().Str("code", event.Code().String()).Msg("kafka producer event")
			}
		}
	}()

	return &Producer{
		p:             p,
		eventsDrained: eventsDrained,
	}, nil
}

func (pr *Producer) WithAvrov2(avrov2 *Avrov2) {
	pr.Avrov2 = avrov2
}

func (pr *Producer) Close() {
	pr.mu.Lock()
	if pr.closed {
		pr.mu.Unlock()
		return
	}
	pr.closed = true
	pr.mu.Unlock()
	outstanding := pr.p.Flush(30_000) // 30 seconds
	if outstanding > 0 {
		log.Warn().Int("count", outstanding).Msg("kafka producer has unflushed events during shutdown")
	}
	pr.p.Close()
	if pr.eventsDrained != nil {
		<-pr.eventsDrained
	}
}

func (pr *Producer) Produce(topic string, key, value []byte) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.closed {
		return ErrClientClosed
	}
	return pr.p.Produce(&ckf.Message{
		TopicPartition: ckf.TopicPartition{Topic: &topic, Partition: ckf.PartitionAny},
		Key:            key,
		Value:          value,
		Timestamp:      time.Now(),
	}, nil)
}

// ProduceConfirmed waits for broker delivery, not merely local queue admission.
func (pr *Producer) ProduceConfirmed(ctx context.Context, topic string, key, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	report := make(chan ckf.Event, 1)
	pr.mu.Lock()
	if pr.closed {
		pr.mu.Unlock()
		return ErrClientClosed
	}
	err := pr.p.Produce(&ckf.Message{
		TopicPartition: ckf.TopicPartition{Topic: &topic, Partition: ckf.PartitionAny},
		Key:            key,
		Value:          value,
		Timestamp:      time.Now(),
	}, report)
	pr.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case event := <-report:
		msg, ok := event.(*ckf.Message)
		if !ok || msg == nil {
			return fmt.Errorf("kafka: unexpected delivery report %T", event)
		}
		return msg.TopicPartition.Error
	}
}
