package config

import "context"

type StripeConfig struct {
	SecretKey      string
	WebhookSecret  string
	PublishableKey string
}

func (c *Config) LoadStripeConfig(ctx context.Context) (*StripeConfig, error) {
	secretKey, err := c.GetSecret(ctx, "STRIPE_SECRET_KEY", "stripe/secret_key")
	if err != nil {
		return nil, err
	}

	webhookSecret, err := c.GetSecret(ctx, "STRIPE_WEBHOOK_SECRET", "stripe/webhook_secret")
	if err != nil {
		return nil, err
	}

	publishableKey, err := c.GetString(ctx, "STRIPE_PUBLISHABLE_KEY", "stripe/publishable_key")
	if err != nil {
		return nil, err
	}

	return &StripeConfig{
		SecretKey:      secretKey,
		WebhookSecret:  webhookSecret,
		PublishableKey: publishableKey,
	}, nil
}
