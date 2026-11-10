package models

import (
	"time"

	"github.com/google/uuid"
)

// AgencyOrganizationHeader names the client workspace an agency key acts on.
const AgencyOrganizationHeader = "Warmbly-Organization"

// AgencyAPIKey is a credential for an agency: its own endpoints and each client workspace.
type AgencyAPIKey struct {
	ID                 uuid.UUID    `json:"id"`
	AgencyID           uuid.UUID    `json:"agency_id"`
	CreatedBy          uuid.UUID    `json:"created_by"`
	Name               string       `json:"name"`
	KeyPrefix          string       `json:"key_prefix"`
	KeySuffix          string       `json:"key_suffix"`
	Permissions        uint64       `json:"permissions"`
	AllowedIPs         []string     `json:"allowed_ips"`
	RateLimitPerMinute int          `json:"rate_limit_per_minute"`
	Status             APIKeyStatus `json:"status"`
	LastUsedAt         *time.Time   `json:"last_used_at,omitempty"`
	LastRequestIP      *string      `json:"last_request_ip,omitempty"`
	ExpiresAt          *time.Time   `json:"expires_at,omitempty"`
	RevokedAt          *time.Time   `json:"revoked_at,omitempty"`
	RevokedReason      *string      `json:"revoked_reason,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

type AgencyAPIKeyWithSecret struct {
	AgencyAPIKey
	Secret string `json:"secret"` // Only returned on creation
}

type CreateAgencyAPIKey struct {
	Name               string     `json:"name"`
	Permissions        uint64     `json:"permissions"`
	AllowedIPs         []string   `json:"allowed_ips,omitempty"`
	RateLimitPerMinute *int       `json:"rate_limit_per_minute,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
}

type UpdateAgencyAPIKey struct {
	Name       *string  `json:"name,omitempty"`
	AllowedIPs []string `json:"allowed_ips,omitempty"`
}

type AgencyAPIKeysResult struct {
	Data       []AgencyAPIKey `json:"data"`
	Pagination Pagination     `json:"pagination"`
}
