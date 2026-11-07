package models

import (
	"time"

	"github.com/google/uuid"
)

// MailboxPushProvider names the provider channel that announces a mailbox's changes.
type MailboxPushProvider string

const (
	// MailboxPushGmail is a Gmail users.watch publishing to Cloud Pub/Sub.
	MailboxPushGmail MailboxPushProvider = "gmail"
	// MailboxPushGraph is a Microsoft Graph change-notification subscription.
	MailboxPushGraph MailboxPushProvider = "graph"
)

// AddWorkerEmailPushData is the push channel the control plane wants open for a mailbox.
type AddWorkerEmailPushData struct {
	// GraphNotificationURL is where Graph posts this mailbox's notifications; empty means no Graph push.
	GraphNotificationURL string `json:"graph_notification_url,omitempty" avro:"graph_notification_url"`
	// GraphClientState is echoed by Graph on every notification so the backend can authenticate it.
	GraphClientState string `json:"graph_client_state,omitempty" avro:"graph_client_state"`
	// GmailTopic is the Pub/Sub topic users.watch publishes to; empty means no Gmail push.
	GmailTopic string `json:"gmail_topic,omitempty" avro:"gmail_topic"`
	// State is the channel last recorded, so a reloaded mailbox renews it instead of opening another.
	State *MailboxPushState `json:"state,omitempty" avro:"state"`
}

// MailboxPushState is one open push channel.
type MailboxPushState struct {
	Provider MailboxPushProvider `json:"provider" avro:"provider"`
	// SubscriptionID is the Graph subscription id; Gmail has none.
	SubscriptionID string    `json:"subscription_id,omitempty" avro:"subscription_id"`
	ExpiresAt      time.Time `json:"expires_at" avro:"expires_at"`
	// Credential fingerprints the client state the channel was opened with, so a changed one reopens it.
	Credential string `json:"credential,omitempty" avro:"credential"`
}

// JobEventPushState is a worker reporting the push channel it holds for a mailbox; a nil State means none.
type JobEventPushState struct {
	UserID  uuid.UUID         `json:"user_id" avro:"user_id"`
	EmailID uuid.UUID         `json:"email_id" avro:"email_id"`
	State   *MailboxPushState `json:"state,omitempty" avro:"state"`
}

// SyncWake asks the worker holding a mailbox to sync it now.
type SyncWake struct {
	UserID  uuid.UUID `json:"user_id" avro:"user_id"`
	EmailID uuid.UUID `json:"email_id" avro:"email_id"`
	// Renew also asks the worker to renew or reopen the mailbox's push channel.
	Renew bool `json:"renew,omitempty" avro:"renew"`
}
