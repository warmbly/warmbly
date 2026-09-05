package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Device-code sign-in for the `warmbly` CLI. The public half (start, poll) is
// what the CLI calls; the session half is the browser approval screen.

func (h *Handler) cliAuthReady(c *gin.Context) bool {
	if h.CLIAuthService == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "CLI sign-in is not enabled on this instance"))
		return false
	}
	return true
}

// CLIAuthStart opens a handshake for a CLI that holds no key yet.
func (h *Handler) CLIAuthStart(c *gin.Context) {
	if !h.cliAuthReady(c) {
		return
	}
	var req models.CLIAuthStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	res, xerr := h.CLIAuthService.StartCode(c.Request.Context(), req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, res)
}

// CLIAuthPoll is polled by the CLI until a member approves the code. The key is
// handed out exactly once, on the poll that follows approval.
func (h *Handler) CLIAuthPoll(c *gin.Context) {
	if !h.cliAuthReady(c) {
		return
	}
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.DeviceCode == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "device_code is required"))
		return
	}
	res, xerr := h.CLIAuthService.PollCode(c.Request.Context(), req.DeviceCode)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, res)
}

// CLIAuthDescribeCode shows the approving member what they are authorizing.
func (h *Handler) CLIAuthDescribeCode(c *gin.Context) {
	if !h.cliAuthReady(c) {
		return
	}
	code, xerr := h.CLIAuthService.DescribeCode(c.Request.Context(), c.Param("code"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, code)
}

// CLIAuthApproveCode mints the key into the workspace named in the body, not
// the session's, because a member with several workspaces picks on the screen.
func (h *Handler) CLIAuthApproveCode(c *gin.Context) {
	if !h.cliAuthReady(c) {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var req models.CLIAuthApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	orgID, perr := uuid.Parse(req.OrganizationID)
	if perr != nil {
		if sessionOrg := middleware.GetOrganizationID(c); sessionOrg != nil {
			orgID = *sessionOrg
		} else {
			errx.JSON(c, errx.ErrNoOrganization)
			return
		}
	}

	code, xerr := h.CLIAuthService.ApproveCode(c.Request.Context(), c.Param("code"), orgID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	// Logged against the org that was picked on screen, which is not always the
	// session's, so this cannot go through auditOrg.
	h.AuditService.LogAction(c.Request.Context(), orgID, userID, models.AuditActionCreate, models.AuditEntityAPIKey, code.APIKeyID,
		c.ClientIP(), c.Request.UserAgent(), nil, map[string]string{"source": "cli", "client": code.ClientName, "hostname": code.Hostname})
	c.JSON(http.StatusOK, code)
}

// CLIAuthDenyCode declines the request. Deliberately not audited: nothing was
// created, and a denial is not a change to the workspace.
func (h *Handler) CLIAuthDenyCode(c *gin.Context) {
	if !h.cliAuthReady(c) {
		return
	}
	if xerr := h.CLIAuthService.DenyCode(c.Request.Context(), c.Param("code")); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
