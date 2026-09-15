package events

import (
	"time"

	"github.com/google/uuid"
)

// Event types
const (
	EventTypeEmailSent       = "EMAIL_SENT"
	EventTypeWarmupEmailSent = "WARMUP_EMAIL_SENT"
	EventTypeWarmupAction    = "WARMUP_ACTION"
	EventTypeEmailOpened     = "EMAIL_OPENED"
	EventTypeEmailClicked    = "EMAIL_CLICKED"
)

// Kafka topics
const (
	TopicEmailEvents    = "email-events"
	TopicWarmupEvents   = "warmup-events"
	TopicTrackingEvents = "tracking-events"
)

// EmailSentEvent represents an email sent event
type EmailSentEvent struct {
	EventType  string    `json:"event_type" avro:"event_type"`
	TaskID     uuid.UUID `json:"task_id" avro:"task_id"`
	AccountID  uuid.UUID `json:"account_id" avro:"account_id"`
	CampaignID uuid.UUID `json:"campaign_id" avro:"campaign_id"`
	ContactID  uuid.UUID `json:"contact_id" avro:"contact_id"`
	SequenceID uuid.UUID `json:"sequence_id" avro:"sequence_id"`
	MessageID  string    `json:"message_id" avro:"message_id"`
	Recipient  string    `json:"recipient" avro:"recipient"`
	Subject    string    `json:"subject" avro:"subject"`
	SentAt     time.Time `json:"sent_at" avro:"sent_at"`
}

// WarmupEmailSentEvent represents a warmup email sent event
type WarmupEmailSentEvent struct {
	EventType       string    `json:"event_type" avro:"event_type"`
	TaskID          uuid.UUID `json:"task_id" avro:"task_id"`
	SenderAccountID uuid.UUID `json:"sender_account_id" avro:"sender_account_id"`
	TargetAccountID uuid.UUID `json:"target_account_id" avro:"target_account_id"`
	MessageID       string    `json:"message_id" avro:"message_id"`
	IsReply         bool      `json:"is_reply" avro:"is_reply"`
	SentAt          time.Time `json:"sent_at" avro:"sent_at"`
}

// TrackingEvent represents an email open or click tracking event from the Rust tracking service
// Uses Avro serialization via Confluent Schema Registry
type TrackingEvent struct {
	EventType   string  `json:"event_type" avro:"event_type"`     // EMAIL_OPENED or EMAIL_CLICKED
	TaskID      string  `json:"task_id" avro:"task_id"`           // UUID string
	OriginalURL *string `json:"original_url" avro:"original_url"` // For click events only (nullable)
	LinkID      *string `json:"link_id" avro:"link_id"`           // Click ticket id (nullable; absent from older tracking builds)
	Timestamp   string  `json:"timestamp" avro:"timestamp"`       // ISO8601 timestamp
	UserAgent   *string `json:"user_agent" avro:"user_agent"`     // Browser user agent (nullable)
	IPHash      *string `json:"ip_hash" avro:"ip_hash"`           // Hashed IP for privacy (nullable)
	// ClientIP is the source network, not the address: the edge zeroes the
	// last IPv4 octet (or everything past the first 48 IPv6 bits) before
	// publishing, so what the bus retains cannot single out a host. The
	// consumer resolves it to a location and does not store it. Nullable and
	// absent from events written before the field existed.
	ClientIP *string `json:"client_ip" avro:"client_ip"`
	// Scanner names the known-scanner source the request came from, when the
	// tracking edge recognised one: a mail-filtering network rather than a
	// person's own device. Set means machine, whatever the user agent claims,
	// unless ScannerProbable is also set. Nullable and absent from events
	// written before the field existed.
	Scanner *string `json:"scanner" avro:"scanner"`
	// ScannerProbable says that source ALSO carries people's own requests, so
	// the match corroborates the timing rule instead of replacing it: the
	// event is automated only inside the wider probable window. Browser
	// isolation is the case it exists for. False for an ordinary request, for
	// a source that only ever filters mail, and for every event written before
	// the field existed, which is the conservative reading either way.
	ScannerProbable bool `json:"scanner_probable" avro:"scanner_probable"`
}
