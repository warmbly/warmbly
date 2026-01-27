package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// OrganizationHandler holds organization-related middleware dependencies
type OrganizationHandler struct {
	OrganizationService organization.OrganizationService
}

// NewOrganizationHandler creates a new OrganizationHandler
func NewOrganizationHandler(orgService organization.OrganizationService) *OrganizationHandler {
	return &OrganizationHandler{
		OrganizationService: orgService,
	}
}

// RequireOrganization ensures user has an organization selected in their session
func (h *OrganizationHandler) RequireOrganization() gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := GetOrganizationID(c)
		if orgID == nil {
			errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
			c.Abort()
			return
		}

		// Verify the organization exists
		org, err := h.OrganizationService.Get(c.Request.Context(), *orgID)
		if err != nil {
			errx.JSON(c, err)
			c.Abort()
			return
		}
		if org == nil {
			errx.JSON(c, errx.New(errx.NotFound, "organization not found"))
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequirePermission checks if user has specific permission in current organization
func (h *OrganizationHandler) RequirePermission(perm models.OrganizationPermission) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := GetUserUUID(c)
		if err != nil {
			errx.JSON(c, errx.ErrUnauthorized)
			c.Abort()
			return
		}

		orgID := GetOrganizationID(c)
		if orgID == nil {
			errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
			c.Abort()
			return
		}

		has, xerr := h.OrganizationService.HasPermission(c.Request.Context(), *orgID, userID, perm)
		if xerr != nil {
			errx.JSON(c, xerr)
			c.Abort()
			return
		}
		if !has {
			errx.JSON(c, errx.ErrForbidden)
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireMembership ensures user is a member of the specified organization (from path param)
func (h *OrganizationHandler) RequireMembership() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := GetUserUUID(c)
		if err != nil {
			errx.JSON(c, errx.ErrUnauthorized)
			c.Abort()
			return
		}

		orgIDStr := c.Param("org_id")
		if orgIDStr == "" {
			// Try getting from context (current organization)
			orgID := GetOrganizationID(c)
			if orgID == nil {
				errx.JSON(c, errx.New(errx.BadRequest, "organization ID required"))
				c.Abort()
				return
			}
			orgIDStr = orgID.String()
		}

		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid organization ID"))
			c.Abort()
			return
		}

		member, xerr := h.OrganizationService.GetMembership(c.Request.Context(), orgID, userID)
		if xerr != nil {
			errx.JSON(c, xerr)
			c.Abort()
			return
		}
		if member == nil {
			errx.JSON(c, errx.New(errx.Forbidden, "not a member of this organization"))
			c.Abort()
			return
		}

		// Set the organization context for downstream handlers
		c.Set(OrganizationIDKey, orgID)
		c.Set("member", member)

		c.Next()
	}
}

// GetMember returns the current member from context (set by RequireMembership)
func GetMember(c *gin.Context) *models.OrganizationMember {
	if member, exists := c.Get("member"); exists {
		if m, ok := member.(*models.OrganizationMember); ok {
			return m
		}
	}
	return nil
}

// RequireOrganization on main Handler - ensures user has an organization selected
func (h *Handler) RequireOrganization() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.OrganizationService == nil {
			c.Next()
			return
		}

		orgID := GetOrganizationID(c)
		if orgID == nil {
			errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
			c.Abort()
			return
		}

		org, err := h.OrganizationService.Get(c.Request.Context(), *orgID)
		if err != nil {
			errx.JSON(c, err)
			c.Abort()
			return
		}
		if org == nil {
			errx.JSON(c, errx.New(errx.NotFound, "organization not found"))
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequirePermission on main Handler - checks if user has specific permission
func (h *Handler) RequirePermission(perm models.OrganizationPermission) gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.OrganizationService == nil {
			c.Next()
			return
		}

		userID, err := GetUserUUID(c)
		if err != nil {
			errx.JSON(c, errx.ErrUnauthorized)
			c.Abort()
			return
		}

		orgID := GetOrganizationID(c)
		if orgID == nil {
			errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
			c.Abort()
			return
		}

		has, xerr := h.OrganizationService.HasPermission(c.Request.Context(), *orgID, userID, perm)
		if xerr != nil {
			errx.JSON(c, xerr)
			c.Abort()
			return
		}
		if !has {
			errx.JSON(c, errx.ErrForbidden)
			c.Abort()
			return
		}

		c.Next()
	}
}
