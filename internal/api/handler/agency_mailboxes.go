package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/agencyhome"
	"github.com/warmbly/warmbly/internal/app/vendorconn"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ListAgencyMailboxes is GET /agency/:agencyId/mailboxes: mailboxes across the clients whose mailbox list the caller reads.
func (h *Handler) ListAgencyMailboxes(c *gin.Context) {
	userID, agencyID, ok := h.agencyHomeCaller(c)
	if !ok {
		return
	}
	clients, ok := queryUUIDs(c, "client")
	if !ok {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid client id"))
		return
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_limit", "limit must be between 1 and 200."))
			return
		}
		limit = n
	}
	page, xerr := h.AgencyHomeService.Mailboxes(c.Request.Context(), agencyID, userID, agencyhome.MailboxFilter{
		Clients: clients, Search: c.Query("q"), Attention: c.Query("attention") == "true", Limit: limit, Cursor: c.Query("cursor"),
	})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, page)
}

// agencyVendorCaller is agencyCaller for the routes over an agency's inbox vendor accounts.
func (h *Handler) agencyVendorCaller(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	if h.VendorConnService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "inbox vendors are not available on this instance"))
		return uuid.Nil, uuid.Nil, false
	}
	return h.agencyCaller(c)
}

func (h *Handler) agencyVendorID(c *gin.Context) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	userID, agencyID, ok := h.agencyVendorCaller(c)
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid vendor account id"))
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	return userID, agencyID, id, true
}

// ListAgencyVendors is GET /agency/:agencyId/vendors: the agency's vendor accounts and the clients each is used in, never their keys.
func (h *Handler) ListAgencyVendors(c *gin.Context) {
	userID, agencyID, ok := h.agencyVendorCaller(c)
	if !ok {
		return
	}
	list, xerr := h.VendorConnService.AgencyAccounts(c.Request.Context(), agencyID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "catalog": h.VendorConnService.Catalog()})
}

// CreateAgencyVendor is POST /agency/:agencyId/vendors: verify a key with the vendor and hold it for every client.
func (h *Handler) CreateAgencyVendor(c *gin.Context) {
	userID, agencyID, ok := h.agencyVendorCaller(c)
	if !ok {
		return
	}
	var req vendorconn.CreateInput
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	a, xerr := h.VendorConnService.CreateAgencyAccount(c.Request.Context(), agencyID, userID, req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionCreate, map[string]string{"vendor_account_id": a.ID.String(), "vendor": a.Vendor})
	c.JSON(http.StatusCreated, a)
}

// UpdateAgencyVendor is PATCH /agency/:agencyId/vendors/:id: rename, or replace the key in every client that uses it.
func (h *Handler) UpdateAgencyVendor(c *gin.Context) {
	userID, agencyID, id, ok := h.agencyVendorID(c)
	if !ok {
		return
	}
	var req vendorconn.CreateInput
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	a, xerr := h.VendorConnService.UpdateAgencyAccount(c.Request.Context(), agencyID, userID, id, req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"vendor_account_id": id.String(), "vendor": a.Vendor})
	c.JSON(http.StatusOK, a)
}

// DeleteAgencyVendor is DELETE /agency/:agencyId/vendors/:id: forget the key in the agency and in every client.
func (h *Handler) DeleteAgencyVendor(c *gin.Context) {
	userID, agencyID, id, ok := h.agencyVendorID(c)
	if !ok {
		return
	}
	if xerr := h.VendorConnService.DeleteAgencyAccount(c.Request.Context(), agencyID, userID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionDelete, map[string]string{"vendor_account_id": id.String()})
	c.Status(http.StatusNoContent)
}

// UseAgencyVendor is PUT /agency/:agencyId/vendors/:id/clients/:orgId: the client workspace's connection to the
// account, made the first time. The caller has to manage mailboxes in that workspace.
func (h *Handler) UseAgencyVendor(c *gin.Context) {
	userID, agencyID, id, ok := h.agencyVendorID(c)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization id"))
		return
	}
	conn, xerr := h.VendorConnService.UseAgencyAccount(c.Request.Context(), agencyID, userID, id, orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditInOrg(c, orgID, models.AuditActionCreate, models.AuditEntityMailboxVendor, &conn.ID, nil, map[string]string{"vendor": conn.Vendor, "agency_account_id": id.String()})
	c.JSON(http.StatusOK, conn)
}
