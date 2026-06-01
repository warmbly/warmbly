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
