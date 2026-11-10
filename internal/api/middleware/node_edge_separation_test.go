package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// With NODE_BROKER_TOKEN set, the token the tracking and forms services hold opens no node route.
func TestEdgeAndNodeTokensOpenOnlyTheirOwnRouteGroups(t *testing.T) {
	t.Setenv("INTERNAL_API_TOKEN", "edge-token")
	t.Setenv("NODE_BROKER_TOKEN", "node-token")
	t.Setenv("NODE_ACCEPT_INTERNAL_TOKEN", "")
	internalTokenOnce, brokerTokenOnce = sync.Once{}, sync.Once{}
	t.Cleanup(func() {
		internalTokenOnce, brokerTokenOnce = sync.Once{}, sync.Once{}
		internalToken, brokerToken = nil, nil
	})
	h := &Handler{}
	r := gin.New()
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.GET("/edge", h.InternalAuthMiddleware(), ok)
	r.GET("/broker", h.NodeBrokerAuthMiddleware(), ok)
	r.GET("/node", h.NodeAuthMiddleware(), ok)
	for _, tc := range []struct {
		path, token string
		want        int
	}{
		{"/edge", "edge-token", 200},
		{"/edge", "node-token", 401},
		{"/broker", "edge-token", 401},
		{"/node", "edge-token", 401},
		{"/broker", "node-token", 200},
		{"/node", "node-token", 200},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s with %s: %d, want %d", tc.path, tc.token, rec.Code, tc.want)
		}
	}
}

// PORTAL_EDGE_TOKEN opens the portal group and nothing else, and no other token opens it.
func TestPortalEdgeTokenOpensOnlyThePortalGroup(t *testing.T) {
	const portal = "portal-edge-token-0123456789abcdef"
	t.Setenv("INTERNAL_API_TOKEN", "edge-token")
	t.Setenv("NODE_BROKER_TOKEN", "node-token")
	t.Setenv("PORTAL_EDGE_TOKEN", portal)
	t.Setenv("NODE_ACCEPT_INTERNAL_TOKEN", "")
	reset := func() {
		internalTokenOnce, brokerTokenOnce, portalEdgeTokenOnce = sync.Once{}, sync.Once{}, sync.Once{}
		internalToken, brokerToken, portalEdgeToken = nil, nil, nil
	}
	reset()
	t.Cleanup(reset)
	h := &Handler{}
	r := gin.New()
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.GET("/edge", h.InternalAuthMiddleware(), ok)
	r.GET("/portal", h.PortalEdgeAuthMiddleware(), ok)
	r.GET("/broker", h.NodeBrokerAuthMiddleware(), ok)
	r.GET("/node", h.NodeAuthMiddleware(), ok)
	code := func(path, token string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, tc := range []struct {
		path, token string
		want        int
	}{
		{"/portal", portal, 200},
		{"/portal", "edge-token", 401},
		{"/portal", "node-token", 401},
		{"/edge", portal, 401},
		{"/broker", portal, 401},
		{"/node", portal, 401},
	} {
		if got := code(tc.path, tc.token); got != tc.want {
			t.Errorf("%s with %q: %d, want %d", tc.path, tc.token, got, tc.want)
		}
	}

	// A value under the floor is never accepted.
	t.Setenv("PORTAL_EDGE_TOKEN", "short")
	reset()
	if got := code("/portal", "short"); got != 401 {
		t.Fatalf("short portal token: %d, want 401", got)
	}
}

// Without PORTAL_EDGE_TOKEN the portal group opens for nobody, INTERNAL_API_TOKEN included.
func TestPortalGroupFailsClosedWithoutItsToken(t *testing.T) {
	t.Setenv("INTERNAL_API_TOKEN", "edge-token")
	t.Setenv("PORTAL_EDGE_TOKEN", "")
	reset := func() {
		internalTokenOnce, portalEdgeTokenOnce = sync.Once{}, sync.Once{}
		internalToken, portalEdgeToken = nil, nil
	}
	reset()
	t.Cleanup(reset)
	h := &Handler{}
	r := gin.New()
	r.GET("/portal", h.PortalEdgeAuthMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, token := range []string{"edge-token", ""} {
		req := httptest.NewRequest(http.MethodGet, "/portal", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("portal group with %q and no PORTAL_EDGE_TOKEN: %d", token, rec.Code)
		}
	}
}
