package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/oauth"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// GetIdentity returns the authenticated caller's identity for GET /v1/me.
// It is reachable by any valid credential (API key, OAuth token, or JWT) with
// no specific scope, so integrations can validate a connection and show a
// human-readable label (org name + user email).
func (h *Handler) GetIdentity(c *gin.Context) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUnauthorized)
		return
	}

	ctx := c.Request.Context()

	u, xerr := h.UserService.GetUser(ctx, uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	resp := models.Identity{
		UserID:    u.ID,
		Email:     u.Email,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Name:      strings.TrimSpace(u.FirstName + " " + u.LastName),
		AuthType:  middleware.GetAuthType(c),
		Scopes:    []string{},
	}

	if orgID := middleware.GetOrganizationID(c); orgID != nil {
		resp.OrganizationID = orgID
		if org, oerr := h.OrganizationService.Get(ctx, *orgID); oerr == nil {
			resp.OrganizationName = org.Name
		}
	}

	// API-key and OAuth callers carry a scope bitmask; surface it so a client
	// can see what the credential is allowed to do. JWT sessions have no API
	// scope mask (they use organization-role permissions), so this stays empty.
	if mask := middleware.GetAPIKeyPermissions(c); mask != 0 {
		resp.Scopes = oauth.ScopeList(mask)
	}

	c.JSON(http.StatusOK, resp)
}
