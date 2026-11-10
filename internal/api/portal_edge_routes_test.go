package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/api/middleware"
)

// The portal service's token reaches the routes its client pages call and no other internal route, and no other token reaches them.
func TestPortalEdgeTokenReachesOnlyThePortalRoutes(t *testing.T) {
	const portalToken = "portal-edge-route-token-0123456789abcdef"
	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	t.Setenv("INTERNAL_API_TOKEN", "edge-route-token")
	t.Setenv("NODE_BROKER_TOKEN", "node-route-token")
	t.Setenv("PORTAL_EDGE_TOKEN", portalToken)
	r := Run(&handler.Handler{}, &middleware.Handler{}, &middleware.OidcHandler{}, "", gin.TestMode, nil)
	code := func(method, path, token string) int {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	link := strings.Repeat("a", 43)
	portalRoutes := [][2]string{
		{http.MethodGet, "/api/v1/internal/portals/portal.agency.test"},
		{http.MethodGet, "/api/v1/internal/reports/" + link},
		{http.MethodPost, "/api/v1/internal/connect-links/lookup"},
		{http.MethodPost, "/api/v1/internal/connect-links/oauth/start"},
		{http.MethodPost, "/api/v1/internal/connect-links/oauth/result"},
		{http.MethodPost, "/api/v1/internal/connect-links/smtp-imap"},
	}
	for _, rt := range portalRoutes {
		if got := code(rt[0], rt[1], portalToken); got == http.StatusUnauthorized {
			t.Errorf("%s %s refused the portal token", rt[0], rt[1])
		}
		for _, token := range []string{"edge-route-token", "node-route-token"} {
			if got := code(rt[0], rt[1], token); got != http.StatusUnauthorized {
				t.Errorf("%s %s admitted %q: %d", rt[0], rt[1], token[:4], got)
			}
		}
	}
	for _, rt := range [][2]string{
		{http.MethodGet, "/api/v1/internal/forms/pub123"},
		{http.MethodPost, "/api/v1/internal/forms/pub123/submissions"},
		{http.MethodGet, "/api/v1/internal/tracked-links/" + uuid.NewString()},
		{http.MethodGet, "/api/v1/internal/domain-redirects/acme.test"},
		{http.MethodPost, "/api/v1/internal/page-hits"},
		{http.MethodGet, "/api/v1/internal/dek/" + uuid.NewString()},
		{http.MethodPost, "/api/v1/internal/dek/decrypt"},
		{http.MethodGet, "/api/v1/internal/fleet/node-env"},
	} {
		if got := code(rt[0], rt[1], portalToken); got != http.StatusUnauthorized {
			t.Errorf("%s %s admitted the portal token: %d", rt[0], rt[1], got)
		}
	}
}
