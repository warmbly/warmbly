package models

import "github.com/google/uuid"

// Identity is the response for GET /v1/me: the caller's user and organization
// identity plus the credential's auth type and granted scopes. Unlike
// /v1/auth/me (JWT-only, no org name), this endpoint is reachable by API keys
// and OAuth tokens through the combined-auth gate, so integrations can validate
// a credential and render a human-readable connection label.
type Identity struct {
	UserID           uuid.UUID  `json:"user_id"`
	Email            string     `json:"email"`
	Name             string     `json:"name"`
	FirstName        string     `json:"first_name"`
	LastName         string     `json:"last_name"`
	OrganizationID   *uuid.UUID `json:"organization_id,omitempty"`
	OrganizationName string     `json:"organization_name,omitempty"`
	// AuthType is "api_key", "oauth", or "jwt".
	AuthType string `json:"auth_type"`
	// Scopes lists the granted API scopes for api_key/oauth callers; empty for
	// JWT sessions, which carry organization-role permissions instead.
	Scopes []string `json:"scopes"`
}
