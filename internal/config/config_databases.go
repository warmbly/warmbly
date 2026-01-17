package config

import "context"

func (c *Config) LoadGeoDBPath(ctx context.Context) (string, error) {
	val, err := c.params.Get(ctx, c.GetKeyID("geodb_path"))
	if err != nil {
		return "", err
	}

	return val, nil
}

func (c *Config) LoadPrimaryDBEndpoint(ctx context.Context) (string, error) {
	val, err := c.secrets.Get(ctx, c.GetKeyID("postgres/primary"))
	if err != nil {
		return "", err
	}

	return val, nil
}

func (c *Config) LoadPrimaryRedisEndpoint(ctx context.Context) (string, error) {
	val, err := c.secrets.Get(ctx, c.GetKeyID("redis/primary"))
	if err != nil {
		return "", err
	}

	return val, nil
}

func (c *Config) LoadKafkaClusterEndpoint(ctx context.Context) (string, error) {
	val, err := c.secrets.Get(ctx, c.GetKeyID("kafka_cluster"))
	if err != nil {
		return "", err
	}

	return val, nil
}

// Cassandra (NoSQL)

type AstraConfig struct {
	AstraDBID             string
	AstraDBRegion         string
	AstraKeyspaceName     string
	AstraApplicationToken string
}

func (c *Config) LoadAstraConfig(ctx context.Context) (*AstraConfig, error) {
	astraDBID, err := c.params.Get(ctx, c.GetKeyID("astra/db_id"))
	if err != nil {
		return nil, err
	}

	astraDBRegion, err := c.params.Get(ctx, c.GetKeyID("astra/db_region"))
	if err != nil {
		return nil, err
	}

	astraDBKeyspaceName, err := c.params.Get(ctx, c.GetKeyID("astra/keyspace_name"))
	if err != nil {
		return nil, err
	}

	applicationToken, err := c.secrets.Get(ctx, c.GetKeyID("astra/application_token"))
	if err != nil {
		return nil, err
	}

	return &AstraConfig{
		AstraDBID:             astraDBID,
		AstraDBRegion:         astraDBRegion,
		AstraKeyspaceName:     astraDBKeyspaceName,
		AstraApplicationToken: applicationToken,
	}, nil
}
