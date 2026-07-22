package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/warmupcontent"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// AdminCancelWarmupBatch cancels an in-flight batch generation job (OpenAI + the
// local job row).
func (h *Handler) AdminCancelWarmupBatch(c *gin.Context) {
	if h.WarmupContentService == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "warmup generation is not configured"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	if err := h.WarmupContentService.CancelBatch(c.Request.Context(), id); err != nil {
		if errors.Is(err, warmupcontent.ErrNotConfigured) {
			errx.JSON(c, errx.New(errx.BadRequest, "warmup AI generation is not configured (set AI_PROVIDER=openai and AI_API_KEY)"))
			return
		}
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.audit(c, models.AuditActionUpdate, warmupContentEntity, &id, map[string]string{"action": "cancel_batch"})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
