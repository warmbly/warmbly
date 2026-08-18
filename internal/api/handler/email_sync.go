package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// emailSyncResponse is GET /emails/:id/sync: where the mailbox's import
// stands, whether fair use is holding it, and the budget it runs under.
type emailSyncResponse struct {
	// State is null until the worker has reported once.
	State  *models.SyncState `json:"state"`
	Policy models.SyncPolicy `json:"policy"`
}

// GetEmailSync reports a mailbox's sync progress and fair-use status.
func (h *Handler) GetEmailSync(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	state, policy, xerr := h.EmailService.GetSyncState(c.Request.Context(), userID.String(), c.Param("id"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, emailSyncResponse{State: state, Policy: policy})
}
