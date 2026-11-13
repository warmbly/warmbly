package middleware

import (
	"context"
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

// agencyClientAccess is the one check that lets an agency key act in a client workspace: the
// workspace is a current client of the key's agency and the key's creator is an accepted member of it.
// The key's bits are narrowed to the ones it may use in a workspace.
func (h *Handler) agencyClientAccess(ctx context.Context, k *models.AgencyAPIKey, orgID uuid.UUID) (*models.OrganizationMember, uint64, *errx.Error) {
	if xerr := h.AgencyKeys.RequireClient(ctx, k, orgID); xerr != nil {
		return nil, 0, xerr
	}
	if h.OrganizationService == nil {
		return nil, 0, errx.InternalError()
	}
	member, xerr := h.OrganizationService.GetMembership(ctx, orgID, k.CreatedBy)
	if xerr != nil {
		return nil, 0, xerr
	}
	if member == nil || member.AcceptedAt == nil {
		return nil, 0, errAgencyKeyMemberGone
	}
	return member, k.Permissions & models.AgencyKeyWorkspaceScopes, nil
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
	member, perms, xerr := h.agencyClientAccess(c.Request.Context(), k, orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		c.Abort()
		return
	}
	c.Set(APIKeyPermissionsKey, perms)
	c.Set(OrganizationIDKey, orgID)
	c.Set(SessionMemberKey, member)
	c.Next()
}

// AgencyClient is what an agency key may do in one client workspace.
type AgencyClient struct {
	// Perms are the member's permissions there; an owner holds every one.
	Perms models.OrganizationPermission
	// APIPerms are the key's workspace bits.
	APIPerms uint64
}

// agencyClientResolver resolves a client workspace for an agency key that named none up front.
type agencyClientResolver func(ctx context.Context, orgID uuid.UUID) (*AgencyClient, *errx.Error)

const agencyClientResolverKey = "agency_client_resolver"

// validateAgencyKeyForAgency admits an agency key for its agency as a whole; a client workspace
// is resolved per call through ResolveAgencyClient, with the same check the header path runs.
func (h *Handler) validateAgencyKeyForAgency(c *gin.Context, raw string) {
	k, ok := h.authenticateAgencyKey(c, raw)
	if !ok {
		return
	}
	c.Set(APIKeyPermissionsKey, k.Permissions)
	c.Set(agencyClientResolverKey, agencyClientResolver(func(ctx context.Context, orgID uuid.UUID) (*AgencyClient, *errx.Error) {
		member, perms, xerr := h.agencyClientAccess(ctx, k, orgID)
		if xerr != nil {
			return nil, xerr
		}
		// Nothing reached this way is scope-aware, which is how the gate treats a restricted member.
		if member.IsRestricted() {
			return nil, errMemberAccessRestricted
		}
		return &AgencyClient{Perms: memberPermissions(member), APIPerms: perms}, nil
	}))
	c.Next()
}

// ResolveAgencyClient checks an agency key admitted for its whole agency against one client workspace.
func ResolveAgencyClient(c *gin.Context, orgID uuid.UUID) (*AgencyClient, *errx.Error) {
	v, ok := c.Get(agencyClientResolverKey)
	resolve, _ := v.(agencyClientResolver)
	if !ok || resolve == nil {
		return nil, errAgencyKeyOrgRequired
	}
	if orgID == uuid.Nil {
		return nil, errAgencyKeyOrgRequired
	}
	return resolve(c.Request.Context(), orgID)
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
