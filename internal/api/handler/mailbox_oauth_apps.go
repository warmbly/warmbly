package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) mailboxOAuthAppsReady(c *gin.Context) bool {
	if h.MailboxOAuthAppService == nil {
		errx.Handle(c, errx.New(errx.ServiceUnavailable, "workspace OAuth apps are not available on this instance"))
		return false
	}
	return true
}

// oauthAppOrg admits Partnership, and a workspace keeping an app it saved: that app still refreshes its mailboxes.
func (h *Handler) oauthAppOrg(c *gin.Context, provider models.InboxProvider) (uuid.UUID, bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return uuid.Nil, false
	}
	if !h.mailboxOAuthAppsReady(c) {
		return uuid.Nil, false
	}
	// Partnership only: a self-hosted instance signs mailboxes in with its own instance apps.
	if h.FeatureGateService != nil {
		if ok, _ := h.FeatureGateService.HasPartnership(c.Request.Context(), *orgID); ok {
			return *orgID, true
		}
	}
	apps, xerr := h.MailboxOAuthAppService.List(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return uuid.Nil, false
	}
	for _, a := range apps {
		if provider == "" || a.Provider == provider {
			return *orgID, true
		}
	}
	errx.Handle(c, ErrPartnershipRequired)
	return uuid.Nil, false
}

// ListMailboxOAuthApps is GET /emails/oauth-apps: the workspace's own Google and Microsoft clients, secrets never included.
func (h *Handler) ListMailboxOAuthApps(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	if !h.mailboxOAuthAppsReady(c) {
		return
	}
	apps, xerr := h.MailboxOAuthAppService.List(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": apps, "callbacks": h.MailboxOAuthAppService.Callbacks()})
}

// PutMailboxOAuthApp is PUT /emails/oauth-apps/:provider; an empty client_secret keeps the stored one.
func (h *Handler) PutMailboxOAuthApp(c *gin.Context) {
	provider := models.InboxProvider(c.Param("provider"))
	orgID, ok := h.oauthAppOrg(c, provider)
	if !ok {
		return
	}
	userID, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.Handle(c, errx.ErrUnauthorized)
		return
	}
	var req models.MailboxOAuthAppInput
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	app, xerr := h.MailboxOAuthAppService.Save(c.Request.Context(), orgID, userID, provider, req)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	details := map[string]string{"provider": string(app.Provider), "client_id": app.ClientID}
	if req.ClientSecret != "" {
		details["secret"] = "replaced"
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityMailboxOAuthApp, &app.ID, nil, details)
	c.JSON(http.StatusOK, app)
}

// DeleteMailboxOAuthApp is DELETE /emails/oauth-apps/:provider; refused while mailboxes hold grants from it.
func (h *Handler) DeleteMailboxOAuthApp(c *gin.Context) {
	provider := models.InboxProvider(c.Param("provider"))
	orgID, ok := h.oauthAppOrg(c, provider)
	if !ok {
		return
	}
	if xerr := h.MailboxOAuthAppService.Delete(c.Request.Context(), orgID, provider); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityMailboxOAuthApp, nil, map[string]string{"provider": string(provider)}, nil)
	c.Status(http.StatusNoContent)
}
