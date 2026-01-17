package models

type WorkerEventType string

const (
	WorkerEventTypeSendEmail       WorkerEventType = "SEND_EMAIL"
	WorkerEventTypeAddEmail        WorkerEventType = "ADD_EMAIL"
	WorkerEventTypeRemoveEmail     WorkerEventType = "REMOVE_EMAIL"
	WorkerEventTypeEmailValidation WorkerEventType = "EMAIL_VALIDATION"
)

type WorkerEvent struct {
	Type WorkerEventType `json:"type"`
	Body any             `json:"body"`
}

type JobEventType string

const (
	JobEventTypeNewEmail      JobEventType = "NEW_EMAIL"
	JobEventTypeRemoveEmail   JobEventType = "REMOVE_EMAIL"
	JobEventTypeFlagsAdd      JobEventType = "FLAGS_ADD"
	JobEventTypeFlagsRemove   JobEventType = "FLAGS_REMOVE"
	JobEventTypeEmailUpdate   JobEventType = "UPDATE_EMAIL"
	JobEventTypeMailboxUpdate JobEventType = "UPDATE_MAILBOX"
	JobEventTypeMailboxDelete JobEventType = "DELETE_MAILBOX"

	JobEventTypeTokenUpdate     JobEventType = "TOKEN_UPDATE"
	JobEventTypeHistoryIDUpdate JobEventType = "HISTORY_ID_UPDATE"
)

type JobEvent struct {
	Type JobEventType `json:"type"`
	Body any          `json:"body"`
}
