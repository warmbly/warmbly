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
