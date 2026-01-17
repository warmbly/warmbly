package config

import (
	"context"

	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
)

func (c *Config) LoadKafkaBootstrapServers(ctx context.Context) (string, error) {
	kafkaBootstrapServers, err := c.params.Get(ctx, "kafka/bootstrap_servers")
	if err != nil {
		return "", err
	}

	return kafkaBootstrapServers, nil
}

func (c *Config) LoadKafkaConfigSasl(ctx context.Context) (*kafka.SASLConfig, error) {
	kafkaSaslUsername, err := c.secrets.Get(ctx, "kafka/sasl/username")
	if err != nil {
		return nil, err
	}

	kafkaSaslPassword, err := c.secrets.Get(ctx, "kafka/sasl/password")
	if err != nil {
		return nil, err
	}

	return &kafka.SASLConfig{
		Username: kafkaSaslUsername,
		Password: kafkaSaslPassword,
	}, nil
}
