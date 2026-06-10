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

// WorkerRiskPool buckets shared workers by acceptable mailbox risk level.
// Dedicated workers don't use it (one customer per worker — no
// cross-tenant contamination risk).
type WorkerRiskPool string

const (
	WorkerRiskPoolClean      WorkerRiskPool = "clean"
	WorkerRiskPoolRisky      WorkerRiskPool = "risky"
	WorkerRiskPoolQuarantine WorkerRiskPool = "quarantine"
)

// WorkerEgressKind describes how a worker is wired up to actually send mail.
// Different egress profiles ship with very different safe capacities, which
// is why the capacity view branches on this column to derive base_capacity.
type WorkerEgressKind string

const (
	WorkerEgressColdSMTP   WorkerEgressKind = "cold_smtp"
	WorkerEgressOAuthAPI   WorkerEgressKind = "oauth_api"
	WorkerEgressWarmupOnly WorkerEgressKind = "warmup_only"
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

type Worker struct {
	ID           uuid.UUID         `json:"id"`
	Name         string            `json:"name"`
	Notes        string            `json:"notes"`
	IPAddr       string            `json:"ip_addr"`
	Active       bool              `json:"active"`
	FreeTier     bool              `json:"free_tier"`
	WorkerType   WorkerType        `json:"worker_type"`
	AccountCount int               `json:"account_count"`
	RiskPool     WorkerRiskPool    `json:"risk_pool"`
	EgressKind   WorkerEgressKind  `json:"egress_kind"`
	HealthState  WorkerHealthState `json:"health_state"`
	LoadScore    float64           `json:"load_score"`

	// SSH management (none of these expose secret material — the encrypted
	// private key is fetched separately via GetWorkerSSHCredentials).
	SSHHost            string             `json:"ssh_host,omitempty"`
	SSHPort            int                `json:"ssh_port,omitempty"`
	SSHUser            string             `json:"ssh_user,omitempty"`
	SSHPublicKey       string             `json:"ssh_public_key,omitempty"`
	SSHHostFingerprint string             `json:"ssh_host_fingerprint,omitempty"`
	InstallState       WorkerInstallState `json:"install_state"`
	LastSeenAt         *time.Time         `json:"last_seen_at,omitempty"`
	LastError          string             `json:"last_error,omitempty"`

	// Profile assignment. Nil means "use backend env defaults".
	ProfileID       *uuid.UUID `json:"profile_id,omitempty"`
	ConfigAppliedAt *time.Time `json:"config_applied_at,omitempty"`

	// Image tag the worker is currently running, captured on every successful
	// Update. Used for the "v1.2.3 → v1.2.4" badge in the dashboard.
	ImageVersion string `json:"image_version,omitempty"`

	// Admin-applied free-form tags (eu-west, hetzner, warmup-only, ...).
	// Auto-derived "smart" labels (tier:free, pool:risky, state:error) are
	// computed client-side from the worker row and never stored here.
	Tags []string `json:"tags,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WorkerInstallState mirrors the worker_install_state enum.
type WorkerInstallState string

const (
	WorkerInstallStatePending      WorkerInstallState = "pending"
	WorkerInstallStateProvisioning WorkerInstallState = "provisioning"
	WorkerInstallStateInstalled    WorkerInstallState = "installed"
	WorkerInstallStateError        WorkerInstallState = "error"
	WorkerInstallStateUninstalling WorkerInstallState = "uninstalling"
	WorkerInstallStateUninstalled  WorkerInstallState = "uninstalled"
)

// WorkerSSHCredentials carries the encrypted private key alongside the
// connection info. Only the orchestrator should ever fetch this; the field is
// never serialised to admin clients.
type WorkerSSHCredentials struct {
	WorkerID               uuid.UUID
	SSHHost                string
	SSHPort                int
	SSHUser                string
	SSHPublicKey           string
	SSHPrivateKeyEncrypted string
	SSHHostFingerprint     string
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
	TaskID         uuid.UUID       `json:"task_id" avro:"task_id"`
	Success        bool            `json:"success" avro:"success"`
	MessageID      string          `json:"message_id,omitempty" avro:"message_id"`
	ProviderMsgID  string          `json:"provider_msg_id,omitempty" avro:"provider_msg_id"`
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

type AddWorkerEmail struct {
	ID        uuid.UUID                   `json:"id" avro:"id"`
	UserID    uuid.UUID                   `json:"user_id" avro:"user_id"`
	ImapSync  bool                        `json:"imap_sync" avro:"imap_sync"`
	Email     string                      `json:"email" avro:"email"`
	FirstName string                      `json:"first_name" avro:"first_name"`
	LastName  string                      `json:"last_name" avro:"last_name"`
	Type      InboxProvider               `json:"type" avro:"type"`
	Google    *AddWorkerEmailGoogleData   `json:"google" avro:"google"`
	SmtpImap  *AddWorkerEmailSmtpImapData `json:"smtp_imap" avro:"smtp_imap"`

	Cfg oauth2.Config `json:"-" avro:"-"`
}

type RemoveWorkerEmail struct {
	UserID  string `json:"user_id" avro:"user_id"`
	EmailID string `json:"email_id" avro:"email_id"`
}
