package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/vendorconn"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) vendorsReady(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	if h.VendorConnService == nil {
		errx.Handle(c, errx.ErrNotFound)
		return uuid.Nil, uuid.Nil, false
	}
	return importCaller(c)
}

func (h *Handler) vendorID(c *gin.Context) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	orgID, userID, ok := h.vendorsReady(c)
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	return orgID, userID, id, true
}

// ListMailboxVendorCatalog is GET /emails/vendors/catalog: the supported vendors and what each asks for.
func (h *Handler) ListMailboxVendorCatalog(c *gin.Context) {
	if _, _, ok := h.vendorsReady(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.VendorConnService.Catalog()})
}

// ListMailboxVendors is GET /emails/vendors: the workspace's vendor accounts, never their keys.
func (h *Handler) ListMailboxVendors(c *gin.Context) {
	orgID, _, ok := h.vendorsReady(c)
	if !ok {
		return
	}
	list, xerr := h.VendorConnService.List(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// CreateMailboxVendor is POST /emails/vendors: verify a key with the vendor and store it sealed.
func (h *Handler) CreateMailboxVendor(c *gin.Context) {
	orgID, userID, ok := h.vendorsReady(c)
	if !ok {
		return
	}
	var req vendorconn.CreateInput
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	conn, xerr := h.VendorConnService.Create(c.Request.Context(), orgID, userID, req)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityMailboxVendor, &conn.ID, nil, map[string]string{"vendor": conn.Vendor})
	c.JSON(http.StatusCreated, conn)
}

// UpdateMailboxVendor is PATCH /emails/vendors/:id: rename, or replace the key after verifying it.
func (h *Handler) UpdateMailboxVendor(c *gin.Context) {
	orgID, _, id, ok := h.vendorID(c)
	if !ok {
		return
	}
	var req vendorconn.CreateInput
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	conn, xerr := h.VendorConnService.Update(c.Request.Context(), orgID, id, req)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityMailboxVendor, &conn.ID, nil, map[string]string{"vendor": conn.Vendor})
	c.JSON(http.StatusOK, conn)
}

// DeleteMailboxVendor is DELETE /emails/vendors/:id. Mailboxes it brought in stay connected.
func (h *Handler) DeleteMailboxVendor(c *gin.Context) {
	orgID, _, id, ok := h.vendorID(c)
	if !ok {
		return
	}
	if xerr := h.VendorConnService.Delete(c.Request.Context(), orgID, id); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityMailboxVendor, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

// ListMailboxVendorMailboxes is GET /emails/vendors/:id/mailboxes.
func (h *Handler) ListMailboxVendorMailboxes(c *gin.Context) {
	orgID, _, id, ok := h.vendorID(c)
	if !ok {
		return
	}
	boxes, xerr := h.VendorConnService.Mailboxes(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": boxes})
}

// ImportMailboxVendorMailboxes is POST /emails/vendors/:id/import: a background
// import of the picked mailboxes. Repeating it updates what the first connected.
func (h *Handler) ImportMailboxVendorMailboxes(c *gin.Context) {
	orgID, userID, id, ok := h.vendorID(c)
	if !ok {
		return
	}
	var req vendorconn.ImportInput
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	imp, xerr := h.VendorConnService.Import(c.Request.Context(), orgID, userID, id, req)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, imp)
}

// vendorReconnectRequest names one mailbox or several; without either every Google mailbox imported from the vendor account is reconnected.
type vendorReconnectRequest struct {
	EmailAccountID  *uuid.UUID  `json:"email_account_id"`
	EmailAccountIDs []uuid.UUID `json:"email_account_ids"`
}

// ReconnectMailboxVendorMailboxes is POST /emails/vendors/:id/reconnect: the vendor signs in again as its Google mailboxes, renewing each in place.
func (h *Handler) ReconnectMailboxVendorMailboxes(c *gin.Context) {
	orgID, userID, id, ok := h.vendorID(c)
	if !ok {
		return
	}
	var req vendorReconnectRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	if len(req.EmailAccountIDs) > 0 && req.EmailAccountID != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	var (
		out  []vendorconn.ReconnectMailbox
		xerr *errx.Error
	)
	if len(req.EmailAccountIDs) > 0 {
		out, xerr = h.VendorConnService.ReconnectMailboxes(c.Request.Context(), orgID, userID, id, req.EmailAccountIDs)
	} else {
		out, xerr = h.VendorConnService.Reconnect(c.Request.Context(), orgID, userID, id, req.EmailAccountID)
	}
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityMailboxVendor, &id, nil, map[string]string{"action": "reconnect", "mailboxes": strconv.Itoa(len(out))})
	c.JSON(http.StatusAccepted, gin.H{"data": out})
}

// MailboxVendorReconnectStatus is GET /emails/vendors/:id/reconnect: where the last reconnect stands, mailbox by mailbox.
func (h *Handler) MailboxVendorReconnectStatus(c *gin.Context) {
	orgID, _, id, ok := h.vendorID(c)
	if !ok {
		return
	}
	out, xerr := h.VendorConnService.ReconnectStatus(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// GetEmailSignin is GET /emails/:id/signin: how the mailbox signs in, the switches that can succeed for it, and its last vendor reconnect.
func (h *Handler) GetEmailSignin(c *gin.Context) {
	orgID, _, id, ok := h.vendorID(c)
	if !ok {
		return
	}
	out, xerr := h.VendorConnService.SigninOptions(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}
