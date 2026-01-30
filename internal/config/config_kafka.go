package config

import (
	"context"

	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
)

func (c *Config) LoadKafkaBootstrapServers(ctx context.Context) (string, error) {
	return c.GetStringRaw(ctx, "KAFKA_BOOTSTRAP_SERVERS", "kafka/bootstrap_servers")
}

func (c *Config) LoadKafkaConfigSasl(ctx context.Context) (*kafka.SASLConfig, error) {
	kafkaSaslUsername, err := c.GetSecretRaw(ctx, "KAFKA_SASL_USERNAME", "kafka/sasl/username")
	if err != nil {
		return nil, err
	}

	kafkaSaslPassword, err := c.GetSecretRaw(ctx, "KAFKA_SASL_PASSWORD", "kafka/sasl/password")
	if err != nil {
		return nil, err
	}

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

	key, err = c.GetSecretRaw(ctx, "SCHEMA_REGISTRY_KEY", "kafka/schema_registry/key")
	if err != nil {
		return "", "", "", err
	}

	secret, err = c.GetSecretRaw(ctx, "SCHEMA_REGISTRY_SECRET", "kafka/schema_registry/secret")
	if err != nil {
		return "", "", "", err
	}

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

// LoadTrackingConsumerConfig loads configuration for the tracking events consumer
func (c *Config) LoadTrackingConsumerConfig(ctx context.Context) (*TrackingConsumerConfig, error) {
	brokers, err := c.LoadKafkaBootstrapServers(ctx)
	if err != nil {
		return nil, err
	}

	// Default topic and group ID, can be overridden via env or params
	topic := c.GetStringOptionalRaw(ctx, "KAFKA_TRACKING_TOPIC", "kafka/tracking/topic", "tracking-events")
	groupID := c.GetStringOptionalRaw(ctx, "KAFKA_CONSUMER_GROUP", "kafka/tracking/group_id", "tracking-consumer")

	// Load SASL credentials - optional for dev environments
	saslUsername := c.GetSecretOptionalRaw(ctx, "KAFKA_SASL_USERNAME", "kafka/sasl/username", "")
	if saslUsername == "" {
		return &TrackingConsumerConfig{
			Brokers:     brokers,
			Topic:       topic,
			GroupID:     groupID,
			SASLEnabled: false,
		}, nil
	}

	saslPassword := c.GetSecretOptionalRaw(ctx, "KAFKA_SASL_PASSWORD", "kafka/sasl/password", "")

	return &TrackingConsumerConfig{
		Brokers:      brokers,
		Topic:        topic,
		GroupID:      groupID,
		SASLEnabled:  true,
		SASLUsername: saslUsername,
		SASLPassword: saslPassword,
	}, nil
}
