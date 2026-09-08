package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	auditActionSyncClearThrottle   models.AuditAction = "sync_clear_throttle"
	auditActionSyncRestartBackfill models.AuditAction = "sync_restart_backfill"
)

// AdminSearchSync is GET /admin/sync: every mailbox's sync governor state.
func (h *Handler) AdminSearchSync(c *gin.Context) {
	if h.AdminSyncRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "sync operations are not available on this instance"))
		return
	}
	var search models.AdminSyncSearch
	if err := c.ShouldBindQuery(&search); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid query parameters"))
		return
	}
	result, err := h.AdminSyncRepo.Search(c.Request.Context(), &search)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrAdminSyncBadCursor):
			errx.JSON(c, errx.New(errx.BadRequest, "invalid cursor"))
		case errors.Is(err, repository.ErrAdminSyncBadState):
			errx.JSON(c, errx.New(errx.BadRequest, "invalid state filter"))
		default:
			errx.JSON(c, errx.New(errx.Internal, "failed to load sync state"))
		}
		return
	}
	c.JSON(http.StatusOK, result)
}

// AdminSyncClearThrottle is POST /admin/sync/:id/clear-throttle.
func (h *Handler) AdminSyncClearThrottle(c *gin.Context) {
	if h.AdminSyncRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "sync operations are not available on this instance"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid mailbox ID"))
		return
	}
	cleared, err := h.AdminSyncRepo.ClearThrottle(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to clear throttle"))
		return
	}
	if !cleared {
		errx.JSON(c, errx.New(errx.NotFound, "mailbox has no sync state or is not throttled"))
		return
	}
	resp := gin.H{"email_id": id, "cleared": true}
	h.reloadSyncedMailbox(c, id, resp)
	h.audit(c, auditActionSyncClearThrottle, models.AuditEntityEmailAccount, &id, map[string]string{
		"reloaded": boolString(resp["reloaded"].(bool)),
	})
	c.JSON(http.StatusOK, resp)
}

// AdminSyncRestartBackfill is POST /admin/sync/:id/restart-backfill.
func (h *Handler) AdminSyncRestartBackfill(c *gin.Context) {
	if h.AdminSyncRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "sync operations are not available on this instance"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid mailbox ID"))
		return
	}
	reset, err := h.AdminSyncRepo.ResetBackfill(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to reset backfill"))
		return
	}
	if !reset {
		errx.JSON(c, errx.New(errx.NotFound, "mailbox has no sync state"))
		return
	}
	resp := gin.H{"email_id": id, "reset": true}
	h.reloadSyncedMailbox(c, id, resp)
	h.audit(c, auditActionSyncRestartBackfill, models.AuditEntityEmailAccount, &id, map[string]string{
		"reloaded": boolString(resp["reloaded"].(bool)),
	})
	c.JSON(http.StatusOK, resp)
}

// reloadSyncedMailbox re-ships the mailbox so the worker's live copy follows
// the platform copy; a failure is reported in the response, not as an error.
func (h *Handler) reloadSyncedMailbox(c *gin.Context, id uuid.UUID, resp gin.H) {
	resp["reloaded"] = false
	if h.EmailService == nil {
		resp["reload_error"] = "email service not configured"
		return
	}
	if err := h.EmailService.LoadAccountOntoWorker(c.Request.Context(), id); err != nil {
		resp["reload_error"] = err.Error()
		return
	}
	resp["reloaded"] = true
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
