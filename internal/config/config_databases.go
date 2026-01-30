package config

import "context"

func (c *Config) LoadGeoDBPath(ctx context.Context) (string, error) {
	return c.GetString(ctx, "GEODB_PATH", "geodb_path")
}

func (c *Config) LoadPrimaryDBEndpoint(ctx context.Context) (string, error) {
	return c.GetSecret(ctx, "PRIMARY_DB", "postgres/primary")
}

func (c *Config) LoadPrimaryRedisEndpoint(ctx context.Context) (string, error) {
	return c.GetSecret(ctx, "REDIS", "redis/primary")
}

func (c *Config) LoadKafkaClusterEndpoint(ctx context.Context) (string, error) {
	return c.GetSecret(ctx, "KAFKA_CLUSTER", "kafka_cluster")
}

// Cassandra (NoSQL)

type AstraConfig struct {
	AstraDBID             string
	AstraDBRegion         string
	AstraKeyspaceName     string
	AstraApplicationToken string
}

func (c *Config) LoadAstraConfig(ctx context.Context) (*AstraConfig, error) {
	astraDBID, err := c.GetString(ctx, "ASTRA_DB_ID", "astra/db_id")
	if err != nil {
		return nil, err
	}

	astraDBRegion, err := c.GetString(ctx, "ASTRA_DB_REGION", "astra/db_region")
	if err != nil {
		return nil, err
	}

	astraDBKeyspaceName, err := c.GetString(ctx, "ASTRA_KEYSPACE_NAME", "astra/keyspace_name")
	if err != nil {
		return nil, err
	}

	applicationToken, err := c.GetSecret(ctx, "ASTRA_APPLICATION_TOKEN", "astra/application_token")
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
