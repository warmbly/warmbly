package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ---------- Organization danger zone ----------

// GetOrganizationDangerZone returns the danger-zone status (any pending
// deletion plus the confirmation phrase the client should display).
func (h *Handler) GetOrganizationDangerZone(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	status, xerr := h.DangerZoneService.GetOrganizationStatus(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, status)
}

// ScheduleOrganizationDeletion schedules the current organization for a
// delayed hard delete. Owner-only and requires the user to type the org
// name as confirmation.
func (h *Handler) ScheduleOrganizationDeletion(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req models.ScheduleDeletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	d, xerr := h.DangerZoneService.ScheduleOrganizationDeletion(c.Request.Context(), *orgID, userID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityOrganization, orgID, nil, map[string]string{"scheduled": "true"})

	c.JSON(http.StatusAccepted, d)
}

// CancelOrganizationDeletion cancels a pending org deletion.
func (h *Handler) CancelOrganizationDeletion(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req models.CancelDeletionRequest
	_ = c.ShouldBindJSON(&req) // body optional

	if xerr := h.DangerZoneService.CancelOrganizationDeletion(c.Request.Context(), *orgID, userID, &req); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deletion cancelled"})
}

// ---------- Account danger zone ----------

// GetAccountDangerZone returns the user's account danger-zone status.
func (h *Handler) GetAccountDangerZone(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	status, xerr := h.DangerZoneService.GetUserStatus(c.Request.Context(), userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, status)
}

// ScheduleAccountDeletion schedules the caller's own account for delayed
// hard delete. Requires the user to type their email as confirmation.
func (h *Handler) ScheduleAccountDeletion(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	var req models.ScheduleDeletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	d, xerr := h.DangerZoneService.ScheduleUserDeletion(c.Request.Context(), userID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityUser, &userID, nil, map[string]string{"scheduled": "true"})

	c.JSON(http.StatusAccepted, d)
}

// CancelAccountDeletion cancels a pending account deletion.
func (h *Handler) CancelAccountDeletion(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	var req models.CancelDeletionRequest
	_ = c.ShouldBindJSON(&req)

	if xerr := h.DangerZoneService.CancelUserDeletion(c.Request.Context(), userID, &req); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deletion cancelled"})
}
