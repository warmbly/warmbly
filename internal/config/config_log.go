package config

import "context"

func (c *Config) LoadSentryDSNApi(ctx context.Context) (string, error) {
	return c.GetSecret(ctx, "SENTRY_DSN_API", "sentry_dsn/api")
}

func (c *Config) LoadSentryDSNBackend(ctx context.Context) (string, error) {
	return c.GetSecret(ctx, "SENTRY_DSN", "sentry_dsn/backend")
}
