package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// WorkerHealthState is the rolled-up health label maintained by the
// assignment loop. Authoritative for "can this worker accept new
// mailboxes" placement decisions. Mirrors the warmup health vocabulary
// but applies to whole workers, not per-mailbox warmup state.
type WorkerHealthState string

const (
	WorkerHealthHealthy     WorkerHealthState = "healthy"
	WorkerHealthWatch       WorkerHealthState = "watch"
	WorkerHealthThrottled   WorkerHealthState = "throttled"
	WorkerHealthQuarantined WorkerHealthState = "quarantined"
	WorkerHealthBlocked     WorkerHealthState = "blocked"
)

// Worker is a node that carries mailboxes. It is a flat view over
// workers JOIN fleet_nodes: the placement columns (AccountCount, HealthState,
// LoadScore) live on `workers`, and the machine columns below are hydrated
// from the node, because they describe the box rather than the mail on it.
type Worker struct {
	ID           uuid.UUID         `json:"id"`
	AccountCount int               `json:"account_count"`
	HealthState  WorkerHealthState `json:"health_state"`
	LoadScore    float64           `json:"load_score"`

	// From the node. Read-only here; a worker never writes them.
	Name       string     `json:"name"`
	Notes      string     `json:"notes"`
	IPAddr     string     `json:"ip_addr"`
	Active     bool       `json:"active"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
	Version    string     `json:"version,omitempty"`

	// Region is a sign-in geography hint for placement, not a partition. A
	// mailbox scores better on a worker whose egress geolocates near where
	// its provider expects logins from; empty scores neutral.
	Region string `json:"region"`

	// Admin-applied free-form tags (eu-west, spare, ...). Auto-derived
	// "smart" labels (health:watch) are computed client-side and never stored.
	Tags []string `json:"tags,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateWorker struct {
	Active *bool   `json:"active"`
	Region *string `json:"region,omitempty"`
}

// DedicatedWorkerAssignment binds an organization entitled to isolated egress
// to a worker. It is a preference the placer converges on, not a hard pin: the
// worker itself carries no category, and if it dies the org's mailboxes place
// normally rather than stranding.
type DedicatedWorkerAssignment struct {
	ID             uuid.UUID  `json:"id"`
	WorkerID       uuid.UUID  `json:"worker_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
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
	TaskID         uuid.UUID     `json:"task_id" avro:"task_id"`
	EmailID        uuid.UUID     `json:"email_id" avro:"email_id"`
	OrgID          uuid.UUID     `json:"org_id" avro:"org_id"`
	To             []string      `json:"to" avro:"to"`
	Cc             []string      `json:"cc" avro:"cc"`
	Bcc            []string      `json:"bcc" avro:"bcc"`
	Subject        string        `json:"subject" avro:"subject"`
	BodyS3Key      string        `json:"body_s3_key" avro:"body_s3_key"`
	MessageID      string        `json:"message_id" avro:"message_id"`
	InReplyTo      string        `json:"in_reply_to,omitempty" avro:"in_reply_to"`
	Parent         *EmailParent  `json:"parent,omitempty" avro:"parent"`
	IsWarmup       bool          `json:"is_warmup" avro:"is_warmup"`
	TrackingInfo   *TrackingInfo `json:"tracking_info,omitempty" avro:"tracking_info"`
	WarmupToken    string        `json:"warmup_token,omitempty" avro:"warmup_token"`
	UnsubscribeURL string        `json:"unsubscribe_url,omitempty" avro:"unsubscribe_url"`
}

// TrackingInfo contains tracking configuration for campaign emails
type TrackingInfo struct {
	OpenTracking   bool   `json:"open_tracking" avro:"open_tracking"`
	LinkTracking   bool   `json:"link_tracking" avro:"link_tracking"`
	TrackingDomain string `json:"tracking_domain" avro:"tracking_domain"`
}

// EmailSendError contains detailed error information for failed email sends
type EmailSendError struct {
	Code           string `json:"code" avro:"code"`
	Type           string `json:"type" avro:"type"`
	Message        string `json:"message" avro:"message"`
	ResolveMethod  string `json:"resolve_method" avro:"resolve_method"`
	UserVisible    bool   `json:"user_visible" avro:"user_visible"`
	UserTitle      string `json:"user_title,omitempty" avro:"user_title"`
	UserMessage    string `json:"user_message,omitempty" avro:"user_message"`
	ActionRequired string `json:"action_required,omitempty" avro:"action_required"`
}

// SendEmailResult is the result from worker after sending email
type SendEmailResult struct {
	TaskID        uuid.UUID `json:"task_id" avro:"task_id"`
	Success       bool      `json:"success" avro:"success"`
	MessageID     string    `json:"message_id,omitempty" avro:"message_id"`
	ProviderMsgID string    `json:"provider_msg_id,omitempty" avro:"provider_msg_id"`
	// ThreadID is the provider-side conversation the message landed in, for
	// the providers that have one (Gmail). The control plane records it
	// against the task so the next step of the sequence can be appended to
	// the same thread instead of opening a new one (issue #472).
	ThreadID       string          `json:"thread_id,omitempty" avro:"thread_id"`
	SentAt         time.Time       `json:"sent_at,omitempty" avro:"sent_at"`
	Error          *EmailSendError `json:"error,omitempty" avro:"error"`
	LegacyErrorMsg string          `json:"legacy_error,omitempty" avro:"legacy_error"` // Deprecated: use Error instead
}

type AddWorkerEmailGoogleData struct {
	LastHistoryID uint64        `json:"last_history_id" avro:"last_history_id"`
	Token         *oauth2.Token `json:"token" avro:"token"`
}

type AddWorkerEmailSmtpImapData struct {
	Mailboxes   []Mailbox     `json:"mailboxes" avro:"mailboxes"`
	Token       *oauth2.Token `json:"token" avro:"token"`
	Credentials *SmtpImap     `json:"credentials" avro:"credentials"`
}

// AddWorkerEmailGraphData seeds a Microsoft Graph mailbox on the worker: the
// delegated OAuth token and the opaque per-folder delta cursors persisted by the
// control plane (empty on first connect, which primes the cursor without
// backfilling history).
type AddWorkerEmailGraphData struct {
	Token      *oauth2.Token     `json:"token" avro:"token"`
	DeltaLinks map[string]string `json:"delta_links" avro:"delta_links"`
}

type AddWorkerEmail struct {
	ID     uuid.UUID `json:"id" avro:"id"`
	UserID uuid.UUID `json:"user_id" avro:"user_id"`
	// OrganizationID scopes the organization-wide sync budget. Nil for a
	// legacy personal mailbox, which then only has per-mailbox budgets.
	OrganizationID *uuid.UUID `json:"organization_id" avro:"organization_id"`
	ImapSync       bool       `json:"imap_sync" avro:"imap_sync"`
	// SaveToSent is a pointer so an older control plane that does not send the
	// field is read as "unset" and takes the default (on) rather than as an
	// explicit false, which would silently stop filing sent mail.
	SaveToSent *bool                       `json:"save_to_sent,omitempty" avro:"save_to_sent"`
	Email      string                      `json:"email" avro:"email"`
	FirstName  string                      `json:"first_name" avro:"first_name"`
	LastName   string                      `json:"last_name" avro:"last_name"`
	Type       InboxProvider               `json:"type" avro:"type"`
	Google     *AddWorkerEmailGoogleData   `json:"google" avro:"google"`
	SmtpImap   *AddWorkerEmailSmtpImapData `json:"smtp_imap" avro:"smtp_imap"`
	Graph      *AddWorkerEmailGraphData    `json:"graph" avro:"graph"`
	// Sync is the fair-use budget and resume state. Nil only from a publisher
	// older than the sync policy; the worker then applies compiled defaults.
	Sync *AddWorkerEmailSyncData `json:"sync" avro:"sync"`
	// Brokered: no credential travels; the worker fetches access tokens from the backend.
	Brokered bool `json:"brokered" avro:"brokered"`

	Cfg oauth2.Config `json:"-" avro:"-"`
	// TokenSource is set by the worker for brokered mailboxes.
	TokenSource oauth2.TokenSource `json:"-" avro:"-"`
}

// SavesSentCopy reports whether the worker should APPEND a copy of each sent
// message to the mailbox's Sent folder. Unset means yes: SMTP files nothing on
// its own, so the copy is the useful default.
func (a *AddWorkerEmail) SavesSentCopy() bool {
	return a.SaveToSent == nil || *a.SaveToSent
}

type RemoveWorkerEmail struct {
	UserID  string `json:"user_id" avro:"user_id"`
	EmailID string `json:"email_id" avro:"email_id"`
}
