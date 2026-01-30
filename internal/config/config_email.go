package config

import "context"

type EmailConfig struct {
	EmailName      string
	EmailAddress   string
	TrackingDomain string
}

func (c *Config) LoadEmailConfig(ctx context.Context) (*EmailConfig, error) {
	emailName, err := c.GetStringRaw(ctx, "EMAIL_NAME", "email/name")
	if err != nil {
		return nil, err
	}

	emailAddress, err := c.GetStringRaw(ctx, "EMAIL_ADDRESS", "email/address")
	if err != nil {
		return nil, err
	}

	trackingDomain, err := c.GetStringRaw(ctx, "TRACKING_DOMAIN", "tracking_domain")
	if err != nil {
		return nil, err
	}

	return &EmailConfig{
		EmailName:      emailName,
		EmailAddress:   emailAddress,
		TrackingDomain: trackingDomain,
	}, nil
}
