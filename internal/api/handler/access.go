package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// hasAccess is middleware.RequireAccess as an inline check, for a permission a
// handler needs only when an optional request field is present.
func (h *Handler) hasAccess(c *gin.Context, orgPerm models.OrganizationPermission, apiPerm uint64) *errx.Error {
	switch middleware.GetAuthType(c) {
	case middleware.AuthTypeAPIKey, middleware.AuthTypeOAuth:
		if !models.HasAPIPermission(middleware.GetAPIKeyPermissions(c), apiPerm) {
			return errx.New(errx.Forbidden, "insufficient API key permissions")
		}
		if middleware.GetAuthType(c) == middleware.AuthTypeOAuth {
			if perms, ok := middleware.GetMemberPermissions(c); !ok || !perms.HasPermission(orgPerm) {
				return errx.New(errx.Forbidden, "the member who authorized this app does not have this permission")
			}
		}
		return nil
	default:
		if h.OrganizationService == nil {
			return nil
		}
		userID, err := middleware.GetUserUUID(c)
		if err != nil {
			return errx.ErrUnauthorized
		}
		orgID := middleware.GetOrganizationID(c)
		if orgID == nil {
			return errx.ErrNoOrganization
		}
		has, xerr := h.OrganizationService.HasPermission(c.Request.Context(), *orgID, userID, orgPerm)
		if xerr != nil {
			return xerr
		}
		if !has {
			return errx.ErrForbidden
		}
		return nil
	}
}

// bindOAuthMember holds an OAuth caller's tools to its member's permissions as well as its scopes.
func bindOAuthMember(c *gin.Context, inv *aitools.Invocation) {
	if t := middleware.GetAuthType(c); t != middleware.AuthTypeOAuth && t != middleware.AuthTypeAPIKey {
		return
	}
	inv.ActsForMember = true
	inv.OrgPerms, _ = middleware.GetMemberPermissions(c)
}

// apiKeyCeiling is the widest permission set the caller may put on an API key.
func (h *Handler) apiKeyCeiling(c *gin.Context) (uint64, *errx.Error) {
	if middleware.GetAuthType(c) == middleware.AuthTypeAPIKey {
		return middleware.GetAPIKeyPermissions(c), nil
	}
	member, xerr := h.callerMember(c)
	if xerr != nil {
		return 0, xerr
	}
	return models.APIPermissionsFor(member), nil
}

// callerMember is the caller's membership in the request's workspace, or nil.
func (h *Handler) callerMember(c *gin.Context) (*models.OrganizationMember, *errx.Error) {
	if m := middleware.GetAuthMember(c); m != nil {
		return m, nil
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		return nil, errx.ErrNoOrganization
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		return nil, errx.ErrUnauthorized
	}
	if h.OrganizationService == nil {
		return nil, errx.ErrForbidden
	}
	return h.OrganizationService.GetMembership(c.Request.Context(), *orgID, userID)
}

// mailboxAllowed enforces the caller's mailbox allowlist on an account id from
// a request body, the way RequireEmailAccountParam does for a route parameter.
func mailboxAllowed(c *gin.Context, accountID uuid.UUID) *errx.Error {
	if !middleware.EmailAccountAllowed(c, accountID) {
		return middleware.ErrEmailAccountNotAllowed(c)
	}
	return nil
}

// apiKeyMailboxLimited is the stable code for a mailbox-limited key refused on a workspace-wide surface.
const apiKeyMailboxLimited = "api_key_mailbox_limited"

// keyMailboxLimited reports whether the caller is held to an allowlist of mailboxes.
func keyMailboxLimited(c *gin.Context) bool {
	return middleware.AllowedEmailAccounts(c) != nil
}
