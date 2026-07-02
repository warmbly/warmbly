package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) CreateSequence(c *gin.Context) {
	userID := middleware.GetUserID(c)

	id := c.Param("id")

	resp, err := h.SequenceService.Create(c.Request.Context(), userID, id)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntitySequence, &resp.ID, nil, map[string]string{"campaign_id": id})

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetSequences(c *gin.Context) {
	userID := middleware.GetUserID(c)

	id := c.Param("id")

	resp, err := h.SequenceService.Get(c.Request.Context(), userID, id)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateSequence(c *gin.Context) {
	userID := middleware.GetUserID(c)

	id := c.Param("id")
	sequenceID := c.Param("sid")

	var data models.UpdateSequence

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, err)
		return
	}

	resp, err := h.SequenceService.Update(c.Request.Context(), userID, id, sequenceID, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySequence, &resp.ID, nil, map[string]string{"campaign_id": id})

	c.JSON(http.StatusOK, resp)
}

// PatchSequenceLayout persists only the canvas coordinates of a campaign's
// steps so a teammate's arrangement "sticks" across visits. Like the automation
// layout endpoint it is written continuously as steps are dragged (the live
// cursor/drag stream is the realtime half), so it is deliberately NOT audited
// and does not bump updated_at: a reposition is cosmetic and must not spam the
// audit log or read as a content change. Retries are naturally safe (positions
// are last-write-wins), so no Idempotency-Key is required.
func (h *Handler) PatchSequenceLayout(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id := c.Param("id")

	var data models.SequenceLayout
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, err)
		return
	}
	if len(data.Positions) > 1000 {
		errx.JSON(c, errx.New(errx.BadRequest, "too many positions"))
		return
	}
	if err := h.SequenceService.UpdateLayout(c.Request.Context(), userID, id, data.Positions); err != nil {
		errx.Handle(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) DeleteSequence(c *gin.Context) {
	userID := middleware.GetUserID(c)

	id := c.Param("id")
	sequenceID := c.Param("sid")

	if err := h.SequenceService.Delete(c.Request.Context(), userID, id, sequenceID); err != nil {
		errx.Handle(c, err)
		return
	}

	if sid, perr := uuid.Parse(sequenceID); perr == nil {
		h.auditOrg(c, models.AuditActionDelete, models.AuditEntitySequence, &sid, nil, map[string]string{"campaign_id": id})
	} else {
		h.auditOrg(c, models.AuditActionDelete, models.AuditEntitySequence, nil, nil, map[string]string{"campaign_id": id})
	}

	c.Status(http.StatusOK)
}
