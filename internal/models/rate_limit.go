package models

import (
	"time"

	"github.com/google/uuid"
)

type RateLimitCategory string

const (
	RateLimitRead      RateLimitCategory = "read"
	RateLimitWrite     RateLimitCategory = "write"
	RateLimitBulk      RateLimitCategory = "bulk"
	RateLimitUnibox    RateLimitCategory = "unibox"
	RateLimitAnalytics RateLimitCategory = "analytics"
)

type UserRateLimits struct {
	UserID uuid.UUID `json:"user_id"`

	LimitReadPM      int `json:"limit_read_pm"`
	LimitWritePM     int `json:"limit_write_pm"`
	LimitBulkPM      int `json:"limit_bulk_pm"`
	LimitUniboxPM    int `json:"limit_unibox_pm"`
	LimitAnalyticsPM int `json:"limit_analytics_pm"`

	LimitAPICallsDaily int `json:"limit_api_calls_daily"`
	LimitBulkOpsDaily  int `json:"limit_bulk_ops_daily"`

	// Realtime/WebSocket limits
	LimitWSMessagePM int `json:"limit_ws_message_pm"`
	LimitWSJoinPM    int `json:"limit_ws_join_pm"`
	LimitWSEventPM   int `json:"limit_ws_event_pm"`
	MaxConnections   int `json:"max_connections"`

	Notes     *string    `json:"notes,omitempty"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PlanRateLimits struct {
	PlanID uuid.UUID `json:"plan_id"`

	LimitReadPM      int `json:"limit_read_pm"`
	LimitWritePM     int `json:"limit_write_pm"`
	LimitBulkPM      int `json:"limit_bulk_pm"`
	LimitUniboxPM    int `json:"limit_unibox_pm"`
	LimitAnalyticsPM int `json:"limit_analytics_pm"`

	LimitAPICallsDaily int `json:"limit_api_calls_daily"`
	LimitBulkOpsDaily  int `json:"limit_bulk_ops_daily"`

	// Realtime/WebSocket limits
	LimitWSMessagePM int `json:"limit_ws_message_pm"`
	LimitWSJoinPM    int `json:"limit_ws_join_pm"`
	LimitWSEventPM   int `json:"limit_ws_event_pm"`
	MaxConnections   int `json:"max_connections"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateUserRateLimits struct {
	LimitReadPM      *int `json:"limit_read_pm,omitempty"`
	LimitWritePM     *int `json:"limit_write_pm,omitempty"`
	LimitBulkPM      *int `json:"limit_bulk_pm,omitempty"`
	LimitUniboxPM    *int `json:"limit_unibox_pm,omitempty"`
	LimitAnalyticsPM *int `json:"limit_analytics_pm,omitempty"`

	LimitAPICallsDaily *int `json:"limit_api_calls_daily,omitempty"`
	LimitBulkOpsDaily  *int `json:"limit_bulk_ops_daily,omitempty"`

	Notes *string `json:"notes,omitempty"`
}

type RateLimitStatus struct {
	Category     RateLimitCategory `json:"category"`
	Limit        int               `json:"limit"`
	Remaining    int               `json:"remaining"`
	ResetAt      time.Time         `json:"reset_at"`
	RetryAfterMs *int64            `json:"retry_after_ms,omitempty"`
}

// DefaultRateLimits returns the default rate limits. Sized to allow paid
// customers to sustain ~100 req/s on read and write categories, matching
// the API throughput competitors (Instantly) advertise as of mid-2026.
// 100 req/s = 6000 req/min. Each Limit*PM value *is* the ceiling — there
// is no burst multiplier. Enterprise customers get bumped by raising the
// limit fields directly on user_rate_limits.
func DefaultRateLimits() *UserRateLimits {
	return &UserRateLimits{
		LimitReadPM:        6000,
		LimitWritePM:       6000,
		LimitBulkPM:        600,
		LimitUniboxPM:      1200,
		LimitAnalyticsPM:   600,
		LimitAPICallsDaily: 500000,
		LimitBulkOpsDaily:  1000,
		LimitWSMessagePM:   120,
		LimitWSJoinPM:      30,
		LimitWSEventPM:     60,
		MaxConnections:     10,
	}
}

// ToRealtimeLimits extracts WebSocket-specific limits
func (r *UserRateLimits) ToRealtimeLimits() *RealtimeRateLimits {
	return &RealtimeRateLimits{
		LimitWSMessagePM: r.LimitWSMessagePM,
		LimitWSJoinPM:    r.LimitWSJoinPM,
		LimitWSEventPM:   r.LimitWSEventPM,
		MaxConnections:   r.MaxConnections,
	}
}

// ToRealtimeLimits extracts WebSocket-specific limits from plan
func (r *PlanRateLimits) ToRealtimeLimits() *RealtimeRateLimits {
	return &RealtimeRateLimits{
		LimitWSMessagePM: r.LimitWSMessagePM,
		LimitWSJoinPM:    r.LimitWSJoinPM,
		LimitWSEventPM:   r.LimitWSEventPM,
		MaxConnections:   r.MaxConnections,
	}
}

// GetLimitForCategory returns the per-minute limit for a given category
func (r *UserRateLimits) GetLimitForCategory(category RateLimitCategory) int {
	switch category {
	case RateLimitRead:
		return r.LimitReadPM
	case RateLimitWrite:
		return r.LimitWritePM
	case RateLimitBulk:
		return r.LimitBulkPM
	case RateLimitUnibox:
		return r.LimitUniboxPM
	case RateLimitAnalytics:
		return r.LimitAnalyticsPM
	default:
		return r.LimitReadPM
	}
}
