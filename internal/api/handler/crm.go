package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
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

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityCRMNote, &note.ID, nil, map[string]string{"contact_id": contactID.String()})

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
		id, err := paging.DecodeUUID(cursorStr)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "invalid cursor"))
			return
		}
		cursor = &id
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

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCRMNote, &noteID, nil, nil)

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

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityCRMNote, &noteID, nil, nil)

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
		id, err := paging.DecodeUUID(cursorStr)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "invalid cursor"))
			return
		}
		cursor = &id
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

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityCRMPipeline, &pipeline.ID, nil, map[string]string{"name": pipeline.Name})

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

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCRMPipeline, &pipelineID, nil, nil)

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

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityCRMPipeline, &pipelineID, nil, nil)

	c.Status(http.StatusNoContent)
}

// =====================
// Pipeline Stages
// =====================

func (h *Handler) CreateStage(c *gin.Context) {
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

	var data models.CreatePipelineStage
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	stage, xerr := h.CRMService.CreateStage(c.Request.Context(), *orgID, pipelineID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityCRMStage, &stage.ID, nil, map[string]string{"pipeline_id": pipelineID.String(), "name": stage.Name})

	c.JSON(http.StatusCreated, stage)
}

func (h *Handler) UpdateStage(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
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

	stage, xerr := h.CRMService.UpdateStage(c.Request.Context(), *orgID, stageID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCRMStage, &stageID, nil, nil)

	c.JSON(http.StatusOK, stage)
}

func (h *Handler) DeleteStage(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	stageID, err := uuid.Parse(c.Param("stageId"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	xerr := h.CRMService.DeleteStage(c.Request.Context(), *orgID, stageID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityCRMStage, &stageID, nil, nil)

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

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityCRMDeal, &deal.ID, nil, map[string]string{"name": deal.Name, "pipeline_id": deal.PipelineID.String()})

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
		id, err := paging.DecodeUUID(cursorStr)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "invalid cursor"))
			return
		}
		cursor = &id
	}

	result, xerr := h.CRMService.ListDeals(c.Request.Context(), *orgID, pipelineID, stageID, status, limit, cursor)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// SearchDeals is the faceted, server-paginated deals surface that powers the
// cross-pipeline table view. Filters arrive in the JSON body; limit + offset
// are query params (mirrors the contacts search ergonomics).
func (h *Handler) SearchDeals(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var filters models.SearchDeals
	if err := c.ShouldBindJSON(&filters); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	offset := 0
	if cur := c.Query("cursor"); cur != "" {
		o, cerr := paging.DecodeOffsetCursor(cur)
		if cerr != nil {
			errx.Handle(c, cerr)
			return
		}
		offset = o
	}

	result, xerr := h.CRMService.SearchDeals(c.Request.Context(), *orgID, filters, limit, offset)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// DealsSummary returns COUNT + SUM(value) aggregates over the SAME filter body
// as SearchDeals, so header totals and per-stage board headers are true totals
// over the whole matching set rather than a client reduce over a loaded page.
func (h *Handler) DealsSummary(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var filters models.SearchDeals
	if err := c.ShouldBindJSON(&filters); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	summary, xerr := h.CRMService.DealsSummary(c.Request.Context(), *orgID, filters)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, summary)
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

	var dealMeta map[string]string
	if data.StageID != nil {
		// Moving the deal between stages.
		dealMeta = map[string]string{"stage": deal.StageID.String()}
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCRMDeal, &dealID, nil, dealMeta)

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

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityCRMDeal, &dealID, nil, nil)

	c.Status(http.StatusNoContent)
}

func (h *Handler) GetDealsByContact(c *gin.Context) {
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

	deals, xerr := h.CRMService.GetDealsByContact(c.Request.Context(), *orgID, contactID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, deals)
}

// =====================
// CRM Task Types
// =====================

func (h *Handler) ListTaskTypes(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	types, xerr := h.CRMService.ListTaskTypes(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": types})
}

func (h *Handler) CreateTaskType(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	var data models.CreateCRMTaskType
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	t, xerr := h.CRMService.CreateTaskType(c.Request.Context(), *orgID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, t)
}

func (h *Handler) UpdateTaskType(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	typeID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	var data models.UpdateCRMTaskType
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	t, xerr := h.CRMService.UpdateTaskType(c.Request.Context(), *orgID, typeID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, t)
}

func (h *Handler) DeleteTaskType(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	typeID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	if xerr := h.CRMService.DeleteTaskType(c.Request.Context(), *orgID, typeID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
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

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityCRMTask, &task.ID, nil, map[string]string{"title": task.Title})

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
		id, err := paging.DecodeUUID(cursorStr)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "invalid cursor"))
			return
		}
		cursor = &id
	}

	result, xerr := h.CRMService.ListCRMTasks(c.Request.Context(), *orgID, contactID, dealID, assignedTo, status, limit, cursor)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// SearchCRMTasks is the faceted, server-paginated tasks surface that powers the
// tasks list view at scale. Filters arrive in the JSON body; limit + offset are
// query params (mirrors the deals search ergonomics).
func (h *Handler) SearchCRMTasks(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var filters models.SearchTasks
	if err := c.ShouldBindJSON(&filters); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	offset := 0
	if cur := c.Query("cursor"); cur != "" {
		o, cerr := paging.DecodeOffsetCursor(cur)
		if cerr != nil {
			errx.Handle(c, cerr)
			return
		}
		offset = o
	}

	result, xerr := h.CRMService.SearchTasks(c.Request.Context(), *orgID, filters, limit, offset)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// TasksSummary returns COUNT aggregates over the SAME filter body as
// SearchCRMTasks, so header totals (by status, overdue, high priority) are true
// totals over the whole matching set rather than a client reduce over a page.
func (h *Handler) TasksSummary(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var filters models.SearchTasks
	if err := c.ShouldBindJSON(&filters); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	summary, xerr := h.CRMService.TasksSummary(c.Request.Context(), *orgID, filters)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, summary)
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

	var taskMeta map[string]string
	if data.Status != nil && *data.Status == string(models.CRMTaskStatusCompleted) {
		taskMeta = map[string]string{"completed": "true"}
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCRMTask, &taskID, nil, taskMeta)

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

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityCRMTask, &taskID, nil, nil)

	c.Status(http.StatusNoContent)
}
