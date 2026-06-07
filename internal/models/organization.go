package models

import (
	"time"

	"github.com/google/uuid"
)

// Organization represents a multi-user organization/workspace
type Organization struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        *string   `json:"slug,omitempty"`
	AvatarURL   *string   `json:"avatar_url,omitempty"`
	OwnerUserID uuid.UUID `json:"owner_user_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Soft-delete window. When DeletionScheduledFor is non-nil the
	// organization is "pending deletion" and will be hard-deleted at
	// that timestamp unless cancelled.
	DeletionScheduledAt  *time.Time `json:"deletion_scheduled_at,omitempty"`
	DeletionScheduledFor *time.Time `json:"deletion_scheduled_for,omitempty"`

	// Joined data
	Owner *User `json:"owner,omitempty"`
}

// IsPendingDeletion reports whether the organization is currently
// scheduled for a delayed hard delete.
func (o *Organization) IsPendingDeletion() bool {
	return o.DeletionScheduledFor != nil
}

// OrganizationMember represents a user's membership in an organization
type OrganizationMember struct {
	ID             uuid.UUID              `json:"id"`
	OrganizationID uuid.UUID              `json:"organization_id"`
	UserID         uuid.UUID              `json:"user_id"`
	Role           string                 `json:"role"`
	Permissions    OrganizationPermission `json:"permissions"`
	InvitedBy      *uuid.UUID             `json:"invited_by,omitempty"`
	InvitedAt      time.Time              `json:"invited_at"`
	AcceptedAt     *time.Time             `json:"accepted_at,omitempty"`

	// Joined data
	User         *User         `json:"user,omitempty"`
	Organization *Organization `json:"organization,omitempty"`

	// Flattened convenience fields populated from the joined user, so clients
	// can read member.email / member.name directly (the member list UIs,
	// assignee pickers, and team pickers all rely on these).
	Email string `json:"email"`
	Name  string `json:"name"`
}

// HasPermission checks if the member has a specific permission
func (m *OrganizationMember) HasPermission(perm OrganizationPermission) bool {
	return m.Permissions.HasPermission(perm)
}

// IsOwner returns true if the member is the organization owner
func (m *OrganizationMember) IsOwner() bool {
	return m.Role == string(RoleOwner)
}

// OrganizationInvitation represents a pending invitation to join an organization
type OrganizationInvitation struct {
	ID             uuid.UUID              `json:"id"`
	OrganizationID uuid.UUID              `json:"organization_id"`
	Email          string                 `json:"email"`
	Role           string                 `json:"role"`
	Permissions    OrganizationPermission `json:"permissions"`
	InvitedBy      uuid.UUID              `json:"invited_by"`
	Token          string                 `json:"-"` // Never expose token in JSON
	ExpiresAt      time.Time              `json:"expires_at"`
	CreatedAt      time.Time              `json:"created_at"`

	// Joined data
	Organization  *Organization `json:"organization,omitempty"`
	InvitedByUser *User         `json:"invited_by_user,omitempty"`
}

// IsExpired returns true if the invitation has expired
func (i *OrganizationInvitation) IsExpired() bool {
	return time.Now().After(i.ExpiresAt)
}

// EnterpriseInquiry represents a contact request for enterprise pricing
type EnterpriseInquiry struct {
	ID              uuid.UUID  `json:"id"`
	CompanyName     string     `json:"company_name"`
	ContactName     string     `json:"contact_name"`
	ContactEmail    string     `json:"contact_email"`
	EstimatedVolume *int       `json:"estimated_volume,omitempty"`
	TeamSize        *int       `json:"team_size,omitempty"`
	Notes           string     `json:"notes,omitempty"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`
	ProcessedBy     *uuid.UUID `json:"processed_by,omitempty"`
}

// EnterpriseInquiryStatus represents the status of an enterprise inquiry
type EnterpriseInquiryStatus string

const (
	EnterpriseInquiryStatusPending   EnterpriseInquiryStatus = "pending"
	EnterpriseInquiryStatusContacted EnterpriseInquiryStatus = "contacted"
	EnterpriseInquiryStatusConverted EnterpriseInquiryStatus = "converted"
	EnterpriseInquiryStatusDeclined  EnterpriseInquiryStatus = "declined"
)

// CreateOrganizationRequest represents the request to create a new organization
type CreateOrganizationRequest struct {
	Name string `json:"name" binding:"required,min=1,max=255"`
}

// UpdateOrganizationRequest represents the request to update an organization
type UpdateOrganizationRequest struct {
	Name *string `json:"name,omitempty"`
	Slug *string `json:"slug,omitempty"`
}

// InviteMemberRequest represents the request to invite a new member
type InviteMemberRequest struct {
	Email       string  `json:"email" binding:"required,email"`
	Role        string  `json:"role,omitempty"`
	Permissions *uint16 `json:"permissions,omitempty"`
}

// UpdateMemberRequest represents the request to update a member's role/permissions
type UpdateMemberRequest struct {
	Role        *string `json:"role,omitempty"`
	Permissions *uint16 `json:"permissions,omitempty"`
}

// TransferOwnershipRequest represents the request to transfer organization ownership
type TransferOwnershipRequest struct {
	NewOwnerUserID uuid.UUID `json:"new_owner_user_id" binding:"required"`
}

// AcceptInvitationRequest represents the request to accept an invitation
type AcceptInvitationRequest struct {
	Token string `json:"token" binding:"required"`
}

// OrganizationCounts represents resource counts for an organization
type OrganizationCounts struct {
	TotalCampaigns  int `json:"total_campaigns"`
	ActiveCampaigns int `json:"active_campaigns"`
	TotalContacts   int `json:"total_contacts"`
	TotalMembers    int `json:"total_members"`
	EmailAccounts   int `json:"email_accounts"`
	EmailsSentToday int `json:"emails_sent_today"`
}

// OrganizationLimits represents the limits for an organization based on their plan
type OrganizationLimits struct {
	MaxCampaigns       *int `json:"max_campaigns,omitempty"`
	MaxActiveCampaigns *int `json:"max_active_campaigns,omitempty"`
	MaxTeamMembers     *int `json:"max_team_members,omitempty"`
	MaxEmailAccounts   *int `json:"max_email_accounts,omitempty"`
	MaxContacts        *int `json:"max_contacts,omitempty"`
	DailyCampaignLimit *int `json:"daily_campaign_limit,omitempty"`
}

// OrganizationWithLimits combines organization with its limits and counts
type OrganizationWithLimits struct {
	Organization
	Limits *OrganizationLimits `json:"limits,omitempty"`
	Counts *OrganizationCounts `json:"counts,omitempty"`
}

// OrganizationLimitOverrides is the per-org override row. Each numeric
// field uses 0 as the "no override, inherit from plan" sentinel —
// resolution happens in GetEffectiveLimits, not here. The row is upsert-
// only; reverting an override is a write of 0 so the granted_by audit
// trail survives across revisions.
type OrganizationLimitOverrides struct {
	OrganizationID     uuid.UUID  `json:"organization_id"`
	MaxCampaigns       int        `json:"max_campaigns"`
	MaxActiveCampaigns int        `json:"max_active_campaigns"`
	MaxTeamMembers     int        `json:"max_team_members"`
	MaxEmailAccounts   int        `json:"max_email_accounts"`
	MaxContacts        int        `json:"max_contacts"`
	DailyCampaignLimit int        `json:"daily_campaign_limit"`
	GrantedBy          *uuid.UUID `json:"granted_by,omitempty"`
	GrantedAt          time.Time  `json:"granted_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	Notes              string     `json:"notes"`
}

// UpdateOrgOverridesRequest is the payload for PUT /admin/organizations/:id/overrides.
// Pointer fields so the caller can leave any column untouched; setting
// a value to 0 explicitly removes that column's override.
type UpdateOrgOverridesRequest struct {
	MaxCampaigns       *int    `json:"max_campaigns,omitempty"`
	MaxActiveCampaigns *int    `json:"max_active_campaigns,omitempty"`
	MaxTeamMembers     *int    `json:"max_team_members,omitempty"`
	MaxEmailAccounts   *int    `json:"max_email_accounts,omitempty"`
	MaxContacts        *int    `json:"max_contacts,omitempty"`
	DailyCampaignLimit *int    `json:"daily_campaign_limit,omitempty"`
	Notes              *string `json:"notes,omitempty"`
}

// LimitRequestStatus mirrors the postgres enum from migration 000046.
type LimitRequestStatus string

const (
	LimitRequestStatusPending   LimitRequestStatus = "pending"
	LimitRequestStatusApproved  LimitRequestStatus = "approved"
	LimitRequestStatusRejected  LimitRequestStatus = "rejected"
	LimitRequestStatusCancelled LimitRequestStatus = "cancelled"
)

// LimitIncreaseRequest is one row of the queue. Approving translates
// the row into a write on organization_limit_overrides — same path the
// admin override editor uses, so the audit story is unified.
type LimitIncreaseRequest struct {
	ID               uuid.UUID          `json:"id"`
	OrganizationID   uuid.UUID          `json:"organization_id"`
	Field            string             `json:"field"`
	CurrentEffective int                `json:"current_effective"`
	Requested        int                `json:"requested"`
	Reason           string             `json:"reason"`
	Status           LimitRequestStatus `json:"status"`
	SubmittedBy      uuid.UUID          `json:"submitted_by"`
	SubmittedAt      time.Time          `json:"submitted_at"`
	ReviewedBy       *uuid.UUID         `json:"reviewed_by,omitempty"`
	ReviewedAt       *time.Time         `json:"reviewed_at,omitempty"`
	ReviewNotes      string             `json:"review_notes"`

	// Joined data — populated by admin queries.
	Organization    *Organization `json:"organization,omitempty"`
	SubmittedByUser *User         `json:"submitted_by_user,omitempty"`
}

// CreateLimitIncreaseRequest is what the dashboard sends. Field must
// match one of the OrganizationLimits keys; the service rejects unknown
// fields and refuses Requested values that aren't strictly greater than
// the current effective limit.
type CreateLimitIncreaseRequest struct {
	Field     string `json:"field" binding:"required"`
	Requested int    `json:"requested" binding:"required,min=1"`
	Reason    string `json:"reason" binding:"required,min=1,max=2000"`
}

// ReviewLimitRequestBody is what the admin sends to approve/reject. The
// approve handler writes the corresponding override; reject just stamps
// the row.
type ReviewLimitRequestBody struct {
	Notes string `json:"notes"`
}
