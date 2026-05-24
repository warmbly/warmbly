package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// =====================
// Contact Notes
// =====================

func (h *Handler) CreateContactNote(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.CreateContactNote
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	note, xerr := h.CRMService.CreateNote(c.Request.Context(), *orgID, contactID, userID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusCreated, note)
}

func (h *Handler) ListContactNotes(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	var cursor *uuid.UUID
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		if id, err := uuid.Parse(cursorStr); err == nil {
			cursor = &id
		}
	}

	result, xerr := h.CRMService.ListNotes(c.Request.Context(), *orgID, contactID, limit, cursor)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *Handler) UpdateContactNote(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	noteID, err := uuid.Parse(c.Param("noteId"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.UpdateContactNote
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	note, xerr := h.CRMService.UpdateNote(c.Request.Context(), *orgID, noteID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, note)
}

func (h *Handler) DeleteContactNote(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	noteID, err := uuid.Parse(c.Param("noteId"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	xerr := h.CRMService.DeleteNote(c.Request.Context(), *orgID, noteID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}

// =====================
// Contact Activities
// =====================

func (h *Handler) ListContactActivities(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	var cursor *uuid.UUID
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		if id, err := uuid.Parse(cursorStr); err == nil {
			cursor = &id
		}
	}

	result, xerr := h.CRMService.ListActivities(c.Request.Context(), *orgID, contactID, limit, cursor)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// =====================
// Pipelines
// =====================

func (h *Handler) CreatePipeline(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var data models.CreatePipeline
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	pipeline, xerr := h.CRMService.CreatePipeline(c.Request.Context(), *orgID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusCreated, pipeline)
}

func (h *Handler) ListPipelines(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	pipelines, xerr := h.CRMService.ListPipelines(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, pipelines)
}

func (h *Handler) GetPipeline(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	pipeline, xerr := h.CRMService.GetPipeline(c.Request.Context(), *orgID, pipelineID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, pipeline)
}

func (h *Handler) UpdatePipeline(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.UpdatePipeline
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	pipeline, xerr := h.CRMService.UpdatePipeline(c.Request.Context(), *orgID, pipelineID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, pipeline)
}

func (h *Handler) DeletePipeline(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	xerr := h.CRMService.DeletePipeline(c.Request.Context(), *orgID, pipelineID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}

// =====================
// Pipeline Stages
// =====================

func (h *Handler) CreateStage(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.CreatePipelineStage
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	stage, xerr := h.CRMService.CreateStage(c.Request.Context(), pipelineID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusCreated, stage)
}

func (h *Handler) UpdateStage(c *gin.Context) {
	stageID, err := uuid.Parse(c.Param("stageId"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.UpdatePipelineStage
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	stage, xerr := h.CRMService.UpdateStage(c.Request.Context(), stageID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, stage)
}

func (h *Handler) DeleteStage(c *gin.Context) {
	stageID, err := uuid.Parse(c.Param("stageId"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	xerr := h.CRMService.DeleteStage(c.Request.Context(), stageID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}

// =====================
// Deals
// =====================

func (h *Handler) CreateDeal(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var data models.CreateDeal
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	deal, xerr := h.CRMService.CreateDeal(c.Request.Context(), *orgID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusCreated, deal)
}

func (h *Handler) ListDeals(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var pipelineID, stageID *uuid.UUID
	if s := c.Query("pipeline_id"); s != "" {
		if id, err := uuid.Parse(s); err == nil {
			pipelineID = &id
		}
	}
	if s := c.Query("stage_id"); s != "" {
		if id, err := uuid.Parse(s); err == nil {
			stageID = &id
		}
	}
	var status *string
	if s := c.Query("status"); s != "" {
		status = &s
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	var cursor *uuid.UUID
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		if id, err := uuid.Parse(cursorStr); err == nil {
			cursor = &id
		}
	}

	result, xerr := h.CRMService.ListDeals(c.Request.Context(), *orgID, pipelineID, stageID, status, limit, cursor)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetDeal(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	dealID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	deal, xerr := h.CRMService.GetDeal(c.Request.Context(), *orgID, dealID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, deal)
}

func (h *Handler) UpdateDeal(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	dealID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.UpdateDeal
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	userID, _ := middleware.GetUserUUID(c)
	deal, xerr := h.CRMService.UpdateDeal(c.Request.Context(), *orgID, dealID, &userID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, deal)
}

func (h *Handler) DeleteDeal(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	dealID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	xerr := h.CRMService.DeleteDeal(c.Request.Context(), *orgID, dealID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) GetDealsByContact(c *gin.Context) {
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	deals, xerr := h.CRMService.GetDealsByContact(c.Request.Context(), contactID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, deals)
}

// =====================
// CRM Tasks
// =====================

func (h *Handler) CreateCRMTask(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}

	var data models.CreateCRMTask
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	task, xerr := h.CRMService.CreateCRMTask(c.Request.Context(), *orgID, userID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusCreated, task)
}

func (h *Handler) ListCRMTasks(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var contactID, dealID, assignedTo *uuid.UUID
	if s := c.Query("contact_id"); s != "" {
		if id, err := uuid.Parse(s); err == nil {
			contactID = &id
		}
	}
	if s := c.Query("deal_id"); s != "" {
		if id, err := uuid.Parse(s); err == nil {
			dealID = &id
		}
	}
	if s := c.Query("assigned_to"); s != "" {
		if id, err := uuid.Parse(s); err == nil {
			assignedTo = &id
		}
	}
	var status *string
	if s := c.Query("status"); s != "" {
		status = &s
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	var cursor *uuid.UUID
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		if id, err := uuid.Parse(cursorStr); err == nil {
			cursor = &id
		}
	}

	result, xerr := h.CRMService.ListCRMTasks(c.Request.Context(), *orgID, contactID, dealID, assignedTo, status, limit, cursor)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetCRMTask(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	task, xerr := h.CRMService.GetCRMTask(c.Request.Context(), *orgID, taskID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, task)
}

func (h *Handler) UpdateCRMTask(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.UpdateCRMTask
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	userID, _ := middleware.GetUserUUID(c)
	task, xerr := h.CRMService.UpdateCRMTask(c.Request.Context(), *orgID, taskID, &userID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, task)
}

func (h *Handler) DeleteCRMTask(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	xerr := h.CRMService.DeleteCRMTask(c.Request.Context(), *orgID, taskID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}
