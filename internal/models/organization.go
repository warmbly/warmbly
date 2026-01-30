package models

import (
	"time"

	"github.com/google/uuid"
)

// Organization represents a multi-user organization/workspace
type Organization struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Slug        *string    `json:"slug,omitempty"`
	OwnerUserID uuid.UUID  `json:"owner_user_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// Joined data
	Owner *User `json:"owner,omitempty"`
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
	Organization *Organization `json:"organization,omitempty"`
	InvitedByUser *User        `json:"invited_by_user,omitempty"`
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
	EnterpriseInquiryStatusPending    EnterpriseInquiryStatus = "pending"
	EnterpriseInquiryStatusContacted  EnterpriseInquiryStatus = "contacted"
	EnterpriseInquiryStatusConverted  EnterpriseInquiryStatus = "converted"
	EnterpriseInquiryStatusDeclined   EnterpriseInquiryStatus = "declined"
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
}

// OrganizationLimits represents the limits for an organization based on their plan
type OrganizationLimits struct {
	MaxCampaigns       *int `json:"max_campaigns,omitempty"`
	MaxActiveCampaigns *int `json:"max_active_campaigns,omitempty"`
	MaxTeamMembers     *int `json:"max_team_members,omitempty"`
	MaxEmailAccounts   *int `json:"max_email_accounts,omitempty"`
	MaxContacts        *int `json:"max_contacts,omitempty"`
}

// OrganizationWithLimits combines organization with its limits and counts
type OrganizationWithLimits struct {
	Organization
	Limits *OrganizationLimits `json:"limits,omitempty"`
	Counts *OrganizationCounts `json:"counts,omitempty"`
}
