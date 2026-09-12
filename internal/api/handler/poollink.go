package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Cloud side of the self-hosted warmup pool link.

func (h *Handler) poolLinkReady(c *gin.Context) bool {
	if h.PoolLinkService == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "pool link is not enabled on this instance"))
		return false
	}
	return true
}

// PoolLinkStart opens a handshake for a self-hosted instance.
func (h *Handler) PoolLinkStart(c *gin.Context) {
	if !h.poolLinkReady(c) {
		return
	}
	var req models.PoolLinkStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	res, xerr := h.PoolLinkService.StartCode(c.Request.Context(), req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, res)
}

// PoolLinkPoll is polled by the instance until a member approves the code.
func (h *Handler) PoolLinkPoll(c *gin.Context) {
	if !h.poolLinkReady(c) {
		return
	}
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.DeviceCode == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "device_code is required"))
		return
	}
	res, xerr := h.PoolLinkService.PollCode(c.Request.Context(), req.DeviceCode)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, res)
}

// PoolLinkDescribeCode shows the approving member what they are linking.
func (h *Handler) PoolLinkDescribeCode(c *gin.Context) {
	if !h.poolLinkReady(c) {
		return
	}
	code, xerr := h.PoolLinkService.DescribeCode(c.Request.Context(), c.Param("code"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, code)
}

// PoolLinkApproveCode binds the code to the workspace named in the body, not the session's.
func (h *Handler) PoolLinkApproveCode(c *gin.Context) {
	if !h.poolLinkReady(c) {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var req struct {
		OrganizationID string `json:"organization_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	orgID, err := uuid.Parse(req.OrganizationID)
	if err != nil {
		if sessionOrg := middleware.GetOrganizationID(c); sessionOrg != nil {
			orgID = *sessionOrg
		} else {
			errx.JSON(c, errx.ErrNoOrganization)
			return
		}
	}
	allowed, xerr := h.OrganizationService.HasPermission(c.Request.Context(), orgID, userID, models.PermManageSettings)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if !allowed {
		errx.JSON(c, errx.New(errx.Forbidden, "manage settings permission is required to link an instance"))
		return
	}
	inst, xerr := h.PoolLinkService.ApproveCode(c.Request.Context(), c.Param("code"), orgID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.AuditService.LogAction(c.Request.Context(), orgID, userID, models.AuditActionConnect, models.AuditEntityPoolLink, &inst.ID,
		c.ClientIP(), c.Request.UserAgent(), nil, map[string]string{"instance_name": inst.Name})
	c.JSON(http.StatusOK, inst)
}

func (h *Handler) PoolLinkDenyCode(c *gin.Context) {
	if !h.poolLinkReady(c) {
		return
	}
	if xerr := h.PoolLinkService.DenyCode(c.Request.Context(), c.Param("code")); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}

// PoolLinkListInstances lists the workspace's linked instances.
func (h *Handler) PoolLinkListInstances(c *gin.Context) {
	if !h.poolLinkReady(c) {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	list, xerr := h.PoolLinkService.ListInstances(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	plan, xerr := h.PoolLinkService.Plan(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "plan": plan})
}

// PoolLinkRevokeInstance ends a link and removes its mailboxes.
func (h *Handler) PoolLinkRevokeInstance(c *gin.Context) {
	if !h.poolLinkReady(c) {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	if xerr := h.PoolLinkService.RevokeInstance(c.Request.Context(), *orgID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDisconnect, models.AuditEntityPoolLink, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

// Instance-token routes.

func (h *Handler) PoolLinkInstanceInfo(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	info, xerr := h.PoolLinkService.InstanceInfo(c.Request.Context(), inst)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, info)
}

func (h *Handler) PoolLinkInstanceDisconnect(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if xerr := h.PoolLinkService.RevokeInstance(c.Request.Context(), inst.OrganizationID, inst.ID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) PoolLinkInstanceMailboxes(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	list, xerr := h.PoolLinkService.ListMailboxes(c.Request.Context(), inst)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) PoolLinkEnroll(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var req models.PoolLinkEnrollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	state, xerr := h.PoolLinkService.Enroll(c.Request.Context(), inst, req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, state)
}

func poolLinkRemoteID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("remoteId"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) PoolLinkGetMailbox(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, ok := poolLinkRemoteID(c)
	if !ok {
		return
	}
	state, xerr := h.PoolLinkService.GetMailbox(c.Request.Context(), inst, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, state)
}

func (h *Handler) PoolLinkPatchMailbox(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, ok := poolLinkRemoteID(c)
	if !ok {
		return
	}
	var patch models.PoolLinkMailboxPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	state, xerr := h.PoolLinkService.PatchMailbox(c.Request.Context(), inst, id, patch)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, state)
}

func (h *Handler) PoolLinkUnenroll(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, ok := poolLinkRemoteID(c)
	if !ok {
		return
	}
	if xerr := h.PoolLinkService.Unenroll(c.Request.Context(), inst, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}

// Cloud-managed mailboxes for a linked instance.

func (h *Handler) PoolLinkOAuthStart(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var req models.PoolLinkOAuthStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	res, xerr := h.PoolLinkService.StartOAuth(c.Request.Context(), inst, req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, res)
}

func (h *Handler) PoolLinkOAuthFinish(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var req models.PoolLinkOAuthFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	state, xerr := h.PoolLinkService.FinishOAuth(c.Request.Context(), inst, req.Session)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, state)
}

func (h *Handler) PoolLinkAccessToken(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	remoteID, ok := poolLinkRemoteID(c)
	if !ok {
		return
	}
	tok, xerr := h.PoolLinkService.AccessToken(c.Request.Context(), inst, remoteID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, tok)
}

func (h *Handler) PoolLinkWorkspaceMailboxes(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	list, xerr := h.PoolLinkService.ListWorkspaceMailboxes(c.Request.Context(), inst)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) PoolLinkAdopt(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var req models.PoolLinkAdoptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	state, xerr := h.PoolLinkService.Adopt(c.Request.Context(), inst, req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, state)
}

func (h *Handler) PoolLinkVerifyWarmupToken(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	remoteID, ok := poolLinkRemoteID(c)
	if !ok {
		return
	}
	token, err := uuid.Parse(c.Param("token"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"valid": false})
		return
	}
	valid, xerr := h.PoolLinkService.VerifyWarmupToken(c.Request.Context(), inst, remoteID, token)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": valid})
}

// PoolLinkVerifyWarmupDelivery answers for warmup mail that reached the mailbox
// without its verify header, which the instance cannot recognise on its own.
func (h *Handler) PoolLinkVerifyWarmupDelivery(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	remoteID, ok := poolLinkRemoteID(c)
	if !ok {
		return
	}
	var q models.PoolLinkWarmupDeliveryQuery
	if err := c.ShouldBindJSON(&q); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	valid, xerr := h.PoolLinkService.VerifyWarmupDelivery(c.Request.Context(), inst, remoteID, q)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": valid})
}
