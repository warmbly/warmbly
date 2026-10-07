package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/repository"
	"net/http"
)

func (h *Handler) InternalWarmupActions(c *gin.Context) {
	var req struct {
		MailboxID uuid.UUID `json:"mailbox_id"`
		WorkerID  uuid.UUID `json:"worker_id"`
		Actions   []string  `json:"actions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.MailboxID == uuid.Nil || req.WorkerID == uuid.Nil || len(req.Actions) > 16 {
		c.Status(http.StatusBadRequest)
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
