package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
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
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	org, xerr := h.OrganizationService.Create(c.Request.Context(), userID, req.Name)
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

// GetCurrentOrganization returns the current organization from session
func (h *Handler) GetCurrentOrganization(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	org, xerr := h.OrganizationService.Get(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	// Get counts and limits
	counts, _ := h.OrganizationService.GetOrganizationCounts(c.Request.Context(), *orgID)
	limits, _ := h.OrganizationService.GetOrganizationLimits(c.Request.Context(), *orgID)

	result := models.OrganizationWithLimits{
		Organization: *org,
		Limits:       limits,
		Counts:       counts,
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
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	org, xerr := h.OrganizationService.Update(c.Request.Context(), *orgID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityOrganization, orgID, nil, nil)

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
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	inv, xerr := h.OrganizationService.InviteMember(c.Request.Context(), *orgID, userID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionInvite, models.AuditEntityOrganizationMember, nil, nil, map[string]string{"email": inv.Email, "role": inv.Role})

	// Get organization name for email
	org, _ := h.OrganizationService.Get(c.Request.Context(), *orgID)
	orgName := "your organization"
	if org != nil {
		orgName = org.Name
	}

	// Get inviter name
	inviter, _ := h.UserService.GetUser(c.Request.Context(), userID)
	inviterName := "A team member"
	if inviter != nil && inviter.FirstName != "" {
		inviterName = inviter.FirstName
		if inviter.LastName != "" {
			inviterName += " " + inviter.LastName
		}
	}

	// Send invitation email
	if h.EmailNotificationService != nil {
		subject := fmt.Sprintf("You've been invited to join %s on Warmbly", orgName)
		body := fmt.Sprintf(`
			<h2>You've been invited!</h2>
			<p>%s has invited you to join <strong>%s</strong> on Warmbly.</p>
			<p>Click the link below to accept the invitation:</p>
			<p><a href="https://app.warmbly.com/invite?token=%s" style="display:inline-block;padding:12px 24px;background:#4F46E5;color:white;text-decoration:none;border-radius:6px;">Accept Invitation</a></p>
			<p>This invitation expires in 7 days.</p>
			<p>If you don't have an account yet, you'll be able to create one when you accept the invitation.</p>
		`, inviterName, orgName, inv.Token)

		go h.EmailNotificationService.Send(c.Request.Context(), []string{req.Email}, nil, nil, subject, body)
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
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	member, xerr := h.OrganizationService.UpdateMemberRole(c.Request.Context(), *orgID, memberUserID, &req)
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

	if xerr := h.OrganizationService.RemoveMember(c.Request.Context(), *orgID, memberUserID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionRemove, models.AuditEntityOrganizationMember, &memberUserID, nil, nil)

	c.JSON(http.StatusOK, gin.H{"message": "member removed"})
}

// TransferOwnership transfers organization ownership to another member
func (h *Handler) TransferOwnership(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req models.TransferOwnershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	if xerr := h.OrganizationService.TransferOwnership(c.Request.Context(), *orgID, req.NewOwnerUserID); xerr != nil {
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
	invIDStr := c.Param("id")
	invID, err := uuid.Parse(invIDStr)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid invitation ID"))
		return
	}

	if xerr := h.OrganizationService.CancelInvitation(c.Request.Context(), invID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityInvitation, &invID, nil, nil)

	c.JSON(http.StatusOK, gin.H{"message": "invitation cancelled"})
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
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	// Get user email from user service
	user, xerr := h.UserService.GetUser(c.Request.Context(), userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	member, xerr := h.OrganizationService.AcceptInvitation(c.Request.Context(), req.Token, userID, user.Email)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityOrganizationMember, &member.UserID, nil, map[string]string{"via": "invitation"})

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

// GetOrganizationLimits returns the organization's limits and current usage
func (h *Handler) GetOrganizationLimits(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	limits, xerr := h.OrganizationService.GetOrganizationLimits(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	counts, xerr := h.OrganizationService.GetOrganizationCounts(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"limits": limits,
		"counts": counts,
	})
}
