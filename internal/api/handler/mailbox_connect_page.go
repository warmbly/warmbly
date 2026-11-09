package handler

import (
	"crypto/subtle"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/mailboxconnect"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// A sign-in link opens here. The page names the workspace asking, and the
// cookie it sets is what the provider callback requires, so the mailbox
// connects only in the browser where its owner read this and continued.

const connectCookieName = "warmbly_connect"

// connectFormTargets are where Continue may redirect: the Google and Microsoft authorize hosts.
const connectFormTargets = "https://accounts.google.com https://login.microsoftonline.com"

var connectLinkPage = template.Must(template.New("connect-link").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Connect your mailbox</title>
<style>
  html,body{margin:0;min-height:100%;font:14px/1.5 -apple-system,Segoe UI,Inter,sans-serif;color:#0f172a;background:#f8fafc}
  .wrap{display:flex;align-items:center;justify-content:center;min-height:100vh;padding:16px;box-sizing:border-box}
  .card{max-width:460px;width:100%;padding:24px 28px;border:1px solid #e2e8f0;border-radius:8px;background:#fff;box-shadow:0 8px 24px -12px rgba(15,23,42,.18);box-sizing:border-box}
  .h{font-size:15px;font-weight:600;margin:0 0 10px}
  .t{font-size:13px;color:#334155;margin:0 0 10px}
  .w{font-size:12.5px;color:#92400e;background:#fffbeb;border:1px solid #fde68a;border-radius:6px;padding:8px 10px;margin:12px 0 16px}
  .b{display:inline-block;background:#0284c7;color:#fff;border:0;cursor:pointer;font:inherit;font-size:13px;font-weight:500;padding:8px 14px;border-radius:6px}
  .m{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12.5px;background:#f1f5f9;padding:1px 5px;border-radius:4px;word-break:break-all}
</style></head>
<body><div class="wrap"><div class="card">
  <p class="h">Connect your {{.Provider}} mailbox</p>
  <p class="t"><strong>{{.Workspace}}</strong> asked you to connect {{if .Email}}<span class="m">{{.Email}}</span>{{else}}a mailbox{{end}}{{if .App}} through <strong>{{.App}}</strong>{{end}}.</p>
  <p class="t">If you continue and sign in, that workspace can send and read mail as this mailbox, and warm it up.</p>
  <p class="w">Only continue if you expected this request. If you do not know who sent you this link, close this tab.</p>
  <form method="post" action="{{.ContinueURL}}"><input type="hidden" name="t" value="{{.Token}}"><button class="b" type="submit">Continue to {{.Provider}}</button></form>
</div></div></body></html>`))

func setConnectCookie(c *gin.Context, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: connectCookieName, Value: value, Path: brokerCookiePath, MaxAge: maxAge,
		HttpOnly: true, Secure: brokerCookieSecure(c.Request), SameSite: http.SameSiteLaxMode,
	})
}

func renderConnectNotice(c *gin.Context, title string, xerr *errx.Error) {
	status, body := http.StatusBadRequest, mailboxconnect.ErrLinkGone.Message
	if xerr != nil && xerr.Code != errx.Internal {
		body = xerr.Message
	} else if xerr != nil {
		status, body = http.StatusInternalServerError, "Something went wrong. Open the link again in a moment."
	}
	renderNotice(c, status, title, body)
}

// MailboxConnectLinkPage is GET /addresses/link/:token.
func (h *Handler) MailboxConnectLinkPage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if h.MailboxConnectService == nil {
		renderConnectNotice(c, "Link unavailable", nil)
		return
	}
	token := c.Param("token")
	view, xerr := h.MailboxConnectService.Describe(c.Request.Context(), token)
	if xerr != nil {
		renderConnectNotice(c, "Link unavailable", xerr)
		return
	}
	binding, _ := c.Cookie(connectCookieName)
	if len(binding) < 16 {
		var err error
		if binding, err = crypt.Nonce(); err != nil {
			renderConnectNotice(c, "Link unavailable", errx.InternalError())
			return
		}
	}
	setConnectCookie(c, binding, 20*60)
	data := struct{ Provider, Workspace, Email, App, ContinueURL, Token string }{
		Provider:    brokerProviderName(view.Provider),
		Workspace:   displayname.DisplayableOr(view.Workspace, "A Warmbly workspace"),
		Email:       view.Email,
		App:         displayname.Displayable(view.App),
		ContinueURL: mailboxconnect.LinkPath + token + "/continue",
		Token:       binding,
	}
	// Browsers hold the redirect a form submission ends in to form-action, so the providers' sign-in hosts are listed.
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' "+connectFormTargets)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = connectLinkPage.Execute(c.Writer, data)
}

// MailboxConnectLinkContinue is POST /addresses/link/:token/continue: it binds the sign-in to this browser and opens the provider.
func (h *Handler) MailboxConnectLinkContinue(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if h.MailboxConnectService == nil {
		renderConnectNotice(c, "Link unavailable", nil)
		return
	}
	// The form posts the cookie's value, which only the page rendered in this browser knows.
	cookie, _ := c.Cookie(connectCookieName)
	if cookie == "" || subtle.ConstantTimeCompare([]byte(cookie), []byte(c.PostForm("t"))) != 1 {
		renderConnectNotice(c, "Sign-in not started", mailboxconnect.ErrLinkBrowser)
		return
	}
	to, xerr := h.MailboxConnectService.Continue(c.Request.Context(), c.Param("token"), cookie)
	if xerr != nil {
		renderConnectNotice(c, "Sign-in not started", xerr)
		return
	}
	c.Redirect(http.StatusSeeOther, to)
}

// finishConnectLink lands a provider callback a sign-in link started.
func (h *Handler) finishConnectLink(c *gin.Context, code, state, providerErr string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	binding, _ := c.Cookie(connectCookieName)
	res, xerr := h.MailboxConnectService.Finish(c.Request.Context(), code, state, providerErr, binding)
	if res != nil && res.Link != nil {
		// No session here; the link's creator is the actor, as the mailbox's owner of record.
		if res.Account != nil && h.AuditService != nil {
			h.AuditService.LogAction(c.Request.Context(), res.Link.OrganizationID, res.Link.CreatedBy, models.AuditActionConnect, models.AuditEntityEmailAccount, &res.Account.ID,
				c.ClientIP(), c.Request.UserAgent(), nil, map[string]string{
					"provider": res.Account.Provider, "email": res.Account.Email, "via": "connect_link", "link_id": res.Link.ID.String(),
				})
		}
		// The cookie is left to expire: other sign-ins open in this browser still need it.
		if res.ReturnURL != "" {
			c.Redirect(http.StatusFound, res.ReturnURL)
			return
		}
	}
	if xerr != nil {
		renderConnectNotice(c, "Mailbox not connected", xerr)
		return
	}
	if res != nil && res.WarmupNote != "" {
		renderNotice(c, http.StatusOK, "Mailbox connected", "Your mailbox is connected, but not all of its warmup applied: "+res.WarmupNote+" The workspace that sent this link can finish it. You can close this tab.")
		return
	}
	renderNotice(c, http.StatusOK, "Mailbox connected", "Your mailbox is connected. You can close this tab.")
}
