package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// requireLeadSyncActor resolves the org + user for a lead-sync request the same
// way the contact and integration handlers do.
func (h *Handler) requireLeadSyncActor(c *gin.Context) (orgID, userID uuid.UUID, ok bool) {
	orgID, ok = requireOrgID(c)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	uid, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return uuid.Nil, uuid.Nil, false
	}
	return orgID, uid, true
}

// leadSyncConnectionResponse is the minimal connection shape the lead-sync UI
// needs to decide whether to show "Connect Google" or the sheet picker.
type leadSyncConnectionResponse struct {
	Connected  bool                       `json:"connected"`
	Connection *leadSyncConnectionSummary `json:"connection"`
}

type leadSyncConnectionSummary struct {
	ID                  uuid.UUID `json:"id"`
	ExternalAccountName string    `json:"external_account_name"`
	Status              string    `json:"status"`
}

// GetLeadSyncGoogleConnection reports whether the org has a connected Google
// account usable for lead-sync (the hidden google_sheets OAuth connection).
func (h *Handler) GetLeadSyncGoogleConnection(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	conn, err := h.LeadSyncService.Connection(c.Request.Context(), orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to resolve google connection"))
		return
	}
	if conn == nil {
		c.JSON(http.StatusOK, leadSyncConnectionResponse{Connected: false, Connection: nil})
		return
	}
	c.JSON(http.StatusOK, leadSyncConnectionResponse{
		Connected: true,
		Connection: &leadSyncConnectionSummary{
			ID:                  conn.ID,
			ExternalAccountName: conn.ExternalAccountName,
			Status:              string(conn.Status),
		},
	})
}

type leadSyncSpreadsheetPayload struct {
	ConnectionID string `json:"connection_id"`
	SheetID      string `json:"sheet_id"`
}

// GetLeadSyncSpreadsheet returns a sheet's title + tabs so the UI can render a
// tab picker before mapping columns.
func (h *Handler) GetLeadSyncSpreadsheet(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var p leadSyncSpreadsheetPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	connID, err := uuid.Parse(strings.TrimSpace(p.ConnectionID))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid connection_id"))
		return
	}
	if strings.TrimSpace(p.SheetID) == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "sheet_id is required"))
		return
	}
	meta, merr := h.LeadSyncService.SpreadsheetMeta(c.Request.Context(), orgID, connID, strings.TrimSpace(p.SheetID))
	if merr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "failed to read spreadsheet: "+merr.Error()))
		return
	}
	c.JSON(http.StatusOK, meta)
}

type leadSyncPreviewPayload struct {
	ConnectionID string `json:"connection_id"`
	SheetID      string `json:"sheet_id"`
	TabTitle     string `json:"tab_title"`
}

// PreviewLeadSync returns an ImportPreview-shaped payload (columns, sample_rows,
// total_rows, has_header, suggested_mapping) so the frontend reuses its
// contact-import column mapper verbatim.
func (h *Handler) PreviewLeadSync(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var p leadSyncPreviewPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	connID, err := uuid.Parse(strings.TrimSpace(p.ConnectionID))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid connection_id"))
		return
	}
	preview, xerr := h.LeadSyncService.Preview(c.Request.Context(), orgID, connID, strings.TrimSpace(p.SheetID), strings.TrimSpace(p.TabTitle))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, preview)
}

// ListLeadSyncSources lists this org's saved sources, optionally filtered to a
// campaign via ?campaign_id=.
func (h *Handler) ListLeadSyncSources(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var campaignID *uuid.UUID
	if raw := strings.TrimSpace(c.Query("campaign_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid campaign_id"))
			return
		}
		campaignID = &id
	}
	sources, err := h.LeadSyncService.List(c.Request.Context(), orgID, campaignID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list sync sources"))
		return
	}
	if sources == nil {
		sources = []models.LeadSyncSource{}
	}
	c.JSON(http.StatusOK, gin.H{"data": sources})
}

// CreateLeadSyncSource saves a new on-demand sync source.
func (h *Handler) CreateLeadSyncSource(c *gin.Context) {
	orgID, userID, ok := h.requireLeadSyncActor(c)
	if !ok {
		return
	}
	var in models.CreateLeadSyncSource
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	src, xerr := h.LeadSyncService.Create(c.Request.Context(), orgID, userID, &in)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, src)
}

// GetLeadSyncSource returns one saved source.
func (h *Handler) GetLeadSyncSource(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	src, xerr := h.LeadSyncService.Get(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, src)
}

// UpdateLeadSyncSource edits a saved source.
func (h *Handler) UpdateLeadSyncSource(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var in models.UpdateLeadSyncSource
	if berr := c.ShouldBindJSON(&in); berr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	src, xerr := h.LeadSyncService.Update(c.Request.Context(), orgID, id, &in)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, src)
}

// DeleteLeadSyncSource removes a saved source.
func (h *Handler) DeleteLeadSyncSource(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	if xerr := h.LeadSyncService.Delete(c.Request.Context(), orgID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}

// SyncLeadSyncSourceNow runs the source on-demand: read the sheet -> upsert
// contacts via ImportCommit. Naturally idempotent (email upsert), so retries
// are safe without an Idempotency-Key.
func (h *Handler) SyncLeadSyncSourceNow(c *gin.Context) {
	orgID, userID, ok := h.requireLeadSyncActor(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	result, xerr := h.LeadSyncService.SyncNow(c.Request.Context(), userID, orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, result)
}
