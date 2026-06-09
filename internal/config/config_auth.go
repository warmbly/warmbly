package config

import (
	"context"
	"net/url"
	"os"
	"strings"
)

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

	// TwoFASecret is the server-wide key the TOTP secret is sealed with (AES-GCM).
	// Must be set when 2FA is in use; falls back to AuthSecret when unset so a
	// deployment without the env var still functions (sealed under the auth key).
	TwoFASecret string

	// WebAuthn / passkey relying-party configuration.
	//
	// WebAuthnRPID is the relying-party ID a passkey is cryptographically
	// bound to — the registrable domain the dashboard is served from
	// (e.g. "app.warmbly.com"), with no scheme or port. WebAuthnRPOrigins
	// are the full scheme-qualified origins allowed to run ceremonies
	// (e.g. "https://app.warmbly.com"). Both are derived from APP_URL /
	// CORS_ALLOW_ORIGINS so self-hosted and local-dev deployments work out
	// of the box, and can be overridden explicitly with WEBAUTHN_RP_ID /
	// WEBAUTHN_RP_ORIGINS. Changing the RP ID invalidates every enrolled
	// passkey, so it must stay stable for a given deployment.
	WebAuthnRPID          string
	WebAuthnRPDisplayName string
	WebAuthnRPOrigins     []string
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

	// 2FA sealing key. Optional: falls back to AUTH_SECRET so existing
	// deployments keep working; rotating it invalidates enrolled TOTP secrets.
	twoFASecret := c.GetSecretOptional(ctx, "TWOFA_SECRET", "twofa_secret", authSecret)

	rpDisplayName := c.GetStringOptional(ctx, "WEBAUTHN_RP_DISPLAY_NAME", "webauthn/rp_display_name", "Warmbly")
	rpIDRaw := c.GetStringOptional(ctx, "WEBAUTHN_RP_ID", "webauthn/rp_id", "")
	rpOriginsRaw := c.GetStringOptional(ctx, "WEBAUTHN_RP_ORIGINS", "webauthn/rp_origins", "")
	rpID, rpOrigins := resolveWebAuthnRP(rpIDRaw, rpOriginsRaw)

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
		TwoFASecret:     twoFASecret,

		WebAuthnRPID:          rpID,
		WebAuthnRPDisplayName: rpDisplayName,
		WebAuthnRPOrigins:     rpOrigins,
	}, nil
}

// resolveWebAuthnRP derives the passkey relying-party ID and allowed origins.
//
// Origins come from (in order): an explicit WEBAUTHN_RP_ORIGINS list, then
// CORS_ALLOW_ORIGINS, then APP_URL, then a localhost dev fallback. The RP ID
// is an explicit override if given, otherwise the host of the first origin
// (scheme and port stripped), otherwise "localhost". This keeps prod, self
// hosted, and local-dev deployments correct without per-environment code.
func resolveWebAuthnRP(explicitRPID, explicitOrigins string) (string, []string) {
	originsRaw := explicitOrigins
	if originsRaw == "" {
		originsRaw = os.Getenv("CORS_ALLOW_ORIGINS")
	}
	if originsRaw == "" {
		originsRaw = os.Getenv("APP_URL")
	}

	origins := splitCSV(originsRaw)
	if len(origins) == 0 {
		origins = []string{"http://localhost:5173"}
	}

	rpID := strings.TrimSpace(explicitRPID)
	if rpID == "" {
		rpID = hostFromOrigin(origins[0])
	}
	if rpID == "" {
		rpID = "localhost"
	}

	return rpID, origins
}

// hostFromOrigin returns the bare hostname of an origin (no scheme, no port),
// suitable for use as a WebAuthn RP ID. Returns "" if the origin can't be
// parsed into a host (e.g. a wildcard "*").
func hostFromOrigin(origin string) string {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname()
}
