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

	// DeviceToken is set when a confirmed login code asked to remember the
	// browser. The client sends it back with the next password sign-in.
	DeviceToken string `json:"device_token,omitempty"`

	// LinkRequired: a federated sign-in on an existing password account, which
	// links and signs in only once that password reaches POST /auth/sso/link.
	LinkRequired bool   `json:"link_required,omitempty"`
	LinkEmail    string `json:"link_email,omitempty"`
	LinkProvider string `json:"link_provider,omitempty"`
}

// SSOLinkPending is the Redis-backed state for a federated sign-in waiting on
// the account's password; the identity is held here, never taken from the
// confirming request.
type SSOLinkPending struct {
	UserID   uuid.UUID `json:"user_id"`
	Nonce    string    `json:"nonce"`
	Provider string    `json:"provider"`
	Issuer   string    `json:"issuer"`
	Subject  string    `json:"subject"`
	Email    string    `json:"email"`
}

// TwoFAPending is the Redis-backed state for an in-flight 2FA login challenge,
// keyed by the pending session id. It binds the pending JWT's nonce (single-use)
// and counts attempts (brute-force guard, since RateLimitMiddleware is a no-op
// pre-login).
type TwoFAPending struct {
	UserID uuid.UUID `json:"user_id"`
	Nonce  string    `json:"nonce"`
	// LinkIdentity is attached to the account only once the code passes, so a
	// federated sign-in on a 2FA account links after both factors, not one.
	LinkIdentity *UserIdentity `json:"link_identity,omitempty"`
	// AuthProvider is what the resulting session records; empty means email.
	AuthProvider string `json:"auth_provider,omitempty"`
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

	// MFAVerified is true when a second factor was presented to establish this
	// session: a TOTP code, a recovery code, or a passkey (which proves
	// possession of the device and, through the platform, the person). It is
	// separate from AuthProvider because a password sign-in that then passed
	// TOTP and one that did not both record "email".
	MFAVerified bool `json:"mfa_verified"`

	// ReauthAt is when this session last re-proved the account holder. Nil
	// means never. Sensitive account changes require it to be recent; see
	// RequireFreshAuth.
	ReauthAt *time.Time `json:"reauth_at,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	ExpiresAt *time.Time `json:"expires_at"`

	LastRefreshedAt time.Time `json:"last_refreshed_at"`
	RefreshNonce    string    `json:"refresh_nonce"`
	AccessNonce     string    `json:"access_nonce"`
}

// Live reports whether the session can still authenticate at now: not revoked
// and not past its absolute expiry, which no refresh moves.
func (s *Session) Live(now time.Time) bool {
	if s == nil || s.RevokedAt != nil {
		return false
	}
	return s.ExpiresAt == nil || now.Before(*s.ExpiresAt)
}

// AuthSession is what LoginStart and RegistrationStart return.
//
// Session plus CodeRequired=true is the original two-step flow: a code was
// emailed and the caller POSTs it to the matching /confirm endpoint. When the
// deployment has AUTH_LOGIN_CODE off, or the device is already known, no code
// is sent and the remaining fields carry the completed login instead. The extra
// fields are additive, so a client that only reads `session` still works.
type AuthSession struct {
	Session string `json:"session,omitempty"`

	CodeRequired bool `json:"code_required"`

	Token         *Token `json:"token,omitempty"`
	TwoFARequired bool   `json:"two_fa_required,omitempty"`
	PendingToken  string `json:"pending_token,omitempty"`
	ExpiresIn     int    `json:"expires_in,omitempty"`

	// PasswordChangeRequired: the password was an issued initial one, and
	// PendingToken goes to POST /auth/login/initial-password with the new one.
	PasswordChangeRequired bool `json:"password_change_required,omitempty"`
}

// InitialPasswordPending is a password sign-in parked until the holder of an
// issued initial password chooses their own.
type InitialPasswordPending struct {
	UserID        uuid.UUID `json:"user_id"`
	Nonce         string    `json:"nonce"`
	Email         string    `json:"email"`
	AnomalyReason string    `json:"anomaly_reason,omitempty"`
}

type LoginSession struct {
	CodeHash string `json:"code_hash"`
	Nonce    string `json:"nonce"`
	// AnomalyReason carries the verdict that CAUSED this challenge, so the
	// completed sign-in is recorded as the anomaly it was. Recomputing it at
	// confirm would judge different history, and possibly a different address,
	// and could store a challenged sign-in as clean — which is exactly the
	// accounting the repeat threshold depends on.
	AnomalyReason string `json:"anomaly_reason,omitempty"`
}

type RegistrationSession struct {
	PasswordHash string `json:"password_hash"`
	CodeHash     string `json:"code_hash"`
	Nonce        string `json:"nonce"`
	// ReferralCode is the optional referral code captured at RegistrationStart,
	// applied for attribution once the account + org are created at confirm.
	ReferralCode string `json:"referral_code,omitempty"`
	// Invite is the invitation token captured at RegistrationStart, re-checked
	// and redeemed at confirm so the account lands in the inviting org.
	Invite string `json:"invite,omitempty"`
	// Acquisition is where the signup came from, captured at
	// RegistrationStart and written onto the org once it exists at confirm.
	// Omitted when the signup carried nothing, which is most of them.
	Acquisition *OrgAcquisition `json:"acquisition,omitempty"`
}
