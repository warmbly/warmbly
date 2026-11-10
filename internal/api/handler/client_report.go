package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/formwire"
	"github.com/warmbly/warmbly/internal/models"
)

var errClientReportsOff = errx.NewWithIdentifier(errx.NotFound, "report_not_found", "Client reports are not available on this instance.")

// clientReportScope resolves the workspace and, on /:id routes, the report id.
func (h *Handler) clientReportScope(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	if h.ClientReportService == nil {
		errx.Handle(c, errClientReportsOff)
		return uuid.Nil, uuid.Nil, false
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, uuid.Nil, false
	}
	raw := c.Param("id")
	if raw == "" {
		return *orgID, uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid report id"))
		return uuid.Nil, uuid.Nil, false
	}
	return *orgID, id, true
}

func (h *Handler) ListClientReports(c *gin.Context) {
	orgID, _, ok := h.clientReportScope(c)
	if !ok {
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			errx.Handle(c, errx.New(errx.BadRequest, "limit must be between 1 and 100"))
			return
		}
		limit = n
	}
	rows, next, xerr := h.ClientReportService.List(c.Request.Context(), orgID, c.Query("cursor"), limit)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "pagination": models.CPagination{NextCursor: next, HasMore: next != nil}})
}

func (h *Handler) GetClientReport(c *gin.Context) {
	orgID, id, ok := h.clientReportScope(c)
	if !ok {
		return
	}
	out, xerr := h.ClientReportService.Get(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) PreviewClientReport(c *gin.Context) {
	orgID, id, ok := h.clientReportScope(c)
	if !ok {
		return
	}
	out, xerr := h.ClientReportService.Preview(c.Request.Context(), orgID, id, models.ClientReportRange(c.Query("range")))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) CreateClientReport(c *gin.Context) {
	orgID, _, ok := h.clientReportScope(c)
	if !ok {
		return
	}
	var in models.ClientReportInput
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	userID, _ := middleware.GetUserUUID(c)
	out, xerr := h.ClientReportService.Create(c.Request.Context(), orgID, userID, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityClientReport, &out.ID, nil, map[string]string{"name": out.Name})
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) UpdateClientReport(c *gin.Context) {
	orgID, id, ok := h.clientReportScope(c)
	if !ok {
		return
	}
	var in models.ClientReportInput
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	out, xerr := h.ClientReportService.Update(c.Request.Context(), orgID, id, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityClientReport, &out.ID, nil, map[string]string{"name": out.Name})
	c.JSON(http.StatusOK, out)
}

func (h *Handler) DeleteClientReport(c *gin.Context) {
	orgID, id, ok := h.clientReportScope(c)
	if !ok {
		return
	}
	if xerr := h.ClientReportService.Delete(c.Request.Context(), orgID, id); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityClientReport, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

// CreateClientReportLink mints a share link, replacing any earlier one; the response is the only time the URL is shown.
func (h *Handler) CreateClientReportLink(c *gin.Context) {
	orgID, id, ok := h.clientReportScope(c)
	if !ok {
		return
	}
	var in models.CreateClientReportLinkRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&in); err != nil {
			errx.Handle(c, errx.InvalidBody(err))
			return
		}
	}
	out, xerr := h.ClientReportService.CreateLink(c.Request.Context(), orgID, id, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionRotate, models.AuditEntityClientReport, &id, nil, map[string]string{"link": "created"})
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) RevokeClientReportLink(c *gin.Context) {
	orgID, id, ok := h.clientReportScope(c)
	if !ok {
		return
	}
	out, xerr := h.ClientReportService.RevokeLinks(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionRevoke, models.AuditEntityClientReport, &id, nil, map[string]string{"link": "revoked"})
	c.JSON(http.StatusOK, out)
}

func (h *Handler) SetClientReportSchedule(c *gin.Context) {
	orgID, id, ok := h.clientReportScope(c)
	if !ok {
		return
	}
	var in models.SetClientReportScheduleRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	out, xerr := h.ClientReportService.SetSchedule(c.Request.Context(), orgID, id, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityClientReport, &id, nil, map[string]string{"schedule": string(in.Cadence)})
	c.JSON(http.StatusOK, out)
}

func (h *Handler) DeleteClientReportSchedule(c *gin.Context) {
	orgID, id, ok := h.clientReportScope(c)
	if !ok {
		return
	}
	out, xerr := h.ClientReportService.DeleteSchedule(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityClientReport, &id, nil, map[string]string{"schedule": "removed"})
	c.JSON(http.StatusOK, out)
}

// InternalGetClientReport answers the portal service for one public report link; an agency portal domain only serves its own clients' reports.
func (h *Handler) InternalGetClientReport(c *gin.Context) {
	if h.ClientReportService == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	out, xerr := h.ClientReportService.Public(c.Request.Context(), c.Param("token"), models.ClientReportRange(c.Query("range")))
	if xerr != nil {
		if xerr.Code == errx.BadRequest {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_range"})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	if out == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	owners, restricted, err := h.CustomHosts.Guard(c.Request.Context(), c.Query("host"))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	if restricted && !customhost.Allowed(owners, &out.OrganizationID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	c.JSON(http.StatusOK, formwire.Report{Brand: out.Brand, Report: *out.View, Ranges: models.ClientReportRanges})
}
