package models

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ContactLinkedInField is the custom field contacts.linkedin_url is derived from (a trigger keeps the column in step).
const ContactLinkedInField = "linkedin_url"

var (
	// linkedInProfile matches public.linkedin_public_id in the migration that added contacts.linkedin_public_id.
	linkedInProfile = regexp.MustCompile(`(?i)(?:^|[/.])linkedin\.com/in/([^/?#\s"'<>\\]+)`)
	linkedInSlug    = regexp.MustCompile(`^[\p{L}\p{N}_%.-]{1,200}$`)
)

// LinkedInPublicID is the lowercase profile slug a LinkedIn URL or bare public id names, "" when it names none.
func LinkedInPublicID(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := linkedInProfile.FindStringSubmatch(raw); m != nil {
		return strings.ToLower(m[1])
	}
	if !strings.Contains(raw, "/") && linkedInSlug.MatchString(raw) {
		return strings.ToLower(raw)
	}
	return ""
}

// NormalizeLinkedInURL is the canonical profile URL for a LinkedIn URL or public id; ok is false when it names no profile.
func NormalizeLinkedInURL(raw string) (string, bool) {
	id := LinkedInPublicID(raw)
	if id == "" {
		return "", false
	}
	return "https://www.linkedin.com/in/" + id, true
}

// LinkedInOf is the contact's LinkedIn profile from its custom fields, nil when it has none.
func LinkedInOf(fields map[string]string) *string {
	if id := linkedInProfile.FindStringSubmatch(fields[ContactLinkedInField]); id != nil {
		v := "https://www.linkedin.com/in/" + strings.ToLower(id[1])
		return &v
	}
	return nil
}

// LinkedIn workspace modes: none, Warmbly's own GraphKit workspace billed per seat, or the customer's own GraphKit workspace.
const (
	LinkedInModeNone    = "none"
	LinkedInModeManaged = "managed"
	LinkedInModeLinked  = "linked"
)

// LinkedIn seat billing modes.
const (
	LinkedInBillingStore  = "store"
	LinkedInBillingLinked = "linked"
)

// LinkedInWorkspace is the workspace's LinkedIn mode, mirrored from GraphKit.
type LinkedInWorkspace struct {
	OrganizationID uuid.UUID
	Mode           string
	WorkspaceName  *string
	LinkedAt       *time.Time
	LinkSessionID  *string
	UpdatedAt      time.Time
}

// LinkedInAccount is one connected LinkedIn account; Snapshot is GraphKit's SocialAccount as last read.
type LinkedInAccount struct {
	ID                  uuid.UUID
	OrganizationID      uuid.UUID
	UserID              *uuid.UUID
	GraphKitID          string
	Status              string
	StatusReason        *string
	DisplayName         string
	Headline            string
	AvatarURL           string
	ProfileURL          string
	PublicID            string
	Licences            []string
	Snapshot            json.RawMessage
	BillingMode         string
	MonthlyRetailMicros Micro
	PaidThrough         *time.Time
	BillingFailures     int
	BillingAttemptedAt  *time.Time
	PendingChargeKey    *string
	PendingChargeMicros Micro
	PaidKey             *string
	PaidMicros          Micro
	UnpaidSince         *time.Time
	UnpaidPaused        bool
	LastNotice          string
	ConnectedAt         *time.Time
	RemovedAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// LinkedInConnectSession is one connect or reconnect at GraphKit and the first month it charged.
type LinkedInConnectSession struct {
	ID                string
	OrganizationID    uuid.UUID
	UserID            *uuid.UUID
	Kind              string
	AccountID         *uuid.UUID
	ChargeKey         *string
	ChargedMicros     Micro
	Status            string
	AccountGraphKitID *string
	RefundedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// LinkedInConversation is one synced LinkedIn conversation.
type LinkedInConversation struct {
	ID                 uuid.UUID       `json:"id"`
	OrganizationID     uuid.UUID       `json:"-"`
	AccountID          uuid.UUID       `json:"account_id"`
	GraphKitID         string          `json:"-"`
	ContactID          *uuid.UUID      `json:"contact_id"`
	Participant        json.RawMessage `json:"participant"`
	Kind               string          `json:"kind"`
	UnreadCount        int             `json:"unread_count"`
	LastMessageAt      *time.Time      `json:"last_message_at"`
	LastMessagePreview string          `json:"last_message_preview"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// LinkedInMessage is one message of a conversation; BodySealed is the body under the workspace's data key.
type LinkedInMessage struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	ConversationID uuid.UUID
	GraphKitID     string
	Direction      string
	BodySealed     string
	SentAt         *time.Time
	Status         string
	Kind           string
	ActionID       *uuid.UUID
	SentByUserID   *uuid.UUID
	CreatedAt      time.Time
}

// LinkedInSavedSearch is a named LinkedIn lead search; Query is GraphKit's Query verbatim.
type LinkedInSavedSearch struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"-"`
	Name           string          `json:"name"`
	Query          json.RawMessage `json:"query"`
	CreatedBy      *uuid.UUID      `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// LinkedInContactMatch is a workspace contact a LinkedIn profile already is, with what Warmbly did with it.
type LinkedInContactMatch struct {
	PublicID  string
	ContactID uuid.UUID
	Company   string
	Campaigns []LinkedInCampaignRef
	// Contacted is a sent campaign email; Replied a reply from the contact.
	Contacted bool
	Replied   bool
}

// LinkedInCampaignRef names one campaign a contact is in.
type LinkedInCampaignRef struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
}

// LinkedIn action row statuses: pending is reserved here and not yet queued at GraphKit.
const (
	LinkedInActionPending   = "pending"
	LinkedInActionQueued    = "queued"
	LinkedInActionRunning   = "running"
	LinkedInActionSucceeded = "succeeded"
	LinkedInActionSkipped   = "skipped"
	LinkedInActionFailed    = "failed"
	LinkedInActionCancelled = "cancelled"
	LinkedInActionExpired   = "expired"
)

// LinkedInAction is one LinkedIn step queued at GraphKit for one contact.
type LinkedInAction struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"-"`
	GraphKitID     *string    `json:"-"`
	AccountID      *uuid.UUID `json:"account_id"`
	CampaignID     *uuid.UUID `json:"campaign_id"`
	CampaignName   string     `json:"campaign_name,omitempty"`
	ContactID      uuid.UUID  `json:"contact_id"`
	SequenceID     *uuid.UUID `json:"step_id"`
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	SkipReason     *string    `json:"skip_reason"`
	ErrorCode      *string    `json:"error_code"`
	Reference      string     `json:"-"`
	Attempts       int        `json:"-"`
	QueuedAt       *time.Time `json:"queued_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// LinkedInStepRequest is one due LinkedIn step of a campaign for one contact.
type LinkedInStepRequest struct {
	OrganizationID uuid.UUID
	CampaignID     uuid.UUID
	ContactID      uuid.UUID
	SequenceID     uuid.UUID
	Config         ActionConfig
	LinkedInURL    string
	// Render renders a template for the contact as email copy is, with the sending account's name as the sender.
	Render func(text, part, senderName string) string
}

// LinkedIn step outcomes: done routes on, retry keeps the step due.
const (
	LinkedInStepQueued  = "queued"
	LinkedInStepSkipped = "skipped"
	LinkedInStepFailed  = "failed"
	LinkedInStepRetry   = "retry"
)

// LinkedInStepOutcome is what became of a due LinkedIn step; Reason is a plain sentence for the campaign feed.
type LinkedInStepOutcome struct {
	State  string
	Reason string
	// Code is the skip reason or error code behind a skipped or failed step.
	Code string
	// RetryAfter is how soon a retry is due; zero is the default.
	RetryAfter time.Duration
}

// LinkedInReply is an inbound LinkedIn message for the reply path; the campaign fields are set when it answers a campaign.
type LinkedInReply struct {
	OrganizationID uuid.UUID
	ConversationID uuid.UUID
	AccountID      uuid.UUID
	NotifyUserID   *uuid.UUID
	ContactID      *uuid.UUID
	CampaignID     *uuid.UUID
	SequenceID     *uuid.UUID
	Sender         string
	Body           string
}

// LinkedInCampaignStats is the LinkedIn half of a campaign's analytics.
type LinkedInCampaignStats struct {
	InvitesSent     int     `json:"invites_sent"`
	InvitesAccepted int     `json:"invites_accepted"`
	AcceptanceRate  float64 `json:"acceptance_rate"`
	MessagesSent    int     `json:"messages_sent"`
	InmailsSent     int     `json:"inmails_sent"`
	Replies         int     `json:"replies"`
}

// LinkedIn import statuses.
const (
	LinkedInImportQueued  = "queued"
	LinkedInImportRunning = "running"
	LinkedInImportDone    = "done"
	LinkedInImportFailed  = "failed"
)

// LinkedInImport is a LinkedIn import that looks up work emails in the background.
type LinkedInImport struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"-"`
	UserID         *uuid.UUID      `json:"-"`
	Status         string          `json:"status"`
	Total          int             `json:"total"`
	Processed      int             `json:"processed"`
	Created        int             `json:"created"`
	Updated        int             `json:"updated"`
	Skipped        int             `json:"skipped"`
	NoEmail        int             `json:"no_email"`
	EmailsFound    int             `json:"emails_found"`
	ContactIDs     []uuid.UUID     `json:"contact_ids"`
	Request        json.RawMessage `json:"-"`
	Error          *string         `json:"error"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}
