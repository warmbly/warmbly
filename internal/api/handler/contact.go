package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const maxBulkOperationSize = 1000

func (h *Handler) AddContacts(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var data []models.AddContact

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if len(data) == 0 {
		errx.Handle(c, errx.New(errx.BadRequest, "no contacts provided"))
		return
	}
	if len(data) > config.MaxContactSize {
		errx.Handle(c, errx.New(errx.BadRequest, fmt.Sprintf("too many contacts, maximum is %d", config.MaxContactSize)))
		return
	}

	resp, err := h.ContactService.Add(c.Request.Context(), userIDStr, *orgID, data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log - bulk import
	h.auditOrg(c, models.AuditActionImport, models.AuditEntityContact, nil, nil, map[string]string{"count": fmt.Sprintf("%d", len(data))})

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) SearchContacts(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	cursor := c.Query("cursor")
	category := c.Query("category")
	limit := c.Query("limit")

	var data models.SearchContacts

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.ContactService.Search(c.Request.Context(), orgID.String(), cursor, category, limit, data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Org-wide facet counts for the browse sidebar, returned by default on the
	// first page (no cursor); a `loadMore` reuses the counts the client already
	// has. Best-effort: a counts failure must not fail the search itself.
	if cursor == "" {
		if counts, cerr := h.ContactService.SearchCounts(c.Request.Context(), orgID.String()); cerr == nil {
			resp.Counts = counts
		}
		// Per-status lead totals for the campaign Leads view: only when the
		// search targets exactly one campaign. Independent of the request's
		// lead_status filter so every scope chip shows its own total.
		if len(data.CampaignIDs) == 1 {
			if lc, cerr := h.ContactService.CampaignLeadCounts(c.Request.Context(), orgID.String(), data.CampaignIDs[0]); cerr == nil {
				resp.LeadCounts = lc
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateContactBulk(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var data models.BulkEditContactsData

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if len(data.Contacts) == 0 {
		errx.Handle(c, errx.New(errx.BadRequest, "no contacts provided"))
		return
	}
	if len(data.Contacts) > maxBulkOperationSize {
		errx.Handle(c, errx.New(errx.BadRequest, fmt.Sprintf("too many contacts, maximum is %d per batch", maxBulkOperationSize)))
		return
	}

	resp, err := h.ContactService.BulkUpdate(c.Request.Context(), userIDStr, *orgID, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log - bulk update
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityContact, nil, nil, map[string]string{"bulk": "true", "count": fmt.Sprintf("%d", len(data.Contacts))})

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateContact(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	id := c.Param("id")

	var data models.UpdateContact

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.ContactService.Update(c.Request.Context(), userIDStr, id, *orgID, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if contactID, err := uuid.Parse(id); err == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityContact, &contactID, nil, nil)
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) DeleteContactBulk(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var data []string

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if len(data) == 0 {
		errx.Handle(c, errx.New(errx.BadRequest, "no contacts provided"))
		return
	}
	if len(data) > maxBulkOperationSize {
		errx.Handle(c, errx.New(errx.BadRequest, fmt.Sprintf("too many contacts, maximum is %d per batch", maxBulkOperationSize)))
		return
	}

	if err := h.ContactService.BulkDelete(c.Request.Context(), userIDStr, *orgID, data); err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log - bulk delete
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityContact, nil, nil, map[string]string{"bulk": "true", "count": fmt.Sprintf("%d", len(data))})

	c.Status(http.StatusNoContent)
}

func (h *Handler) DeleteContact(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	id := c.Param("id")

	if err := h.ContactService.Delete(c.Request.Context(), userIDStr, *orgID, id); err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if contactID, err := uuid.Parse(id); err == nil {
		h.auditOrg(c, models.AuditActionDelete, models.AuditEntityContact, &contactID, nil, nil)
	}

	c.Status(http.StatusNoContent)
}
