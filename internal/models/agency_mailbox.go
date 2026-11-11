package models

import (
	"time"

	"github.com/google/uuid"
)

// AgencyMailbox is one client mailbox as the agency's mailbox list shows it.
type AgencyMailbox struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Client         string    `json:"client"`
	Email          string    `json:"email"`
	Name           string    `json:"name"`
	Provider       string    `json:"provider"`
	Status         string    `json:"status"`
	// Erroring is a connected mailbox with an unresolved critical error.
	Erroring bool `json:"erroring"`
	Warmup   bool `json:"warmup"`
	// WarmupState is watch, throttled, quarantined or blocked; empty in good standing.
	WarmupState string `json:"warmup_state,omitempty"`
	// Vendor is the inbox vendor it was imported from, when there is one.
	Vendor        string    `json:"vendor,omitempty"`
	CampaignLimit int       `json:"campaign_limit"`
	CreatedAt     time.Time `json:"created_at"`
}

// AgencyMailboxQuery selects a page of mailboxes across client workspaces.
type AgencyMailboxQuery struct {
	OrgIDs []uuid.UUID
	Search string
	// Attention keeps only mailboxes that are disconnected, erroring or held from warmup.
	Attention bool
	Limit     int
	// Before is the last row of the previous page; zero starts at the newest.
	BeforeAt time.Time
	BeforeID uuid.UUID
}

// AgencyMailboxList is one page of the agency's mailbox list.
type AgencyMailboxList struct {
	Data       []AgencyMailbox `json:"data"`
	Pagination CPagination     `json:"pagination"`
}
