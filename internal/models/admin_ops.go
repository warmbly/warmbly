package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Operator-facing read models for the admin panel's operations pages: mailbox
// sync, in-flight sends, dead letters, scheduled jobs, fleet placement,
// workspace transfers and abuse signals. Every row here is instance-wide and
// carries the organization it belongs to, because the operator reads across
// workspaces.

// ---- mailbox sync ----

// AdminSyncSearch filters the sync governor page.
type AdminSyncSearch struct {
	// State: all | throttled | backfilling | stalled | pending | complete.
	State  string `form:"state"`
	Q      string `form:"q"`
	Cursor string `form:"cursor"`
	Limit  int    `form:"limit"`
}

// AdminSyncSummary counts every mailbox that has reported sync state once.
type AdminSyncSummary struct {
	Total       int `json:"total"`
	Throttled   int `json:"throttled"`
	Backfilling int `json:"backfilling"`
	Stalled     int `json:"stalled"`
	Pending     int `json:"pending"`
	Complete    int `json:"complete"`
	Deferred    int `json:"deferred"`
}

// AdminSyncRow is one mailbox's sync state with its owner attached.
type AdminSyncRow struct {
	EmailID          uuid.UUID  `json:"email_id"`
	UserID           uuid.UUID  `json:"user_id"`
	Email            string     `json:"email"`
	Provider         string     `json:"provider"`
	AccountStatus    string     `json:"account_status"`
	OrganizationID   *uuid.UUID `json:"organization_id,omitempty"`
	OrganizationName string     `json:"organization_name"`
	WorkerID         *uuid.UUID `json:"worker_id,omitempty"`

	BackfillStatus      string     `json:"backfill_status"`
	BackfillSynced      int        `json:"backfill_synced"`
	BackfillSince       *time.Time `json:"backfill_since,omitempty"`
	BackfillStartedAt   *time.Time `json:"backfill_started_at,omitempty"`
	BackfillCompletedAt *time.Time `json:"backfill_completed_at,omitempty"`

	ThrottledUntil *time.Time `json:"throttled_until,omitempty"`
	ThrottleReason string     `json:"throttle_reason"`
	Deferred       int        `json:"deferred"`
	// Stalled is a running backfill whose state has not moved for an hour.
	Stalled      bool       `json:"stalled"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type AdminSyncResult struct {
	Data       []AdminSyncRow   `json:"data"`
	Pagination *Pagination      `json:"pagination"`
	Summary    AdminSyncSummary `json:"summary"`
}

// ---- in-flight sends ----

// AdminInFlightSend is a reserved campaign send no worker result has resolved.
type AdminInFlightSend struct {
	CampaignID       uuid.UUID  `json:"campaign_id"`
	CampaignName     string     `json:"campaign_name"`
	OrganizationID   *uuid.UUID `json:"organization_id,omitempty"`
	OrganizationName string     `json:"organization_name"`
	ContactID        uuid.UUID  `json:"contact_id"`
	ContactEmail     string     `json:"contact_email"`
	SequenceID       uuid.UUID  `json:"sequence_id"`
	TaskID           *uuid.UUID `json:"task_id,omitempty"`
	TaskStatus       string     `json:"task_status"`
	// HasMessageID means the worker did put the mail on the wire and only the
	// stamp was lost; the reclaimer will stamp it rather than retry.
	HasMessageID   bool       `json:"has_message_id"`
	EmailAccountID *uuid.UUID `json:"email_account_id,omitempty"`
	MailboxEmail   string     `json:"mailbox_email"`
	WorkerID       *uuid.UUID `json:"worker_id,omitempty"`
	DispatchedAt   time.Time  `json:"dispatched_at"`
	AgeSeconds     int64      `json:"age_seconds"`
}

type AdminInFlightSummary struct {
	Total int `json:"total"`
	// Age buckets, in minutes since dispatch.
	Under5m           int        `json:"under_5m"`
	Under30m          int        `json:"under_30m"`
	PastReclaimWindow int        `json:"past_reclaim_window"`
	OldestDispatched  *time.Time `json:"oldest_dispatched_at,omitempty"`
	// ReclaimAfterMinutes is config.CampaignSendReclaimAfterMinutes, so the
	// page can say when the sweep will pick a row up.
	ReclaimAfterMinutes int `json:"reclaim_after_minutes"`
}

type AdminInFlightResult struct {
	Summary AdminInFlightSummary `json:"summary"`
	Data    []AdminInFlightSend  `json:"data"`
}

// AdminDeadLetterRow is a task dead letter with the workspace it belongs to.
type AdminDeadLetterRow struct {
	TaskDeadLetter
	OrganizationID   *uuid.UUID `json:"organization_id,omitempty"`
	OrganizationName string     `json:"organization_name"`
}

type AdminDeadLettersResult struct {
	Data       []AdminDeadLetterRow `json:"data"`
	Pagination *Pagination          `json:"pagination"`
	// Counts by status across the instance, independent of the filter.
	Pending  int `json:"pending"`
	Replayed int `json:"replayed"`
	Failed   int `json:"failed"`
}

// AdminTaskFailureRow is one row of task_failures with the task it names.
type AdminTaskFailureRow struct {
	TaskID           uuid.UUID  `json:"task_id"`
	TaskType         string     `json:"task_type"`
	TaskStatus       string     `json:"task_status"`
	Title            string     `json:"title"`
	Message          string     `json:"message"`
	EmailAccountID   uuid.UUID  `json:"email_account_id"`
	MailboxEmail     string     `json:"mailbox_email"`
	OrganizationID   *uuid.UUID `json:"organization_id,omitempty"`
	OrganizationName string     `json:"organization_name"`
	OccurredAt       time.Time  `json:"occurred_at"`
}

// AdminWebhookEndpointRow is a customer webhook endpoint as the operator sees it.
type AdminWebhookEndpointRow struct {
	ID                  uuid.UUID  `json:"id"`
	OrganizationID      uuid.UUID  `json:"organization_id"`
	OrganizationName    string     `json:"organization_name"`
	URL                 string     `json:"url"`
	Description         string     `json:"description"`
	Enabled             bool       `json:"enabled"`
	EventTypes          []string   `json:"event_types"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt       *time.Time `json:"last_failure_at,omitempty"`
	LastFailureReason   string     `json:"last_failure_reason"`
	DeliveriesLast7d    int64      `json:"deliveries_last_7d"`
	FailedLast7d        int64      `json:"failed_last_7d"`
	DropsLast7d         int64      `json:"drops_last_7d"`
}

// AdminWebhookHealth is the instance-wide delivery picture.
type AdminWebhookHealth struct {
	// InFlightStale counts deliveries claimed longer ago than the lease.
	InFlightStale    int64 `json:"in_flight_stale"`
	PendingDue       int64 `json:"pending_due"`
	DeliveredLast24h int64 `json:"delivered_last_24h"`
	FailedLast24h    int64 `json:"failed_last_24h"`
	AbandonedLast24h int64 `json:"abandoned_last_24h"`
	DropsLast7d      int64 `json:"drops_last_7d"`
	LeaseMinutes     int   `json:"lease_minutes"`
	// FailingEndpoints lists endpoints with consecutive failures, worst first.
	FailingEndpoints []AdminWebhookEndpointRow `json:"failing_endpoints"`
}

// ---- scheduled jobs ----

// ScheduledJobRun is the persisted record of one background loop: what it is,
// when it last ran and how it went. One row per job name, shared by every
// service that runs loops (backend, consumer).
type ScheduledJobRun struct {
	Name            string     `json:"name"`
	Service         string     `json:"service"`
	IntervalSeconds int        `json:"interval_seconds"`
	LastStartedAt   *time.Time `json:"last_started_at,omitempty"`
	LastFinishedAt  *time.Time `json:"last_finished_at,omitempty"`
	LastDurationMs  int64      `json:"last_duration_ms"`
	// LastStatus: idle | running | ok | error.
	LastStatus string `json:"last_status"`
	LastError  string `json:"last_error"`
	RunCount   int64  `json:"run_count"`
	ErrorCount int64  `json:"error_count"`
	// RunRequestedAt is set by the panel's "run now"; the owning loop clears it
	// when it picks the request up.
	RunRequestedAt *time.Time `json:"run_requested_at,omitempty"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ---- fleet ----

// AdminFleetWorkerRow is a worker with its capacity-view row attached.
type AdminFleetWorkerRow struct {
	WorkerID     uuid.UUID         `json:"worker_id"`
	Name         string            `json:"name"`
	IPAddr       string            `json:"ip_addr"`
	Active       bool              `json:"active"`
	FreeTier     bool              `json:"free_tier"`
	WorkerType   WorkerType        `json:"worker_type"`
	RiskPool     WorkerRiskPool    `json:"risk_pool"`
	EgressKind   WorkerEgressKind  `json:"egress_kind"`
	HealthState  WorkerHealthState `json:"health_state"`
	InstallState string            `json:"install_state"`
	LastSeenAt   *time.Time        `json:"last_seen_at,omitempty"`
	Live         bool              `json:"live"`
	AccountCount int               `json:"account_count"`
	Tags         []string          `json:"tags"`

	LoadScore         float64 `json:"load_score"`
	BaseCapacity      float64 `json:"base_capacity"`
	HealthMultiplier  float64 `json:"health_multiplier"`
	AgeMultiplier     float64 `json:"age_multiplier"`
	EffectiveCapacity float64 `json:"effective_capacity"`
	// Utilization is load over effective capacity; the rebalancer calls a
	// worker hot above 0.8 and cold below 0.5.
	Utilization      float64 `json:"utilization"`
	SendsAttempted1h int64   `json:"sends_attempted_1h"`
	SendsSucceeded1h int64   `json:"sends_succeeded_1h"`
	BouncesHard1h    int64   `json:"bounces_hard_1h"`
	BouncesSoft1h    int64   `json:"bounces_soft_1h"`
	Complaints1h     int64   `json:"complaints_1h"`
	AuthErrors1h     int64   `json:"auth_errors_1h"`
}

// AdminFleetDecision is one decision_log row: what a control loop did and why.
type AdminFleetDecision struct {
	ID          int64           `json:"id"`
	Kind        string          `json:"kind"`
	WorkerID    *uuid.UUID      `json:"worker_id,omitempty"`
	WorkerName  string          `json:"worker_name"`
	MailboxID   *uuid.UUID      `json:"mailbox_id,omitempty"`
	Before      json.RawMessage `json:"before,omitempty"`
	After       json.RawMessage `json:"after,omitempty"`
	Reason      string          `json:"reason"`
	TriggeredBy string          `json:"triggered_by"`
	CreatedAt   time.Time       `json:"created_at"`
}

// AdminDedicatedAssignment is an active worker-to-organization binding.
type AdminDedicatedAssignment struct {
	ID               uuid.UUID  `json:"id"`
	WorkerID         uuid.UUID  `json:"worker_id"`
	WorkerName       string     `json:"worker_name"`
	WorkerLive       bool       `json:"worker_live"`
	OrganizationID   uuid.UUID  `json:"organization_id"`
	OrganizationName string     `json:"organization_name"`
	SubscriptionID   uuid.UUID  `json:"subscription_id"`
	AssignedAt       time.Time  `json:"assigned_at"`
	ReleasedAt       *time.Time `json:"released_at,omitempty"`
	AccountCount     int        `json:"account_count"`
}

// AdminConvertDedicatedRequest is the body of POST /admin/workers/:id/convert-dedicated.
type AdminConvertDedicatedRequest struct {
	OrganizationID  string  `json:"organization_id"`
	SubscriptionID  string  `json:"subscription_id"`
	DrainToWorkerID *string `json:"drain_to_worker_id"`
}

// ---- workspace transfers ----

// AdminTransferJob is an export or import job with its workspace attached.
type AdminTransferJob struct {
	// Kind: export | import.
	Kind             string            `json:"kind"`
	ID               uuid.UUID         `json:"id"`
	OrganizationID   uuid.UUID         `json:"organization_id"`
	OrganizationName string            `json:"organization_name"`
	RequestedBy      *uuid.UUID        `json:"requested_by,omitempty"`
	RequestedByEmail string            `json:"requested_by_email"`
	Status           OrgTransferStatus `json:"status"`
	Groups           []OrgDataGroup    `json:"groups"`
	IncludeSecrets   bool              `json:"include_secrets"`
	ProgressPercent  int               `json:"progress_percent"`
	ProgressStage    string            `json:"progress_stage"`
	ArchiveBytes     *int64            `json:"archive_bytes,omitempty"`
	ErrorMessage     *string           `json:"error_message,omitempty"`
	StartedAt        *time.Time        `json:"started_at,omitempty"`
	CompletedAt      *time.Time        `json:"completed_at,omitempty"`
	ExpiresAt        *time.Time        `json:"expires_at,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
}

// ---- abuse and insight ----

// AdminWarmupAbuseRow ranks a mailbox by invalid warmup-token attempts.
type AdminWarmupAbuseRow struct {
	EmailAccountID   uuid.UUID  `json:"email_account_id"`
	Email            string     `json:"email"`
	OrganizationID   *uuid.UUID `json:"organization_id,omitempty"`
	OrganizationName string     `json:"organization_name"`
	Attempts         int        `json:"attempts"`
	LastAttemptAt    time.Time  `json:"last_attempt_at"`
	Blocked          bool       `json:"blocked"`
	SpamScore        int        `json:"spam_score"`
	HealthState      string     `json:"health_state"`
}

// AdminWarmupAction is one warmup_admin_actions row with both parties named.
type AdminWarmupAction struct {
	ID             uuid.UUID `json:"id"`
	AdminUserID    uuid.UUID `json:"admin_user_id"`
	AdminEmail     string    `json:"admin_email"`
	EmailAccountID uuid.UUID `json:"email_account_id"`
	Email          string    `json:"email"`
	Action         string    `json:"action"`
	Reason         *string   `json:"reason,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// AdminOrgAPIKey is an API key as the operator sees it: never the secret.
type AdminOrgAPIKey struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	KeyPrefix      string     `json:"key_prefix"`
	KeySuffix      string     `json:"key_suffix"`
	Status         string     `json:"status"`
	Permissions    int64      `json:"permissions"`
	UserID         uuid.UUID  `json:"user_id"`
	UserEmail      string     `json:"user_email"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	RequestsLast7d int64      `json:"requests_last_7d"`
}

// AdminAcquisitionChannel is signups grouped by UTM source and medium.
type AdminAcquisitionChannel struct {
	Source    string `json:"source"`
	Medium    string `json:"medium"`
	Signups   int    `json:"signups"`
	Converted int    `json:"converted"`
}

type AdminAcquisitionReferrer struct {
	Host    string `json:"host"`
	Signups int    `json:"signups"`
}

// AdminAcquisition is signups by channel over a window, with how many of them
// went on to a paid subscription and how many trials are about to end.
type AdminAcquisition struct {
	Days             int                        `json:"days"`
	Signups          int                        `json:"signups"`
	WithChannel      int                        `json:"with_channel"`
	Converted        int                        `json:"converted"`
	TrialsExpiring7d int                        `json:"trials_expiring_7d"`
	Channels         []AdminAcquisitionChannel  `json:"channels"`
	Referrers        []AdminAcquisitionReferrer `json:"referrers"`
}
