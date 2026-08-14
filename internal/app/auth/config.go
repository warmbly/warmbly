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

	// KnownDeviceTTL is how long a device stays exempt from the login code
	// under AUTH_LOGIN_CODE=new_device.
	KnownDeviceTTL = 90 * 24 * time.Hour
)

// Email send-budget flows. Separate keys so registration traffic cannot exhaust
// a user's login budget.
const (
	emailFlowLogin        = "login"
	emailFlowRegistration = "registration"
)
