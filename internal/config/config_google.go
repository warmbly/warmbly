package config

import "context"

func (c *Config) LoadGoogleServiceAccount(ctx context.Context) (string, error) {
	serviceAccount, err := c.params.Get(ctx, "google-service-account")
	if err != nil {
		return "", err
	}

	return serviceAccount, nil
}
