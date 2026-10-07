package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Self-hosted side of the warmup pool link: Settings > Warmbly Cloud.

func (h *Handler) cloudLinkReady(c *gin.Context) bool {
	if h.CloudLinkService == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "cloud link is not enabled on this instance"))
		return false
	}
	return true
}

func (h *Handler) CloudLinkStatus(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	st, xerr := h.CloudLinkService.Status(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, st)
}

func (h *Handler) CloudLinkConnectStart(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var req struct {
		CloudURL string `json:"cloud_url"`
	}
	_ = c.ShouldBindJSON(&req)
	p, xerr := h.CloudLinkService.StartConnect(c.Request.Context(), *orgID, userID, req.CloudURL)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (h *Handler) CloudLinkConnectPoll(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	res, xerr := h.CloudLinkService.PollConnect(c.Request.Context(), *orgID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if res.Status == models.PoolLinkCodeApproved && res.Link != nil {
		h.auditOrg(c, models.AuditActionConnect, models.AuditEntityCloudLink, &res.Link.InstanceID, nil, map[string]string{"organization_name": res.Link.OrganizationName})
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) CloudLinkDisconnect(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	if xerr := h.CloudLinkService.Disconnect(c.Request.Context(), *orgID, c.Query("legacy") == "true"); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDisconnect, models.AuditEntityCloudLink, nil, nil, nil)
	c.Status(http.StatusNoContent)
}

func (h *Handler) CloudLinkMailboxes(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	rows, xerr := h.CloudLinkService.ListMailboxes(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func cloudLinkAccountID(c *gin.Context) (uuid.UUID, *uuid.UUID, bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return uuid.Nil, nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return uuid.Nil, nil, false
	}
	return id, orgID, true
}

func (h *Handler) CloudLinkEnroll(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	id, orgID, ok := cloudLinkAccountID(c)
	if !ok {
		return
	}
	row, xerr := h.CloudLinkService.Enroll(c.Request.Context(), *orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionStart, models.AuditEntityCloudLink, &id, nil, map[string]string{"email": row.Email})
	c.JSON(http.StatusOK, row)
}

func (h *Handler) CloudLinkUnenroll(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	id, orgID, ok := cloudLinkAccountID(c)
	if !ok {
		return
	}
	if xerr := h.CloudLinkService.Unenroll(c.Request.Context(), *orgID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionStop, models.AuditEntityCloudLink, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

func (h *Handler) CloudLinkPause(c *gin.Context) {
	h.cloudLinkLifecycle(c, "pause", models.AuditActionPause)
}
func (h *Handler) CloudLinkResume(c *gin.Context) {
	h.cloudLinkLifecycle(c, "resume", models.AuditActionResume)
}

func (h *Handler) CloudLinkParticipation(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	id, orgID, ok := cloudLinkAccountID(c)
	if !ok {
		return
	}
	var p models.DiagnosticParticipation
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	row, xerr := h.CloudLinkService.SetParticipation(c.Request.Context(), *orgID, id, p)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCloudLink, &id, nil, nil)
	c.JSON(http.StatusOK, row)
}

func (h *Handler) cloudLinkLifecycle(c *gin.Context, action string, audit models.AuditAction) {
	if !h.cloudLinkReady(c) {
		return
	}
	id, orgID, ok := cloudLinkAccountID(c)
	if !ok {
		return
	}
	row, xerr := h.CloudLinkService.SetLifecycle(c.Request.Context(), *orgID, id, action)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, audit, models.AuditEntityCloudLink, &id, nil, nil)
	c.JSON(http.StatusOK, row)
}

// Cloud-managed mailboxes: sign in through Warmbly Cloud, adopt workspace mailboxes.

func (h *Handler) CloudLinkOAuthStart(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	userID, err := middleware.GetUserUUID(c)
	if orgID == nil || err != nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	var req struct {
		Provider models.InboxProvider `json:"provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	res, xerr := h.CloudLinkService.StartOAuth(c.Request.Context(), *orgID, userID, req.Provider)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, res)
}

func (h *Handler) CloudLinkOAuthFinish(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	userID, err := middleware.GetUserUUID(c)
	if orgID == nil || err != nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	var req struct {
		Session string `json:"session"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	acc, xerr := h.CloudLinkService.FinishOAuth(c.Request.Context(), *orgID, userID, req.Session)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityEmailAccount, &acc.ID, nil, map[string]string{"email": acc.Email, "via": "warmbly_cloud"})
	c.JSON(http.StatusCreated, acc)
}

func (h *Handler) CloudLinkWorkspaceMailboxes(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	list, xerr := h.CloudLinkService.ListWorkspaceMailboxes(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *Handler) CloudLinkAdopt(c *gin.Context) {
	if !h.cloudLinkReady(c) {
		return
	}
	id, orgID, ok := cloudLinkAccountID(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	acc, xerr := h.CloudLinkService.Adopt(c.Request.Context(), *orgID, userID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityEmailAccount, &acc.ID, nil, map[string]string{"email": acc.Email, "via": "warmbly_cloud"})
	c.JSON(http.StatusCreated, acc)
}

// InternalCloudLinkToken is the worker's credential for a managed mailbox.
//
//	GET /api/v1/internal/cloud-link/token/:id -> 200 {"access_token","expires_at"} | 4xx with the cloud's reason
func (h *Handler) InternalCloudLinkToken(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	// A mailbox under an administrator's grant is minted for here, not by the cloud.
	if h.DelegationService != nil {
		tok, handled, xerr := h.DelegationService.AccessToken(c.Request.Context(), id)
		if handled {
			if xerr != nil {
				errx.JSON(c, xerr)
				return
			}
			c.Header("Cache-Control", "no-store")
			c.JSON(http.StatusOK, tok)
			return
		}
	}
	if h.CloudLinkService == nil {
		errx.JSON(c, errx.ErrNotFound)
		return
	}
	tok, xerr := h.CloudLinkService.AccessToken(c.Request.Context(), id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, tok)
}
