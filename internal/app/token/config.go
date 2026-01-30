package token

import "time"

const (
	SessionTTL           = 30 * time.Minute
	RefreshTokenTTL      = 15 * time.Minute
	AccessTokenLifeTime  = 10 * time.Minute
	RefreshTokenLifeTime = 2 * 30 * 24 * time.Hour

	AuthProviderEmail  = "email"
	AuthProviderApple  = "apple"
	AuthProviderGoogle = "google"
)
