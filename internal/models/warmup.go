package models

import (
	"time"

	"github.com/google/uuid"
)

type WarmupToken struct {
	Token              uuid.UUID  `json:"token"`
	TaskID             uuid.UUID  `json:"task_id"`
	SenderAccountID    uuid.UUID  `json:"sender_account_id"`
	RecipientAccountID uuid.UUID  `json:"recipient_account_id"`
	CreatedAt          time.Time  `json:"created_at"`
	ConsumedAt         *time.Time `json:"consumed_at,omitempty"`
	ExpiresAt          time.Time  `json:"expires_at"`
}

// WarmupEmailAction represents actions to perform on a detected warmup email
type WarmupEmailAction struct {
	UserID  uuid.UUID `json:"user_id"`
	EmailID uuid.UUID `json:"email_id"`
	GmailID string    `json:"gmail_id"`
	UID     uint32    `json:"uid"`
	Actions []string  `json:"actions"` // "move_to_warmbly", "mark_read", "remove_from_spam", "mark_important"
}

type WarmupHealthState string

const (
	WarmupHealthHealthy     WarmupHealthState = "healthy"
	WarmupHealthWatch       WarmupHealthState = "watch"
	WarmupHealthThrottled   WarmupHealthState = "throttled"
	WarmupHealthQuarantined WarmupHealthState = "quarantined"
	WarmupHealthBlocked     WarmupHealthState = "blocked"
)

type WarmupParticipantHealth struct {
	PoolID                uuid.UUID         `json:"pool_id"`
	PoolType              string            `json:"pool_type"`
	EmailAccountID        uuid.UUID         `json:"email_account_id"`
	JoinedAt              time.Time         `json:"joined_at"`
	BlockedAt             *time.Time        `json:"blocked_at,omitempty"`
	BlockedUntil          *time.Time        `json:"blocked_until,omitempty"`
	BlockedReason         *string           `json:"blocked_reason,omitempty"`
	SpamScore             int               `json:"spam_score"`
	HealthState           WarmupHealthState `json:"health_state"`
	LastHealthScore       float64           `json:"last_health_score"`
	LastHealthReason      *string           `json:"last_health_reason,omitempty"`
	LastHealthEvaluatedAt *time.Time        `json:"last_health_evaluated_at,omitempty"`
}

type WarmupPoolHealthSummary struct {
	TotalParticipants int            `json:"total_participants"`
	ByState           map[string]int `json:"by_state"`
	AvgSpamScore      float64        `json:"avg_spam_score"`
	AvgSpamPlacement  float64        `json:"avg_spam_placement_rate"`
	BlockedCount      int            `json:"blocked_count"`
	AtRiskCount       int            `json:"at_risk_count"`
}

type WarmupHealthMetrics struct {
	SentLast7d            int     `json:"sent_last_7d"`
	SpamReportsLast7d     int     `json:"spam_reports_last_7d"`
	SpamPlacementRate     float64 `json:"spam_placement_rate"`
	InvalidAttemptsLast24 int     `json:"invalid_attempts_last_24h"`
	SpamScore             int     `json:"spam_score"`
	ComplaintsLast30d     int     `json:"complaints_last_30d"`
	DeliveredLast30d      int     `json:"delivered_last_30d"`
	ComplaintRate         float64 `json:"complaint_rate"`
	BouncesLast30d        int     `json:"bounces_last_30d"`
	BounceRate            float64 `json:"bounce_rate"`
}
