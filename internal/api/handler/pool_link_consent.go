package handler

import (
	"crypto/subtle"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/poollink"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// A brokered mailbox sign-in for a linked instance passes through this page on
// the cloud's own origin. It names who is asking, and the cookie it sets is
// what the provider callback requires, so the sign-in completes only in the
// browser where the person read this page and chose to continue.

const brokerCookieName = "warmbly_broker"

// brokerCookiePath covers the consent page, its continue step and both provider callbacks.
const brokerCookiePath = "/addresses/"

var brokerConsentPage = template.Must(template.New("broker-consent").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Connect a mailbox to Warmbly Cloud</title>
<style>
  html,body{margin:0;min-height:100%;font:14px/1.5 -apple-system,Segoe UI,Inter,sans-serif;color:#0f172a;background:#f8fafc}
  .wrap{display:flex;align-items:center;justify-content:center;min-height:100vh;padding:16px;box-sizing:border-box}
  .card{max-width:460px;padding:24px 28px;border:1px solid #e2e8f0;border-radius:8px;background:#fff;box-shadow:0 8px 24px -12px rgba(15,23,42,.18)}
  .h{font-size:15px;font-weight:600;margin:0 0 10px}
  .t{font-size:13px;color:#334155;margin:0 0 10px}
  .w{font-size:12.5px;color:#92400e;background:#fffbeb;border:1px solid #fde68a;border-radius:6px;padding:8px 10px;margin:12px 0 16px}
  .b{display:inline-block;background:#0284c7;color:#fff;border:0;cursor:pointer;font:inherit;font-size:13px;font-weight:500;padding:8px 14px;border-radius:6px}
  .m{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12.5px;background:#f1f5f9;padding:1px 5px;border-radius:4px;word-break:break-all}
</style></head>
<body><div class="wrap"><div class="card">
  <p class="h">Connect a {{.Provider}} mailbox to Warmbly Cloud</p>
  <p class="t">The self-hosted Warmbly instance at <span class="m">{{.Host}}</span>{{if .Name}} ({{.Name}}){{end}} asked to connect a mailbox through Warmbly Cloud.</p>
  <p class="t">If you continue and sign in, the mailbox is added to the Warmbly Cloud workspace <strong>{{if .Workspace}}{{.Workspace}}{{else}}linked to that instance{{end}}</strong>, and that instance can send and read mail as it.</p>
  <p class="w">Only continue if you started this yourself, from that instance. If someone sent you this link, close this tab.</p>
  <form method="post" action="{{.ContinueURL}}"><input type="hidden" name="state" value="{{.State}}"><input type="hidden" name="t" value="{{.Token}}"><button class="b" type="submit">Continue to {{.Provider}}</button></form>
</div></div></body></html>`))

func brokerProviderName(p models.InboxProvider) string {
	if p == models.InboxProviderOutlook {
		return "Microsoft"
	}
	return "Google"
}

// renderBrokerNotice answers every refused step with a plain page; nothing on it is actionable.
func renderBrokerNotice(c *gin.Context, xerr *errx.Error) {
	status, body := http.StatusBadRequest, "This sign-in link has expired or was already used. Start again from your instance."
	if xerr != nil && xerr.Code != errx.Internal {
		body = xerr.Message
	} else if xerr != nil {
		status, body = http.StatusInternalServerError, "Something went wrong. Start again from your instance."
	}
	renderNotice(c, status, "Sign-in not started", body)
}

// brokerCookieSecure is always true outside development; plain-http dev may omit it.
func brokerCookieSecure(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))) {
	case "", "dev", "development", "local":
		return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	return true
}

func setBrokerCookie(c *gin.Context, value string, maxAge int) {
	secure := brokerCookieSecure(c.Request)
	http.SetCookie(c.Writer, &http.Cookie{
		Name: brokerCookieName, Value: value, Path: brokerCookiePath, MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

// PoolLinkOAuthConsentPage is GET /addresses/connect?state=..., the page a brokered sign-in opens on.
func (h *Handler) PoolLinkOAuthConsentPage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if h.PoolLinkService == nil {
		renderBrokerNotice(c, nil)
		return
	}
	state := c.Query("state")
	consent, xerr := h.PoolLinkService.DescribeOAuthConsent(c.Request.Context(), state)
	if xerr != nil {
		renderBrokerNotice(c, xerr)
		return
	}
	// One value per browser, so two sign-ins open side by side both still complete.
	token, _ := c.Cookie(brokerCookieName)
	if len(token) < 16 {
		var err error
		if token, err = crypt.Nonce(); err != nil {
			renderBrokerNotice(c, errx.InternalError())
			return
		}
	}
	setBrokerCookie(c, token, 15*60)
	// The binding value is posted in the form body, never put in a URL.
	data := struct{ Provider, Host, Name, Workspace, ContinueURL, State, Token string }{
		Provider:    brokerProviderName(consent.Provider),
		Host:        consent.InstanceHost,
		Name:        consent.InstanceName,
		Workspace:   consent.WorkspaceName,
		ContinueURL: poollink.BrokerConsentPath + "/continue",
		State:       state,
		Token:       token,
	}
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = brokerConsentPage.Execute(c.Writer, data)
}

// PoolLinkOAuthContinue is the consent page's button: it binds the sign-in to this browser and opens the provider.
func (h *Handler) PoolLinkOAuthContinue(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if h.PoolLinkService == nil {
		renderBrokerNotice(c, nil)
		return
	}
	// The form posts the cookie's value, which only the consent page rendered in this browser knows.
	if c.Request.Method != http.MethodPost {
		renderBrokerNotice(c, poollink.ErrOAuthBrowser)
		return
	}
	state, sent := c.PostForm("state"), c.PostForm("t")
	cookie, _ := c.Cookie(brokerCookieName)
	if cookie == "" || subtle.ConstantTimeCompare([]byte(cookie), []byte(sent)) != 1 {
		renderBrokerNotice(c, poollink.ErrOAuthBrowser)
		return
	}
	to, xerr := h.PoolLinkService.ContinueOAuth(c.Request.Context(), state, cookie)
	if xerr != nil {
		renderBrokerNotice(c, xerr)
		return
	}
	c.Redirect(http.StatusSeeOther, to)
}
