package organization

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// errInvalidAccessResource refuses a grant naming something outside the workspace.
var errInvalidAccessResource = errx.NewWithIdentifier(errx.BadRequest, "invalid_access_resource",
	"One or more of the selected campaign folders, campaigns or mailboxes are not in this workspace.")

// validateAccess normalizes a and checks every grant names a resource of the organization.
func (s *organizationService) validateAccess(ctx context.Context, orgID uuid.UUID, a *models.MemberAccess) *errx.Error {
	if !a.Scope.Valid() && a.Scope != "" {
		return errx.NewWithIdentifier(errx.BadRequest, "invalid_access_scope", `access scope must be "workspace" or "restricted"`)
	}
	a.Normalize()
	if !a.Restricted() {
		return nil
	}
	for _, list := range [][]uuid.UUID{a.FolderIDs, a.CampaignIDs, a.EmailAccountIDs} {
		if len(list) > models.MaxAccessGrants {
			return errx.NewWithIdentifier(errx.BadRequest, "too_many_access_grants",
				fmt.Sprintf("a member can be granted at most %d campaign folders, %d campaigns and %d mailboxes", models.MaxAccessGrants, models.MaxAccessGrants, models.MaxAccessGrants))
		}
	}
	folders, campaigns, mailboxes, err := s.orgRepo.CountOwnedAccessResources(ctx, orgID, a)
	if err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to check access grants")
	}
	if folders != len(a.FolderIDs) || campaigns != len(a.CampaignIDs) || mailboxes != len(a.EmailAccountIDs) {
		return errInvalidAccessResource
	}
	return nil
}

// memberRolePermissions is the OR of a member's assigned roles, before any scope.
func (s *organizationService) memberRolePermissions(ctx context.Context, orgID, userID uuid.UUID) (models.OrganizationPermission, *errx.Error) {
	roles, err := s.orgRepo.GetMemberRoles(ctx, orgID, userID)
	if err != nil {
		errs.CaptureException(err)
		return 0, errx.New(errx.Internal, "failed to load roles")
	}
	var perms models.OrganizationPermission
	for _, r := range roles {
		role, err := s.orgRepo.GetRoleByID(ctx, orgID, r.ID)
		if err != nil {
			errs.CaptureException(err)
			return 0, errx.New(errx.Internal, "failed to load role")
		}
		if role != nil {
			perms |= role.Permissions
		}
	}
	return perms, nil
}

// GetMemberAccess is one member's access scope and grants.
func (s *organizationService) GetMemberAccess(ctx context.Context, orgID, userID uuid.UUID) (*models.MemberAccess, *errx.Error) {
	a, err := s.orgRepo.GetMemberAccess(ctx, orgID, userID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load member access")
	}
	if a == nil {
		return nil, errx.New(errx.NotFound, "member not found")
	}
	return a, nil
}

// SetMemberAccess replaces a member's access scope and grants.
func (s *organizationService) SetMemberAccess(ctx context.Context, orgID, actorID, memberUserID uuid.UUID, a *models.MemberAccess) (*models.OrganizationMember, *errx.Error) {
	member, err := s.orgRepo.GetMember(ctx, orgID, memberUserID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get member")
	}
	if member == nil {
		return nil, errx.New(errx.NotFound, "member not found")
	}
	if member.IsOwner() {
		return nil, errx.NewWithIdentifier(errx.Forbidden, "owner_access_unrestricted", "the workspace owner always has access to the entire workspace")
	}
	if actorID == memberUserID {
		return nil, errx.New(errx.Forbidden, "you cannot change your own access")
	}
	if a.Scope == "" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_access_scope", `access scope must be "workspace" or "restricted"`)
	}
	if xerr := s.validateAccess(ctx, orgID, a); xerr != nil {
		return nil, xerr
	}
	// Either direction decides what the member's roles reach, so the actor must hold all of it.
	rolePerms, xerr := s.memberRolePermissions(ctx, orgID, memberUserID)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := s.validateActorHoldsPermissions(ctx, orgID, actorID, rolePerms|member.Permissions); xerr != nil {
		return nil, xerr
	}
	if err := s.orgRepo.SetMemberAccess(ctx, orgID, memberUserID, a, actorID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.New(errx.NotFound, "member not found")
		}
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to update member access")
	}
	updated, err := s.orgRepo.GetMember(ctx, orgID, memberUserID)
	if err != nil || updated == nil {
		if err != nil {
			errs.CaptureException(err)
		}
		return nil, errx.New(errx.Internal, "failed to load member")
	}
	updated.Roles, _ = s.orgRepo.GetMemberRoles(ctx, orgID, memberUserID)
	if updated.Access, err = s.orgRepo.GetMemberAccess(ctx, orgID, memberUserID); err != nil {
		errs.CaptureException(err)
	}
	return updated, nil
}

// ResolveMemberScope is what a restricted member reaches for one request.
func (s *organizationService) ResolveMemberScope(ctx context.Context, orgID, userID uuid.UUID) (*models.ResourceScope, *errx.Error) {
	scope, err := s.orgRepo.ResolveMemberScope(ctx, orgID, userID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to resolve member access")
	}
	return scope, nil
}

// SuggestCampaignSenders lists the mailboxes the selected campaigns and folders send from.
func (s *organizationService) SuggestCampaignSenders(ctx context.Context, orgID uuid.UUID, campaignIDs, folderIDs []uuid.UUID) ([]models.SuggestedSender, *errx.Error) {
	out, err := s.orgRepo.SuggestCampaignSenders(ctx, orgID, campaignIDs, folderIDs)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to suggest senders")
	}
	return out, nil
}

// attachMemberAccess fills each member's grants for the roster.
func (s *organizationService) attachMemberAccess(ctx context.Context, orgID uuid.UUID, members []models.OrganizationMember) {
	byUser, err := s.orgRepo.ListMemberAccess(ctx, orgID)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	for i := range members {
		if a, ok := byUser[members[i].UserID]; ok {
			members[i].Access = a
		}
	}
}
