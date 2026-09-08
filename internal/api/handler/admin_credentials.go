// Admin endpoints binding a worker to a reusable worker profile.
//
//	/admin/workers/:id/profile assign / unassign a profile to a worker
//	/admin/workers/:id/apply   re-write env + restart for a single worker

package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// worker → profile binding

type assignProfileBody struct {
	ProfileID *string `json:"profile_id"`
}

func (h *Handler) AdminAssignWorkerProfile(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body assignProfileBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	var pid *uuid.UUID
	if body.ProfileID != nil && *body.ProfileID != "" {
		parsed, err := uuid.Parse(*body.ProfileID)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid profile_id"))
			return
		}
		pid = &parsed
	}
	if err := h.WorkerRepo.AssignWorkerProfile(c.Request.Context(), id, pid); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	meta := map[string]string{}
	if pid != nil {
		meta["profile_id"] = pid.String()
	} else {
		meta["profile_id"] = "(none)"
	}
	h.audit(c, models.AuditActionAssign, models.AuditEntityWorker, &id, meta)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AdminApplyWorkerConfig re-writes env and restarts the service for ONE
// worker. Useful when the admin wants to pick up the latest profile values
// without re-running the full installer.
func (h *Handler) AdminApplyWorkerConfig(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if err := h.WorkerOrchestrator.ApplyConfig(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionApply, models.AuditEntityWorker, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func parseUUID(c *gin.Context, param string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(param))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid "+param))
		return uuid.Nil, false
	}
	return id, true
}
