package handler

import (
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/poollink"
	"github.com/warmbly/warmbly/internal/config"
)

// callbackPage renders a tiny HTML page that hands the OAuth code + state
// back to the opening window via postMessage and then closes itself.
// The opener (the SPA) is expected to POST the code/state to
// /emails/onboarding/oauth/finish with the user's bearer token.
//
// Without an opener, a flow the dashboard started goes to the dashboard's
// /oauth-return page, which hands it to the waiting tab; any other (the native
// app's ASWebAuthenticationSession) redirects to the app's warmbly:// scheme,
// which the session intercepts before the app calls oauth/finish itself.
//
// We keep this on the API rather than the SPA so that the provider's
// registered redirect_uri stays under our control and survives front-end
// reshuffles.
var callbackPage = template.Must(template.New("oauth-cb").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Connecting…</title>
<style>
  html,body{margin:0;height:100%;font:14px/1.4 -apple-system,Segoe UI,Inter,sans-serif;color:#0f172a;background:#f8fafc}
  .wrap{display:flex;align-items:center;justify-content:center;height:100%}
  .card{padding:24px 28px;border:1px solid #e2e8f0;border-radius:8px;background:#fff;box-shadow:0 8px 24px -12px rgba(15,23,42,.18)}
  .t{font-size:13px;color:#64748b}
  .err{color:#b91c1c;margin-top:6px;font-size:12px}
</style></head>
<body><div class="wrap"><div class="card">
  <div class="t" id="status">{{.Status}}</div>
  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
</div></div>
<script>
(function(){
  var payload = {
    type: "email_oauth_callback",
    provider: {{.Provider}},
    code: {{.Code}},
    state: {{.State}},
    error: {{.Error}}
  };
  var origin = {{.AppOrigin}};
  var relay = {{.Relay}};
  var web = {{.Web}};
  var hasOpener = false;
  try { hasOpener = !!window.opener; } catch (e) { /* ignore */ }
  if ((hasOpener || web) && (!origin || !relay)) {
    document.getElementById("status").textContent = {{.NoOriginNotice}};
    return;
  }
  if (hasOpener) {
    try { window.opener.postMessage(payload, origin); } catch (e) { /* ignore */ }
    setTimeout(function(){ try { window.close(); } catch(e){} }, 400);
    return;
  }
  var q = "provider=" + encodeURIComponent(payload.provider || "") +
    "&code=" + encodeURIComponent(payload.code || "") +
    "&state=" + encodeURIComponent(payload.state || "") +
    "&error=" + encodeURIComponent(payload.error || "");
  if (web) {
    try { window.location.replace(relay + "#source=mailbox&" + q); } catch (e) { /* ignore */ }
    return;
  }
  try { window.location.replace("warmbly://email-oauth?" + q); } catch (e) { /* ignore */ }
  setTimeout(function(){ try { window.close(); } catch(e){} }, 400);
})();
</script>
</body></html>`))

type callbackData struct {
	Provider       string
	Code           string
	State          string
	Error          string
	Status         string
	AppOrigin      string
	Relay          string
	Web            bool
	NoOriginNotice string
}

// callbackNoOriginNotice is shown instead of handing a code to an opener whose origin is unknown.
const callbackNoOriginNotice = "This sign-in has expired or has no trusted dashboard address. Close this window and start again from your dashboard. If it keeps happening, ask the operator to check APP_URL and the dashboard origin allowlist."

// callbackTargetOrigin is the one origin an authorization code is posted to:
// APP_ORIGIN when set, else the origin of APP_URL. Empty delivers nothing.
func callbackTargetOrigin() string {
	if v := strings.TrimSpace(os.Getenv("APP_ORIGIN")); v != "" {
		return v
	}
	u, err := url.Parse(config.AppBaseURL())
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// callbackRelayURL is the dashboard page a sign-in window without an opener hands its result to.
func callbackRelayURL() string {
	u, err := url.Parse(config.AppBaseURL())
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.TrimRight(u.String(), "/") + "/oauth-return"
}

// isDashboardState reports whether only the dashboard can have started the flow behind state.
func isDashboardState(state string) bool {
	return email.IsWebState(state) || strings.HasPrefix(state, delegation.GoogleStatePrefix) || strings.HasPrefix(state, delegation.MicrosoftStatePrefix)
}

func (h *Handler) EmailOAuthCallbackGmail(c *gin.Context) {
	h.renderOAuthCallback(c, "gmail")
}

func (h *Handler) EmailOAuthCallbackOutlook(c *gin.Context) {
	h.renderOAuthCallback(c, "outlook")
}

func (h *Handler) renderOAuthCallback(c *gin.Context, provider string) {
	code := c.Query("code")
	state := c.Query("state")
	providerErr := c.Query("error")

	// A brokered consent completes here, only in the browser the consent page bound it to.
	if h.PoolLinkService != nil && poollink.IsBrokerState(state) {
		binding, _ := c.Cookie(brokerCookieName)
		to, xerr := h.PoolLinkService.CompleteOAuthCallback(c.Request.Context(), provider, code, state, providerErr, binding)
		if xerr != nil {
			renderBrokerNotice(c, xerr)
			return
		}
		c.Redirect(http.StatusFound, to)
		return
	}

	// Microsoft reports an admin consent with no code; the admin grant then needs a sign-in, same state.
	if strings.HasPrefix(state, delegation.MicrosoftStatePrefix) && providerErr == "" && code == "" &&
		strings.EqualFold(c.Query("admin_consent"), "true") && h.DelegationService != nil {
		if to := h.DelegationService.MicrosoftSigninURL(c.Request.Context(), state); to != "" {
			c.Redirect(http.StatusFound, to)
			return
		}
	}

	// An administrator approving single-mailbox sign-in may be anywhere, with no dashboard to hand back to.
	if state == email.OutlookAdminApprovalState {
		renderAdminApproval(c, providerErr == "" && strings.EqualFold(c.Query("admin_consent"), "true"))
		return
	}

	data := callbackData{
		Provider:       provider,
		Code:           code,
		State:          state,
		Error:          providerErr,
		Status:         "Connecting your mailbox… this window will close.",
		AppOrigin:      callbackTargetOrigin(),
		Relay:          callbackRelayURL(),
		Web:            isDashboardState(state),
		NoOriginNotice: callbackNoOriginNotice,
	}
	if data.Web {
		origin := ""
		if email.IsWebState(state) && h.EmailService != nil {
			origin = h.EmailService.OAuthReturnOrigin(c.Request.Context(), state)
		} else if h.DelegationService != nil {
			origin = h.DelegationService.OAuthReturnOrigin(c.Request.Context(), state)
		}
		data.AppOrigin = config.DashboardOrigin(origin)
		data.Relay = ""
		if data.AppOrigin != "" {
			data.Relay = data.AppOrigin + "/oauth-return"
		}
	}
	if strings.HasPrefix(state, delegation.GoogleStatePrefix) {
		data.Status = "Signed in. Finishing in Warmbly… this window will close."
	}
	if providerErr != "" {
		data.Status = "Connection cancelled."
	} else if code == "" || state == "" {
		data.Error = "missing_code_or_state"
		data.Status = "Connection cancelled."
	}

	// This page is one inline script that hands the code to the opener and
	// closes. It loads nothing and submits nothing, so the policy says so;
	// 'unsafe-inline' covers the script that is the page itself.
	// Cross-Origin-Opener-Policy is relaxed here because talking to the
	// opener is the whole job, and the message is addressed to one origin.
	c.Header("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	c.Header("Cross-Origin-Opener-Policy", "unsafe-none")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = callbackPage.Execute(c.Writer, data)
}

var noticePage = template.Must(template.New("oauth-notice").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
  html,body{margin:0;height:100%;font:14px/1.5 -apple-system,Segoe UI,Inter,sans-serif;color:#0f172a;background:#f8fafc}
  .wrap{display:flex;align-items:center;justify-content:center;height:100%;padding:16px;box-sizing:border-box}
  .card{max-width:420px;padding:24px 28px;border:1px solid #e2e8f0;border-radius:8px;background:#fff;box-shadow:0 8px 24px -12px rgba(15,23,42,.18)}
  .h{font-size:15px;font-weight:600;margin:0 0 6px}
  .t{font-size:13px;color:#64748b;margin:0}
</style></head>
<body><div class="wrap"><div class="card">
  <p class="h">{{.Title}}</p>
  <p class="t">{{.Body}}</p>
</div></div></body></html>`))

// renderAdminApproval answers the return from an administrator approving single-mailbox Microsoft sign-in.
func renderAdminApproval(c *gin.Context, approved bool) {
	data := struct{ Title, Body string }{
		Title: "Approved",
		Body:  "People in your organization can now connect their Microsoft mailboxes to Warmbly. You can close this window.",
	}
	if !approved {
		data.Title = "Not approved"
		data.Body = "Microsoft did not record the approval. Open the link again and sign in as a Global Administrator, Cloud Application Administrator or Application Administrator."
	}
	renderNotice(c, http.StatusOK, data.Title, data.Body)
}

// renderNotice is a standalone page with a title and one sentence, and nothing to run.
func renderNotice(c *gin.Context, status int, title, body string) {
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	_ = noticePage.Execute(c.Writer, struct{ Title, Body string }{title, body})
}
