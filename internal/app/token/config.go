package token

import "time"

const (
	SessionTTL           = 12 * time.Hour
	RefreshTokenTTL      = 12 * time.Hour
	AccessTokenLifeTime  = 12 * time.Hour
	RefreshTokenLifeTime = 180 * 24 * time.Hour

	AuthProviderEmail    = "email"
	AuthProviderApple    = "apple"
	AuthProviderGoogle   = "google"
	AuthProviderWebAuthn = "webauthn"
)
