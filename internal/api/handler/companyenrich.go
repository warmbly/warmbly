package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/companyenrich"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) companyEnrichReady(c *gin.Context) (orgID, userID, connID uuid.UUID, ok bool) {
	if h.CompanyEnrichService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "CompanyEnrich is not available on this instance"))
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	orgID, userID, ok = h.requireIntegrationActor(c, true)
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	connID, err := uuid.Parse(c.Param("id"))
	if err != nil || connID == uuid.Nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid connection id"))
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	return orgID, userID, connID, true
}

// bindCompanyEnrich keeps numeric tokens exact and rejects excess or unknown
// envelope fields before any provider request can consume credits.
func bindCompanyEnrich(c *gin.Context, dst any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, companyenrich.MaxRequestBytes)
	d := json.NewDecoder(c.Request.Body)
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "request must be a valid JSON object within 256 KiB with supported fields"))
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		errx.JSON(c, errx.New(errx.BadRequest, "request must contain one JSON object"))
		return false
	}
	return true
}

// CompanyEnrichQuery dispatches one explicit operation through the org's key.
func (h *Handler) CompanyEnrichQuery(c *gin.Context) {
	orgID, _, connID, ok := h.companyEnrichReady(c)
	if !ok {
		return
	}
	var in models.CompanyEnrichQuery
	if !bindCompanyEnrich(c, &in) {
		return
	}
	out, err := h.CompanyEnrichService.Query(c.Request.Context(), orgID, connID, in)
	if err != nil {
		errx.Handle(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// EnrichCompanyEnrichContact saves profile fields without enrollment or consent changes.
func (h *Handler) EnrichCompanyEnrichContact(c *gin.Context) {
	orgID, userID, connID, ok := h.companyEnrichReady(c)
	if !ok {
		return
	}
	var in models.CompanyEnrichContactRequest
	if !bindCompanyEnrich(c, &in) {
		return
	}
	out, err := h.CompanyEnrichService.EnrichContact(c.Request.Context(), orgID, connID, userID, in)
	if err != nil {
		errx.Handle(c, err)
		return
	}
	if len(out.UpdatedFields) > 0 && h.AuditService != nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityContact, &in.ContactID, nil, nil)
	}
	c.JSON(http.StatusOK, out)
}
