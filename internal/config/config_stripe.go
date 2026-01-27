package config

import "context"

type StripeConfig struct {
	SecretKey      string
	WebhookSecret  string
	PublishableKey string
}

func (c *Config) LoadStripeConfig(ctx context.Context) (*StripeConfig, error) {
	secretKey, err := c.secrets.Get(ctx, c.GetKeyID("stripe/secret_key"))
	if err != nil {
		return nil, err
	}

	webhookSecret, err := c.secrets.Get(ctx, c.GetKeyID("stripe/webhook_secret"))
	if err != nil {
		return nil, err
	}

	publishableKey, err := c.params.Get(ctx, c.GetKeyID("stripe/publishable_key"))
	if err != nil {
		return nil, err
	}

	return &StripeConfig{
		SecretKey:      secretKey,
		WebhookSecret:  webhookSecret,
		PublishableKey: publishableKey,
	}, nil
}
