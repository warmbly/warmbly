package models

import (
	"time"

	"github.com/google/uuid"
)

// OAuth2 authorization server domain types. Apps register as OAuth clients;
// users grant them scoped access via the authorization-code flow (client secret
// required, PKCE optional); the issued access token carries an API-permission
// bitmask (Scopes) and authenticates API calls through the same gates as an API key.

type OAuthAppStatus string

const (
	OAuthAppActive   OAuthAppStatus = "active"
	OAuthAppDisabled OAuthAppStatus = "disabled"
)

// Credential prefixes mirror the api_keys `wmbly_` convention so a leaked token
// is greppable and self-describing.
const (
	OAuthClientIDPrefix     = "wmcid_"
	OAuthClientSecretPrefix = "wmcs_"
	OAuthAccessTokenPrefix  = "wmat_"
	OAuthRefreshTokenPrefix = "wmrt_"
	OAuthCodePrefix         = "wmac_"
)

// Lifetimes. The authorization code is single-use and short; access tokens are
// short-lived; refresh tokens are long-lived and rotate on every exchange.
const (
	OAuthAuthorizationCodeTTL = 10 * time.Minute
	OAuthAccessTokenTTL       = time.Hour
	OAuthRefreshTokenTTL      = 90 * 24 * time.Hour
)

// OAuthApplication is a registered third-party OAuth client.
type OAuthApplication struct {
	ID               uuid.UUID      `json:"id"`
	OrganizationID   uuid.UUID      `json:"organization_id"`
	CreatedBy        uuid.UUID      `json:"created_by"`
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	LogoURL          string         `json:"logo_url"`
	WebsiteURL       string         `json:"website_url"`
	ClientID         string         `json:"client_id"`
	ClientSecretHash string         `json:"-"`
	RedirectURIs     []string       `json:"redirect_uris"`
	Scopes           uint64         `json:"scopes"`
	Status           OAuthAppStatus `json:"status"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// OAuthApplicationWithSecret is returned exactly once, on create or secret
// rotation; the plaintext secret is never stored or shown again.
type OAuthApplicationWithSecret struct {
	OAuthApplication
	ClientSecret string `json:"client_secret,omitempty"`
}

// OAuthApplicationWrite is the create/update payload from the developer UI.
type OAuthApplicationWrite struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	LogoURL      string   `json:"logo_url"`
	WebsiteURL   string   `json:"website_url"`
	RedirectURIs []string `json:"redirect_uris"`
	Scopes       uint64   `json:"scopes"`
}

// OAuthAuthorizationCode is a single-use code bound to a PKCE challenge and the
// exact scopes/redirect the user consented to.
type OAuthAuthorizationCode struct {
	ID                  uuid.UUID
	CodeHash            string
	ApplicationID       uuid.UUID
	OrganizationID      uuid.UUID
	UserID              uuid.UUID
	RedirectURI         string
	Scopes              uint64
	CodeChallenge       string
	CodeChallengeMethod string
	UsedAt              *time.Time
	ExpiresAt           time.Time
	CreatedAt           time.Time
}

// OAuthAccessGrant is an issued access+refresh token pair (tokens stored hashed).
type OAuthAccessGrant struct {
	ID               uuid.UUID  `json:"id"`
	ApplicationID    uuid.UUID  `json:"application_id"`
	OrganizationID   uuid.UUID  `json:"organization_id"`
	UserID           uuid.UUID  `json:"user_id"`
	Scopes           uint64     `json:"scopes"`
	AccessTokenHash  string     `json:"-"`
	RefreshTokenHash string     `json:"-"`
	AccessExpiresAt  time.Time  `json:"access_expires_at"`
	RefreshExpiresAt *time.Time `json:"refresh_expires_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// OAuthAuthorizedApp is one row in a user's "apps you've authorized" list (a
// grant joined to its application's display fields).
type OAuthAuthorizedApp struct {
	ApplicationID uuid.UUID  `json:"application_id"`
	Name          string     `json:"name"`
	LogoURL       string     `json:"logo_url"`
	WebsiteURL    string     `json:"website_url"`
	Scopes        uint64     `json:"scopes"`
	AuthorizedAt  time.Time  `json:"authorized_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
}
