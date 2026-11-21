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
