package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) CreateCampaign(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var data models.CreateCampaign

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.CampaignService.Create(c.Request.Context(), userID, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetCampaign(c *gin.Context) {
	userID := middleware.GetUserID(c)

	id := c.Param("id")

	resp, err := h.CampaignService.Get(c.Request.Context(), userID, id)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) SearchCampaigns(c *gin.Context) {
	userID := middleware.GetUserID(c)

	query := c.Query("q")
	cursor := c.Query("cursor")
	folder := c.Query("folder")
	limit := c.Query("limit")

	resp, err := h.CampaignService.Search(c.Request.Context(), userID, query, cursor, folder, limit)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateCampaign(c *gin.Context) {
	userID := middleware.GetUserID(c)

	id := c.Param("id")

	var data models.UpdateCampaign

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.CampaignService.Update(c.Request.Context(), userID, id, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) DeleteCampaign(c *gin.Context) {
	userID := middleware.GetUserID(c)

	id := c.Param("id")

	if err := h.CampaignService.Delete(c.Request.Context(), userID, id); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
