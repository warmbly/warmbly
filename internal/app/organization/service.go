package organization

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// InvitationTTL is the default invitation expiration time
	InvitationTTL = 7 * 24 * time.Hour // 7 days

	// InvitationTokenLength is the length of invitation tokens
	InvitationTokenLength = 32
)

// OrganizationService defines the interface for organization management
type OrganizationService interface {
	// CRUD
	Create(ctx context.Context, userID uuid.UUID, name string) (*models.Organization, *errx.Error)
	Get(ctx context.Context, orgID uuid.UUID) (*models.Organization, *errx.Error)
	GetBySlug(ctx context.Context, slug string) (*models.Organization, *errx.Error)
	Update(ctx context.Context, orgID uuid.UUID, req *models.UpdateOrganizationRequest) (*models.Organization, *errx.Error)
	Delete(ctx context.Context, orgID uuid.UUID) *errx.Error

	// User's organizations
	GetUserOrganizations(ctx context.Context, userID uuid.UUID) ([]models.OrganizationMember, *errx.Error)
	GetUserDefaultOrganization(ctx context.Context, userID uuid.UUID) (*models.Organization, *errx.Error)

	// Member management
	GetMembers(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationMember, *errx.Error)
	GetMembership(ctx context.Context, orgID, userID uuid.UUID) (*models.OrganizationMember, *errx.Error)
	InviteMember(ctx context.Context, orgID uuid.UUID, inviterID uuid.UUID, req *models.InviteMemberRequest) (*models.OrganizationInvitation, *errx.Error)
	AcceptInvitation(ctx context.Context, token string, userID uuid.UUID, email string) (*models.OrganizationMember, *errx.Error)
	UpdateMemberRole(ctx context.Context, orgID, memberUserID uuid.UUID, req *models.UpdateMemberRequest) (*models.OrganizationMember, *errx.Error)
	RemoveMember(ctx context.Context, orgID, memberUserID uuid.UUID) *errx.Error

	// Invitations
	GetPendingInvitations(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationInvitation, *errx.Error)
	GetUserPendingInvitations(ctx context.Context, email string) ([]models.OrganizationInvitation, *errx.Error)
	CancelInvitation(ctx context.Context, invitationID uuid.UUID) *errx.Error

	// Ownership transfer
	TransferOwnership(ctx context.Context, orgID, newOwnerUserID uuid.UUID) *errx.Error

	// Permission checks
	HasPermission(ctx context.Context, orgID, userID uuid.UUID, perm models.OrganizationPermission) (bool, *errx.Error)
	RequirePermission(ctx context.Context, orgID, userID uuid.UUID, perm models.OrganizationPermission) *errx.Error

	// Limit checks
	CanAddMember(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
	CanAddCampaign(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
	CanAddEmailAccount(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
	GetCampaignCounts(ctx context.Context, orgID uuid.UUID) (total int, active int, err *errx.Error)
	GetOrganizationLimits(ctx context.Context, orgID uuid.UUID) (*models.OrganizationLimits, *errx.Error)
	GetOrganizationCounts(ctx context.Context, orgID uuid.UUID) (*models.OrganizationCounts, *errx.Error)

	// Enterprise inquiries
	CreateEnterpriseInquiry(ctx context.Context, inquiry *models.EnterpriseInquiry) (*models.EnterpriseInquiry, *errx.Error)

	// Admin permissions (for admin middleware)
	GetUserAdminPermissions(ctx context.Context, userID uuid.UUID) (uint32, error)

	// Admin: read-only org listing for the admin panel. Detail composes
	// the list-shape row with counts, plan defaults, the override row,
	// and the effective limits the runtime actually enforces.
	SearchOrganizationsForAdmin(ctx context.Context, search *models.AdminOrgSearch) (*models.AdminOrgsResult, *errx.Error)
	GetOrganizationAdminDetail(ctx context.Context, orgID uuid.UUID) (*models.AdminOrgDetail, *errx.Error)
	GetOrganizationMembersForAdmin(ctx context.Context, orgID uuid.UUID) ([]models.AdminOrgMember, *errx.Error)

	// Admin: per-org limit overrides. Read returns nil if the org has
	// never been touched. Set is an upsert that stamps granted_by /
	// granted_at on every write.
	GetLimitOverrides(ctx context.Context, orgID uuid.UUID) (*models.OrganizationLimitOverrides, *errx.Error)
	SetLimitOverrides(ctx context.Context, orgID uuid.UUID, req *models.UpdateOrgOverridesRequest, grantedBy uuid.UUID) (*models.OrganizationLimitOverrides, *errx.Error)

	// Effective limits: plan defaults overlaid with overrides. This is
	// what the in-app limit checks (CanAddMember / Campaign / EmailAccount)
	// compare counts against.
	GetEffectiveLimits(ctx context.Context, orgID uuid.UUID) (*models.OrganizationLimits, *errx.Error)

	// WebhookDispatchLimit returns the org's per-minute cap on webhook +
	// integration fan-out, derived from the org's effective plan limits. Used by
	// the webhook dispatch throttle. Never errors — falls back to the generous
	// base cap so dispatch is never blocked by a limits-lookup failure.
	WebhookDispatchLimit(ctx context.Context, orgID uuid.UUID) int

	// Limit-increase request workflow. Users self-serve via
	// SubmitLimitIncreaseRequest from the dashboard; admins drain the
	// queue with the AdminListLimitRequests / ApproveLimitRequest /
	// RejectLimitRequest path. Approving rewrites the override row via
	// SetLimitOverrides so the audit story stays unified.
	SubmitLimitIncreaseRequest(ctx context.Context, orgID, submitterID uuid.UUID, req *models.CreateLimitIncreaseRequest) (*models.LimitIncreaseRequest, *errx.Error)
	ListLimitRequestsForOrg(ctx context.Context, orgID uuid.UUID) ([]models.LimitIncreaseRequest, *errx.Error)
	CancelLimitRequest(ctx context.Context, id, userID uuid.UUID) *errx.Error
	AdminListLimitRequests(ctx context.Context, search *models.AdminLimitRequestSearch) (*models.AdminLimitRequestsResult, *errx.Error)
	ApproveLimitRequest(ctx context.Context, id, reviewerID uuid.UUID, notes string) (*models.LimitIncreaseRequest, *errx.Error)
	RejectLimitRequest(ctx context.Context, id, reviewerID uuid.UUID, notes string) (*models.LimitIncreaseRequest, *errx.Error)
}

type organizationService struct {
	orgRepo  repository.OrganizationRepository
	subRepo  repository.SubscriptionRepository
	userRepo repository.UserRepository
	throttle dailythrottle.Service
}

// NewService creates a new organization service
func NewService(
	orgRepo repository.OrganizationRepository,
	subRepo repository.SubscriptionRepository,
	userRepo repository.UserRepository,
	throttle dailythrottle.Service,
) OrganizationService {
	return &organizationService{
		orgRepo:  orgRepo,
		subRepo:  subRepo,
		userRepo: userRepo,
		throttle: throttle,
	}
}

// Create creates a new organization and adds the user as owner
func (s *organizationService) Create(ctx context.Context, userID uuid.UUID, name string) (*models.Organization, *errx.Error) {
	// Ban-scope enforcement (migration 000045). Block new workspace
	// creation when the admin's set the BanScopeOrgCreate bit, even
	// if the user can otherwise log in.
	if scope, scopeErr := s.userRepo.GetBanState(ctx, userID); scopeErr == nil {
		if models.BanScope(scope).Has(models.BanScopeOrgCreate) {
			return nil, errx.New(errx.Forbidden, "this account cannot create new workspaces")
		}
	}

	// Daily creation throttle — caps "new workspaces per owner per
	// day" so a script can't spawn 100 orgs from one user account.
	// Scope is the owner uuid (not the org, which doesn't exist yet).
	if s.throttle != nil {
		if xerr := s.throttle.CheckAndIncrement(ctx, userID, dailythrottle.ResourceOrg, config.DailyThrottleNewOrgs); xerr != nil {
			return nil, xerr
		}
	}

	// Check organization limit
	user, userErr := s.userRepo.GetUser(ctx, userID)
	if userErr != nil {
		sentry.CaptureException(userErr)
		return nil, errx.New(errx.Internal, "failed to get user")
	}
	if user == nil {
		return nil, errx.New(errx.NotFound, "user not found")
	}

	ownedCount, countErr := s.orgRepo.GetUserOwnedOrganizationCount(ctx, userID)
	if countErr != nil {
		sentry.CaptureException(countErr)
		return nil, errx.New(errx.Internal, "failed to get organization count")
	}
	if ownedCount >= user.MaxOrganizations {
		return nil, errx.New(errx.Forbidden, "maximum organization limit reached")
	}

	org := &models.Organization{
		ID:          uuid.New(),
		Name:        name,
		OwnerUserID: userID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Generate slug from name
	slug := generateSlug(name)
	org.Slug = &slug

	if err := s.orgRepo.Create(ctx, org); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to create organization")
	}

	// Add creator as owner member
	now := time.Now()
	member := &models.OrganizationMember{
		ID:             uuid.New(),
		OrganizationID: org.ID,
		UserID:         userID,
		Role:           string(models.RoleOwner),
		Permissions:    models.RolePermissions[models.RoleOwner],
		InvitedAt:      now,
		AcceptedAt:     &now,
	}

	if err := s.orgRepo.AddMember(ctx, member); err != nil {
		sentry.CaptureException(err)
		// Rollback org creation
		_ = s.orgRepo.Delete(ctx, org.ID)
		return nil, errx.New(errx.Internal, "failed to add owner member")
	}

	return org, nil
}

// Get retrieves an organization by ID
func (s *organizationService) Get(ctx context.Context, orgID uuid.UUID) (*models.Organization, *errx.Error) {
	org, err := s.orgRepo.GetByID(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get organization")
	}
	if org == nil {
		return nil, errx.ErrNotFound
	}
	return org, nil
}

// GetBySlug retrieves an organization by slug
func (s *organizationService) GetBySlug(ctx context.Context, slug string) (*models.Organization, *errx.Error) {
	org, err := s.orgRepo.GetBySlug(ctx, slug)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get organization")
	}
	if org == nil {
		return nil, errx.ErrNotFound
	}
	return org, nil
}

// Update updates an organization
func (s *organizationService) Update(ctx context.Context, orgID uuid.UUID, req *models.UpdateOrganizationRequest) (*models.Organization, *errx.Error) {
	org, err := s.orgRepo.GetByID(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get organization")
	}
	if org == nil {
		return nil, errx.ErrNotFound
	}

	if req.Name != nil {
		org.Name = *req.Name
	}
	if req.Slug != nil {
		// Validate slug uniqueness
		existing, _ := s.orgRepo.GetBySlug(ctx, *req.Slug)
		if existing != nil && existing.ID != orgID {
			return nil, errx.New(errx.Conflict, "slug already in use")
		}
		org.Slug = req.Slug
	}

	org.UpdatedAt = time.Now()

	if err := s.orgRepo.Update(ctx, org); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to update organization")
	}

	return org, nil
}

// Delete deletes an organization
func (s *organizationService) Delete(ctx context.Context, orgID uuid.UUID) *errx.Error {
	if err := s.orgRepo.Delete(ctx, orgID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to delete organization")
	}
	return nil
}

// GetUserOrganizations retrieves all organizations a user is a member of
func (s *organizationService) GetUserOrganizations(ctx context.Context, userID uuid.UUID) ([]models.OrganizationMember, *errx.Error) {
	members, err := s.orgRepo.GetUserOrganizations(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get user organizations")
	}
	return members, nil
}

// GetUserDefaultOrganization retrieves the user's default (first owned) organization
func (s *organizationService) GetUserDefaultOrganization(ctx context.Context, userID uuid.UUID) (*models.Organization, *errx.Error) {
	org, err := s.orgRepo.GetUserDefaultOrganization(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get default organization")
	}
	return org, nil
}

// GetMembers retrieves all members of an organization
func (s *organizationService) GetMembers(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationMember, *errx.Error) {
	members, err := s.orgRepo.GetMembers(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get members")
	}
	return members, nil
}

// GetMembership retrieves a user's membership in an organization
func (s *organizationService) GetMembership(ctx context.Context, orgID, userID uuid.UUID) (*models.OrganizationMember, *errx.Error) {
	member, err := s.orgRepo.GetMember(ctx, orgID, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get membership")
	}
	return member, nil
}

// InviteMember invites a new member to the organization
func (s *organizationService) InviteMember(ctx context.Context, orgID uuid.UUID, inviterID uuid.UUID, req *models.InviteMemberRequest) (*models.OrganizationInvitation, *errx.Error) {
	// Check member limit
	canAdd, err := s.CanAddMember(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if !canAdd {
		return nil, errx.New(errx.Forbidden, "team member limit reached")
	}

	// Check if user is already a member
	// First, we need to check if there's already a user with this email
	// For now, we'll just create the invitation

	// Determine role and permissions
	role := string(models.RoleViewer)
	if req.Role != "" && models.IsValidRole(req.Role) {
		role = req.Role
	}

	var permissions models.OrganizationPermission
	if req.Permissions != nil {
		permissions = models.OrganizationPermission(*req.Permissions)
	} else {
		permissions = models.GetRolePermissions(models.Role(role))
	}

	// Generate invitation token
	token, xerr := generateInvitationToken()
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.New(errx.Internal, "failed to generate invitation token")
	}

	inv := &models.OrganizationInvitation{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Email:          strings.ToLower(req.Email),
		Role:           role,
		Permissions:    permissions,
		InvitedBy:      inviterID,
		Token:          token,
		ExpiresAt:      time.Now().Add(InvitationTTL),
		CreatedAt:      time.Now(),
	}

	if err := s.orgRepo.CreateInvitation(ctx, inv); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to create invitation")
	}

	return inv, nil
}

// AcceptInvitation accepts an invitation and adds the user as a member
func (s *organizationService) AcceptInvitation(ctx context.Context, token string, userID uuid.UUID, email string) (*models.OrganizationMember, *errx.Error) {
	inv, err := s.orgRepo.GetInvitationByToken(ctx, token)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get invitation")
	}
	if inv == nil {
		return nil, errx.New(errx.NotFound, "invitation not found")
	}

	// Verify email matches
	if strings.ToLower(email) != strings.ToLower(inv.Email) {
		return nil, errx.New(errx.Forbidden, "email does not match invitation")
	}

	// Check if invitation is expired
	if inv.IsExpired() {
		// Delete expired invitation
		_ = s.orgRepo.DeleteInvitation(ctx, inv.ID)
		return nil, errx.New(errx.BadRequest, "invitation has expired")
	}

	// Check if already a member
	existing, _ := s.orgRepo.GetMember(ctx, inv.OrganizationID, userID)
	if existing != nil {
		_ = s.orgRepo.DeleteInvitation(ctx, inv.ID)
		return existing, nil
	}

	// Add member
	now := time.Now()
	member := &models.OrganizationMember{
		ID:             uuid.New(),
		OrganizationID: inv.OrganizationID,
		UserID:         userID,
		Role:           inv.Role,
		Permissions:    inv.Permissions,
		InvitedBy:      &inv.InvitedBy,
		InvitedAt:      inv.CreatedAt,
		AcceptedAt:     &now,
	}

	if err := s.orgRepo.AddMember(ctx, member); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to add member")
	}

	// Delete the invitation
	_ = s.orgRepo.DeleteInvitation(ctx, inv.ID)

	return member, nil
}

// UpdateMemberRole updates a member's role and permissions
func (s *organizationService) UpdateMemberRole(ctx context.Context, orgID, memberUserID uuid.UUID, req *models.UpdateMemberRequest) (*models.OrganizationMember, *errx.Error) {
	member, err := s.orgRepo.GetMember(ctx, orgID, memberUserID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get member")
	}
	if member == nil {
		return nil, errx.New(errx.NotFound, "member not found")
	}

	// Cannot change owner's role
	if member.Role == string(models.RoleOwner) {
		return nil, errx.New(errx.Forbidden, "cannot modify owner role")
	}

	if req.Role != nil {
		if !models.IsValidRole(*req.Role) {
			return nil, errx.New(errx.BadRequest, "invalid role")
		}
		// Cannot promote to owner
		if *req.Role == string(models.RoleOwner) {
			return nil, errx.New(errx.Forbidden, "cannot promote to owner, use transfer ownership")
		}
		member.Role = *req.Role
		// Update permissions to match new role unless custom permissions provided
		if req.Permissions == nil {
			member.Permissions = models.GetRolePermissions(models.Role(*req.Role))
		}
	}

	if req.Permissions != nil {
		member.Permissions = models.OrganizationPermission(*req.Permissions)
	}

	if err := s.orgRepo.UpdateMember(ctx, member); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to update member")
	}

	return member, nil
}

// RemoveMember removes a member from the organization
func (s *organizationService) RemoveMember(ctx context.Context, orgID, memberUserID uuid.UUID) *errx.Error {
	member, err := s.orgRepo.GetMember(ctx, orgID, memberUserID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to get member")
	}
	if member == nil {
		return errx.New(errx.NotFound, "member not found")
	}

	// Cannot remove owner
	if member.Role == string(models.RoleOwner) {
		return errx.New(errx.Forbidden, "cannot remove organization owner")
	}

	if err := s.orgRepo.RemoveMember(ctx, orgID, memberUserID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to remove member")
	}

	return nil
}

// GetPendingInvitations retrieves all pending invitations for an organization
func (s *organizationService) GetPendingInvitations(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationInvitation, *errx.Error) {
	invitations, err := s.orgRepo.GetPendingInvitations(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get invitations")
	}
	return invitations, nil
}

// GetUserPendingInvitations retrieves all pending invitations for a user
func (s *organizationService) GetUserPendingInvitations(ctx context.Context, email string) ([]models.OrganizationInvitation, *errx.Error) {
	invitations, err := s.orgRepo.GetUserPendingInvitations(ctx, email)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get invitations")
	}
	return invitations, nil
}

// CancelInvitation cancels a pending invitation
func (s *organizationService) CancelInvitation(ctx context.Context, invitationID uuid.UUID) *errx.Error {
	if err := s.orgRepo.DeleteInvitation(ctx, invitationID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to cancel invitation")
	}
	return nil
}

// TransferOwnership transfers organization ownership
func (s *organizationService) TransferOwnership(ctx context.Context, orgID, newOwnerUserID uuid.UUID) *errx.Error {
	// Verify new owner is a member
	member, err := s.orgRepo.GetMember(ctx, orgID, newOwnerUserID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to verify membership")
	}
	if member == nil {
		return errx.New(errx.BadRequest, "new owner must be a member of the organization")
	}

	if err := s.orgRepo.TransferOwnership(ctx, orgID, newOwnerUserID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to transfer ownership")
	}

	return nil
}

// HasPermission checks if a user has a specific permission
func (s *organizationService) HasPermission(ctx context.Context, orgID, userID uuid.UUID, perm models.OrganizationPermission) (bool, *errx.Error) {
	member, err := s.orgRepo.GetMember(ctx, orgID, userID)
	if err != nil {
		sentry.CaptureException(err)
		return false, errx.New(errx.Internal, "failed to check permission")
	}
	if member == nil {
		return false, nil
	}
	return member.HasPermission(perm), nil
}

// RequirePermission checks permission and returns error if not granted
func (s *organizationService) RequirePermission(ctx context.Context, orgID, userID uuid.UUID, perm models.OrganizationPermission) *errx.Error {
	has, err := s.HasPermission(ctx, orgID, userID, perm)
	if err != nil {
		return err
	}
	if !has {
		return errx.ErrForbidden
	}
	return nil
}

// CanAddMember checks if the organization can add more members based on plan limits
func (s *organizationService) CanAddMember(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	limits, err := s.GetEffectiveLimits(ctx, orgID)
	if err != nil {
		return false, err
	}

	// No limit set = unlimited
	if limits == nil || limits.MaxTeamMembers == nil {
		return true, nil
	}

	count, xerr := s.orgRepo.GetMemberCount(ctx, orgID)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return false, errx.New(errx.Internal, "failed to get member count")
	}

	return count < *limits.MaxTeamMembers, nil
}

// CanAddCampaign checks if the organization can add more campaigns based on plan limits
func (s *organizationService) CanAddCampaign(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	limits, err := s.GetEffectiveLimits(ctx, orgID)
	if err != nil {
		return false, err
	}

	total, active, xerr := s.GetCampaignCounts(ctx, orgID)
	if xerr != nil {
		return false, xerr
	}

	// Check total campaign limit
	if limits != nil && limits.MaxCampaigns != nil && total >= *limits.MaxCampaigns {
		return false, nil
	}

	// Check active campaign limit
	if limits != nil && limits.MaxActiveCampaigns != nil && active >= *limits.MaxActiveCampaigns {
		return false, nil
	}

	return true, nil
}

// CanAddEmailAccount checks if the organization can add more email accounts based on plan limits
func (s *organizationService) CanAddEmailAccount(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	limits, err := s.GetEffectiveLimits(ctx, orgID)
	if err != nil {
		return false, err
	}

	// No limit set = unlimited
	if limits == nil || limits.MaxEmailAccounts == nil {
		return true, nil
	}

	count, xerr := s.orgRepo.GetEmailAccountCount(ctx, orgID)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return false, errx.New(errx.Internal, "failed to get email account count")
	}

	return count < *limits.MaxEmailAccounts, nil
}

// GetCampaignCounts returns total and active campaign counts
func (s *organizationService) GetCampaignCounts(ctx context.Context, orgID uuid.UUID) (total int, active int, err *errx.Error) {
	t, a, xerr := s.orgRepo.GetCampaignCounts(ctx, orgID)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return 0, 0, errx.New(errx.Internal, "failed to get campaign counts")
	}
	return t, a, nil
}

// GetOrganizationLimits retrieves the organization's plan limits
func (s *organizationService) GetOrganizationLimits(ctx context.Context, orgID uuid.UUID) (*models.OrganizationLimits, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get subscription")
	}
	if sub == nil || sub.Plan == nil {
		return nil, nil
	}

	return &models.OrganizationLimits{
		MaxCampaigns:       sub.Plan.MaxCampaigns,
		MaxActiveCampaigns: sub.Plan.MaxActiveCampaigns,
		MaxTeamMembers:     sub.Plan.MaxTeamMembers,
		MaxEmailAccounts:   sub.Plan.MaxEmailAccounts,
		DailyCampaignLimit: sub.Plan.DailyCampaignLimit,
	}, nil
}

// GetOrganizationCounts retrieves the organization's resource counts
func (s *organizationService) GetOrganizationCounts(ctx context.Context, orgID uuid.UUID) (*models.OrganizationCounts, *errx.Error) {
	total, active, err := s.orgRepo.GetCampaignCounts(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get campaign counts")
	}

	members, err := s.orgRepo.GetMemberCount(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get member count")
	}

	emails, err := s.orgRepo.GetEmailAccountCount(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get email account count")
	}

	contacts, err := s.orgRepo.GetContactCount(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get contact count")
	}

	sentToday, _ := s.orgRepo.GetEmailsSentTodayCount(ctx, orgID)

	return &models.OrganizationCounts{
		TotalCampaigns:  total,
		ActiveCampaigns: active,
		TotalMembers:    members,
		EmailAccounts:   emails,
		TotalContacts:   contacts,
		EmailsSentToday: sentToday,
	}, nil
}

// CreateEnterpriseInquiry creates an enterprise inquiry
func (s *organizationService) CreateEnterpriseInquiry(ctx context.Context, inquiry *models.EnterpriseInquiry) (*models.EnterpriseInquiry, *errx.Error) {
	inquiry.ID = uuid.New()
	inquiry.Status = string(models.EnterpriseInquiryStatusPending)
	inquiry.CreatedAt = time.Now()

	if err := s.orgRepo.CreateEnterpriseInquiry(ctx, inquiry); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to create enterprise inquiry")
	}

	return inquiry, nil
}

// Helper functions

func generateSlug(name string) string {
	// Simple slug generation - lowercase, replace spaces with dashes
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	// Remove non-alphanumeric characters except dashes
	var result strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	// Add random suffix to ensure uniqueness
	suffix := make([]byte, 4)
	rand.Read(suffix)
	return result.String() + "-" + hex.EncodeToString(suffix)
}

func generateInvitationToken() (string, error) {
	bytes := make([]byte, InvitationTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GetUserAdminPermissions retrieves the admin permissions for a user
func (s *organizationService) GetUserAdminPermissions(ctx context.Context, userID uuid.UUID) (uint32, error) {
	return s.orgRepo.GetUserAdminPermissions(ctx, userID)
}

// SearchOrganizationsForAdmin returns the paginated admin org listing.
func (s *organizationService) SearchOrganizationsForAdmin(ctx context.Context, search *models.AdminOrgSearch) (*models.AdminOrgsResult, *errx.Error) {
	result, err := s.orgRepo.SearchOrganizationsForAdmin(ctx, search)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to search organizations")
	}
	return result, nil
}

// GetOrganizationAdminDetail returns the per-org admin payload — the
// list-shape row plus a counts snapshot and plan/subscription state.
// Limits come from the org's plan via GetOrganizationLimits so admin
// callers see the same numbers the in-app limit checks enforce.
func (s *organizationService) GetOrganizationAdminDetail(ctx context.Context, orgID uuid.UUID) (*models.AdminOrgDetail, *errx.Error) {
	detail, err := s.orgRepo.GetOrganizationAdminDetail(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load organization")
	}
	if detail == nil {
		return nil, errx.New(errx.NotFound, "organization not found")
	}

	if limits, xerr := s.GetOrganizationLimits(ctx, orgID); xerr == nil {
		detail.Limits = limits
	}
	if overrides, xerr := s.GetLimitOverrides(ctx, orgID); xerr == nil {
		detail.Overrides = overrides
	}
	if effective, xerr := s.GetEffectiveLimits(ctx, orgID); xerr == nil {
		detail.EffectiveLimits = effective
	}
	if counts, xerr := s.GetOrganizationCounts(ctx, orgID); xerr == nil {
		detail.Counts = counts
	}

	return detail, nil
}

// GetOrganizationMembersForAdmin returns the members of an org with
// joined user info for the admin panel.
func (s *organizationService) GetOrganizationMembersForAdmin(ctx context.Context, orgID uuid.UUID) ([]models.AdminOrgMember, *errx.Error) {
	members, err := s.orgRepo.GetOrganizationMembersForAdmin(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load members")
	}
	return members, nil
}

// GetLimitOverrides returns the override row for an org, or nil if no
// admin has touched it yet.
func (s *organizationService) GetLimitOverrides(ctx context.Context, orgID uuid.UUID) (*models.OrganizationLimitOverrides, *errx.Error) {
	o, err := s.orgRepo.GetOrganizationLimitOverrides(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load limit overrides")
	}
	return o, nil
}

// SetLimitOverrides applies a partial update to the override row. The
// caller is responsible for the audit log write — the service stays
// audit-agnostic so it can be reused outside admin handlers later
// (e.g. limit-request approval in slice 3).
func (s *organizationService) SetLimitOverrides(ctx context.Context, orgID uuid.UUID, req *models.UpdateOrgOverridesRequest, grantedBy uuid.UUID) (*models.OrganizationLimitOverrides, *errx.Error) {
	o, err := s.orgRepo.UpsertOrganizationLimitOverrides(ctx, orgID, req, grantedBy)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to write limit overrides")
	}
	return o, nil
}

// GetEffectiveLimits returns the limits the runtime actually enforces.
// Resolution per field:
//
//  1. override > 0  → use override
//  2. plan != nil   → use plan column
//  3. otherwise     → fall back to the product-level hard cap
//
// Never returns nil values: even an "unlimited" plan is bounded by the
// product hard caps in config/constants.go. Admins can raise individual
// caps per-org by writing an override.
func (s *organizationService) GetEffectiveLimits(ctx context.Context, orgID uuid.UUID) (*models.OrganizationLimits, *errx.Error) {
	plan, err := s.GetOrganizationLimits(ctx, orgID)
	if err != nil {
		return nil, err
	}
	override, err := s.GetLimitOverrides(ctx, orgID)
	if err != nil {
		return nil, err
	}

	resolve := func(overrideVal int, planVal *int, hardCap int) *int {
		if overrideVal > 0 {
			v := overrideVal
			return &v
		}
		if planVal != nil {
			return planVal
		}
		v := hardCap
		return &v
	}

	var ovMaxCampaigns, ovMaxActive, ovMaxMembers, ovMaxEmails, ovMaxContacts, ovDaily int
	if override != nil {
		ovMaxCampaigns = override.MaxCampaigns
		ovMaxActive = override.MaxActiveCampaigns
		ovMaxMembers = override.MaxTeamMembers
		ovMaxEmails = override.MaxEmailAccounts
		ovMaxContacts = override.MaxContacts
		ovDaily = override.DailyCampaignLimit
	}

	var planLimits models.OrganizationLimits
	if plan != nil {
		planLimits = *plan
	}

	return &models.OrganizationLimits{
		MaxCampaigns:       resolve(ovMaxCampaigns, planLimits.MaxCampaigns, config.HardCapCampaignsTotal),
		MaxActiveCampaigns: resolve(ovMaxActive, planLimits.MaxActiveCampaigns, config.HardCapCampaignsActive),
		MaxTeamMembers:     resolve(ovMaxMembers, planLimits.MaxTeamMembers, config.HardCapTeamMembers),
		MaxEmailAccounts:   resolve(ovMaxEmails, planLimits.MaxEmailAccounts, config.HardCapMailboxes),
		MaxContacts:        resolve(ovMaxContacts, planLimits.MaxContacts, config.HardCapContacts),
		DailyCampaignLimit: resolve(ovDaily, planLimits.DailyCampaignLimit, config.HardCapDailyCampaignSends),
	}, nil
}

// WebhookDispatchLimit derives the org's per-minute webhook/integration fan-out
// cap from its effective plan limits. Plan-based: it scales with the org's
// resolved mailbox allowance (since webhook volume tracks sending activity),
// floored at a generous baseline so even free/no-plan orgs never trip the
// throttle under normal use, and ceilinged so an "unlimited" plan stays bounded.
// Fail-open generous: any error yields the base cap rather than blocking.
func (s *organizationService) WebhookDispatchLimit(ctx context.Context, orgID uuid.UUID) int {
	limit := config.WebhookDispatchBasePerMinute

	eff, err := s.GetEffectiveLimits(ctx, orgID)
	if err != nil || eff == nil {
		return limit
	}
	if eff.MaxEmailAccounts != nil {
		if scaled := *eff.MaxEmailAccounts * config.WebhookDispatchPerMailboxPerMinute; scaled > limit {
			limit = scaled
		}
	}
	if limit > config.WebhookDispatchMaxPerMinute {
		limit = config.WebhookDispatchMaxPerMinute
	}
	return limit
}

// limitFieldUpdater maps a request's `field` string onto the override
// column it should write. Keeping this map adjacent to the request flow
// makes it obvious what's user-requestable; adding a new field is two
// lines here plus the override model.
var limitFieldUpdater = map[string]func(req *models.UpdateOrgOverridesRequest, v int){
	"max_campaigns":        func(r *models.UpdateOrgOverridesRequest, v int) { r.MaxCampaigns = &v },
	"max_active_campaigns": func(r *models.UpdateOrgOverridesRequest, v int) { r.MaxActiveCampaigns = &v },
	"max_team_members":     func(r *models.UpdateOrgOverridesRequest, v int) { r.MaxTeamMembers = &v },
	"max_email_accounts":   func(r *models.UpdateOrgOverridesRequest, v int) { r.MaxEmailAccounts = &v },
	"max_contacts":         func(r *models.UpdateOrgOverridesRequest, v int) { r.MaxContacts = &v },
	"daily_campaign_limit": func(r *models.UpdateOrgOverridesRequest, v int) { r.DailyCampaignLimit = &v },
}

// limitFieldEffective extracts the currently-enforced value for a field
// off the effective-limits resolver result. Used at submission time so
// the stored row records what the user was looking at when they asked.
func limitFieldEffective(field string, eff *models.OrganizationLimits) int {
	if eff == nil {
		return 0
	}
	get := func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	}
	switch field {
	case "max_campaigns":
		return get(eff.MaxCampaigns)
	case "max_active_campaigns":
		return get(eff.MaxActiveCampaigns)
	case "max_team_members":
		return get(eff.MaxTeamMembers)
	case "max_email_accounts":
		return get(eff.MaxEmailAccounts)
	case "max_contacts":
		return get(eff.MaxContacts)
	case "daily_campaign_limit":
		return get(eff.DailyCampaignLimit)
	}
	return 0
}

func (s *organizationService) SubmitLimitIncreaseRequest(ctx context.Context, orgID, submitterID uuid.UUID, req *models.CreateLimitIncreaseRequest) (*models.LimitIncreaseRequest, *errx.Error) {
	if _, ok := limitFieldUpdater[req.Field]; !ok {
		return nil, errx.New(errx.BadRequest, "unknown field")
	}
	if req.Requested <= 0 {
		return nil, errx.New(errx.BadRequest, "requested value must be positive")
	}

	// Membership check — only members of the org can submit requests
	// on its behalf. Owner check happens at the per-org-permission
	// layer for org-config writes; for limit requests any active
	// member is acceptable.
	if _, xerr := s.GetMembership(ctx, orgID, submitterID); xerr != nil {
		return nil, xerr
	}

	effective, xerr := s.GetEffectiveLimits(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	current := limitFieldEffective(req.Field, effective)
	if req.Requested <= current {
		return nil, errx.New(errx.BadRequest, "requested value must exceed current effective limit")
	}

	lr := &models.LimitIncreaseRequest{
		ID:               uuid.New(),
		OrganizationID:   orgID,
		Field:            req.Field,
		CurrentEffective: current,
		Requested:        req.Requested,
		Reason:           req.Reason,
		Status:           models.LimitRequestStatusPending,
		SubmittedBy:      submitterID,
	}

	if err := s.orgRepo.CreateLimitRequest(ctx, lr); err != nil {
		// The (org, field) WHERE status='pending' unique index trips
		// when the same workspace already has an open request for the
		// same resource. Surface as 409 so the dashboard can render
		// "you already have a pending request" instead of a generic
		// 500.
		if strings.Contains(err.Error(), "uq_limit_requests_one_pending_per_field") {
			return nil, errx.New(errx.Conflict, "a pending request already exists for this field")
		}
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to submit request")
	}
	return lr, nil
}

func (s *organizationService) ListLimitRequestsForOrg(ctx context.Context, orgID uuid.UUID) ([]models.LimitIncreaseRequest, *errx.Error) {
	rows, err := s.orgRepo.ListLimitRequestsForOrg(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load requests")
	}
	return rows, nil
}

// CancelLimitRequest lets the original submitter walk back a pending
// request. Approved / rejected rows are immutable — they survive as
// the audit record of what was decided and when.
func (s *organizationService) CancelLimitRequest(ctx context.Context, id, userID uuid.UUID) *errx.Error {
	lr, err := s.orgRepo.GetLimitRequest(ctx, id)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to load request")
	}
	if lr == nil {
		return errx.New(errx.NotFound, "request not found")
	}
	if lr.SubmittedBy != userID {
		return errx.ErrForbidden
	}
	if lr.Status != models.LimitRequestStatusPending {
		return errx.New(errx.BadRequest, "only pending requests can be cancelled")
	}
	if err := s.orgRepo.UpdateLimitRequestStatus(ctx, id, models.LimitRequestStatusCancelled, userID, ""); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to cancel request")
	}
	return nil
}

func (s *organizationService) AdminListLimitRequests(ctx context.Context, search *models.AdminLimitRequestSearch) (*models.AdminLimitRequestsResult, *errx.Error) {
	// field/status are free-form strings on the wire (field has no DB enum), so
	// validate them here rather than relying on query binding.
	if search.Field != "" {
		if _, ok := limitFieldUpdater[search.Field]; !ok {
			return nil, errx.New(errx.BadRequest, "invalid field filter")
		}
	}
	switch search.Status {
	case "", "all",
		string(models.LimitRequestStatusPending),
		string(models.LimitRequestStatusApproved),
		string(models.LimitRequestStatusRejected),
		string(models.LimitRequestStatusCancelled):
	default:
		return nil, errx.New(errx.BadRequest, "invalid status filter")
	}

	result, err := s.orgRepo.ListLimitRequestsForAdmin(ctx, search)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load limit requests")
	}
	return result, nil
}

// ApproveLimitRequest stamps the row and writes the corresponding
// override column via SetLimitOverrides — same write path the admin
// uses for direct overrides, so the granted_by audit trail is unified.
func (s *organizationService) ApproveLimitRequest(ctx context.Context, id, reviewerID uuid.UUID, notes string) (*models.LimitIncreaseRequest, *errx.Error) {
	lr, err := s.orgRepo.GetLimitRequest(ctx, id)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load request")
	}
	if lr == nil {
		return nil, errx.New(errx.NotFound, "request not found")
	}
	if lr.Status != models.LimitRequestStatusPending {
		return nil, errx.New(errx.BadRequest, "only pending requests can be approved")
	}

	updater, ok := limitFieldUpdater[lr.Field]
	if !ok {
		return nil, errx.New(errx.Internal, "request references an unknown field")
	}

	override := &models.UpdateOrgOverridesRequest{}
	updater(override, lr.Requested)
	if notes != "" {
		notesCopy := "approved request " + lr.ID.String() + ": " + notes
		override.Notes = &notesCopy
	}
	if _, xerr := s.SetLimitOverrides(ctx, lr.OrganizationID, override, reviewerID); xerr != nil {
		return nil, xerr
	}

	if err := s.orgRepo.UpdateLimitRequestStatus(ctx, id, models.LimitRequestStatusApproved, reviewerID, notes); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to mark request approved")
	}
	lr.Status = models.LimitRequestStatusApproved
	lr.ReviewedBy = &reviewerID
	lr.ReviewNotes = notes
	return lr, nil
}

func (s *organizationService) RejectLimitRequest(ctx context.Context, id, reviewerID uuid.UUID, notes string) (*models.LimitIncreaseRequest, *errx.Error) {
	lr, err := s.orgRepo.GetLimitRequest(ctx, id)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load request")
	}
	if lr == nil {
		return nil, errx.New(errx.NotFound, "request not found")
	}
	if lr.Status != models.LimitRequestStatusPending {
		return nil, errx.New(errx.BadRequest, "only pending requests can be rejected")
	}
	if err := s.orgRepo.UpdateLimitRequestStatus(ctx, id, models.LimitRequestStatusRejected, reviewerID, notes); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to mark request rejected")
	}
	lr.Status = models.LimitRequestStatusRejected
	lr.ReviewedBy = &reviewerID
	lr.ReviewNotes = notes
	return lr, nil
}
