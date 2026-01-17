package config

import "context"

type AuthConfig struct {
	GoogleClientID     string
	GoogleRedirectURI  string
	GoogleClientSecret string

	AppleAppID     string
	AppleTeamID    string
	AppleKeyID     string
	AppleKeySecret string

	AuthSecret      string
	TurnstileSecret string
}

func (c *Config) LoadAuthConfig(ctx context.Context) (*AuthConfig, error) {
	googleClientID, err := c.params.Get(ctx, c.GetKeyID("google-auth/client_id"))
	if err != nil {
		return nil, err
	}

	googleRedirectURI, err := c.params.Get(ctx, c.GetKeyID("google-auth/redirect_uri"))
	if err != nil {
		return nil, err
	}

	googleClientSecret, err := c.secrets.Get(ctx, c.GetKeyID("google-auth/client_secret"))
	if err != nil {
		return nil, err
	}

	appleAppID, err := c.params.Get(ctx, c.GetKeyID("apple-auth/app_id"))
	if err != nil {
		return nil, err
	}

	appleTeamID, err := c.params.Get(ctx, c.GetKeyID("apple-auth/team_id"))
	if err != nil {
		return nil, err
	}

	appleKeyID, err := c.params.Get(ctx, c.GetKeyID("apple-auth/key_id"))
	if err != nil {
		return nil, err
	}

	appleKeySecret, err := c.secrets.Get(ctx, c.GetKeyID("apple-auth/key_secret"))
	if err != nil {
		return nil, err
	}

	authSecret, err := c.secrets.Get(ctx, c.GetKeyID("auth_secret"))
	if err != nil {
		return nil, err
	}

	turnstileSecret, err := c.secrets.Get(ctx, c.GetKeyID("turnstile/secret"))
	if err != nil {
		return nil, err
	}

	return &AuthConfig{
		GoogleClientID:     googleClientID,
		GoogleClientSecret: googleClientSecret,
		GoogleRedirectURI:  googleRedirectURI,

		AppleAppID:     appleAppID,
		AppleTeamID:    appleTeamID,
		AppleKeyID:     appleKeyID,
		AppleKeySecret: appleKeySecret,

		AuthSecret:      authSecret,
		TurnstileSecret: turnstileSecret,
	}, nil
}
