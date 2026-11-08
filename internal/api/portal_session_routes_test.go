package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type portalTokens struct {
	token.TokenService
	agency *uuid.UUID
}

func (f portalTokens) ValidateAccessToken(context.Context, string) (*models.Session, *errx.Error) {
	return &models.Session{ID: uuid.New(), UserID: uuid.New(), PortalAgencyID: f.agency}, nil
}

// Every refused entry names a route the router serves, so a renamed route cannot slip out of the list.
func TestPortalRefusedRoutesAreRegistered(t *testing.T) {
	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	r := Run(&handler.Handler{}, &middleware.Handler{}, &middleware.OidcHandler{}, "", gin.TestMode, nil)
	for _, entry := range middleware.PortalRefusedRoutes() {
		method, path := entry[0], entry[1]
		prefix, isPrefix := strings.CutSuffix(path, "/*")
		found := false
		for _, rt := range r.Routes() {
			if method != "" && rt.Method != method {
				continue
			}
			if rt.Path == path || (isPrefix && (rt.Path == prefix || strings.HasPrefix(rt.Path, prefix+"/"))) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("refused route %s %s matches no registered route", method, path)
		}
	}
	registered := map[string]bool{}
	for _, rt := range r.Routes() {
		registered[rt.Path] = true
	}
	for _, path := range middleware.PortalWorkspaceRoutes() {
		if !registered[path] {
			t.Errorf("workspace route %s is not registered", path)
		}
	}
}

// The gate sits in the session middleware, so it holds across every route group.
func TestPortalSessionRefusedOnAccountRoutes(t *testing.T) {
	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	agency := uuid.New()
	routes := []struct{ method, path string }{
		{http.MethodPost, "/v1/auth/me/password"},
		{http.MethodGet, "/v1/auth/sessions"},
		{http.MethodPost, "/v1/auth/2fa/enroll/start"},
		{http.MethodPost, "/v1/auth/passkey/register/begin"},
		{http.MethodDelete, "/v1/auth/me/identities/google"},
		{http.MethodPost, "/v1/me/danger-zone/delete"},
		{http.MethodPost, "/v1/organization"},
		{http.MethodPost, "/v1/api-keys"},
		{http.MethodPost, "/v1/oauth/authorize"},
		{http.MethodPost, "/v1/auth/cli/codes/ABCD/approve"},
		{http.MethodPost, "/v1/agency/" + agency.String() + "/api-keys"},
		{http.MethodGet, "/v1/agency/" + uuid.NewString() + "/clients"},
		{http.MethodPost, "/v1/organization/switch/" + uuid.NewString()},
		{http.MethodPost, "/v1/auth/portal/handoff"},
		{http.MethodPost, "/v1/cloud-link/connect"},
		{http.MethodGet, "/admin/users"},
	}
	r := Run(&handler.Handler{}, &middleware.Handler{TokenService: portalTokens{agency: &agency}}, &middleware.OidcHandler{}, "", gin.TestMode, nil)
	for _, rt := range routes {
		req := httptest.NewRequestWithContext(context.Background(), rt.method, rt.path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer t")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != http.StatusForbidden || body.Code != middleware.CodePortalSessionRestricted {
			t.Errorf("%s %s = %d %q, want 403 %s", rt.method, rt.path, w.Code, body.Code, middleware.CodePortalSessionRestricted)
		}
	}

	// The same routes are not refused for that reason to a session signed in on the main address.
	plain := Run(&handler.Handler{}, &middleware.Handler{TokenService: portalTokens{}}, &middleware.OidcHandler{}, "", gin.TestMode, nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/agency/"+uuid.NewString()+"/clients", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	func() {
		defer func() { _ = recover() }()
		plain.ServeHTTP(w, req)
	}()
	if strings.Contains(w.Body.String(), middleware.CodePortalSessionRestricted) {
		t.Fatalf("a main-address session was refused as a portal session: %s", w.Body.String())
	}
}
