package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// CreateOrganization creates a new organization
func (h *Handler) CreateOrganization(c *gin.Context) {
	userID, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	var req models.CreateOrganizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	org, xerr := h.OrganizationService.Create(c.Request.Context(), userID, req.Name, req.Timezone)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityOrganization, &org.ID, nil, map[string]string{"name": org.Name})

	c.JSON(http.StatusCreated, org)
}

// GetUserOrganizations returns all organizations the user is a member of
func (h *Handler) GetUserOrganizations(c *gin.Context) {
	userID, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	members, xerr := h.OrganizationService.GetUserOrganizations(c.Request.Context(), userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": members})
}

// SwitchOrganization switches the current organization in the session
func (h *Handler) SwitchOrganization(c *gin.Context) {
	userID, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	orgIDStr := c.Param("id")
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization ID"))
		return
	}

	// Verify user is a member
	member, xerr := h.OrganizationService.GetMembership(c.Request.Context(), orgID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if member == nil {
		errx.JSON(c, errx.New(errx.Forbidden, "not a member of this organization"))
		return
	}

	// Get session from context
	session := middleware.GetSession(c)
	if session == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	// Update session with new organization
	if xerr := h.TokenService.SwitchOrganization(c.Request.Context(), session.ID, &orgID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         "organization switched",
		"organization_id": orgID,
	})
}

type scopedOrganizationCounts struct {
	TotalCampaigns  int `json:"total_campaigns"`
	ActiveCampaigns int `json:"active_campaigns"`
	TotalContacts   int `json:"total_contacts"`
	EmailAccounts   int `json:"email_accounts"`
	EmailsSentToday int `json:"emails_sent_today"`
}

type scopedOrganizationWithLimits struct {
	models.OrganizationWithLimits
	// Shadow workspace-only fields so they are absent, not merely empty.
	ProductDescription *string                  `json:"product_description,omitempty"`
	ICPNotes           *string                  `json:"icp_notes,omitempty"`
	VoiceProfile       *string                  `json:"voice_profile,omitempty"`
	Counts             scopedOrganizationCounts `json:"counts"`
}

// GetCurrentOrganization returns the current organization from session.
func (h *Handler) GetCurrentOrganization(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	restricted := middleware.IsScopeRestricted(c)
	if restricted && middleware.GetResourceScope(c) == nil {
		errx.JSON(c, errx.InternalError())
		return
	}

	org, xerr := h.OrganizationService.Get(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	// Get counts and limits
	var counts *models.OrganizationCounts
	if restricted {
		scope := &models.ResourceScope{Campaigns: middleware.AllowedCampaigns(c), Mailboxes: middleware.AllowedEmailAccounts(c)}
		counts, xerr = h.OrganizationService.GetScopedOrganizationCounts(c.Request.Context(), *orgID, scope)
	} else {
		counts, xerr = h.OrganizationService.GetOrganizationCounts(c.Request.Context(), *orgID)
	}
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if counts == nil || org == nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	limits, xerr := h.OrganizationService.GetOrganizationLimits(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	result := models.OrganizationWithLimits{
		Organization: *org,
		Limits:       limits,
		Counts:       counts,
	}
	if restricted {
		c.JSON(http.StatusOK, scopedOrganizationWithLimits{
			OrganizationWithLimits: result,
			Counts: scopedOrganizationCounts{
				TotalCampaigns: counts.TotalCampaigns, ActiveCampaigns: counts.ActiveCampaigns,
				TotalContacts: counts.TotalContacts, EmailAccounts: counts.EmailAccounts,
				EmailsSentToday: counts.EmailsSentToday,
			},
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// UpdateOrganization updates the current organization
func (h *Handler) UpdateOrganization(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req models.UpdateOrganizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	org, xerr := h.OrganizationService.Update(c.Request.Context(), *orgID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityOrganization, orgID, nil, nil)

	// A presence privacy change re-gates connected sockets live (re-track /
	// untrack / strip activity) rather than waiting for members to reconnect.
	if req.PresenceShowOnline != nil || req.PresenceShowActivity != nil {
		h.StreamingPublisher.PublishPresencePolicy(c.Request.Context(), *orgID, org.PresenceShowOnline, org.PresenceShowActivity)
	}

	c.JSON(http.StatusOK, org)
}

// GetMembers returns all members of the current organization
func (h *Handler) GetMembers(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	members, xerr := h.OrganizationService.GetMembers(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": members})
}

// InviteMember invites a new member to the organization
func (h *Handler) InviteMember(c *gin.Context) {
	userID, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req models.InviteMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	inv, xerr := h.OrganizationService.InviteMember(c.Request.Context(), *orgID, userID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	inviteMeta := map[string]string{"email": inv.Email, "role": inv.Role}
	if inv.Access.Restricted() {
		inviteMeta["access_scope"] = string(models.AccessScopeRestricted)
	}
	h.auditOrg(c, models.AuditActionInvite, models.AuditEntityOrganizationMember, nil, nil, inviteMeta)

	// Get organization name for email
	org, _ := h.OrganizationService.Get(c.Request.Context(), *orgID)
	orgName := "your organization"
	if org != nil && displayname.Displayable(org.Name) != "" {
		orgName = displayname.Displayable(org.Name)
	}

	// Names predating the display-name rules fall back rather than render.
	inviter, _ := h.UserService.GetUser(c.Request.Context(), userID)
	inviterName := "A team member"
	if inviter != nil {
		if name := displayname.FullName(inviter.FirstName, inviter.LastName); name != "" {
			inviterName = name
		}
	}

	// Send invitation email
	if h.EmailNotificationService != nil {
		subject := fmt.Sprintf("You've been invited to join %s on %s", orgName, templates.CompanyName())
		acceptURL := config.GetInviteURL(inv.Token, config.DashboardOriginFromContext(c.Request.Context()))
		// GenerateInvitationHTML reports its own render errors.
		if body, gerr := templates.GenerateInvitationHTML(inviterName, orgName, acceptURL); gerr == nil {
			// Detached from the request context: the handler returns before the
			// send completes, and a cancelled context aborted the send mid-dial.
			sendCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 45*time.Second)
			go func() {
				defer cancel()
				_ = h.EmailNotificationService.Send(sendCtx, []string{req.Email}, nil, nil, subject, body)
			}()
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":    "invitation sent",
		"invitation": inv,
	})
}

// UpdateMemberRole updates a member's role and permissions
func (h *Handler) UpdateMemberRole(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	memberIDStr := c.Param("id")
	memberUserID, err := uuid.Parse(memberIDStr)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid member ID"))
		return
	}

	var req models.UpdateMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	actorID, uerr := middleware.GetUserUUID(c)
	if uerr != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return
	}

	member, xerr := h.OrganizationService.UpdateMemberRole(c.Request.Context(), *orgID, actorID, memberUserID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityOrganizationMember, &memberUserID, nil, map[string]string{"role": member.Role})

	c.JSON(http.StatusOK, member)
}

// RemoveMember removes a member from the organization
func (h *Handler) RemoveMember(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	memberIDStr := c.Param("id")
	memberUserID, err := uuid.Parse(memberIDStr)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid member ID"))
		return
	}

	actorID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUser)
		return
	}

	if xerr := h.OrganizationService.RemoveMember(c.Request.Context(), *orgID, actorID, memberUserID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionRemove, models.AuditEntityOrganizationMember, &memberUserID, nil, nil)

	c.JSON(http.StatusOK, gin.H{"message": "member removed"})
}

// TransferOwnership transfers organization ownership to another member
func (h *Handler) TransferOwnership(c *gin.Context) {
	session := middleware.GetSession(c)
	if session == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req models.TransferOwnershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	if xerr := h.OrganizationService.TransferOwnership(c.Request.Context(), *orgID, session.UserID, req.NewOwnerUserID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionTransfer, models.AuditEntityOrganization, orgID, nil, map[string]string{"new_owner": req.NewOwnerUserID.String()})

	c.JSON(http.StatusOK, gin.H{"message": "ownership transferred"})
}

// GetPendingInvitations returns pending invitations for the organization
func (h *Handler) GetPendingInvitations(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	invitations, xerr := h.OrganizationService.GetPendingInvitations(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": invitations})
}

// CancelInvitation cancels a pending invitation
func (h *Handler) CancelInvitation(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	invIDStr := c.Param("id")
	invID, err := uuid.Parse(invIDStr)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid invitation ID"))
		return
	}

	if xerr := h.OrganizationService.CancelInvitation(c.Request.Context(), *orgID, invID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityInvitation, &invID, nil, nil)

	c.JSON(http.StatusOK, gin.H{"message": "invitation cancelled"})
}

// PreviewInvitation is the public landing-page lookup for the /invite link.
// No auth: anyone holding the secret token can see who invited them where.
func (h *Handler) PreviewInvitation(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "token is required"))
		return
	}
	preview, xerr := h.OrganizationService.PreviewInvitation(c.Request.Context(), token)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, preview)
}

// GetInvitationLink returns the shareable /invite token for a pending
// invitation so a team manager can copy a real accept link.
func (h *Handler) GetInvitationLink(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	invitationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	token, xerr := h.OrganizationService.GetInvitationToken(c.Request.Context(), *orgID, invitationID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

// AcceptInvitation accepts an invitation (public endpoint - can be called before login or after)
func (h *Handler) AcceptInvitation(c *gin.Context) {
	userID, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	var req models.AcceptInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	// Get user email from user service
	user, xerr := h.UserService.GetUser(c.Request.Context(), userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	var member *models.OrganizationMember
	if req.Token != "" {
		member, xerr = h.OrganizationService.AcceptInvitation(c.Request.Context(), req.Token, userID, user.Email)
	} else if req.InvitationID != nil {
		member, xerr = h.OrganizationService.AcceptInvitationByID(c.Request.Context(), *req.InvitationID, userID, user.Email)
	} else {
		errx.JSON(c, errx.New(errx.BadRequest, "token or invitation_id is required"))
		return
	}
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityOrganizationMember, &member.UserID, nil, map[string]string{"via": "invitation"})

	// Tell the members who manage the team (not the whole org, and not the
	// joiner). Detached + best-effort: a notification hiccup must not fail
	// the accept.
	if h.NotificationService != nil {
		joiner := displayname.FullName(user.FirstName, user.LastName)
		if joiner == "" {
			joiner = user.Email
		}
		orgID, joinerID := member.OrganizationID, member.UserID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			h.NotificationService.NotifyOrg(ctx, orgID, models.PermManageTeam, joinerID,
				models.NotifTeamActivity,
				joiner+" joined your workspace",
				user.Email+" accepted their invitation.",
				"/app/settings/members", nil,
				"member_joined:"+joinerID.String())
		}()
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "invitation accepted",
		"member":  member,
	})
}

// GetMyPendingInvitations returns pending invitations for the current user
func (h *Handler) GetMyPendingInvitations(c *gin.Context) {
	userID, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	// Get user email
	user, xerr := h.UserService.GetUser(c.Request.Context(), userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	invitations, xerr := h.OrganizationService.GetUserPendingInvitations(c.Request.Context(), user.Email)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": invitations})
}

// GetOrganizationLimits returns the limits the server actually enforces for
// the workspace (plan, then any approved override) beside the live counts,
// plus the mailbox allowance and attachment storage, which have no plan
// column of their own. A nil limit is unmetered.
func (h *Handler) GetOrganizationLimits(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	limits, xerr := h.OrganizationService.GetEffectiveLimits(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	counts, xerr := h.OrganizationService.GetOrganizationCounts(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	mailboxes, xerr := h.OrganizationService.MailboxAllowance(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	// Storage is reported here because nothing else does: a workspace that
	// dropped to a smaller plan is over its quota with no upload refused yet.
	storage := gin.H{"used_bytes": int64(0), "limit_bytes": int64(0)}
	if h.FeatureGateService != nil && h.AttachmentRepo != nil {
		limit, xerr := h.FeatureGateService.GetStorageLimitBytes(c.Request.Context(), *orgID)
		if xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		used, err := h.AttachmentRepo.SumStorageUsedByOrg(c.Request.Context(), *orgID)
		if err != nil {
			errx.JSON(c, errx.InternalError())
			return
		}
		storage = gin.H{"used_bytes": used, "limit_bytes": limit, "over_quota": used > limit}
	}

	c.JSON(http.StatusOK, gin.H{
		"limits":    limits,
		"counts":    counts,
		"mailboxes": mailboxes,
		"storage":   storage,
	})
}

// GetMemberAccess is GET /organization/members/:id/access: which resources the member's role applies to.
func (h *Handler) GetMemberAccess(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	memberUserID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid member ID"))
		return
	}
	access, xerr := h.OrganizationService.GetMemberAccess(c.Request.Context(), *orgID, memberUserID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, access)
}

// SetMemberAccess is PUT /organization/members/:id/access. It replaces the
// whole scope, so a retry lands on the same state.
func (h *Handler) SetMemberAccess(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	memberUserID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid member ID"))
		return
	}
	actorID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var req models.SetMemberAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	member, xerr := h.OrganizationService.SetMemberAccess(c.Request.Context(), *orgID, actorID, memberUserID, req.Access())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	meta := map[string]string{"access_scope": string(member.AccessScope)}
	if a := member.Access; a != nil && a.Restricted() {
		meta["campaign_folders"] = strconv.Itoa(len(a.FolderIDs))
		meta["campaigns"] = strconv.Itoa(len(a.CampaignIDs))
		meta["mailboxes"] = strconv.Itoa(len(a.EmailAccountIDs))
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityOrganizationMember, &memberUserID, nil, meta)
	c.JSON(http.StatusOK, member)
}

// SuggestAccessSenders is GET /organization/access/suggested-senders: the
// mailboxes the given campaigns and folders send from, for an administrator
// to review before granting them. Campaign access never grants a mailbox.
func (h *Handler) SuggestAccessSenders(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	campaignIDs, xerr := parseUUIDList(c.Query("campaign_ids"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	folderIDs, xerr := parseUUIDList(c.Query("campaign_folder_ids"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if len(campaignIDs)+len(folderIDs) > 2*models.MaxAccessGrants {
		errx.JSON(c, errx.New(errx.BadRequest, "too many campaigns or folders"))
		return
	}
	senders, xerr := h.OrganizationService.SuggestCampaignSenders(c.Request.Context(), *orgID, campaignIDs, folderIDs)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": senders})
}

// parseUUIDList reads a comma-separated list of ids; an invalid one is refused.
func parseUUIDList(raw string) ([]uuid.UUID, *errx.Error) {
	parts := splitAndTrim(raw)
	out := make([]uuid.UUID, 0, len(parts))
	for _, p := range parts {
		id, err := uuid.Parse(p)
		if err != nil {
			return nil, errx.ErrUuid
		}
		out = append(out, id)
	}
	return out, nil
}
