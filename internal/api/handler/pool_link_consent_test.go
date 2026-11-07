package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/poollink"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type fakeBroker struct {
	poollink.Service
	bound, completedWith string
}

func (f *fakeBroker) DescribeOAuthConsent(context.Context, string) (*models.PoolLinkOAuthConsent, *errx.Error) {
	return &models.PoolLinkOAuthConsent{Provider: models.InboxProviderGoogle, InstanceHost: "warmbly.acme.io", WorkspaceName: "Acme"}, nil
}

func (f *fakeBroker) ContinueOAuth(_ context.Context, _ string, binding string) (string, *errx.Error) {
	f.bound = binding
	return "https://accounts.google.com/o/oauth2/auth?x=1", nil
}

func (f *fakeBroker) CompleteOAuthCallback(_ context.Context, _, _, _, _, binding string) (string, *errx.Error) {
	f.completedWith = binding
	if binding == "" {
		return "", poollink.ErrOAuthBrowser
	}
	return "https://warmbly.acme.io/cloud-oauth/done?status=ok", nil
}

func serve(t *testing.T, fn gin.HandlerFunc, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	for _, ck := range cookies {
		c.Request.AddCookie(ck)
	}
	fn(c)
	return w
}

func servePost(t *testing.T, fn gin.HandlerFunc, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/addresses/connect/continue", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, ck := range cookies {
		c.Request.AddCookie(ck)
	}
	fn(c)
	c.Writer.WriteHeaderNow()
	return w
}

// The browser-binding cookie is Secure in every environment but plain-http development.
func TestBrokerCookieSecureOutsideDevelopment(t *testing.T) {
	plain := httptest.NewRequest(http.MethodGet, "http://cloud.example/addresses/connect", nil)
	forwarded := httptest.NewRequest(http.MethodGet, "http://cloud.example/addresses/connect", nil)
	forwarded.Header.Set("X-Forwarded-Proto", "https")

	for _, env := range []string{"production", "staging", "prod"} {
		t.Setenv("APP_ENV", env)
		if !brokerCookieSecure(plain) {
			t.Fatalf("APP_ENV=%s over plain http: cookie not Secure", env)
		}
	}
	for _, env := range []string{"", "dev", "development", "local"} {
		t.Setenv("APP_ENV", env)
		if brokerCookieSecure(plain) {
			t.Fatalf("APP_ENV=%q over plain http: cookie Secure", env)
		}
		if !brokerCookieSecure(forwarded) {
			t.Fatalf("APP_ENV=%q behind https: cookie not Secure", env)
		}
	}
}

func TestBrokeredSigninCompletesOnlyInTheBrowserThatContinued(t *testing.T) {
	f := &fakeBroker{}
	h := &Handler{PoolLinkService: f}

	page := serve(t, h.PoolLinkOAuthConsentPage, "/addresses/connect?state=pl_x")
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "warmbly.acme.io") || !strings.Contains(body, "Acme") || strings.Contains(body, "accounts.google.com") {
		t.Fatalf("consent page = %d %s", page.Code, body)
	}
	cookies := page.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != brokerCookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v", cookies)
	}
	token := cookies[0].Value
	if strings.Contains(body, "?state=") || strings.Contains(body, "t="+token) || !strings.Contains(body, `method="post"`) {
		t.Fatalf("the consent page must post the binding value, never link it: %s", body)
	}

	if w := servePost(t, h.PoolLinkOAuthContinue, url.Values{"state": {"pl_x"}, "t": {token}}); w.Code == http.StatusSeeOther || f.bound != "" {
		t.Fatalf("continue without the cookie = %d", w.Code)
	}
	if w := servePost(t, h.PoolLinkOAuthContinue, url.Values{"state": {"pl_x"}, "t": {"wrong"}}, cookies[0]); w.Code == http.StatusSeeOther || f.bound != "" {
		t.Fatalf("continue with a token the page did not render = %d", w.Code)
	}
	if w := servePost(t, h.PoolLinkOAuthContinue, url.Values{"state": {"pl_x"}, "t": {token}}, cookies[0]); w.Code != http.StatusSeeOther || f.bound != token {
		t.Fatalf("continue from the page = %d, bound %q", w.Code, f.bound)
	}
	f.bound = ""
	if w := serve(t, h.PoolLinkOAuthContinue, "/addresses/connect/continue?state=pl_x&t="+token, cookies[0]); w.Code == http.StatusFound || w.Code == http.StatusSeeOther || f.bound != "" {
		t.Fatalf("continue with the binding value in the URL = %d, bound %q", w.Code, f.bound)
	}
	if w := servePost(t, h.PoolLinkOAuthContinue, url.Values{"state": {"pl_x"}, "t": {token}}, cookies[0]); w.Code != http.StatusSeeOther || f.bound != token {
		t.Fatalf("continue from the page again = %d, bound %q", w.Code, f.bound)
	}

	if w := serve(t, h.EmailOAuthCallbackGmail, "/addresses/google/callback?state=pl_x&code=c"); w.Code == http.StatusFound || strings.Contains(w.Body.String(), "postMessage") {
		t.Fatalf("callback in another browser = %d %s", w.Code, w.Body.String())
	}
	if w := serve(t, h.EmailOAuthCallbackGmail, "/addresses/google/callback?state=pl_x&code=c", cookies[0]); w.Code != http.StatusFound || f.completedWith != token {
		t.Fatalf("callback in the bound browser = %d", w.Code)
	}
}

func TestCallbackPostsOnlyToAConfiguredOrigin(t *testing.T) {
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("APP_URL", "")
	t.Setenv("FRONTEND_BASE_URL", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "")
	t.Setenv("PUBLIC_HOST", "")
	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	h := &Handler{}
	for _, fn := range []gin.HandlerFunc{h.EmailOAuthCallbackGmail, h.IntegrationOAuthCallback} {
		body := serve(t, fn, "/cb?state=s&code=c").Body.String()
		if strings.Contains(body, `"*"`) || !strings.Contains(body, "APP_URL") {
			t.Fatalf("unconfigured origin page = %s", body)
		}
	}

	t.Setenv("APP_ORIGIN", "https://app.acme.io")
	body := serve(t, h.EmailOAuthCallbackGmail, "/cb?state=s&code=c").Body.String()
	if !strings.Contains(body, `"https://app.acme.io"`) || strings.Contains(body, `"*"`) {
		t.Fatalf("configured origin page = %s", body)
	}
}
