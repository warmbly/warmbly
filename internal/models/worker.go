package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// WorkerType represents the type of worker
type WorkerType string

const (
	WorkerTypeShared    WorkerType = "shared"
	WorkerTypeDedicated WorkerType = "dedicated"
)

type Worker struct {
	ID           uuid.UUID  `json:"id"`
	IPAddr       string     `json:"ip_addr"`
	Active       bool       `json:"active"`
	FreeTier     bool       `json:"free_tier"`
	WorkerType   WorkerType `json:"worker_type"`
	AccountCount int        `json:"account_count"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateWorker struct {
	IPAddr     *string     `json:"ip_addr"`
	Active     *bool       `json:"active"`
	WorkerType *WorkerType `json:"worker_type,omitempty"`
}

// DedicatedWorkerAssignment represents a dedicated worker assignment to a user
type DedicatedWorkerAssignment struct {
	ID             uuid.UUID  `json:"id"`
	WorkerID       uuid.UUID  `json:"worker_id"`
	UserID         uuid.UUID  `json:"user_id"`
	SubscriptionID uuid.UUID  `json:"subscription_id"`
	AssignedAt     time.Time  `json:"assigned_at"`
	ReleasedAt     *time.Time `json:"released_at,omitempty"`
}

type WorkerStatus string

const (
	WorkerStatusOffline WorkerStatus = "offline"
	WorkerStatusLoading WorkerStatus = "loading"
	WorkerStatusOnline  WorkerStatus = "online"
)

type SendEmail struct {
	TaskID        uuid.UUID    `json:"task_id"`
	EmailID       uuid.UUID    `json:"email_id"`
	To            []string     `json:"to"`
	Cc            []string     `json:"cc"`
	Bcc           []string     `json:"bcc"`
	Subject       string       `json:"subject"`
	BodyS3Key     string       `json:"body_s3_key"`
	MessageID     string       `json:"message_id"`
	InReplyTo     string       `json:"in_reply_to,omitempty"`
	Parent        *EmailParent `json:"parent,omitempty"`
	IsWarmup      bool         `json:"is_warmup"`
	TrackingInfo  *TrackingInfo `json:"tracking_info,omitempty"`
}

// TrackingInfo contains tracking configuration for campaign emails
type TrackingInfo struct {
	OpenTracking   bool   `json:"open_tracking"`
	LinkTracking   bool   `json:"link_tracking"`
	TrackingDomain string `json:"tracking_domain"`
}

// EmailSendError contains detailed error information for failed email sends
type EmailSendError struct {
	Code           string `json:"code"`
	Type           string `json:"type"`
	Message        string `json:"message"`
	ResolveMethod  string `json:"resolve_method"`
	UserVisible    bool   `json:"user_visible"`
	UserTitle      string `json:"user_title,omitempty"`
	UserMessage    string `json:"user_message,omitempty"`
	ActionRequired string `json:"action_required,omitempty"`
}

// SendEmailResult is the result from worker after sending email
type SendEmailResult struct {
	TaskID         uuid.UUID       `json:"task_id"`
	Success        bool            `json:"success"`
	MessageID      string          `json:"message_id,omitempty"`
	ProviderMsgID  string          `json:"provider_msg_id,omitempty"`
	SentAt         time.Time       `json:"sent_at,omitempty"`
	Error          *EmailSendError `json:"error,omitempty"`
	LegacyErrorMsg string          `json:"legacy_error,omitempty"` // Deprecated: use Error instead
}

type AddWorkerEmailGoogleData struct {
	LastHistoryID uint64        `json:"last_history_id"`
	Token         *oauth2.Token `json:"token"`
}

type AddWorkerEmailSmtpImapData struct {
	Mailboxes   []Mailbox     `json:"mailboxes"`
	Token       *oauth2.Token `json:"token"`
	Credentials *SmtpImap     `json:"credentials"`
}

type AddWorkerEmail struct {
	ID        uuid.UUID                   `json:"id"`
	ImapSync  bool                        `json:"imap_sync"`
	Email     string                      `json:"email"`
	FirstName string                      `json:"first_name"`
	LastName  string                      `json:"last_name"`
	Type      InboxProvider               `json:"type"`
	Google    *AddWorkerEmailGoogleData   `json:"google"`
	SmtpImap  *AddWorkerEmailSmtpImapData `json:"smtp_imap"`

	Cfg oauth2.Config `json:"-"`
}

type RemoveWorkerEmail struct {
	UserID  string `json:"user_id"`
	EmailID string `json:"email_id"`
}
