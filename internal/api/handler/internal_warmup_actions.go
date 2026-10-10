package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"net/http"
)

func (h *Handler) InternalWarmupActions(c *gin.Context) {
	var req models.WarmupActionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.MailboxID == uuid.Nil || req.WorkerID == uuid.Nil || len(req.Actions) > 16 {
		c.Status(http.StatusBadRequest)
		return
	}
	if req.FilingID != "" {
		if id, err := uuid.Parse(req.FilingID); err != nil || id == uuid.Nil {
			c.Status(http.StatusBadRequest)
			return
		}
	}
	if r, ok := h.WarmupDispatch.(repository.WarmupActionRecoveryAdmission); ok {
		decision, err := r.AdmitWarmupAction(c.Request.Context(), req)
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.JSON(http.StatusOK, decision)
		return
	}
	r, ok := h.WarmupDispatch.(repository.WarmupActionAdmission)
	if !ok {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	actions, err := r.PermittedWarmupActions(c.Request.Context(), req.MailboxID, req.WorkerID, req.Actions)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.JSON(http.StatusOK, gin.H{"actions": actions})
}
