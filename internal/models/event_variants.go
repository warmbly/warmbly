package models

// The two bus envelopes are tagged unions: a discriminator plus a body whose
// Go type depends on it. Nothing in the type system connects the two, so this
// is where that connection is written down, once, for every reader that needs
// it.
//
// It exists because a schema-carrying codec has to know the shape of a body
// before it sees one. JSON could stay ignorant and decode into a map; Avro
// resolves a union branch, which means every body type has to be declared up
// front. Keeping the declaration here rather than inside the codec means the
// list cannot be built from what a codec happens to have been asked to encode.
//
// A new event type is not carried until it is added here. The round-trip test
// over this map is what turns that from a rule into a failure.

// WorkerEventBodies maps every worker command to the type its body carries.
// Pointer-ness matters and is deliberate: it matches what the publisher sends
// and what the worker's handler expects, so a decoded body satisfies the
// handler's type assertion instead of taking a re-marshalling detour.
var WorkerEventBodies = map[WorkerEventType]any{
	WorkerEventTypeSendEmail:       (*SendEmail)(nil),
	WorkerEventTypeAddEmail:        (*AddWorkerEmail)(nil),
	WorkerEventTypeRemoveEmail:     (*RemoveWorkerEmail)(nil),
	WorkerEventTypeEmailValidation: EventWorkerEmailValidation{},
	WorkerEventTypeWarmupAction:    (*WarmupEmailAction)(nil),
	WorkerEventTypeMessageSeen:     (*MessageSeenAction)(nil),
	WorkerEventTypeMailboxIdentity: EventWorkerMailboxIdentity{},
}

// JobEventBodies is the same for everything a worker reports back.
//
// Several types share a body, which is correct rather than a mistake to tidy
// up: the four error events differ in what they mean and not in what they
// carry, and a send is reported the same way whether it worked. The
// discriminator separates them, so the union only needs each distinct type
// once.
var JobEventBodies = map[JobEventType]any{
	JobEventTypeNewEmail:         (*JobEventNewEmail)(nil),
	JobEventTypeInboundBounce:    (*JobEventInboundBounce)(nil),
	JobEventTypeInboundComplaint: (*JobEventInboundComplaint)(nil),
	JobEventTypeEmailUpdate:      (*JobEventEmailUpdate)(nil),
	JobEventTypeRemoveEmail:      (*JobEventRemoveEmail)(nil),
	JobEventTypeFlagsAdd:         (*JobEventFlags)(nil),
	JobEventTypeFlagsRemove:      (*JobEventFlags)(nil),
	JobEventTypeMailboxUpdate:    (*JobEventMailboxUpdate)(nil),
	JobEventTypeMailboxDelete:    (*JobEventMailboxDelete)(nil),
	JobEventTypeMailboxRename:    (*JobEventMailboxRename)(nil),
	JobEventTypeHistoryIDUpdate:  (*JobEventHistoryIDUpdate)(nil),
	JobEventTypeGraphDeltaUpdate: (*JobEventGraphDeltaUpdate)(nil),
	JobEventTypeSyncState:        (*JobEventSyncState)(nil),
	JobEventTypeTokenUpdate:      (*JobEventTokenUpdate)(nil),
	JobEventTypeEmailSent:        SendEmailResult{},
	JobEventTypeEmailFailed:      SendEmailResult{},
	JobEventTypeEmailAuthError:   EmailErrorEvent{},
	JobEventTypeEmailDisabled:    EmailErrorEvent{},
	JobEventTypeEmailRateLimited: EmailErrorEvent{},
	JobEventTypeEmailServerError: EmailErrorEvent{},
	JobEventTypeWorkerHealth:     WorkerHealthSample{},
}
