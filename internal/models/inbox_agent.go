package models

import (
	"time"

	"github.com/google/uuid"
)

// Inbox-agent draft statuses. A draft is inert until a human acts on it.
const (
	// AIDraftPending is a fresh draft awaiting human review in the unibox.
	AIDraftPending = "pending"
	// AIDraftApproved was approved and sent through the normal reply path.
	AIDraftApproved = "approved"
	// AIDraftDiscarded was dismissed by a human without sending.
	AIDraftDiscarded = "discarded"
)

// AIThreadDraft is a suggested reply the inbox agent drafted for an inbound
// human reply, persisted awaiting a human Approve-and-send / Edit / Discard.
// The agent never sends: approving is the only path that transmits, and it
// reuses the normal unibox send.
type AIThreadDraft struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	EmailAccountID uuid.UUID `json:"email_account_id"`
	// OwnerUserID is the mailbox owner; the approved send goes out as them.
	OwnerUserID uuid.UUID `json:"owner_user_id"`
	ThreadID    string    `json:"thread_id"`
	// SourceMessageID is the inbound message this replies to (dedupe + threading).
	SourceMessageID *uuid.UUID `json:"source_message_id,omitempty"`
	ContactID       *uuid.UUID `json:"contact_id,omitempty"`
	CampaignID      *uuid.UUID `json:"campaign_id,omitempty"`
	ToAddr          string     `json:"to_addr"`
	Subject         string     `json:"subject"`
	// InReplyTo is the RFC Message-Id of the inbound reply, referenced on send.
	InReplyTo   string    `json:"in_reply_to"`
	Body        string    `json:"body"`
	IntentClass string    `json:"intent_class"`
	Confidence  float64   `json:"confidence"`
	Model       string    `json:"model"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// InboxAgentReply is the inbound-reply context handed to the inbox agent to
// draft a suggested reply. It is assembled at the reply-processing hook (all ids
// already resolved) and passed as one struct so the advanced -> inboxagent
// boundary stays a single structural method (no import cycle).
type InboxAgentReply struct {
	OrganizationID uuid.UUID
	EmailAccountID uuid.UUID
	// OwnerUserID is the mailbox owner; the approved send goes out as them.
	OwnerUserID uuid.UUID
	// SourceMessageID is the stored inbound message (dedupe + threading).
	SourceMessageID uuid.UUID
	ThreadID        string
	// Counterpart is the human who replied — the recipient of the drafted reply.
	Counterpart string
	Subject     string
	// Snippet is the inbound reply's body preview, used to skip drafting a reply
	// to a trivial ack ("thanks", "ok") that isn't worth a paid draft.
	Snippet string
	// InReplyTo is the RFC Message-Id of the inbound reply (referenced on send).
	InReplyTo   string
	ContactID   uuid.UUID
	CampaignID  uuid.UUID
	IntentClass string
	Confidence  float64
}
