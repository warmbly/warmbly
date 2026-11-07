package models

import "github.com/google/uuid"

type WorkerEventType string

const (
	WorkerEventTypeSendEmail       WorkerEventType = "SEND_EMAIL"
	WorkerEventTypeAddEmail        WorkerEventType = "ADD_EMAIL"
	WorkerEventTypeRemoveEmail     WorkerEventType = "REMOVE_EMAIL"
	WorkerEventTypeEmailValidation WorkerEventType = "EMAIL_VALIDATION"
	WorkerEventTypeWarmupAction    WorkerEventType = "WARMUP_ACTION"
	// WorkerEventTypeMessageSeen relays a read/unread change a person made in
	// the unibox out to the mailbox provider, so the two agree.
	WorkerEventTypeMessageSeen WorkerEventType = "MESSAGE_SEEN"
	// WorkerEventTypeMessageFolder relays an Archive, Delete or Move to inbox
	// made in the unibox out to the mailbox provider.
	WorkerEventTypeMessageFolder WorkerEventType = "MESSAGE_FOLDER"
	// WorkerEventTypeMailboxIdentity asks the worker holding a mailbox to read
	// its send-as identities (and one signature) from the provider. The
	// control plane never calls a customer's provider itself.
	WorkerEventTypeMailboxIdentity WorkerEventType = "MAILBOX_IDENTITY"
	// WorkerEventTypeSyncWake asks the worker holding a mailbox to run its
	// sync pass now, because the provider said something changed.
	WorkerEventTypeSyncWake WorkerEventType = "SYNC_WAKE"
)

type WorkerEvent struct {
	Type WorkerEventType `json:"type" avro:"type"`
	Body any             `json:"body" avro:"body"`
}

type JobEventType string

const (
	JobEventTypeNewEmail         JobEventType = "NEW_EMAIL"
	JobEventTypeInboundBounce    JobEventType = "INBOUND_BOUNCE"
	JobEventTypeInboundComplaint JobEventType = "INBOUND_COMPLAINT"
	JobEventTypeRemoveEmail      JobEventType = "REMOVE_EMAIL"
	JobEventTypeFlagsAdd         JobEventType = "FLAGS_ADD"
	JobEventTypeFlagsRemove      JobEventType = "FLAGS_REMOVE"
	JobEventTypeEmailUpdate      JobEventType = "UPDATE_EMAIL"
	// JobEventTypeFolderUpdate is a provider moving a message between folders
	// without a full rescan of it (a Gmail label change).
	JobEventTypeFolderUpdate  JobEventType = "UPDATE_FOLDER"
	JobEventTypeMailboxUpdate JobEventType = "UPDATE_MAILBOX"
	JobEventTypeMailboxDelete JobEventType = "DELETE_MAILBOX"
	// JobEventTypeMailboxRename is a folder that kept its UIDVALIDITY under a
	// new name. Distinct from a delete plus an insert because the folder's
	// stored mail has to move with it rather than be orphaned.
	JobEventTypeMailboxRename JobEventType = "RENAME_MAILBOX"

	JobEventTypeTokenUpdate      JobEventType = "TOKEN_UPDATE"
	JobEventTypeHistoryIDUpdate  JobEventType = "HISTORY_ID_UPDATE"
	JobEventTypeGraphDeltaUpdate JobEventType = "GRAPH_DELTA_UPDATE"
	// JobEventTypeSyncState relays a mailbox's backfill progress, fair-use
	// throttle and last-synced time (JobEventSyncState).
	JobEventTypeSyncState JobEventType = "SYNC_STATE"
	// JobEventTypePushState records the provider push channel a worker
	// opened, renewed or closed for a mailbox (JobEventPushState).
	JobEventTypePushState JobEventType = "PUSH_STATE"

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

	// JobEventTypeWarmupRemovalChecked answers a verify_removal warmup action.
	JobEventTypeWarmupRemovalChecked JobEventType = "WARMUP_REMOVAL_CHECKED"
	JobEventTypeWarmupFiled          JobEventType = "WARMUP_FILED"
	// JobEventTypeWarmupRescued answers a spam rescue the control plane asked to hear back about.
	JobEventTypeWarmupRescued JobEventType = "WARMUP_RESCUED"
)

type JobEvent struct {
	Type JobEventType `json:"type" avro:"type"`
	Body any          `json:"body" avro:"body"`
}

// EmailErrorEvent represents an email error event sent from worker to jobsService
type EmailErrorEvent struct {
	TransportGeneration uuid.UUID              `json:"transport_generation,omitempty" avro:"transport_generation"`
	Operation           MailTransportOperation `json:"operation,omitempty" avro:"operation"`
	TaskID              string                 `json:"task_id" avro:"task_id"`
	EmailAccountID      string                 `json:"email_account_id" avro:"email_account_id"`
	UserID              string                 `json:"user_id" avro:"user_id"`
	ErrorCode           string                 `json:"error_code" avro:"error_code"`
	ErrorType           string                 `json:"error_type" avro:"error_type"`
	ResolveMethod       string                 `json:"resolve_method" avro:"resolve_method"`
	Message             string                 `json:"message" avro:"message"`
	UserVisible         bool                   `json:"user_visible" avro:"user_visible"`
	UserTitle           string                 `json:"user_title,omitempty" avro:"user_title"`
	UserMessage         string                 `json:"user_message,omitempty" avro:"user_message"`
	ActionRequired      string                 `json:"action_required,omitempty" avro:"action_required"`
	Timestamp           int64                  `json:"timestamp" avro:"timestamp"`
}
