package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/integration"
)

type returnOriginService struct {
	email.EmailService
	origin string
	state  string
}

func (s *returnOriginService) OAuthReturnOrigin(_ context.Context, state string) string {
	s.state = state
	return s.origin
}

func TestMailboxCallbacksUseStateBoundDashboard(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://app.warmbly.com,https://second.example.com")
	for _, tt := range []struct{ name, origin, want string }{
		{"second dashboard", "https://second.example.com", "https://second.example.com"},
		{"primary dashboard", "https://app.warmbly.com", "https://app.warmbly.com"},
		{"missing or expired state", "", ""},
		{"untrusted origin", "https://evil.example.com", ""},
	} {
		for _, provider := range []string{"gmail", "outlook"} {
			t.Run(tt.name+"/"+provider, func(t *testing.T) {
				svc := &returnOriginService{origin: tt.origin}
				h := &Handler{EmailService: svc}
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodGet, "/addresses/google/callback?code=c&state=w.nonce", nil)
				h.renderOAuthCallback(c, provider)
				body := w.Body.String()
				relay := ""
				if tt.want != "" {
					relay = tt.want + "/oauth-return"
				}
				if w.Code != http.StatusOK || svc.state != "w.nonce" || !strings.Contains(body, `var origin = "`+tt.want+`"`) || !strings.Contains(body, `var relay = "`+relay+`"`) {
					t.Fatalf("callback did not use the expected state-bound target: %d %s", w.Code, body)
				}
				if w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("callback must not leak through referrers or caches")
				}
			})
		}
	}
}

func callbackRecorder(t *testing.T, h *Handler, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/addresses/outlook/callback?"+query, nil)
	h.EmailOAuthCallbackOutlook(c)
	return w
}

// The approval link's return hands nothing to an opener and never falls back to the app scheme.
func TestOutlookAdminApprovalReturnIsAStandalonePage(t *testing.T) {
	h := &Handler{}
	ok := callbackRecorder(t, h, "admin_consent=True&tenant=11111111-1111-1111-1111-111111111111&state=oac_approval")
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), "Approved") || strings.Contains(ok.Body.String(), "postMessage") || strings.Contains(ok.Body.String(), "warmbly://") {
		t.Fatalf("approved page = %d %s", ok.Code, ok.Body.String())
	}
	refused := callbackRecorder(t, h, "error=access_denied&state=oac_approval")
	if !strings.Contains(refused.Body.String(), "Not approved") {
		t.Fatalf("refused page = %s", refused.Body.String())
	}
}

var webFlag = regexp.MustCompile(`var web =\s*true\s*;`)

// Unknown dashboard state never forwards codes; native callbacks retain their scheme.
func TestCallbackRefusesUnknownDashboardState(t *testing.T) {
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("APP_URL", "https://app.acme.io/")
	h := &Handler{}
	for _, state := range []string{"w.abc", "gac_abc", "mac_abc"} {
		w := callbackRecorder(t, h, "code=c&state="+state)
		body := w.Body.String()
		if !strings.Contains(body, `var relay = ""`) || !webFlag.MatchString(body) || strings.Contains(body, `https://app.acme.io/oauth-return`) {
			t.Fatalf("state %s page = %s", state, body)
		}
		if w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("state %s referrer policy = %q", state, w.Header().Get("Referrer-Policy"))
		}
	}
	native := callbackRecorder(t, h, "code=c&state=abc").Body.String()
	if webFlag.MatchString(native) || !strings.Contains(native, "warmbly://email-oauth") {
		t.Fatalf("native page = %s", native)
	}
}

type integrationReturnOriginService struct {
	integration.Service
	origin string
}

func (s *integrationReturnOriginService) OAuthReturnOrigin(_ context.Context, _ string) string {
	return s.origin
}

func TestIntegrationCallbackUsesStateBoundDashboard(t *testing.T) {
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("APP_URL", "https://app.acme.io")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://second.example.com")
	for _, origin := range []string{"https://app.acme.io", "https://second.example.com", "https://evil.example.com", ""} {
		h := &Handler{IntegrationService: &integrationReturnOriginService{origin: origin}}
		w := serve(t, h.IntegrationOAuthCallback, "/cb?state=s&code=c")
		want := origin
		if origin == "https://evil.example.com" {
			want = ""
		}
		relay := ""
		if want != "" {
			relay = want + "/oauth-return"
		}
		if !strings.Contains(w.Body.String(), `var origin = "`+want+`"`) || !strings.Contains(w.Body.String(), `var relay = "`+relay+`"`) || !strings.Contains(w.Body.String(), "source=integration") {
			t.Fatalf("integration origin %q page = %s", origin, w.Body.String())
		}
		if w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("integration callback must not leak through referrers or caches")
		}
	}
}
