package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// RealtimeEventType represents the type of realtime event
type RealtimeEventType string

const (
	// Campaign events
	EventCampaignStatus   RealtimeEventType = "campaign.status"
	EventCampaignProgress RealtimeEventType = "campaign.progress"
	EventCampaignLog      RealtimeEventType = "campaign.log"

	// Email account events
	EventEmailError  RealtimeEventType = "email.error"
	EventEmailStatus RealtimeEventType = "email.status"

	// Task events
	EventTaskStatus RealtimeEventType = "task.status"

	// Contact events
	EventContactChanged RealtimeEventType = "contact.changed"

	// Warmup events
	EventWarmupProgress RealtimeEventType = "warmup.progress"
	EventWarmupBlocked  RealtimeEventType = "warmup.blocked"

	// Worker events
	EventWorkerStatus RealtimeEventType = "worker.status"

	// Inbox events
	EventInboxNew    RealtimeEventType = "inbox.new"
	EventInboxUpdate RealtimeEventType = "inbox.update"

	// Analytics events
	EventAnalyticsEmailOpen  RealtimeEventType = "analytics.email_open"
	EventAnalyticsEmailClick RealtimeEventType = "analytics.email_click"
	EventAnalyticsEmailReply RealtimeEventType = "analytics.email_reply"
	EventAnalyticsBounce     RealtimeEventType = "analytics.email_bounce"
)

// RealtimeEventPriority represents the priority of realtime events
type RealtimeEventPriority string

const (
	PriorityCritical RealtimeEventPriority = "critical"
	PriorityNormal   RealtimeEventPriority = "normal"
)

// CriticalEvents are events that should be persisted for catch-up
var CriticalEvents = map[RealtimeEventType]bool{
	EventEmailError:     true,
	EventEmailStatus:    true,
	EventCampaignStatus: true,
	EventWarmupBlocked:  true,
}

// RealtimeMessage is the base message format sent over WebSocket
type RealtimeMessage struct {
	Type      RealtimeEventType `json:"type"`
	Topic     string            `json:"topic,omitempty"`
	Payload   json.RawMessage   `json:"payload"`
	Timestamp time.Time         `json:"timestamp"`
}

// ClientMessage represents messages from client to server
type ClientMessage struct {
	Action string   `json:"action"` // "subscribe" | "unsubscribe" | "ping"
	Topics []string `json:"topics,omitempty"`
}

// SubscriptionResponse is sent when subscription changes
type SubscriptionResponse struct {
	Action  string   `json:"action"` // "subscribed" | "unsubscribed"
	Topics  []string `json:"topics"`
	Success bool     `json:"success"`
	Error   string   `json:"error,omitempty"`
}

// ConnectionInfo is sent when a client connects
type ConnectionInfo struct {
	ConnectionID string   `json:"connection_id"`
	Topics       []string `json:"available_topics"`
}

// CampaignStatusPayload for campaign status events
type CampaignStatusPayload struct {
	CampaignID uuid.UUID `json:"campaign_id"`
	Status     string    `json:"status"`
	PrevStatus string    `json:"prev_status,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// CampaignProgressPayload for campaign progress events
type CampaignProgressPayload struct {
	CampaignID     uuid.UUID `json:"campaign_id"`
	Sent           int       `json:"sent"`
	Pending        int       `json:"pending"`
	Failed         int       `json:"failed"`
	Delivered      int       `json:"delivered"`
	Opened         int       `json:"opened"`
	Clicked        int       `json:"clicked"`
	Replied        int       `json:"replied"`
	Bounced        int       `json:"bounced"`
	Unsubscribed   int       `json:"unsubscribed"`
	TotalContacts  int       `json:"total_contacts"`
	CompletionPct  float64   `json:"completion_pct"`
	LastActivityAt time.Time `json:"last_activity_at"`
}

// CampaignLogPayload for campaign log events
type CampaignLogPayload struct {
	CampaignID uuid.UUID `json:"campaign_id"`
	LogType    string    `json:"log_type"` // "info" | "warning" | "error"
	Message    string    `json:"message"`
	Metadata   any       `json:"metadata,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// EmailErrorPayload for email account error events
type EmailErrorPayload struct {
	AccountID   uuid.UUID `json:"account_id"`
	ErrorType   string    `json:"error_type"` // "auth_error" | "rate_limit" | "disabled" | "bounce_rate"
	Message     string    `json:"message"`
	Recoverable bool      `json:"recoverable"`
	Timestamp   time.Time `json:"timestamp"`
}

// EmailStatusPayload for email account status events
type EmailStatusPayload struct {
	AccountID   uuid.UUID `json:"account_id"`
	Status      string    `json:"status"` // "healthy" | "warning" | "error" | "disabled"
	HealthScore int       `json:"health_score"`
	Message     string    `json:"message,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// TaskStatusPayload for task lifecycle events
type TaskStatusPayload struct {
	TaskID     uuid.UUID  `json:"task_id"`
	CampaignID *uuid.UUID `json:"campaign_id,omitempty"`
	Status     string     `json:"status"` // "pending" | "processing" | "completed" | "failed"
	Error      string     `json:"error,omitempty"`
	Timestamp  time.Time  `json:"timestamp"`
}

// ContactChangedPayload for contact change events
type ContactChangedPayload struct {
	ContactID  uuid.UUID `json:"contact_id"`
	CampaignID uuid.UUID `json:"campaign_id"`
	Action     string    `json:"action"` // "added" | "updated" | "removed"
	Timestamp  time.Time `json:"timestamp"`
}

// WarmupProgressPayload for warmup progress events
type WarmupProgressPayload struct {
	AccountID   uuid.UUID `json:"account_id"`
	DailySent   int       `json:"daily_sent"`
	DailyTarget int       `json:"daily_target"`
	TotalSent   int       `json:"total_sent"`
	WarmupDays  int       `json:"warmup_days"`
	HealthScore int       `json:"health_score"`
	ReplyRate   float64   `json:"reply_rate"`
	Timestamp   time.Time `json:"timestamp"`
}

// WarmupBlockedPayload for warmup blocked events
type WarmupBlockedPayload struct {
	AccountID uuid.UUID `json:"account_id"`
	Reason    string    `json:"reason"`
	BlockedAt time.Time `json:"blocked_at"`
}

// WorkerStatusPayload for worker status events
type WorkerStatusPayload struct {
	WorkerID     uuid.UUID `json:"worker_id"`
	Status       string    `json:"status"` // "online" | "offline" | "maintenance"
	AccountCount int       `json:"account_count"`
	Timestamp    time.Time `json:"timestamp"`
}

// InboxNewPayload for new inbox email events
type InboxNewPayload struct {
	EmailID   uuid.UUID `json:"email_id"`
	AccountID uuid.UUID `json:"account_id"`
	From      string    `json:"from"`
	Subject   string    `json:"subject"`
	Snippet   string    `json:"snippet"`
	ThreadID  string    `json:"thread_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// InboxUpdatePayload for inbox update events
type InboxUpdatePayload struct {
	EmailID   uuid.UUID `json:"email_id"`
	AccountID uuid.UUID `json:"account_id"`
	Flags     []string  `json:"flags"` // "read" | "starred" | "archived"
	Timestamp time.Time `json:"timestamp"`
}

// AnalyticsEventPayload for analytics events (open, click, reply, bounce)
type AnalyticsEventPayload struct {
	CampaignID uuid.UUID  `json:"campaign_id"`
	ContactID  uuid.UUID  `json:"contact_id"`
	EmailID    *uuid.UUID `json:"email_id,omitempty"`
	Link       string     `json:"link,omitempty"`   // For click events
	Reason     string     `json:"reason,omitempty"` // For bounce events
	Timestamp  time.Time  `json:"timestamp"`
}

// RealtimeEvent represents a persisted realtime event (for catch-up)
type RealtimeEvent struct {
	ID        uuid.UUID             `json:"id"`
	UserID    uuid.UUID             `json:"user_id"`
	OrgID     *uuid.UUID            `json:"org_id,omitempty"`
	EventType RealtimeEventType     `json:"event_type"`
	Priority  RealtimeEventPriority `json:"priority"`
	Payload   json.RawMessage       `json:"payload"`
	Delivered bool                  `json:"delivered"`
	CreatedAt time.Time             `json:"created_at"`
	ExpiresAt time.Time             `json:"expires_at"`
}

// TopicFormat defines topic naming patterns
const (
	TopicUser     = "user:%s"     // user:{userID}
	TopicOrg      = "org:%s"      // org:{orgID}
	TopicCampaign = "campaign:%s" // campaign:{campaignID}
	TopicAccount  = "account:%s"  // account:{accountID}
	TopicInbox    = "inbox:%s"    // inbox:{accountID}
)

// WebSocketToken represents a token for WebSocket authentication
type WebSocketToken struct {
	Token     string    `json:"token"`
	UserID    uuid.UUID `json:"user_id"`
	OrgID     uuid.UUID `json:"org_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RealtimeInfo contains WebSocket connection information
type RealtimeInfo struct {
	WebSocketURL string   `json:"websocket_url"`
	Topics       []string `json:"topics"`
	MaxRetries   int      `json:"max_retries"`
	RetryDelayMs int      `json:"retry_delay_ms"`
}
