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

// ListMailboxOAuthApps is GET /emails/oauth-apps: the workspace's own Google and Microsoft clients, secrets never included.
func (h *Handler) ListMailboxOAuthApps(c *gin.Context) {
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxOAuthAppsReady(c) {
		return
	}
	apps, xerr := h.MailboxOAuthAppService.List(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": apps, "callbacks": h.MailboxOAuthAppService.Callbacks()})
}

// PutMailboxOAuthApp is PUT /emails/oauth-apps/:provider; an empty client_secret keeps the stored one.
func (h *Handler) PutMailboxOAuthApp(c *gin.Context) {
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxOAuthAppsReady(c) {
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
	app, xerr := h.MailboxOAuthAppService.Save(c.Request.Context(), orgID, userID, models.InboxProvider(c.Param("provider")), req)
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
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxOAuthAppsReady(c) {
		return
	}
	provider := models.InboxProvider(c.Param("provider"))
	if xerr := h.MailboxOAuthAppService.Delete(c.Request.Context(), orgID, provider); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityMailboxOAuthApp, nil, map[string]string{"provider": string(provider)}, nil)
	c.Status(http.StatusNoContent)
}
