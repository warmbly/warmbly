package models

import (
	"github.com/google/uuid"
)

// AccessScope is which resources a member's role applies to.
type AccessScope string

const (
	// AccessScopeWorkspace is every resource in the workspace.
	AccessScopeWorkspace AccessScope = "workspace"
	// AccessScopeRestricted is only the resources granted to the member.
	AccessScopeRestricted AccessScope = "restricted"
)

// Valid reports whether s is a known scope.
func (s AccessScope) Valid() bool {
	return s == AccessScopeWorkspace || s == AccessScopeRestricted
}

// RestrictedPermissionMask is the most a restricted member's role can grant; migration 000278's trigger holds the same value.
const RestrictedPermissionMask = PermViewCampaigns | PermViewAnalytics | PermAccessUnibox

// MaxAccessGrants bounds each grant list on one member.
const MaxAccessGrants = 500

// MemberAccess is the resource scope set on a member or an invitation.
type MemberAccess struct {
	Scope AccessScope `json:"scope"`
	// FolderIDs grant every campaign the folder holds at the time of the request.
	FolderIDs       []uuid.UUID `json:"campaign_folder_ids"`
	CampaignIDs     []uuid.UUID `json:"campaign_ids"`
	EmailAccountIDs []uuid.UUID `json:"email_account_ids"`
}

// Restricted reports whether a is a restricted scope.
func (a *MemberAccess) Restricted() bool {
	return a != nil && a.Scope == AccessScopeRestricted
}

// Normalize deduplicates the grant lists and clears them for a workspace scope.
func (a *MemberAccess) Normalize() {
	if a.Scope == "" {
		a.Scope = AccessScopeWorkspace
	}
	if a.Scope != AccessScopeRestricted {
		a.FolderIDs, a.CampaignIDs, a.EmailAccountIDs = []uuid.UUID{}, []uuid.UUID{}, []uuid.UUID{}
		return
	}
	a.FolderIDs = dedupeUUIDs(a.FolderIDs)
	a.CampaignIDs = dedupeUUIDs(a.CampaignIDs)
	a.EmailAccountIDs = dedupeUUIDs(a.EmailAccountIDs)
}

// WorkspaceAccess is the scope every member has unless restricted.
func WorkspaceAccess() *MemberAccess {
	return &MemberAccess{Scope: AccessScopeWorkspace, FolderIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{}, EmailAccountIDs: []uuid.UUID{}}
}

// ResourceScope is what a restricted member may reach, resolved for one request.
type ResourceScope struct {
	// Campaigns is every campaign granted directly or held by a granted folder.
	Campaigns []uuid.UUID
	// Folders are the granted folders and every folder holding a campaign in scope.
	Folders   []uuid.UUID
	Mailboxes []uuid.UUID
	// MailboxTags are the tags on mailboxes in scope.
	MailboxTags []uuid.UUID
}

// HasCampaign reports whether id is in scope.
func (s *ResourceScope) HasCampaign(id uuid.UUID) bool { return containsUUID(s.Campaigns, id) }

// HasMailbox reports whether id is in scope.
func (s *ResourceScope) HasMailbox(id uuid.UUID) bool { return containsUUID(s.Mailboxes, id) }

// HasFolder reports whether id is in scope.
func (s *ResourceScope) HasFolder(id uuid.UUID) bool { return containsUUID(s.Folders, id) }

func containsUUID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// NoneMatch stands in for an empty restriction where an empty list would mean unrestricted; uuid.Nil names no row.
func NoneMatch() []uuid.UUID {
	return []uuid.UUID{uuid.Nil}
}

// SetMemberAccessRequest is PUT /organization/members/:id/access.
type SetMemberAccessRequest struct {
	Scope           AccessScope `json:"scope" binding:"required"`
	FolderIDs       []uuid.UUID `json:"campaign_folder_ids"`
	CampaignIDs     []uuid.UUID `json:"campaign_ids"`
	EmailAccountIDs []uuid.UUID `json:"email_account_ids"`
}

// Access is the request as a MemberAccess.
func (r *SetMemberAccessRequest) Access() *MemberAccess {
	return &MemberAccess{Scope: r.Scope, FolderIDs: r.FolderIDs, CampaignIDs: r.CampaignIDs, EmailAccountIDs: r.EmailAccountIDs}
}

// SuggestedSender is a mailbox sending for the campaigns an administrator is about to grant.
type SuggestedSender struct {
	ID          uuid.UUID   `json:"id"`
	Email       string      `json:"email"`
	Name        string      `json:"name"`
	CampaignIDs []uuid.UUID `json:"campaign_ids"`
}
