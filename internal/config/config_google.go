package config

import "context"

func (c *Config) LoadGoogleServiceAccount(ctx context.Context) (string, error) {
	return c.GetStringRaw(ctx, "GOOGLE_APPLICATION_CREDENTIALS_JSON", "google-service-account")
}
