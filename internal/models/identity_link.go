package models

import "time"

// Federated identity providers.
const (
	IdentityProviderOIDC   = "oidc"
	IdentityProviderGoogle = "google"
	IdentityProviderApple  = "apple"
)

// UserIdentity binds a local account to an external identity. The account is
// resolved by (Issuer, Subject), never by Email alone: an operator-configured
// issuer may let anyone register an arbitrary address.
type UserIdentity struct {
	Provider    string     `json:"provider"`
	Issuer      string     `json:"issuer"`
	Subject     string     `json:"subject"`
	Email       string     `json:"email,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}
