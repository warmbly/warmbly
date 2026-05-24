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
	TurnstileBypass string
}

func (c *Config) LoadAuthConfig(ctx context.Context) (*AuthConfig, error) {
	googleClientID, err := c.GetString(ctx, "GOOGLE_CLIENT_ID", "google-auth/client_id")
	if err != nil {
		return nil, err
	}

	googleRedirectURI, err := c.GetString(ctx, "GOOGLE_REDIRECT_URI", "google-auth/redirect_uri")
	if err != nil {
		return nil, err
	}

	googleClientSecret, err := c.GetSecret(ctx, "GOOGLE_CLIENT_SECRET", "google-auth/client_secret")
	if err != nil {
		return nil, err
	}

	appleAppID, err := c.GetString(ctx, "APPLE_APP_ID", "apple-auth/app_id")
	if err != nil {
		return nil, err
	}

	appleTeamID, err := c.GetString(ctx, "APPLE_TEAM_ID", "apple-auth/team_id")
	if err != nil {
		return nil, err
	}

	appleKeyID, err := c.GetString(ctx, "APPLE_KEY_ID", "apple-auth/key_id")
	if err != nil {
		return nil, err
	}

	appleKeySecret, err := c.GetSecret(ctx, "APPLE_KEY_SECRET", "apple-auth/key_secret")
	if err != nil {
		return nil, err
	}

	authSecret, err := c.GetSecret(ctx, "AUTH_SECRET", "auth_secret")
	if err != nil {
		return nil, err
	}

	turnstileSecret, err := c.GetSecret(ctx, "TURNSTILE_SECRET", "turnstile/secret")
	if err != nil {
		return nil, err
	}
	turnstileBypass := c.GetSecretOptional(ctx, "TURNSTILE_BYPASS_TOKEN", "turnstile/bypass_token", "")

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
		TurnstileBypass: turnstileBypass,
	}, nil
}
