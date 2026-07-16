package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/warmupcontent"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// submitBatchRequest is the admin payload for an async Batch API generation run.
// Admins control every parameter; sensible service-side defaults apply when a
// field is omitted (pool_type=premium, count=100, completion_window=24h, model
// and max_messages from the generation settings).
type submitBatchRequest struct {
	PoolType         string   `json:"pool_type"`
	Segment          string   `json:"segment"`
	Theme            string   `json:"theme"`
	Themes           []string `json:"themes"`
	Model            string   `json:"model"`
	Count            int      `json:"count"`
	MaxMessages      int      `json:"max_messages"`
	CompletionWindow string   `json:"completion_window"`
}

// AdminSubmitWarmupBatch submits an async OpenAI Batch API generation run.
//
// The Batch API is ~50% cheaper than synchronous generation and processes
// asynchronously (up to the completion window, typically 24h). Results are
// ingested into the content bank by the batch poller once OpenAI finishes.
//
// When a non-empty `themes` array is supplied, one batch job is submitted per
// theme (each gets `count` threads of that theme); otherwise a single batch
// runs with the optional pinned `theme` (empty = rotate the default theme set).
func (h *Handler) AdminSubmitWarmupBatch(c *gin.Context) {
	if h.WarmupContentService == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "warmup generation is not configured"))
		return
	}
	var req submitBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	adminID := middleware.GetAdminUserID(c)

	// Normalise the theme set: explicit `themes` wins; otherwise fall back to the
	// single pinned `theme` (which may be empty → service rotates defaults).
	themes := make([]string, 0, len(req.Themes))
	for _, t := range req.Themes {
		if t != "" {
			themes = append(themes, t)
		}
	}
	if len(themes) == 0 {
		themes = []string{req.Theme}
	}

	jobIDs := make([]uuid.UUID, 0, len(themes))
	for _, theme := range themes {
		jobID, err := h.WarmupContentService.GenerateBatch(c.Request.Context(), warmupcontent.GenerateRequest{
			RequestedBy:      adminID,
			Trigger:          "manual",
			PoolType:         req.PoolType,
			Segment:          req.Segment,
			Theme:            theme,
			Model:            req.Model,
			Count:            req.Count,
			MaxMessages:      req.MaxMessages,
			CompletionWindow: req.CompletionWindow,
		})
		if err != nil {
			if errors.Is(err, warmupcontent.ErrNotConfigured) {
				errx.JSON(c, errx.New(errx.BadRequest, "warmup AI generation is not configured (set AI_PROVIDER=openai and AI_API_KEY)"))
				return
			}
			errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
			return
		}
		jobIDs = append(jobIDs, jobID)
	}

	for _, id := range jobIDs {
		jid := id
		h.audit(c, models.AuditActionCreate, warmupContentEntity, &jid, map[string]string{
			"mode":      "batch",
			"pool_type": req.PoolType,
			"segment":   req.Segment,
			"count":     strconv.Itoa(req.Count),
		})
	}

	c.JSON(http.StatusOK, gin.H{"job_ids": jobIDs})
}

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
