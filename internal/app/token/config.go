package token

import "time"

const (
	SessionTTL = 12 * time.Hour
	// revokedTombstoneTTL bounds how long a revoked marker written in place of
	// a failed cache eviction lives before reads fall back to the row.
	revokedTombstoneTTL  = 5 * time.Minute
	RefreshTokenTTL      = 12 * time.Hour
	AccessTokenLifeTime  = 12 * time.Hour
	RefreshTokenLifeTime = 180 * 24 * time.Hour

	AuthProviderEmail    = "email"
	AuthProviderApple    = "apple"
	AuthProviderGoogle   = "google"
	AuthProviderWebAuthn = "webauthn"
	// AuthProviderOIDC covers generic single sign-on. Google and Apple keep
	// their own values, so the security page can name what was actually used.
	AuthProviderOIDC = "oidc"
)

// Token purposes. Every JWT this service signs carries one, and each verifier
// requires the purpose it expects.
//
// Without it, all of these tokens were interchangeable: they share one signing
// key and one claim shape, so a password-reset link token or the challenge
// token issued after a password but before the emailed code would open a
// websocket and stream a workspace's events. The Go side was safe only because
// each token is separately bound to a nonce in Redis or a row in `sessions`;
// the realtime service checks neither, and had nothing else to go on.
const (
	PurposeAccess         = "access"
	PurposeRefresh        = "refresh"
	PurposeWebSocket      = "ws"
	PurposeWebSocketProxy = "ws_proxy"
	PurposeLoginCode      = "login"
	PurposeRegistration   = "registration"
	PurposePasswordReset  = "reset"
	PurposeTwoFAPending   = "2fa"
	// PurposeSSOLink is a federated sign-in parked on an existing password
	// account until that password is presented; it opens nothing else.
	PurposeSSOLink = "sso_link"
)
