package models

import (
	"time"

	"github.com/google/uuid"
)

type APIKeyStatus string

const (
	APIKeyStatusActive  APIKeyStatus = "active"
	APIKeyStatusRevoked APIKeyStatus = "revoked"
	APIKeyStatusExpired APIKeyStatus = "expired"
)

type APIKey struct {
	ID             uuid.UUID `json:"id"`
	UserID         uuid.UUID `json:"user_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Name           string    `json:"name"`
	KeyPrefix      string    `json:"key_prefix"`
	KeySuffix      string    `json:"key_suffix"`
	Permissions    uint64    `json:"permissions"`

	AllowedIPs           []string    `json:"allowed_ips,omitempty"`
	AllowedEmailAccounts []uuid.UUID `json:"allowed_email_accounts,omitempty"`

	Status        APIKeyStatus `json:"status"`
	LastUsedAt    *time.Time   `json:"last_used_at,omitempty"`
	ExpiresAt     *time.Time   `json:"expires_at,omitempty"`
	RevokedAt     *time.Time   `json:"revoked_at,omitempty"`
	RevokedReason *string      `json:"revoked_reason,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type APIKeyWithSecret struct {
	APIKey
	Secret string `json:"secret"` // Only returned on creation
}

type CreateAPIKey struct {
	Name                 string      `json:"name" binding:"required,max=255"`
	Permissions          uint64      `json:"permissions" binding:"required"`
	AllowedIPs           []string    `json:"allowed_ips,omitempty"`
	AllowedEmailAccounts []uuid.UUID `json:"allowed_email_accounts,omitempty"`
	ExpiresAt            *time.Time  `json:"expires_at,omitempty"`
}

type UpdateAPIKey struct {
	Name                 *string     `json:"name,omitempty"`
	Permissions          *uint64     `json:"permissions,omitempty"`
	AllowedIPs           []string    `json:"allowed_ips,omitempty"`
	AllowedEmailAccounts []uuid.UUID `json:"allowed_email_accounts,omitempty"`
}

type APIKeysResult struct {
	Data       []APIKey   `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type APIKeyUsageLog struct {
	ID           uuid.UUID `json:"id"`
	APIKeyID     uuid.UUID `json:"api_key_id"`
	Endpoint     string    `json:"endpoint"`
	Method       string    `json:"method"`
	IPAddress    string    `json:"ip_address"`
	UserAgent    string    `json:"user_agent"`
	ResponseCode int       `json:"response_code"`
	ResponseTime int       `json:"response_time_ms"`
	CreatedAt    time.Time `json:"created_at"`
}
