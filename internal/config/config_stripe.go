package config

import "context"

type StripeConfig struct {
	SecretKey      string
	WebhookSecret  string
	PublishableKey string

	// CreditPackPriceIDs maps a credit-pack key (credits.CreditPack.Key, e.g.
	// "pack_500") to its Stripe one-time price id. Optional: an unset pack is
	// simply not purchasable. Loaded from STRIPE_CREDIT_PACK_<CREDITS>_PRICE_ID.
	CreditPackPriceIDs map[string]string
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

	// Credit top-up pack prices are optional; each is looked up independently so
	// partial configuration (e.g. only the 500 pack) still works.
	packPrices := map[string]string{
		"pack_500":   c.GetStringOptional(ctx, "STRIPE_CREDIT_PACK_500_PRICE_ID", "stripe/credit_pack_500_price_id", ""),
		"pack_2000":  c.GetStringOptional(ctx, "STRIPE_CREDIT_PACK_2000_PRICE_ID", "stripe/credit_pack_2000_price_id", ""),
		"pack_10000": c.GetStringOptional(ctx, "STRIPE_CREDIT_PACK_10000_PRICE_ID", "stripe/credit_pack_10000_price_id", ""),
	}

	return &StripeConfig{
		SecretKey:          secretKey,
		WebhookSecret:      webhookSecret,
		PublishableKey:     publishableKey,
		CreditPackPriceIDs: packPrices,
	}, nil
}
