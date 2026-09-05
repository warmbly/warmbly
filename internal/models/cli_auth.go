package models

import (
	"time"

	"github.com/google/uuid"
)

// CLI sign-in: `warmbly auth login` opens a device-code handshake, a member
// approves it in the browser, and the approval mints an ordinary API key.

// CLIAuthCodeStatus is the lifecycle of one handshake.
type CLIAuthCodeStatus string

const (
	CLIAuthCodePending  CLIAuthCodeStatus = "pending"
	CLIAuthCodeApproved CLIAuthCodeStatus = "approved"
	// Claimed: the CLI has fetched its key, the code is spent.
	CLIAuthCodeClaimed CLIAuthCodeStatus = "claimed"
	CLIAuthCodeDenied  CLIAuthCodeStatus = "denied"
)

// CLIAuthCode is what the approving member is shown before deciding.
type CLIAuthCode struct {
	ID             uuid.UUID         `json:"id"`
	UserCode       string            `json:"user_code"`
	ClientName     string            `json:"client_name"`
	Hostname       string            `json:"hostname"`
	CLIVersion     string            `json:"cli_version"`
	Scopes         uint64            `json:"scopes"`
	ScopeNames     []string          `json:"scope_names"`
	Status         CLIAuthCodeStatus `json:"status"`
	OrganizationID *uuid.UUID        `json:"organization_id,omitempty"`
	// APIKeyID is set only on the approval response: the key that was minted.
	APIKeyID  *uuid.UUID `json:"api_key_id,omitempty"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// CLIAuthStartRequest is what the CLI sends to open a handshake. Every field is
// display-only except Scopes, which bounds the key the approval mints.
type CLIAuthStartRequest struct {
	ClientName string `json:"client_name"`
	Hostname   string `json:"hostname"`
	CLIVersion string `json:"cli_version"`
	Scopes     uint64 `json:"scopes"`
}

// CLIAuthStartResponse is RFC 8628 shaped, so a generic device-flow client works.
type CLIAuthStartResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_uri"`
	// VerificationURLComplete carries the code, so the browser needs no typing.
	VerificationURLComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// CLIAuthPollResponse answers one poll. Status is the only field always set;
// the key fields arrive exactly once, on the poll that claims an approved code.
type CLIAuthPollResponse struct {
	Status CLIAuthCodeStatus `json:"status"`

	Token            string     `json:"token,omitempty"`
	APIKeyID         *uuid.UUID `json:"api_key_id,omitempty"`
	Scopes           uint64     `json:"scopes,omitempty"`
	ScopeNames       []string   `json:"scope_names,omitempty"`
	UserID           *uuid.UUID `json:"user_id,omitempty"`
	UserEmail        string     `json:"user_email,omitempty"`
	UserName         string     `json:"user_name,omitempty"`
	OrganizationID   *uuid.UUID `json:"organization_id,omitempty"`
	OrganizationName string     `json:"organization_name,omitempty"`
}

// CLIAuthApproveRequest names the workspace the key is minted in.
type CLIAuthApproveRequest struct {
	OrganizationID string `json:"organization_id"`
}

// APIScopeNames turns a permission bitmask into the scope names the CLI and the
// approval screen show, in the canonical order of AllAPIPermissions.
func APIScopeNames(mask uint64) []string {
	names := make([]string, 0, len(AllAPIPermissions))
	for _, p := range AllAPIPermissions {
		if mask&p.Value == p.Value {
			names = append(names, p.Name)
		}
	}
	return names
}
