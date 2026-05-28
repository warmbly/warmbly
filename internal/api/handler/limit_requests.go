package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// User-facing endpoints under /v1/organizations/:orgId/limit-requests.

// SubmitLimitIncreaseRequest is POST /v1/organizations/:orgId/limit-requests.
func (h *Handler) SubmitLimitIncreaseRequest(c *gin.Context) {
	session := middleware.GetSession(c)
	if session == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization ID"))
		return
	}
	var req models.CreateLimitIncreaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	lr, xerr := h.OrganizationService.SubmitLimitIncreaseRequest(c.Request.Context(), orgID, session.UserID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, lr)
}

// ListOrgLimitRequests is GET /v1/organizations/:orgId/limit-requests.
func (h *Handler) ListOrgLimitRequests(c *gin.Context) {
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization ID"))
		return
	}
	rows, xerr := h.OrganizationService.ListLimitRequestsForOrg(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// CancelLimitRequest is DELETE /v1/limit-requests/:id (submitter only).
func (h *Handler) CancelLimitRequest(c *gin.Context) {
	session := middleware.GetSession(c)
	if session == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request ID"))
		return
	}
	if xerr := h.OrganizationService.CancelLimitRequest(c.Request.Context(), id, session.UserID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}

// Admin-facing endpoints under /admin/limit-requests.

// AdminListLimitRequests is GET /admin/limit-requests?status=pending&limit=50.
func (h *Handler) AdminListLimitRequests(c *gin.Context) {
	status := c.DefaultQuery("status", "pending")
	limit, _ := strconv.Atoi(c.Query("limit"))
	rows, xerr := h.OrganizationService.AdminListLimitRequests(c.Request.Context(), status, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// AdminApproveLimitRequest is POST /admin/limit-requests/:id/approve.
func (h *Handler) AdminApproveLimitRequest(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request ID"))
		return
	}
	var body models.ReviewLimitRequestBody
	_ = c.ShouldBindJSON(&body) // notes are optional

	lr, xerr := h.OrganizationService.ApproveLimitRequest(c.Request.Context(), id, *adminID, body.Notes)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.AdminService.LogAdminAction(
		c.Request.Context(),
		*adminID,
		"approve_limit_request",
		"limit_request",
		&id,
		map[string]any{
			"field":     lr.Field,
			"requested": lr.Requested,
			"notes":     body.Notes,
		},
		c.ClientIP(),
		c.Request.UserAgent(),
	)
	c.JSON(http.StatusOK, lr)
}

// AdminRejectLimitRequest is POST /admin/limit-requests/:id/reject.
func (h *Handler) AdminRejectLimitRequest(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request ID"))
		return
	}
	var body models.ReviewLimitRequestBody
	if err := c.ShouldBindJSON(&body); err != nil || body.Notes == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "review notes are required when rejecting"))
		return
	}

	lr, xerr := h.OrganizationService.RejectLimitRequest(c.Request.Context(), id, *adminID, body.Notes)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.AdminService.LogAdminAction(
		c.Request.Context(),
		*adminID,
		"reject_limit_request",
		"limit_request",
		&id,
		map[string]any{
			"field":     lr.Field,
			"requested": lr.Requested,
			"notes":     body.Notes,
		},
		c.ClientIP(),
		c.Request.UserAgent(),
	)
	c.JSON(http.StatusOK, lr)
}
