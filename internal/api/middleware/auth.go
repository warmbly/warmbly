package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	UserIDKey         = "user_id"
	AccessTokenKey    = "access_token"
	SessionKey        = "session"
	OrganizationIDKey = "organization_id"
	// SessionMemberKey holds the caller's membership in the session's workspace.
	SessionMemberKey = "session_member"
)

func (h *Handler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			errx.Handle(c, errx.ErrAuth)
			c.Abort()
			return
		}
		if refuseAgencyKeyHeader(c) {
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")

		session, err := h.TokenService.ValidateAccessToken(c.Request.Context(), token)
		if err != nil {
			errx.Handle(c, err)
			c.Abort()
			return
		}

		c.Set(UserIDKey, session.UserID.String())
		c.Set(SessionKey, session)
		c.Set(AccessTokenKey, token)

		if !h.enforcePortalSession(c, session) {
			return
		}
		if xerr := h.setSessionOrganization(c, session); xerr != nil {
			errx.JSON(c, xerr)
			c.Abort()
			return
		}

		c.Next()
	}
}

// setSessionOrganization puts the session's workspace in the request only
// while the session's user is a member of it; otherwise the request has none.
func (h *Handler) setSessionOrganization(c *gin.Context, session *models.Session) *errx.Error {
	if session.CurrentOrganizationID == nil {
		return nil
	}
	orgID := *session.CurrentOrganizationID
	// A portal session keeps a workspace only while it is a client of the session's agency.
	if allowed, xerr := PortalAllowsWorkspace(c, orgID); xerr != nil {
		return xerr
	} else if !allowed {
		detached := *session
		detached.CurrentOrganizationID = nil
		c.Set(SessionKey, &detached)
		return nil
	}
	if h.OrganizationService == nil {
		c.Set(OrganizationIDKey, orgID)
		return nil
	}
	member, xerr := h.OrganizationService.GetMembership(c.Request.Context(), orgID, session.UserID)
	if xerr != nil {
		return xerr
	}
	if member == nil {
		// Clear a selection that outlived its membership, and stop the request's copy of the session naming it.
		if h.TokenService != nil {
			_ = h.TokenService.LeaveOrganization(c.Request.Context(), session.UserID, orgID)
		}
		detached := *session
		detached.CurrentOrganizationID = nil
		c.Set(SessionKey, &detached)
		return nil
	}
	c.Set(OrganizationIDKey, orgID)
	c.Set(SessionMemberKey, member)
	return nil
}

// memberHasPermission answers from the membership resolved at authentication
// when it is for orgID, and from the database otherwise.
func (h *Handler) memberHasPermission(c *gin.Context, orgID, userID uuid.UUID, perm models.OrganizationPermission) (bool, *errx.Error) {
	if v, ok := c.Get(SessionMemberKey); ok {
		if m, ok := v.(*models.OrganizationMember); ok && m.OrganizationID == orgID && m.UserID == userID {
			return m.HasPermission(perm), nil
		}
	}
	return h.OrganizationService.HasPermission(c.Request.Context(), orgID, userID, perm)
}

func GetUserID(c *gin.Context) string {
	return c.GetString(UserIDKey)
}

func GetUserUUID(c *gin.Context) (uuid.UUID, error) {
	return uuid.Parse(c.GetString(UserIDKey))
}

func GetAccessToken(c *gin.Context) string {
	return c.GetString(AccessTokenKey)
}

func GetSession(c *gin.Context) *models.Session {
	if session, exists := c.Get(SessionKey); exists {
		if s, ok := session.(*models.Session); ok {
			return s
		}
	}
	return nil
}

func GetOrganizationID(c *gin.Context) *uuid.UUID {
	if orgID, exists := c.Get(OrganizationIDKey); exists {
		if id, ok := orgID.(uuid.UUID); ok {
			return &id
		}
	}
	return nil
}
