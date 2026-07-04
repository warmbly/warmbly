package models

import (
	"time"

	"github.com/google/uuid"
)

type WarmupToken struct {
	Token              uuid.UUID `json:"token"`
	TaskID             uuid.UUID `json:"task_id"`
	SenderAccountID    uuid.UUID `json:"sender_account_id"`
	RecipientAccountID uuid.UUID `json:"recipient_account_id"`
	ConversationTheme  string    `json:"conversation_theme"`
	// ContentSource records which content cohort produced this send
	// ("static" or "ai") so the A/B harness can compare spam-placement
	// rate by cohort. ConversationID points at the cached warmup_conversations
	// row when the body came from the AI bank (nil for static content).
	ContentSource  string     `json:"content_source"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	ConsumedAt     *time.Time `json:"consumed_at,omitempty"`
	ExpiresAt      time.Time  `json:"expires_at"`
}

// WarmupEmailAction represents actions to perform on a detected warmup email.
//
// For Gmail accounts the worker uses GmailID to issue Users.Messages.Modify
// requests. For IMAP-backed accounts (Outlook + custom SMTP/IMAP) the worker
// needs UID + the source mailbox's UIDValidity to locate the message; the
// mailbox name is then resolved against the worker's cached folder list.
type WarmupEmailAction struct {
	UserID             uuid.UUID `json:"user_id"`
	EmailID            uuid.UUID `json:"email_id"`
	GmailID            string    `json:"gmail_id"`
	UID                uint32    `json:"uid"`
	MailboxUIDValidity uint32    `json:"mailbox_uid_validity"`
	// RFCMessageID is the immutable RFC 5322 Message-ID. Graph provider ids
	// change when a message is moved (copy+delete), so the worker re-resolves
	// the live Graph id from this stable key at action time.
	RFCMessageID string   `json:"rfc_message_id,omitempty"`
	Actions      []string `json:"actions"` // "move_to_warmbly", "mark_read", "remove_from_spam", "mark_important"

	// DelaySeconds is retained for wire compatibility but is now always 0: the
	// recipient-side "dwell" is owned by the consumer's durable schedule
	// (warmup_pending_engagements + the engagement poller), which publishes the
	// immediate leg (folder + spam-rescue) now and the delayed leg (read /
	// important / star) when due. The worker runs whatever it receives
	// immediately. This survives a worker restart, which the old in-process
	// timer did not.
	DelaySeconds int `json:"delay_seconds,omitempty"`
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

// WarmupBanStatus is the user-facing view of a mailbox's warmup standing,
// returned by GetBanStatus so the dashboard can show why warmup is blocked and
// whether the user can appeal.
type WarmupBanStatus struct {
	EmailAccountID uuid.UUID  `json:"email_account_id"`
	Blocked        bool       `json:"blocked"`
	HealthState    string     `json:"health_state"`
	Reason         string     `json:"reason,omitempty"`
	BlockedAt      *time.Time `json:"blocked_at,omitempty"`
	BlockedUntil   *time.Time `json:"blocked_until,omitempty"`
	CanAppeal      bool       `json:"can_appeal"`
	PendingAppeal  bool       `json:"pending_appeal"`
}

type WarmupPoolHealthSummary struct {
	TotalParticipants int            `json:"total_participants"`
	ByState           map[string]int `json:"by_state"`
	AvgSpamScore      float64        `json:"avg_spam_score"`
	AvgSpamPlacement  float64        `json:"avg_spam_placement_rate"`
	// SpamPlacementByProvider breaks recent spam-placement counts down by the
	// recipient provider so the admin can see where warmup mail is being
	// filtered (e.g. mostly at Outlook vs Gmail) rather than one flat rate.
	SpamPlacementByProvider map[string]int `json:"spam_placement_by_provider"`
	BlockedCount            int            `json:"blocked_count"`
	AtRiskCount             int            `json:"at_risk_count"`
}

type WarmupHealthMetrics struct {
	SentLast7d int `json:"sent_last_7d"`

	// SpamReportsLast7d is the combined warmup-pool spam signal (placement +
	// user complaints). Retained for callers that want a single number.
	SpamReportsLast7d int `json:"spam_reports_last_7d"`

	// SpamPlacementsLast7d counts warmup messages that landed in the
	// recipient's Junk/Spam folder on delivery. SpamPlacementRate is the
	// ratio against SentLast7d.
	SpamPlacementsLast7d int     `json:"spam_placements_last_7d"`
	SpamPlacementRate    float64 `json:"spam_placement_rate"`

	// UserComplaintsLast7d counts warmup messages the recipient explicitly
	// flagged as spam. WarmupComplaintRate is the ratio against SentLast7d.
	// This is distinct from external-recipient complaints captured in
	// deliverability_events (ComplaintsLast30d / ComplaintRate below).
	UserComplaintsLast7d int     `json:"user_complaints_last_7d"`
	WarmupComplaintRate  float64 `json:"warmup_complaint_rate"`

	InvalidAttemptsLast24 int     `json:"invalid_attempts_last_24h"`
	SpamScore             int     `json:"spam_score"`
	ComplaintsLast30d     int     `json:"complaints_last_30d"`
	DeliveredLast30d      int     `json:"delivered_last_30d"`
	ComplaintRate         float64 `json:"complaint_rate"`
	BouncesLast30d        int     `json:"bounces_last_30d"`
	BounceRate            float64 `json:"bounce_rate"`
}
