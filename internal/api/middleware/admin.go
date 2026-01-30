package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	AdminPermissionsKey = "admin_permissions"
	AdminUserIDKey      = "admin_user_id"
)

// AdminMiddleware checks if the user has any admin permissions
// This should be used as a guard for all admin routes
func (h *Handler) AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := GetSession(c)
		if session == nil {
			errx.JSON(c, errx.ErrUnauthorized)
			c.Abort()
			return
		}

		// Get admin permissions from the session/user
		// For now we'll need to fetch from the database
		perms, err := h.getAdminPermissions(c, session.UserID)
		if err != nil {
			errx.JSON(c, errx.New(errx.Internal, "failed to check admin status"))
			c.Abort()
			return
		}

		if perms == 0 {
			errx.JSON(c, errx.New(errx.Forbidden, "admin access required"))
			c.Abort()
			return
		}

		// Store admin permissions in context
		c.Set(AdminPermissionsKey, perms)
		c.Set(AdminUserIDKey, session.UserID)
		c.Next()
	}
}

// RequireAdminPermission checks if the admin has a specific permission
func RequireAdminPermission(perm models.AdminPermission) gin.HandlerFunc {
	return func(c *gin.Context) {
		perms := GetAdminPermissions(c)
		if perms == 0 {
			errx.JSON(c, errx.New(errx.Forbidden, "admin access required"))
			c.Abort()
			return
		}

		if !perms.HasPermission(perm) {
			errx.JSON(c, errx.New(errx.Forbidden, "insufficient admin permissions"))
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAnyAdminPermission checks if the admin has at least one of the specified permissions
func RequireAnyAdminPermission(perms ...models.AdminPermission) gin.HandlerFunc {
	return func(c *gin.Context) {
		adminPerms := GetAdminPermissions(c)
		if adminPerms == 0 {
			errx.JSON(c, errx.New(errx.Forbidden, "admin access required"))
			c.Abort()
			return
		}

		for _, perm := range perms {
			if adminPerms.HasPermission(perm) {
				c.Next()
				return
			}
		}

		errx.JSON(c, errx.New(errx.Forbidden, "insufficient admin permissions"))
		c.Abort()
	}
}

// GetAdminPermissions returns the admin permissions from the context
func GetAdminPermissions(c *gin.Context) models.AdminPermission {
	if perms, exists := c.Get(AdminPermissionsKey); exists {
		if p, ok := perms.(models.AdminPermission); ok {
			return p
		}
	}
	return 0
}

// GetAdminUserID returns the admin user ID from the context
func GetAdminUserID(c *gin.Context) *uuid.UUID {
	if userID, exists := c.Get(AdminUserIDKey); exists {
		if id, ok := userID.(uuid.UUID); ok {
			return &id
		}
	}
	return nil
}

// IsAdmin returns true if the current user has admin permissions
func IsAdmin(c *gin.Context) bool {
	return GetAdminPermissions(c) > 0
}

// IsSuperAdmin returns true if the current user has all admin permissions
func IsSuperAdmin(c *gin.Context) bool {
	return GetAdminPermissions(c) == models.AllAdminPermissions
}

// getAdminPermissions fetches admin permissions for a user
// This uses the OrganizationService to query the user's admin_permissions field
func (h *Handler) getAdminPermissions(c *gin.Context, userID uuid.UUID) (models.AdminPermission, error) {
	// This will be implemented via the admin service
	// For now, we delegate to the organization service which has user access
	if h.OrganizationService == nil {
		return 0, nil
	}

	perms, err := h.OrganizationService.GetUserAdminPermissions(c.Request.Context(), userID)
	if err != nil {
		return 0, err
	}

	return models.AdminPermission(perms), nil
}
