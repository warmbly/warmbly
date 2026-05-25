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
	Description    *string   `json:"description,omitempty"`
	KeyPrefix      string    `json:"key_prefix"`
	KeySuffix      string    `json:"key_suffix"`
	Permissions    uint64    `json:"permissions"`

	AllowedIPs           []string    `json:"allowed_ips,omitempty"`
	AllowedEmailAccounts []uuid.UUID `json:"allowed_email_accounts,omitempty"`

	// RateLimitPerMinute is enforced via Redis sliding window in the auth
	// middleware. 60 r/m is the default; 0 is treated as "use the default"
	// to leave room for an explicit unlimited bit later.
	RateLimitPerMinute int `json:"rate_limit_per_minute"`

	Status        APIKeyStatus `json:"status"`
	LastUsedAt    *time.Time   `json:"last_used_at,omitempty"`
	LastRequestIP *string      `json:"last_request_ip,omitempty"`
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
	Description          *string     `json:"description,omitempty"`
	Permissions          uint64      `json:"permissions" binding:"required"`
	AllowedIPs           []string    `json:"allowed_ips,omitempty"`
	AllowedEmailAccounts []uuid.UUID `json:"allowed_email_accounts,omitempty"`
	RateLimitPerMinute   *int        `json:"rate_limit_per_minute,omitempty"`
	ExpiresAt            *time.Time  `json:"expires_at,omitempty"`
}

type UpdateAPIKey struct {
	Name                 *string     `json:"name,omitempty"`
	Description          *string     `json:"description,omitempty"`
	Permissions          *uint64     `json:"permissions,omitempty"`
	AllowedIPs           []string    `json:"allowed_ips,omitempty"`
	AllowedEmailAccounts []uuid.UUID `json:"allowed_email_accounts,omitempty"`
	RateLimitPerMinute   *int        `json:"rate_limit_per_minute,omitempty"`
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

// APIKeyUsageSummary is the org-level overview surfaced at the top of the
// API keys dashboard. All counts cover the last 24 hours.
type APIKeyUsageSummary struct {
	ActiveKeys      int        `json:"active_keys"`
	RevokedKeys     int        `json:"revoked_keys"`
	ExpiredKeys     int        `json:"expired_keys"`
	Requests24h     int64      `json:"requests_24h"`
	Errors24h       int64      `json:"errors_24h"`
	AvgLatencyMs24h float64    `json:"avg_latency_ms_24h"`
	LastCallAt      *time.Time `json:"last_call_at,omitempty"`
}

// APIKeyUsageBucket is one point on a time-bucketed request graph.
type APIKeyUsageBucket struct {
	Bucket       time.Time `json:"bucket"`
	Total        int64     `json:"total"`
	Success      int64     `json:"success"`       // 2xx
	ClientErrors int64     `json:"client_errors"` // 4xx
	ServerErrors int64     `json:"server_errors"` // 5xx
	AvgLatencyMs float64   `json:"avg_latency_ms"`
}

// APIKeyEndpointStat is one row in the per-key top-endpoints breakdown.
type APIKeyEndpointStat struct {
	Endpoint     string  `json:"endpoint"`
	Method       string  `json:"method"`
	Count        int64   `json:"count"`
	ErrorCount   int64   `json:"error_count"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

// APIKeyAnalytics is the per-key dashboard payload: a series of buckets
// (graph), a per-endpoint breakdown (table), and a quick total. The
// caller picks the bucket interval via ?interval=hour|day.
type APIKeyAnalytics struct {
	APIKeyID  uuid.UUID            `json:"api_key_id"`
	From      time.Time            `json:"from"`
	To        time.Time            `json:"to"`
	Interval  string               `json:"interval"`
	Buckets   []APIKeyUsageBucket  `json:"buckets"`
	Endpoints []APIKeyEndpointStat `json:"endpoints"`
	Total     int64                `json:"total"`
	Errors    int64                `json:"errors"`
}

type APIKeyUsageLogsResult struct {
	Data       []APIKeyUsageLog `json:"data"`
	Pagination Pagination       `json:"pagination"`
}
