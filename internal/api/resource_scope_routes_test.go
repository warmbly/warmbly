package api

import (
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/api/middleware"
)

// Every route a restricted member may call has to exist, or the allowlist drifts from the router unnoticed.
func TestScopeAwareRoutesExist(t *testing.T) {
	r := Run(&handler.Handler{}, &middleware.Handler{}, &middleware.OidcHandler{}, "", gin.TestMode, nil)
	registered := map[string]bool{}
	for _, rt := range r.Routes() {
		registered[rt.Method+" "+rt.Path] = true
	}
	if len(registered) < 100 {
		t.Fatalf("router registered only %d routes", len(registered))
	}
	for _, route := range middleware.ScopeAwareRoutes() {
		if !registered[route] {
			t.Errorf("scope-aware route %q is not registered", route)
		}
	}
}
