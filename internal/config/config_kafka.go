package config

import (
	"context"

	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
)

func (c *Config) LoadKafkaBootstrapServers(ctx context.Context) (string, error) {
	return c.GetStringRaw(ctx, "KAFKA_BOOTSTRAP_SERVERS", "kafka/bootstrap_servers")
}

func (c *Config) LoadKafkaConfigSasl(ctx context.Context) (*kafka.SASLConfig, error) {
	kafkaSaslUsername := c.GetSecretOptionalRaw(ctx, "KAFKA_SASL_USERNAME", "kafka/sasl/username", "")
	if kafkaSaslUsername == "" {
		return nil, nil
	}

	kafkaSaslPassword := c.GetSecretOptionalRaw(ctx, "KAFKA_SASL_PASSWORD", "kafka/sasl/password", "")

	return &kafka.SASLConfig{
		Username: kafkaSaslUsername,
		Password: kafkaSaslPassword,
	}, nil
}

func (c *Config) LoadSchemaRegistryConfig(ctx context.Context) (endpoint, key, secret string, err error) {
	endpoint, err = c.GetStringRaw(ctx, "SCHEMA_REGISTRY_URL", "kafka/schema_registry/endpoint")
	if err != nil {
		return "", "", "", err
	}

	key = c.GetSecretOptionalRaw(ctx, "SCHEMA_REGISTRY_KEY", "kafka/schema_registry/key", "")
	secret = c.GetSecretOptionalRaw(ctx, "SCHEMA_REGISTRY_SECRET", "kafka/schema_registry/secret", "")

	return endpoint, key, secret, nil
}

// TrackingConsumerConfig holds configuration for the tracking events consumer
type TrackingConsumerConfig struct {
	Brokers      string
	Topic        string
	GroupID      string
	SASLEnabled  bool
	SASLUsername string
	SASLPassword string
}

// LoadTrackingConsumerConfig loads the tracking-events topic + group. Transport
// (brokers / SASL) is owned by the shared event bus, so this never fails and a
// NATS self-host that sets no Kafka env still gets a valid topic + group.
func (c *Config) LoadTrackingConsumerConfig(ctx context.Context) (*TrackingConsumerConfig, error) {
	return &TrackingConsumerConfig{
		Topic:   c.GetStringOptionalRaw(ctx, "KAFKA_TRACKING_TOPIC", "kafka/tracking/topic", "tracking-events"),
		GroupID: c.GetStringOptionalRaw(ctx, "KAFKA_CONSUMER_GROUP", "kafka/tracking/group_id", "tracking-consumer"),
	}, nil
}
