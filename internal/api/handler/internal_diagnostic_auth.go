package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (h *Handler) InternalDiagnosticAuth(c *gin.Context) {
	var request models.DiagnosticAuthRequest
	if c.ShouldBindJSON(&request) != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if !h.nodeWorkerMailbox(c, "worker.diagnostic_auth", http.StatusForbidden, request.WorkerID, request.MailboxID) {
		return
	}
	authority, ok := h.WarmupDispatch.(repository.DiagnosticAuthAuthority)
	if !ok {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	grant, err := authority.DiagnosticAuth(c.Request.Context(), request)
	if err != nil {
		c.Status(http.StatusForbidden)
		return
	}
	c.JSON(http.StatusOK, grant)
}
