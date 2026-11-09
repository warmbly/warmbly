package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/mailboxconnect"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrPartnershipRequired refuses a Partnership tool to a workspace on another plan.
var ErrPartnershipRequired = errx.NewWithIdentifier(errx.Forbidden, "partnership_plan_required",
	"This is part of the Partnership plan. Ask Warmbly to offer it to this workspace.")

// partnerOrg resolves the workspace and refuses it unless it may use the Partnership tools.
func (h *Handler) partnerOrg(c *gin.Context) (uuid.UUID, bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return uuid.Nil, false
	}
	if h.FeatureGateService == nil {
		errx.Handle(c, ErrPartnershipRequired)
		return uuid.Nil, false
	}
	ok, xerr := h.FeatureGateService.CanUsePartnerTools(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return uuid.Nil, false
	}
	if !ok {
		errx.Handle(c, ErrPartnershipRequired)
		return uuid.Nil, false
	}
	return *orgID, true
}

func (h *Handler) mailboxConnectReady(c *gin.Context) bool {
	if h.MailboxConnectService == nil {
		errx.Handle(c, errx.New(errx.ServiceUnavailable, "mailbox connect is not available on this instance"))
		return false
	}
	return true
}

// ConnectSMTPRequest connects one SMTP/IMAP mailbox and optionally starts its warmup.
type ConnectSMTPRequest struct {
	OnboardingSMTPIMAPRequest
	Warmup *models.MailboxConnectWarmup `json:"warmup"`
}

// ConnectSMTPBulkRequest connects up to MailboxBulkBatchMax SMTP/IMAP mailboxes with one warmup setting.
type ConnectSMTPBulkRequest struct {
	Accounts []OnboardingSMTPIMAPRequest  `json:"accounts"`
	Warmup   *models.MailboxConnectWarmup `json:"warmup"`
}

// ConnectMailboxSMTP is POST /emails/connect/smtp-imap.
func (h *Handler) ConnectMailboxSMTP(c *gin.Context) {
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxConnectReady(c) {
		return
	}
	var req ConnectSMTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	userID := middleware.GetUserID(c)
	acc, note, xerr := h.MailboxConnectService.ConnectSMTP(c.Request.Context(), orgID, userID, &models.NewSMTPIMAPAccount{
		Email: req.Email, Name: req.Name, SMTP: req.SMTP, IMAP: req.IMAP,
	}, req.Warmup)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityEmailAccount, &acc.ID, nil, map[string]string{
		"provider": "smtp_imap", "email": acc.Email, "via": "connect_api",
	})
	c.JSON(http.StatusCreated, gin.H{"mailbox": acc, "warmup_note": note})
}

// ConnectMailboxSMTPBulk is POST /emails/connect/smtp-imap/bulk; it always answers 200 with a status per row.
func (h *Handler) ConnectMailboxSMTPBulk(c *gin.Context) {
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxConnectReady(c) {
		return
	}
	var req ConnectSMTPBulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	if len(req.Accounts) == 0 {
		errx.Handle(c, errx.New(errx.BadRequest, "accounts must carry at least one row"))
		return
	}
	if len(req.Accounts) > config.MailboxBulkBatchMax {
		errx.Handle(c, errx.New(errx.BadRequest, fmt.Sprintf("accounts may carry at most %d rows per request", config.MailboxBulkBatchMax)))
		return
	}
	rows := make([]models.NewSMTPIMAPAccount, len(req.Accounts))
	for i, a := range req.Accounts {
		rows[i] = models.NewSMTPIMAPAccount{Email: a.Email, Name: a.Name, SMTP: a.SMTP, IMAP: a.IMAP}
	}
	result, xerr := h.MailboxConnectService.ConnectSMTPBulk(c.Request.Context(), orgID, middleware.GetUserID(c), rows, req.Warmup)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	for _, r := range result.Data {
		if r.Status != models.MailboxBulkConnected || r.ID == nil {
			continue
		}
		h.auditOrg(c, models.AuditActionConnect, models.AuditEntityEmailAccount, r.ID, nil, map[string]string{
			"provider": "smtp_imap", "email": r.Email, "bulk": "true", "via": "connect_api",
		})
	}
	c.JSON(http.StatusOK, result)
}

// CreateMailboxConnectLink is POST /emails/connect/links.
func (h *Handler) CreateMailboxConnectLink(c *gin.Context) {
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxConnectReady(c) {
		return
	}
	userID, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.Handle(c, errx.ErrUnauthorized)
		return
	}
	var req models.NewMailboxConnectLink
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	link, xerr := h.MailboxConnectService.CreateLink(c.Request.Context(), orgID, userID, req)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityMailboxConnectLink, &link.ID, nil, map[string]string{
		"provider": string(link.Provider), "email": link.ExpectedEmail,
	})
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, link)
}

// ListMailboxConnectLinks is GET /emails/connect/links?cursor=&limit=.
func (h *Handler) ListMailboxConnectLinks(c *gin.Context) {
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxConnectReady(c) {
		return
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			errx.Handle(c, mailboxconnect.ErrLimit)
			return
		}
		limit = n
	}
	links, next, xerr := h.MailboxConnectService.ListLinks(c.Request.Context(), orgID, c.Query("cursor"), limit)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	var cursor *string
	if next != "" {
		cursor = &next
	}
	c.JSON(http.StatusOK, gin.H{"data": links, "pagination": models.CPagination{NextCursor: cursor, HasMore: cursor != nil}})
}

func connectLinkParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return uuid.Nil, false
	}
	return id, true
}

// GetMailboxConnectLink is GET /emails/connect/links/:id.
func (h *Handler) GetMailboxConnectLink(c *gin.Context) {
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxConnectReady(c) {
		return
	}
	id, ok := connectLinkParam(c)
	if !ok {
		return
	}
	link, xerr := h.MailboxConnectService.GetLink(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, link)
}

// RevokeMailboxConnectLink is DELETE /emails/connect/links/:id; a link that already connected is left as it is.
func (h *Handler) RevokeMailboxConnectLink(c *gin.Context) {
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxConnectReady(c) {
		return
	}
	id, ok := connectLinkParam(c)
	if !ok {
		return
	}
	link, xerr := h.MailboxConnectService.RevokeLink(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityMailboxConnectLink, &link.ID, nil, map[string]string{"status": link.Status})
	c.JSON(http.StatusOK, link)
}

// RefuseOnPartnership stops a flow that only this deployment's own Google or Microsoft app can serve.
func (h *Handler) RefuseOnPartnership(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil || h.EmailService == nil {
		c.Next()
		return
	}
	if xerr := h.EmailService.RequireOwnOAuthApp(c.Request.Context(), *orgID); xerr != nil {
		errx.Handle(c, xerr)
		c.Abort()
		return
	}
	c.Next()
}

// ConnectGrantRequest hands over one sign-in the workspace's own OAuth app completed.
type ConnectGrantRequest struct {
	models.MailboxOAuthGrant
	Warmup *models.MailboxConnectWarmup `json:"warmup"`
}

// ConnectGrantBulkRequest hands over many sign-ins with one warmup setting.
type ConnectGrantBulkRequest struct {
	Grants []models.MailboxOAuthGrant   `json:"grants"`
	Warmup *models.MailboxConnectWarmup `json:"warmup"`
}

// ConnectMailboxGrant is POST /emails/connect/oauth: 201 for a new mailbox, 200 when it renewed one the workspace had.
func (h *Handler) ConnectMailboxGrant(c *gin.Context) {
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxConnectReady(c) {
		return
	}
	var req ConnectGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	acc, renewed, note, xerr := h.MailboxConnectService.ConnectGrant(c.Request.Context(), orgID, middleware.GetUserID(c), req.MailboxOAuthGrant, req.Warmup, mailboxAllowance(c))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	action, status := models.AuditActionConnect, http.StatusCreated
	if renewed {
		action, status = models.AuditActionUpdate, http.StatusOK
	}
	h.auditOrg(c, action, models.AuditEntityEmailAccount, &acc.ID, nil, map[string]string{
		"provider": acc.Provider, "email": acc.Email, "via": "connect_grant",
	})
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"mailbox": acc, "renewed": renewed, "warmup_note": note})
}

// ConnectMailboxGrantBulk is POST /emails/connect/oauth/bulk; it always answers 200 with a status per sign-in.
func (h *Handler) ConnectMailboxGrantBulk(c *gin.Context) {
	orgID, ok := h.partnerOrg(c)
	if !ok || !h.mailboxConnectReady(c) {
		return
	}
	var req ConnectGrantBulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	if xerr := mailboxconnect.ValidateGrantBatch(len(req.Grants)); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	result, xerr := h.MailboxConnectService.ConnectGrantBulk(c.Request.Context(), orgID, middleware.GetUserID(c), req.Grants, req.Warmup, mailboxAllowance(c))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	for _, r := range result.Data {
		if r.ID == nil || r.Status == models.MailboxBulkFailed {
			continue
		}
		action := models.AuditActionConnect
		if r.Status == models.MailboxBulkRenewed {
			action = models.AuditActionUpdate
		}
		h.auditOrg(c, action, models.AuditEntityEmailAccount, r.ID, nil, map[string]string{"email": r.Email, "bulk": "true", "via": "connect_grant"})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

// mailboxAllowance is the caller's mailbox allowlist as a check; nil when it may act on every mailbox.
func mailboxAllowance(c *gin.Context) func(uuid.UUID) bool {
	if middleware.AllowedEmailAccounts(c) == nil {
		return nil
	}
	return func(id uuid.UUID) bool { return middleware.EmailAccountAllowed(c, id) }
}
