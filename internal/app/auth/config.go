package auth

import "time"

const (
	SessionTTL   = 10 * time.Minute
	AuthLimit    = 45 * time.Minute
	AuthAttempts = 3

	AuthSessionTTL   = 10 * time.Minute
	AuthEmailTTL     = 30 * time.Minute
	AuthEmailLimit   = 5
	PasswordResetTTL = 1 * time.Hour

	PasswordResetLimit    = 2
	PasswordResetLimitTTL = 4 * time.Hour
)
