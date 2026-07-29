// AI contact research endpoints. A run researches one contact on the public web
// and saves cited findings; it charges 2 credits per run (billable even when it
// finds nothing). Sync runs execute in the request; the batch endpoint queues
// and drains in the background. Gated by the AI_RESEARCH scope (JWT callers by
// the matching contact permission).
package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/errx"
)

// aiActorInvocation builds a minimal invocation for AI feature endpoints. The
// route middleware already gated access; the research tools (search_web,
// fetch_url) require no permission, so only identity is needed here.
func (h *Handler) aiActorInvocation(c *gin.Context) (aitools.Invocation, *errx.Error) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		return aitools.Invocation{}, errx.New(errx.BadRequest, "no organization selected")
	}
	userID, _ := middleware.GetUserUUID(c) // uuid.Nil for API-key callers
	return aitools.Invocation{
		OrgID:     *orgID,
		UserID:    userID,
		IsAPIKey:  middleware.GetAPIKeyPermissions(c) != 0,
		APIPerms:  middleware.GetAPIKeyPermissions(c),
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}, nil
}

// ResearchContact — POST /contacts/:id/research (sync)
func (h *Handler) ResearchContact(c *gin.Context) {
	if h.ResearchService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "AI research is not configured"))
		return
	}
	inv, xerr := h.aiActorInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid contact id"))
		return
	}
	var req struct {
		Objective string `json:"objective"`
	}
	_ = c.ShouldBindJSON(&req)

	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	run, rerr := h.ResearchService.RunResearch(c.Request.Context(), inv, contactID, req.Objective, idemKey)
	if rerr != nil {
		errx.JSON(c, rerr)
		return
	}
	c.JSON(http.StatusOK, run)
}

// ListContactResearch — GET /contacts/:id/research
func (h *Handler) ListContactResearch(c *gin.Context) {
	if h.ResearchService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "AI research is not configured"))
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid contact id"))
		return
	}
	limit, xerr := parseCreditLimit(c.Query("limit"), 20, 100)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	runs, rerr := h.ResearchService.ListRuns(c.Request.Context(), *orgID, contactID, limit)
	if rerr != nil {
		errx.JSON(c, rerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": runs})
}

// BatchResearch — POST /contacts/research/batch
func (h *Handler) BatchResearch(c *gin.Context) {
	if h.ResearchService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "AI research is not configured"))
		return
	}
	inv, xerr := h.aiActorInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	var req struct {
		ContactIDs []uuid.UUID `json:"contact_ids" binding:"required"`
		Objective  string      `json:"objective"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	queued, rerr := h.ResearchService.Batch(c.Request.Context(), inv, req.ContactIDs, req.Objective)
	if rerr != nil {
		errx.JSON(c, rerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"queued": queued})
}
