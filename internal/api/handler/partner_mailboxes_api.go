package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/partner"
	"github.com/warmbly/warmbly/internal/errx"
)

// PartnerListMailboxes is GET /v1/partner/organizations/:external_id/mailboxes.
func (h *Handler) PartnerListMailboxes(c *gin.Context) {
	limit, xerr := partner.ParseLimit(c.Query("limit"), 100)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	page, xerr := h.Partners.ListMailboxes(c.Request.Context(), partnerCaller(c), c.Param("external_id"), c.Query("cursor"), limit)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusOK, page)
}

// PartnerAttachMailbox is PUT /v1/partner/organizations/:external_id/mailboxes/:partner_mailbox_id.
func (h *Handler) PartnerAttachMailbox(c *gin.Context) {
	var in partner.AttachInput
	if !bindPartnerJSON(c, &in) {
		return
	}
	m, created, xerr := h.Partners.Attach(c.Request.Context(), partnerCaller(c), c.Param("external_id"), c.Param("partner_mailbox_id"), in)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, m)
}

// PartnerDetachMailbox is DELETE /v1/partner/organizations/:external_id/mailboxes/:partner_mailbox_id.
func (h *Handler) PartnerDetachMailbox(c *gin.Context) {
	if xerr := h.Partners.Detach(c.Request.Context(), partnerCaller(c), c.Param("external_id"), c.Param("partner_mailbox_id")); xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}

// PartnerSetEnablement is PUT /v1/partner/organizations/:external_id/enablements/:mailbox_id.
func (h *Handler) PartnerSetEnablement(c *gin.Context) {
	var in struct {
		Enablement string `json:"enablement"`
	}
	if !bindPartnerJSON(c, &in) {
		return
	}
	m, xerr := h.Partners.SetEnablement(c.Request.Context(), partnerCaller(c), c.Param("external_id"), c.Param("mailbox_id"), in.Enablement)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusOK, m)
}

// PartnerCreateConnectLink is POST /v1/partner/organizations/:external_id/connect-links.
func (h *Handler) PartnerCreateConnectLink(c *gin.Context) {
	var in partner.ConnectLinkInput
	if !bindPartnerJSON(c, &in) {
		return
	}
	link, xerr := h.Partners.CreateConnectLink(c.Request.Context(), partnerCaller(c), c.Param("external_id"), in)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, link)
}

// PartnerGetBalance is GET /v1/partner/organizations/:external_id/balance.
func (h *Handler) PartnerGetBalance(c *gin.Context) {
	b, xerr := h.Partners.GetBalance(c.Request.Context(), partnerCaller(c), c.Param("external_id"))
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusOK, b)
}

// PartnerCreditBalance is POST /v1/partner/organizations/:external_id/balance/credits.
func (h *Handler) PartnerCreditBalance(c *gin.Context) {
	var in partner.CreditInput
	if !bindPartnerJSON(c, &in) {
		return
	}
	b, xerr := h.Partners.CreditBalance(c.Request.Context(), partnerCaller(c), c.Param("external_id"), in)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusOK, b)
}

// PartnerUsage is GET /v1/partner/usage?month=.
func (h *Handler) PartnerUsage(c *gin.Context) {
	out, xerr := h.Partners.Usage(c.Request.Context(), partnerCaller(c), c.Query("month"))
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// PartnerMailboxDays is GET /v1/partner/usage/mailbox-days?month=&cursor=&limit=.
func (h *Handler) PartnerMailboxDays(c *gin.Context) {
	limit, xerr := partner.ParseLimit(c.DefaultQuery("limit", "1000"), partner.MaxMailboxDaysPage)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	out, xerr := h.Partners.MailboxDays(c.Request.Context(), partnerCaller(c), c.Query("month"), c.Query("cursor"), limit)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// NoRoute answers an unknown path: the partner API's error envelope under /v1/partner, gin's plain 404 elsewhere.
func (h *Handler) NoRoute(c *gin.Context) {
	if p := c.Request.URL.Path; p == "/v1/partner" || strings.HasPrefix(p, "/v1/partner/") {
		partnerError(c, errx.NewWithIdentifier(errx.NotFound, "not_found", "There is no such partner API route."))
		return
	}
	c.String(http.StatusNotFound, "404 page not found")
}

// WorkspacePartnerMailboxes is GET /v1/emails/partner: each mailbox's enablement in a workspace a partner manages.
func (h *Handler) WorkspacePartnerMailboxes(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	data, xerr := h.Partners.WorkspaceMailboxes(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// AdminPartnerStatements is GET /admin/partners/:id/statements: closed months, newest first.
func (h *Handler) AdminPartnerStatements(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	list, xerr := h.Partners.AdminStatements(c.Request.Context(), id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// AdminPartnerStatement is GET /admin/partners/:id/statements/:month: one month, closed or running.
func (h *Handler) AdminPartnerStatement(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	st, xerr := h.Partners.AdminStatement(c.Request.Context(), id, c.Param("month"))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, st)
}

// PartnerCreateMicrosoftConsentLink is POST /v1/partner/organizations/:external_id/microsoft-consent-links.
func (h *Handler) PartnerCreateMicrosoftConsentLink(c *gin.Context) {
	var in partner.MicrosoftConsentInput
	if !bindPartnerJSON(c, &in) {
		return
	}
	link, xerr := h.Partners.CreateMicrosoftConsentLink(c.Request.Context(), partnerCaller(c), c.Param("external_id"), in)
	if xerr != nil {
		partnerError(c, xerr)
		return
	}
	status := http.StatusCreated
	if link.Consented {
		status = http.StatusOK
	}
	c.JSON(status, link)
}
