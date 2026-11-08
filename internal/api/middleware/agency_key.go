package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	AgencyKeyIDKey       = "agency_key_id"
	AgencyKeyAgencyIDKey = "agency_key_agency_id"
)

var (
	errAgencyKeyNotAllowed = errx.NewWithIdentifier(errx.Forbidden, apikey.CodeAgencyKeyNotAllowed,
		"Agency API keys cannot call this endpoint. Use a signed-in session.")
	errAgencyKeyIPNotAllowed = errx.NewWithIdentifier(errx.Forbidden, apikey.CodeAgencyKeyIPNotAllowed,
		"This agency API key does not accept requests from this IP address.")
	errAgencyKeyOrgRequired = errx.NewWithIdentifier(errx.BadRequest, apikey.CodeAgencyKeyOrgRequired,
		"An agency API key acts on one client workspace: name it in the "+models.AgencyOrganizationHeader+" header.")
	errAgencyKeyMemberGone = errx.NewWithIdentifier(errx.Unauthorized, apikey.CodeAgencyKeyHolderLeft,
		"The staff member who created this agency API key no longer has access to this workspace. Create a new key.")
	errAgencyCredentialRequired = errx.NewWithIdentifier(errx.Forbidden, "agency_credential_required",
		"Agency endpoints take a signed-in session or an agency API key, not a workspace API key or app token.")
	errAgencyKeyOtherAgency = errx.NewWithIdentifier(errx.NotFound, "agency_not_found",
		"That agency does not exist or you are not part of it.")
)

func isAgencyKeyHeader(authHeader string) bool {
	return strings.HasPrefix(authHeader, "Bearer "+apikey.AgencyKeyPrefix)
}

// GetAgencyKeyID is the agency API key the request authenticated with, or nil.
func GetAgencyKeyID(c *gin.Context) *uuid.UUID {
	if v, ok := c.Get(AgencyKeyIDKey); ok {
		if id, ok := v.(uuid.UUID); ok {
			return &id
		}
	}
	return nil
}

// GetAgencyKeyAgencyID is the agency an agency API key belongs to, or nil for any other caller.
func GetAgencyKeyAgencyID(c *gin.Context) *uuid.UUID {
	if v, ok := c.Get(AgencyKeyAgencyIDKey); ok {
		if id, ok := v.(uuid.UUID); ok {
			return &id
		}
	}
	return nil
}

// authenticateAgencyKey runs the checks every agency-key request shares and names the caller.
func (h *Handler) authenticateAgencyKey(c *gin.Context, raw string) (*models.AgencyAPIKey, bool) {
	if h.AgencyKeys == nil {
		errx.Handle(c, errx.ErrAuth)
		c.Abort()
		return nil, false
	}
	k, xerr := h.AgencyKeys.Authenticate(c.Request.Context(), raw)
	if xerr != nil {
		errx.JSON(c, xerr)
		c.Abort()
		return nil, false
	}
	if !h.AgencyKeys.IPAllowed(k, c.ClientIP()) {
		errx.JSON(c, errAgencyKeyIPNotAllowed)
		c.Abort()
		return nil, false
	}
	remaining, retryAfter, allowed := h.AgencyKeys.RateLimit(c.Request.Context(), k)
	if !applyKeyRateLimit(c, k.RateLimitPerMinute, remaining, retryAfter, allowed) {
		return nil, false
	}
	// Every gate already holds AuthTypeAPIKey to its key bits and its member's role.
	c.Set(AuthTypeKey, AuthTypeAPIKey)
	c.Set(AgencyKeyIDKey, k.ID)
	c.Set(AgencyKeyAgencyIDKey, k.AgencyID)
	c.Set(APIKeyNameKey, k.Name)
	c.Set(UserIDKey, k.CreatedBy.String())
	h.AgencyKeys.Touch(k.ID, c.ClientIP())
	return k, true
}

// validateAgencyKeyForWorkspace admits an agency key to one client workspace named by header,
// checked to be a current client before the tenant is set.
func (h *Handler) validateAgencyKeyForWorkspace(c *gin.Context, raw string) {
	k, ok := h.authenticateAgencyKey(c, raw)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(strings.TrimSpace(c.GetHeader(models.AgencyOrganizationHeader)))
	if err != nil || orgID == uuid.Nil {
		errx.JSON(c, errAgencyKeyOrgRequired)
		c.Abort()
		return
	}
	if xerr := h.AgencyKeys.RequireClient(c.Request.Context(), k, orgID); xerr != nil {
		errx.JSON(c, xerr)
		c.Abort()
		return
	}
	if h.OrganizationService == nil {
		errx.JSON(c, errx.InternalError())
		c.Abort()
		return
	}
	member, xerr := h.OrganizationService.GetMembership(c.Request.Context(), orgID, k.CreatedBy)
	if xerr != nil {
		errx.JSON(c, xerr)
		c.Abort()
		return
	}
	if member == nil || member.AcceptedAt == nil {
		errx.JSON(c, errAgencyKeyMemberGone)
		c.Abort()
		return
	}
	c.Set(APIKeyPermissionsKey, k.Permissions&models.AgencyKeyWorkspaceScopes)
	c.Set(OrganizationIDKey, orgID)
	c.Set(SessionMemberKey, member)
	c.Next()
}

// AgencyAuthMiddleware admits a signed-in session or an agency API key to the agency endpoints.
func (h *Handler) AgencyAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		switch {
		case isAgencyKeyHeader(authHeader):
			k, ok := h.authenticateAgencyKey(c, strings.TrimPrefix(authHeader, "Bearer "))
			if !ok {
				return
			}
			c.Set(APIKeyPermissionsKey, k.Permissions)
			c.Next()
		case strings.HasPrefix(authHeader, "Bearer "+apikey.KeyPrefix),
			strings.HasPrefix(authHeader, "Bearer "+models.OAuthAccessTokenPrefix):
			errx.JSON(c, errAgencyCredentialRequired)
			c.Abort()
		case strings.HasPrefix(authHeader, "Bearer "):
			h.validateJWT(c, strings.TrimPrefix(authHeader, "Bearer "))
		default:
			errx.Handle(c, errx.ErrAuth)
			c.Abort()
		}
	}
}

// RequireAgencyAccess holds an agency key to its own agency and to perm; sessions pass to the
// service's staff check. A zero perm keeps the route session-only.
func RequireAgencyAccess(perm uint64) gin.HandlerFunc {
	return func(c *gin.Context) {
		agencyID := GetAgencyKeyAgencyID(c)
		if agencyID == nil {
			c.Next()
			return
		}
		if perm == 0 {
			errx.JSON(c, errAgencyKeyNotAllowed)
			c.Abort()
			return
		}
		if raw := c.Param("agencyId"); raw != "" {
			if id, err := uuid.Parse(raw); err != nil || id != *agencyID {
				errx.JSON(c, errAgencyKeyOtherAgency)
				c.Abort()
				return
			}
		}
		if !models.HasAPIPermission(GetAPIKeyPermissions(c), perm) {
			errx.JSON(c, errx.New(errx.Forbidden, "insufficient API key permissions"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// RefuseAgencyKey keeps agency keys off a route that only a session or a workspace credential may call.
func RefuseAgencyKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		if GetAgencyKeyID(c) != nil {
			errx.JSON(c, errAgencyKeyNotAllowed)
			c.Abort()
			return
		}
		c.Next()
	}
}

// refuseAgencyKeyHeader answers a session-only route that was handed an agency key.
func refuseAgencyKeyHeader(c *gin.Context) bool {
	if !isAgencyKeyHeader(c.GetHeader("Authorization")) {
		return false
	}
	errx.JSON(c, errAgencyKeyNotAllowed)
	c.Abort()
	return true
}
