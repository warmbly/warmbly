package kafka

import (
	"context"
	"fmt"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog/log"
)

type Consumer struct {
	c      *ckf.Consumer
	Avrov2 *Avrov2
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

func (cons *Consumer) WithAvrov2(avrov2 *Avrov2) {
	cons.Avrov2 = avrov2
}

func (cons *Consumer) Close() {
	cons.c.Close()
}

func (cons *Consumer) SubscribeTopics(topics []string) error {
	return cons.c.SubscribeTopics(topics, nil)
}

func (cons *Consumer) Consume(ctx context.Context, handler func(msg *ckf.Message) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			msg, err := cons.c.ReadMessage(100 * time.Millisecond)
			if err != nil {
				if kafkaErr, ok := err.(ckf.Error); ok {
					if kafkaErr.Code() == ckf.ErrTimedOut {
						continue
					}
					// Log transient Kafka errors and retry after a brief delay
					log.Warn().Str("code", fmt.Sprintf("%d", kafkaErr.Code())).Err(kafkaErr).Msg("kafka consumer error")
					time.Sleep(time.Second)
					continue
				}
				return fmt.Errorf("error reading message: %w", err)
			}

			if err := handler(msg); err != nil {
				log.Error().Err(err).Msg("kafka message handler error")
			}

			cons.c.CommitMessage(msg)
		}
	}
}
