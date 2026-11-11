package handler

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/connectlink"
	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/formwire"
	"github.com/warmbly/warmbly/internal/models"
)

// The slice of the internal API for client connect links, called by the portal service. The caller owns
// the page, the render token and the per-visitor budgets; every decision about the link and the mailbox is made here.

// connectRefusal answers the portal service with the stable code and a client-facing message.
func connectRefusal(c *gin.Context, xerr *errx.Error) {
	status := int(xerr.Code)
	msg := xerr.Message
	if status < 400 || status > 599 || xerr.Code == errx.Internal || xerr.Code == errx.ServiceUnavailable {
		if status < 400 || status > 599 {
			status = http.StatusInternalServerError
		}
		msg = "Something went wrong on our side, so nothing was connected. Try again in a few minutes."
	}
	c.JSON(status, formwire.ConnectError{Code: xerr.ResponseCode(), Message: msg})
}

// connectLinkOn resolves a link as seen on host: another workspace's custom host knows no link of this one.
func (h *Handler) connectLinkOn(c *gin.Context, token, host string) (*models.ClientConnectLink, bool) {
	return h.resolveConnectLink(c, token, host, false)
}

// connectLinkOnAnyStatus is connectLinkOn for a link that may have expired or filled up since the page loaded.
func (h *Handler) connectLinkOnAnyStatus(c *gin.Context, token, host string) (*models.ClientConnectLink, bool) {
	return h.resolveConnectLink(c, token, host, true)
}

func (h *Handler) resolveConnectLink(c *gin.Context, token, host string, anyStatus bool) (*models.ClientConnectLink, bool) {
	if h.ConnectLinkService == nil {
		connectRefusal(c, connectlink.ErrNotFound)
		return nil, false
	}
	link, xerr := h.ConnectLinkService.Resolve(c.Request.Context(), token)
	if link == nil {
		if xerr == nil {
			xerr = connectlink.ErrNotFound
		}
		connectRefusal(c, xerr)
		return nil, false
	}
	owners, restricted, err := h.CustomHosts.Guard(c.Request.Context(), host)
	if err != nil {
		connectRefusal(c, errx.InternalError())
		return nil, false
	}
	if restricted && !customhost.Allowed(owners, &link.OrganizationID) {
		connectRefusal(c, connectlink.ErrNotFound)
		return nil, false
	}
	if xerr != nil && !anyStatus {
		connectRefusal(c, xerr)
		return nil, false
	}
	return link, true
}

// InternalLookupConnectLink is POST /api/v1/internal/connect-links/lookup.
func (h *Handler) InternalLookupConnectLink(c *gin.Context) {
	var req formwire.ConnectLookup
	if err := c.ShouldBindJSON(&req); err != nil {
		connectRefusal(c, errx.New(errx.BadRequest, "The request could not be read."))
		return
	}
	link, ok := h.connectLinkOn(c, req.Token, req.Host)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, h.ConnectLinkService.Page(c.Request.Context(), link))
}

// InternalStartConnectOAuth is POST /api/v1/internal/connect-links/oauth/start.
func (h *Handler) InternalStartConnectOAuth(c *gin.Context) {
	var req formwire.ConnectOAuthStart
	if err := c.ShouldBindJSON(&req); err != nil {
		connectRefusal(c, errx.New(errx.BadRequest, "The request could not be read."))
		return
	}
	link, ok := h.connectLinkOn(c, req.Token, req.Host)
	if !ok {
		return
	}
	returnURL := h.connectReturnURL(c.Request.Context(), req.Host, link, req.Token)
	if returnURL == "" {
		connectRefusal(c, connectlink.ErrNotFound)
		return
	}
	started, xerr := h.ConnectLinkService.StartOAuth(c.Request.Context(), link, models.InboxProvider(req.Provider), returnURL, req.RemoteIP)
	if xerr != nil {
		connectRefusal(c, xerr)
		return
	}
	c.JSON(http.StatusOK, started)
}

// connectReturnURL is the connect page a finished sign-in returns to: on the shared portal address, or on an
// agency portal domain verified for the link's own workspace. Any other host gets nothing.
func (h *Handler) connectReturnURL(ctx context.Context, host string, link *models.ClientConnectLink, token string) string {
	host = customhost.Normalize(host)
	if host == "" {
		return ""
	}
	if host == config.PortalCNAMETarget() {
		return connectlink.URL(config.PortalBaseURL(), token)
	}
	owners, restricted, err := h.CustomHosts.Guard(ctx, host)
	if err != nil || !restricted || !customhost.Allowed(owners, &link.OrganizationID) {
		return ""
	}
	return connectlink.URL(config.PortalOriginOn(host), token)
}

// InternalConnectSMTPIMAP is POST /api/v1/internal/connect-links/smtp-imap.
func (h *Handler) InternalConnectSMTPIMAP(c *gin.Context) {
	var req formwire.ConnectSMTPIMAP
	if err := c.ShouldBindJSON(&req); err != nil {
		connectRefusal(c, errx.New(errx.BadRequest, "The request could not be read."))
		return
	}
	link, ok := h.connectLinkOn(c, req.Token, req.Host)
	if !ok {
		return
	}
	res, xerr := h.ConnectLinkService.ConnectSMTPIMAP(c.Request.Context(), link, connectlink.SMTPIMAPInput{
		Email: req.Email, Name: req.Name, SMTP: req.SMTP, IMAP: req.IMAP,
		CaptchaToken: req.CaptchaToken, RemoteIP: req.RemoteIP,
	})
	if xerr != nil {
		connectRefusal(c, xerr)
		return
	}
	h.auditConnectLinkUse(c, res, req.RemoteIP)
	c.JSON(http.StatusCreated, formwire.ConnectedMailbox{
		Email: res.Account.Email, Provider: res.Account.Provider,
		Connected: res.Link.MailboxesConnected, MaxMailboxes: res.Link.MaxMailboxes,
	})
}

// InternalConnectVendorMailboxes is POST /api/v1/internal/connect-links/vendor/mailboxes: check a client's vendor key and list what it reaches.
func (h *Handler) InternalConnectVendorMailboxes(c *gin.Context) {
	var req formwire.ConnectVendorAccount
	if err := c.ShouldBindJSON(&req); err != nil {
		connectRefusal(c, errx.New(errx.BadRequest, "The request could not be read."))
		return
	}
	link, ok := h.connectLinkOn(c, req.Token, req.Host)
	if !ok {
		return
	}
	out, xerr := h.ConnectLinkService.VendorMailboxes(c.Request.Context(), link, connectlink.VendorAccountInput{
		Vendor: req.Vendor, Fields: req.Fields, CaptchaToken: req.CaptchaToken, RemoteIP: req.RemoteIP,
	})
	if xerr != nil {
		connectRefusal(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// InternalConnectVendorImport is POST /api/v1/internal/connect-links/vendor/import: connect the mailboxes a client picked.
func (h *Handler) InternalConnectVendorImport(c *gin.Context) {
	var req formwire.ConnectVendorImport
	if err := c.ShouldBindJSON(&req); err != nil {
		connectRefusal(c, errx.New(errx.BadRequest, "The request could not be read."))
		return
	}
	link, ok := h.connectLinkOn(c, req.Token, req.Host)
	if !ok {
		return
	}
	res, xerr := h.ConnectLinkService.VendorImport(c.Request.Context(), link, connectlink.VendorImportInput{
		Ticket: req.Ticket, MailboxIDs: req.MailboxIDs, RemoteIP: req.RemoteIP,
	})
	if xerr != nil {
		connectRefusal(c, xerr)
		return
	}
	// Recorded in the creator's name, which is what refreshes every teammate's mailbox list and imports.
	if h.AuditService != nil && res.Import != nil {
		importID := res.Import.ID
		meta := map[string]string{"via": "connect_link", "connect_link_id": res.Link.ID.String(), "vendor": res.Vendor, "mailboxes": strconv.Itoa(len(res.Emails))}
		h.AuditService.LogAction(c.Request.Context(), res.Link.OrganizationID, res.ActorID, models.AuditActionCreate, models.AuditEntityMailboxImport, &importID,
			req.RemoteIP, c.Request.UserAgent(), nil, meta)
		h.AuditService.LogAction(c.Request.Context(), res.Link.OrganizationID, res.ActorID, models.AuditActionUpdate, models.AuditEntityClientConnectLink, &res.Link.ID,
			req.RemoteIP, c.Request.UserAgent(), nil, meta)
	}
	c.JSON(http.StatusCreated, formwire.ConnectVendorStarted{
		Mailboxes: len(res.Emails), Connected: res.Link.MailboxesConnected, MaxMailboxes: res.Link.MaxMailboxes,
	})
}

// auditConnectLinkUse records the connect in the creator's name, which is what refreshes every teammate's mailbox list.
func (h *Handler) auditConnectLinkUse(c *gin.Context, res *connectlink.Result, remoteIP string) {
	acc := res.Account
	if h.AuditService == nil || acc == nil || acc.OrganizationID == nil {
		return
	}
	linkID := res.Link.ID.String()
	h.AuditService.LogAction(c.Request.Context(), *acc.OrganizationID, res.ActorID, models.AuditActionConnect, models.AuditEntityEmailAccount, &acc.ID,
		remoteIP, c.Request.UserAgent(), nil, map[string]string{"provider": acc.Provider, "email": acc.Email, "via": "connect_link", "connect_link_id": linkID})
	h.AuditService.LogAction(c.Request.Context(), *acc.OrganizationID, res.ActorID, models.AuditActionUpdate, models.AuditEntityClientConnectLink, &res.Link.ID,
		remoteIP, c.Request.UserAgent(), nil, map[string]string{"connected": acc.Email})
	h.refreshAdvisorNow(*acc.OrganizationID)
}

// finishConnectLinkOAuth lands a client's provider sign-in and sends the browser back to the connect page.
func (h *Handler) finishConnectLinkOAuth(c *gin.Context, provider, code, state, providerErr string) {
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Cache-Control", "no-store")
	if h.ConnectLinkService == nil {
		renderNotice(c, http.StatusBadRequest, "Not connected", connectlink.ErrSigninExpired.Message)
		return
	}
	res, returnURL, xerr := h.ConnectLinkService.FinishOAuth(c.Request.Context(), provider, code, state, providerErr)
	if returnURL == "" {
		msg := connectlink.ErrSigninExpired.Message
		if xerr != nil && xerr.Code != errx.Internal {
			msg = xerr.Message
		}
		renderNotice(c, http.StatusBadRequest, "Not connected", msg)
		return
	}
	// The outcome rides in the fragment, which no server or log ever sees.
	frag := url.Values{}
	if xerr != nil {
		if xerr.Code == errx.Internal || xerr.Code == errx.ServiceUnavailable {
			log.Warn().Str("reason", xerr.ResponseCode()).Msg("connect link: client sign-in did not connect a mailbox")
		}
		frag.Set("error", xerr.ResponseCode())
	} else {
		h.auditConnectLinkUse(c, res, c.ClientIP())
		frag.Set("connected", res.Account.Email)
		frag.Set("provider", res.Account.Provider)
	}
	c.Redirect(http.StatusFound, returnURL+"#"+frag.Encode())
}

// InternalConnectOAuthResult is POST /api/v1/internal/connect-links/oauth/result: the page polls it while the
// client signs in, because a sign-in window often loses the page that opened it.
func (h *Handler) InternalConnectOAuthResult(c *gin.Context) {
	var req formwire.ConnectSigninResultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		connectRefusal(c, errx.New(errx.BadRequest, "The request could not be read."))
		return
	}
	link, ok := h.connectLinkOnAnyStatus(c, req.Token, req.Host)
	if !ok {
		return
	}
	out, xerr := h.ConnectLinkService.SigninResult(c.Request.Context(), link, req.ResultKey)
	if xerr != nil {
		connectRefusal(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}
