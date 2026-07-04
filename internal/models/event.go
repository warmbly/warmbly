package models

type WorkerEventType string

const (
	WorkerEventTypeSendEmail       WorkerEventType = "SEND_EMAIL"
	WorkerEventTypeAddEmail        WorkerEventType = "ADD_EMAIL"
	WorkerEventTypeRemoveEmail     WorkerEventType = "REMOVE_EMAIL"
	WorkerEventTypeEmailValidation WorkerEventType = "EMAIL_VALIDATION"
	WorkerEventTypeWarmupAction    WorkerEventType = "WARMUP_ACTION"
)

type WorkerEvent struct {
	Type WorkerEventType `json:"type" avro:"type"`
	Body any             `json:"body" avro:"body"`
}

type JobEventType string

const (
	JobEventTypeNewEmail      JobEventType = "NEW_EMAIL"
	JobEventTypeInboundBounce JobEventType = "INBOUND_BOUNCE"
	JobEventTypeRemoveEmail   JobEventType = "REMOVE_EMAIL"
	JobEventTypeFlagsAdd      JobEventType = "FLAGS_ADD"
	JobEventTypeFlagsRemove   JobEventType = "FLAGS_REMOVE"
	JobEventTypeEmailUpdate   JobEventType = "UPDATE_EMAIL"
	JobEventTypeMailboxUpdate JobEventType = "UPDATE_MAILBOX"
	JobEventTypeMailboxDelete JobEventType = "DELETE_MAILBOX"

	JobEventTypeTokenUpdate      JobEventType = "TOKEN_UPDATE"
	JobEventTypeHistoryIDUpdate  JobEventType = "HISTORY_ID_UPDATE"
	JobEventTypeGraphDeltaUpdate JobEventType = "GRAPH_DELTA_UPDATE"

	// Task result events from worker
	JobEventTypeEmailSent   JobEventType = "EMAIL_SENT"
	JobEventTypeEmailFailed JobEventType = "EMAIL_FAILED"

	// Error-specific events for worker -> jobsService
	JobEventTypeEmailAuthError   JobEventType = "EMAIL_AUTH_ERROR"   // Needs re-auth
	JobEventTypeEmailDisabled    JobEventType = "EMAIL_DISABLED"     // Account disabled
	JobEventTypeEmailRateLimited JobEventType = "EMAIL_RATE_LIMITED" // Rate limit hit
	JobEventTypeEmailServerError JobEventType = "EMAIL_SERVER_ERROR" // Temporary server error

	// Per-worker health telemetry. Emitted every 30s by every worker;
	// consumer writes it into worker_health_samples for the capacity view.
	JobEventTypeWorkerHealth JobEventType = "WORKER_HEALTH"
)

type JobEvent struct {
	Type JobEventType `json:"type" avro:"type"`
	Body any          `json:"body" avro:"body"`
}

// EmailErrorEvent represents an email error event sent from worker to jobsService
type EmailErrorEvent struct {
	TaskID         string `json:"task_id" avro:"task_id"`
	EmailAccountID string `json:"email_account_id" avro:"email_account_id"`
	UserID         string `json:"user_id" avro:"user_id"`
	ErrorCode      string `json:"error_code" avro:"error_code"`
	ErrorType      string `json:"error_type" avro:"error_type"`
	ResolveMethod  string `json:"resolve_method" avro:"resolve_method"`
	Message        string `json:"message" avro:"message"`
	UserVisible    bool   `json:"user_visible" avro:"user_visible"`
	UserTitle      string `json:"user_title,omitempty" avro:"user_title"`
	UserMessage    string `json:"user_message,omitempty" avro:"user_message"`
	ActionRequired string `json:"action_required,omitempty" avro:"action_required"`
	Timestamp      int64  `json:"timestamp" avro:"timestamp"`
}
