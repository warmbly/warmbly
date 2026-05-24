package handler

import (
	"html/template"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// callbackPage renders a tiny HTML page that hands the OAuth code + state
// back to the opening window via postMessage and then closes itself.
// The opener (the SPA) is expected to POST the code/state to
// /emails/onboarding/oauth/finish with the user's bearer token.
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
  <div class="t">{{.Status}}</div>
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
  try {
    if (window.opener) {
      window.opener.postMessage(payload, origin || "*");
    }
  } catch (e) { /* ignore */ }
  setTimeout(function(){ try { window.close(); } catch(e){} }, 400);
})();
</script>
</body></html>`))

type callbackData struct {
	Provider  string
	Code      string
	State     string
	Error     string
	Status    string
	AppOrigin string
}

func (h *Handler) EmailOAuthCallbackGmail(c *gin.Context) {
	renderOAuthCallback(c, "gmail")
}

func (h *Handler) EmailOAuthCallbackOutlook(c *gin.Context) {
	renderOAuthCallback(c, "outlook")
}

func renderOAuthCallback(c *gin.Context, provider string) {
	code := c.Query("code")
	state := c.Query("state")
	providerErr := c.Query("error")

	data := callbackData{
		Provider:  provider,
		Code:      code,
		State:     state,
		Error:     providerErr,
		Status:    "Connecting your mailbox… this window will close.",
		AppOrigin: os.Getenv("APP_ORIGIN"),
	}
	if providerErr != "" {
		data.Status = "Connection cancelled."
	} else if code == "" || state == "" {
		data.Error = "missing_code_or_state"
		data.Status = "Connection cancelled."
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = callbackPage.Execute(c.Writer, data)
}
