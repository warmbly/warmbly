package models

import (
	"time"

	"github.com/google/uuid"
)

type Token struct {
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

// LoginResult is what LoginConfirm returns: either the full token pair, or a
// 2FA challenge (a short-lived, single-use pending token instead of a session).
// The embedded *Token is nil when TwoFARequired (its fields are then omitted).
type LoginResult struct {
	*Token
	TwoFARequired bool   `json:"two_fa_required,omitempty"`
	PendingToken  string `json:"pending_token,omitempty"`
	ExpiresIn     int    `json:"expires_in,omitempty"`
}

// TwoFAPending is the Redis-backed state for an in-flight 2FA login challenge,
// keyed by the pending session id. It binds the pending JWT's nonce (single-use)
// and counts attempts (brute-force guard, since RateLimitMiddleware is a no-op
// pre-login).
type TwoFAPending struct {
	UserID uuid.UUID `json:"user_id"`
	Nonce  string    `json:"nonce"`
	Tries  int       `json:"tries"`
}

type Session struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`

	// Current organization context for multi-org support
	CurrentOrganizationID *uuid.UUID `json:"current_organization_id,omitempty"`

	LocationCity        string `json:"location_city"`
	LocationRegion      string `json:"location_region"`
	LocationCountry     string `json:"location_country"`
	LocationCountryCode string `json:"location_country_code"`
	LocationPostalCode  string `json:"location_postal_code"`

	BrowserName string `json:"browser_name"`
	OSName      string `json:"os_name"`

	// How this session authenticated: email, google, apple, webauthn.
	AuthProvider string `json:"auth_provider"`

	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	ExpiresAt *time.Time `json:"expires_at"`

	LastRefreshedAt time.Time `json:"last_refreshed_at"`
	RefreshNonce    string    `json:"refresh_nonce"`
	AccessNonce     string    `json:"access_nonce"`
}

type AuthSession struct {
	Session string `json:"session"`
}

type LoginSession struct {
	CodeHash string `json:"code_hash"`
	Tries    int    `json:"tries"`
	Nonce    string `json:"nonce"`
}

type RegistrationSession struct {
	PasswordHash string `json:"password_hash"`
	CodeHash     string `json:"code_hash"`
	Tries        int    `json:"tries"`
	Nonce        string `json:"nonce"`
	// ReferralCode is the optional referral code captured at RegistrationStart,
	// applied for attribution once the account + org are created at confirm.
	ReferralCode string `json:"referral_code,omitempty"`
}
