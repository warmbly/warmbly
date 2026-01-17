package config

import "context"

func (c *Config) LoadSentryDSNApi(ctx context.Context) (string, error) {
	val, err := c.secrets.Get(ctx, c.GetKeyID("sentry_dsn/api"))
	if err != nil {
		return "", err
	}

	return val, nil
}

func (c *Config) LoadSentryDSNBackend(ctx context.Context) (string, error) {
	val, err := c.secrets.Get(ctx, c.GetKeyID("sentry_dsn/backend"))
	if err != nil {
		return "", err
	}

	return val, nil
}
