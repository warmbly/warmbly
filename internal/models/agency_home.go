package models

import (
	"time"

	"github.com/google/uuid"
)

// AgencyClientMembership is one client workspace of an agency with the caller's membership in it, if any.
type AgencyClientMembership struct {
	OrganizationID   uuid.UUID
	Name             string
	AvatarURL        *string
	MonthlySendLimit *int
	// Role is nil when the caller is not a member of the workspace.
	Role        *string
	Permissions OrganizationPermission
	AccessScope AccessScope
}

func (m AgencyClientMembership) asMember() *OrganizationMember {
	if m.Role == nil {
		return nil
	}
	return &OrganizationMember{OrganizationID: m.OrganizationID, Role: *m.Role, Permissions: m.Permissions, AccessScope: m.AccessScope}
}

// IsMember reports whether the caller belongs to the workspace.
func (m AgencyClientMembership) IsMember() bool { return m.Role != nil }

// Restricted reports a membership limited to granted campaigns and mailboxes.
func (m AgencyClientMembership) Restricted() bool { return m.asMember().IsRestricted() }

// Can reports whether the membership holds perm across the whole workspace.
func (m AgencyClientMembership) Can(perm OrganizationPermission) bool {
	mem := m.asMember()
	return mem != nil && !mem.IsRestricted() && mem.HasPermission(perm)
}

// CanReadInbox reports whether the membership reads the inbox, wholly or within its grants.
func (m AgencyClientMembership) CanReadInbox() bool {
	mem := m.asMember()
	return mem != nil && mem.HasPermission(PermAccessUnibox)
}

// AgencyClientAccess is how much of a client workspace the caller reaches.
type AgencyClientAccess string

const (
	AgencyClientAccessFull       AgencyClientAccess = "full"
	AgencyClientAccessRestricted AgencyClientAccess = "restricted"
	AgencyClientAccessNone       AgencyClientAccess = "none"
)

// Access classifies the membership for the overview.
func (m AgencyClientMembership) Access() AgencyClientAccess {
	switch {
	case !m.IsMember():
		return AgencyClientAccessNone
	case m.Restricted():
		return AgencyClientAccessRestricted
	}
	return AgencyClientAccessFull
}

// Campaign statuses the agency home treats as paused by limits or by billing.
var (
	CampaignStatusesPausedByLimits  = []string{"paused_no_accounts", "paused_guardrail", "paused_undeliverable"}
	CampaignStatusesPausedByBilling = []string{"paused_trial_expired"}
)

// AgencyClientCampaigns is a client's campaign activity; needs view_campaigns.
type AgencyClientCampaigns struct {
	Active            int     `json:"active"`
	PausedByLimits    int     `json:"paused_by_limits"`
	PausedByBilling   int     `json:"paused_by_billing"`
	SentToday         int64   `json:"sent_today"`
	Sent7d            int64   `json:"sent_7d"`
	Bounced7d         int64   `json:"bounced_7d"`
	BounceRate7d      float64 `json:"bounce_rate_7d"`
	PositiveReplies7d int64   `json:"positive_replies_7d"`
}

// AgencyClientInbox is a client's unanswered human mail; needs access_unibox.
type AgencyClientInbox struct {
	RepliesWaiting int64 `json:"replies_waiting"`
}

// AgencyClientMailboxes is a client's mailbox health; needs view_campaigns, like the mailbox list.
type AgencyClientMailboxes struct {
	Total        int `json:"total"`
	Disconnected int `json:"disconnected"`
	Erroring     int `json:"erroring"`
	Watch        int `json:"watch"`
	Throttled    int `json:"throttled"`
	Quarantined  int `json:"quarantined"`
	Blocked      int `json:"blocked"`
}

// AgencyClientAdvisor counts a client's open Advisor findings; needs view_analytics.
type AgencyClientAdvisor struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
}

// AgencyOverviewClient is one row of the agency home. A section is nil when the caller lacks its permission.
type AgencyOverviewClient struct {
	OrganizationID   uuid.UUID              `json:"organization_id"`
	Name             string                 `json:"name"`
	AvatarURL        *string                `json:"avatar_url,omitempty"`
	Access           AgencyClientAccess     `json:"access"`
	PoolUsed         int64                  `json:"pool_used"`
	MonthlySendLimit *int                   `json:"monthly_send_limit,omitempty"`
	Campaigns        *AgencyClientCampaigns `json:"campaigns,omitempty"`
	Inbox            *AgencyClientInbox     `json:"inbox,omitempty"`
	Mailboxes        *AgencyClientMailboxes `json:"mailboxes,omitempty"`
	Advisor          *AgencyClientAdvisor   `json:"advisor,omitempty"`
}

// AgencyOverviewTotals adds up what the caller can see across clients.
type AgencyOverviewTotals struct {
	Clients           int   `json:"clients"`
	SentToday         int64 `json:"sent_today"`
	RepliesWaiting    int64 `json:"replies_waiting"`
	PositiveReplies7d int64 `json:"positive_replies_7d"`
	ActiveCampaigns   int   `json:"active_campaigns"`
	Mailboxes         int   `json:"mailboxes"`
	MailboxProblems   int   `json:"mailbox_problems"`
}

// AgencyAttentionKind names a problem on the agency home's attention list.
type AgencyAttentionKind string

const (
	AttentionMailboxDisconnected    AgencyAttentionKind = "mailbox_disconnected"
	AttentionMailboxHeld            AgencyAttentionKind = "mailbox_held"
	AttentionBounceSpike            AgencyAttentionKind = "bounce_spike"
	AttentionCampaignsPausedLimits  AgencyAttentionKind = "campaigns_paused_limits"
	AttentionCampaignsPausedBilling AgencyAttentionKind = "campaigns_paused_billing"
	AttentionClientCapNear          AgencyAttentionKind = "client_cap_near"
	AttentionPoolLow                AgencyAttentionKind = "pool_low"
)

// AgencyAttention is one item on the attention list; OrganizationID is nil for the agency itself.
type AgencyAttention struct {
	Kind           AgencyAttentionKind `json:"kind"`
	Severity       string              `json:"severity"`
	OrganizationID *uuid.UUID          `json:"organization_id,omitempty"`
	ClientName     string              `json:"client_name,omitempty"`
	Count          int64               `json:"count"`
	// Rate is the bounce rate for bounce_spike, and the share of the cap used for client_cap_near.
	Rate float64 `json:"rate,omitempty"`
}

// AgencyOverview is the agency home: the pool, totals, problems first, then every client.
type AgencyOverview struct {
	GeneratedAt time.Time              `json:"generated_at"`
	Usage       *AgencyUsage           `json:"usage"`
	Totals      AgencyOverviewTotals   `json:"totals"`
	Attention   []AgencyAttention      `json:"attention"`
	Clients     []AgencyOverviewClient `json:"clients"`
}

// AgencyInboxThread is one conversation in the cross-client inbox: its newest message in scope.
type AgencyInboxThread struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	ID             uuid.UUID `json:"id"`
	EmailID        uuid.UUID `json:"email_id"`
	ThreadID       string    `json:"thread_id"`
	FromAddr       []string  `json:"from_addr"`
	ToAddr         []string  `json:"to_addr"`
	Subject        string    `json:"subject"`
	Snippet        string    `json:"snippet"`
	InternalDate   time.Time `json:"internal_date"`
	Seen           bool      `json:"seen"`
	MessageCount   int64     `json:"message_count"`
	HasUnread      bool      `json:"has_unread"`
}

// AgencyInboxPage is one page of the cross-client inbox.
type AgencyInboxPage struct {
	Data       []AgencyInboxThread `json:"data"`
	Pagination CPagination         `json:"pagination"`
}

// AgencyInboxQuery selects cross-client threads. The organization lists are
// computed server-side from the caller's memberships, never from the request.
type AgencyInboxQuery struct {
	// OrgIDs are workspaces read whole.
	OrgIDs []uuid.UUID
	// ScopedOrgIDs are restricted memberships, read only in ScopedMailboxes.
	ScopedOrgIDs    []uuid.UUID
	ScopedMailboxes []uuid.UUID
	Unread          bool
	NeedsReply      bool
	Positive        bool
	Search          string
	// BeforeAt and BeforeID are the keyset position to continue after; zero starts at the top.
	BeforeAt time.Time
	BeforeID uuid.UUID
	Limit    int
}
