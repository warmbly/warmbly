package models

import (
	"time"

	"github.com/google/uuid"
)

// MiniCategory is the lightweight shape we attach to contact responses
// so the UI can render category chips without doing a second lookup. It
// is a denormalised slice of the row in `categories` plus nothing else.
type MiniCategory struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Color string    `json:"color"`
}

type Contact struct {
	ID uuid.UUID `json:"id"`

	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Company   string `json:"company"`
	Phone     string `json:"phone"`

	CustomFields map[string]string `json:"custom_fields"`

	Subscribed bool           `json:"subscribed"`
	Campaigns  []MiniCampaign `json:"campaigns"`
	Categories []MiniCategory `json:"categories"`

	// Pre-send verification state (see internal/pkg/emailverify). Populated by
	// the verification scheduler / on-demand verify; the campaign send path uses
	// VerificationStatus == "invalid" to drop addresses before a worker sends.
	// VerificationStatus is one of: valid | risky | invalid | unknown.
	VerificationStatus    string     `json:"verification_status"`
	VerificationReason    string     `json:"verification_reason"`
	IsCatchAll            bool       `json:"is_catch_all"`
	VerificationCheckedAt *time.Time `json:"verification_checked_at,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type ContactsResult struct {
	Data       []Contact  `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// ContactEngagement summarises every email touchpoint we have for a
// single contact. It's denormalised on read so the contact 360 view
// can render counts and "last X" timestamps in a single round-trip.
type ContactEngagement struct {
	TotalSent       int `json:"total_sent"`
	TotalOpened     int `json:"total_opened"`
	TotalClicked    int `json:"total_clicked"`
	TotalReplied    int `json:"total_replied"`
	TotalBounced    int `json:"total_bounced"`
	TotalComplained int `json:"total_complained"`

	LastSentAt    *time.Time `json:"last_sent_at,omitempty"`
	LastOpenedAt  *time.Time `json:"last_opened_at,omitempty"`
	LastClickedAt *time.Time `json:"last_clicked_at,omitempty"`
	LastRepliedAt *time.Time `json:"last_replied_at,omitempty"`
	LastBouncedAt *time.Time `json:"last_bounced_at,omitempty"`
}

// ContactSuppression mirrors a row from suppressed_recipients for the
// contact's email. Null on the wire when the contact is not suppressed.
type ContactSuppression struct {
	Reason    string     `json:"reason"`
	Source    string     `json:"source"` // bounce | complaint | unsubscribe
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// ContactDetail is the hydrated read model returned by GET /contacts/:id.
// It bundles everything the slide-over needs in one payload so the UI
// doesn't have to fan out a half-dozen requests on open.
type ContactDetail struct {
	Contact
	Engagement  ContactEngagement   `json:"engagement"`
	Suppression *ContactSuppression `json:"suppression,omitempty"`
}

// ContactSentEmail is one row in the "Emails sent to this contact"
// list. Each row corresponds to a single delivered (or attempted)
// task. Engagement timestamps come from campaign_contact_progress
// when present; some legacy rows may have nil progress.
type ContactSentEmail struct {
	TaskID    uuid.UUID `json:"task_id"`
	Status    string    `json:"status"`
	MessageID string    `json:"message_id"`
	Subject   string    `json:"subject"`
	SentAt    time.Time `json:"sent_at"`

	// Sender mailbox
	EmailAccountID    *uuid.UUID `json:"email_account_id,omitempty"`
	EmailAccountEmail *string    `json:"email_account_email,omitempty"`
	EmailAccountName  *string    `json:"email_account_name,omitempty"`

	// Campaign + sequence context
	CampaignID   *uuid.UUID `json:"campaign_id,omitempty"`
	CampaignName *string    `json:"campaign_name,omitempty"`
	SequenceID   *uuid.UUID `json:"sequence_id,omitempty"`
	SequenceName *string    `json:"sequence_name,omitempty"`

	// Engagement (from campaign_contact_progress, may be nil).
	OpenedAt  *time.Time `json:"opened_at,omitempty"`
	ClickedAt *time.Time `json:"clicked_at,omitempty"`
	RepliedAt *time.Time `json:"replied_at,omitempty"`
	BouncedAt *time.Time `json:"bounced_at,omitempty"`
}

type ContactSentEmailsResult struct {
	Data       []ContactSentEmail `json:"data"`
	Pagination Pagination         `json:"pagination"`
}

// ContactTimelineEventType is a closed enum so the frontend can pick
// the right icon / colour without parsing free text.
type ContactTimelineEventType string

const (
	TimelineEmailSent      ContactTimelineEventType = "email_sent"
	TimelineEmailOpened    ContactTimelineEventType = "email_opened"
	TimelineEmailClicked   ContactTimelineEventType = "email_clicked"
	TimelineEmailReplied   ContactTimelineEventType = "email_replied"
	TimelineEmailBounced   ContactTimelineEventType = "email_bounced"
	TimelineReplyReceived  ContactTimelineEventType = "reply_received"
	TimelineDeliverability ContactTimelineEventType = "deliverability"
	TimelineSuppressed     ContactTimelineEventType = "suppressed"
	TimelineNote           ContactTimelineEventType = "note"
)

// ContactTimelineEvent is one entry in the merged activity feed. The
// optional fields are tagged with omitempty so the JSON stays compact
// for event types that don't carry that data.
type ContactTimelineEvent struct {
	Type ContactTimelineEventType `json:"type"`
	At   time.Time                `json:"at"`

	// Mailbox sender (email_sent / opened / clicked / replied / bounced).
	EmailAccountID    *uuid.UUID `json:"email_account_id,omitempty"`
	EmailAccountEmail *string    `json:"email_account_email,omitempty"`
	EmailAccountName  *string    `json:"email_account_name,omitempty"`

	// Campaign / sequence linkage. Optional because notes, suppression,
	// and out-of-campaign reply intents don't always have one.
	CampaignID   *uuid.UUID `json:"campaign_id,omitempty"`
	CampaignName *string    `json:"campaign_name,omitempty"`
	SequenceID   *uuid.UUID `json:"sequence_id,omitempty"`
	SequenceName *string    `json:"sequence_name,omitempty"`

	// Task linkage for engagement events.
	TaskID  *uuid.UUID `json:"task_id,omitempty"`
	Subject *string    `json:"subject,omitempty"`

	// Type-specific.
	Reason   *string `json:"reason,omitempty"`   // deliverability / suppression
	Source   *string `json:"source,omitempty"`   // suppression: bounce/complaint/unsubscribe
	Provider *string `json:"provider,omitempty"` // deliverability provider
	Intent   *string `json:"intent,omitempty"`   // reply_intent classification
	Content  *string `json:"content,omitempty"`  // note body

	// Author (notes).
	UserID *uuid.UUID `json:"user_id,omitempty"`
}

type ContactTimelineResult struct {
	Data []ContactTimelineEvent `json:"data"`
	// True if we hit the per-call cap and the caller should paginate
	// via the `before` query param.
	HasMore bool `json:"has_more"`
}

type UpdateContact struct {
	FirstName        *string            `json:"first_name"`
	LastName         *string            `json:"last_name"`
	Company          *string            `json:"company"`
	Phone            *string            `json:"phone"`
	CustomFields     *map[string]string `json:"custom_fields"`
	Subscribed       *bool              `json:"subscribed"`
	Campaigns        []string           `json:"campaigns"`         // List of campaign IDs to set (nil = leave as-is)
	Categories       []string           `json:"categories"`        // List of category IDs to set (nil = leave as-is)
	AddCategories    []string           `json:"add_categories"`    // Diff-style add (ignored when Categories is set)
	RemoveCategories []string           `json:"remove_categories"` // Diff-style remove (ignored when Categories is set)
}

type AddContact struct {
	FirstName  string   `json:"first_name"`
	LastName   string   `json:"last_name"`
	Email      string   `json:"email"`
	Company    string   `json:"company"`
	Phone      string   `json:"phone"`
	Campaigns  []string `json:"campaigns"`
	Categories []string `json:"categories"`

	CustomFields map[string]string `json:"custom_fields"`
}

type SearchContactsFilterType string

const (
	SearchContactsFilterTypeEqual      SearchContactsFilterType = "equal"
	SearchContactsFilterTypeStartsWith SearchContactsFilterType = "starts_with"
	SearchContactsFilterTypeEndsWith   SearchContactsFilterType = "ends_with"
	SearchContactsFilterTypeContains   SearchContactsFilterType = "contains"
)

type SearchContactsFilter struct {
	Name  string                   `json:"name"`
	Value string                   `json:"value"`
	Type  SearchContactsFilterType `json:"type"`
}

type SearchContacts struct {
	Query              string                 `json:"query"`                // Text search across core fields
	CustomFieldFilters []SearchContactsFilter `json:"custom_field_filters"` // Custom Field Filters
	CampaignIDs        []string               `json:"campaign_ids"`         // Contacts must be in ALL these campaigns
	CategoryIDs        []string               `json:"category_ids"`         // Contacts must have ALL these categories
	MinCampaigns       *int                   `json:"min_campaigns"`        // Minimum number of associated campaigns
	MaxCampaigns       *int                   `json:"max_campaigns"`        // Maximum number of associated campaigns
	Subscribed         *bool                  `json:"subscribed"`           // Filter by subscription status
	CreatedAfter       *time.Time             `json:"created_after"`        // Contacts created after this date
	CreatedBefore      *time.Time             `json:"created_before"`       // Contacts created before this date
	UpdatedAfter       *time.Time             `json:"updated_after"`        // Contacts updated after this date
	UpdatedBefore      *time.Time             `json:"updated_before"`       // Contacts updated before this date
	SortBy             string                 `json:"sort_by"`              // e.g., "first_name ASC", "campaign_count DESC"
	Reverse            bool                   `json:"reverse"`              // ASC or DESC
	Offset             int                    `json:"offset"`               // Pagination
}

type BulkEditContactsFieldType string

const (
	BulkAddField    BulkEditContactsFieldType = "ADD"
	BulkEditField   BulkEditContactsFieldType = "EDIT"
	BulkDeleteField BulkEditContactsFieldType = "DELETE"
	BulkRenameField BulkEditContactsFieldType = "RENAME"
)

type BulkEditContactsField struct {
	Type  BulkEditContactsFieldType `json:"type"`
	Key   string                    `json:"key"`
	Value string                    `json:"value"`
}

type BulkEditContactsData struct {
	Contacts []string `json:"contacts"`

	AddCampaigns     []string                `json:"add_campaigns"`
	RemoveCampaigns  []string                `json:"remove_campaigns"`
	AddCategories    []string                `json:"add_categories,omitempty"`
	RemoveCategories []string                `json:"remove_categories,omitempty"`
	Fields           []BulkEditContactsField `json:"fields"`
	Subscribe        *bool                   `json:"subscribe"`
}
