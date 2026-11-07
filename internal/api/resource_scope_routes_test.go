package api

import (
	"strings"
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

// Every write a restricted member can make names its grant check, and a campaign check sits where the campaigns group applies it.
func TestScopeActionRoutesCarryTheirGrantCheck(t *testing.T) {
	r := Run(&handler.Handler{}, &middleware.Handler{}, &middleware.OidcHandler{}, "", gin.TestMode, nil)
	registered := map[string]bool{}
	for _, rt := range r.Routes() {
		registered[rt.Method+" "+rt.Path] = true
	}
	for route, a := range middleware.ScopeActionRoutes() {
		if !registered[route] {
			t.Errorf("scope action route %q is not registered", route)
		}
		path := route[strings.IndexByte(route, ' ')+1:]
		switch a.Check {
		case middleware.ScopeCheckCampaignParam:
			// The /campaigns group runs RequireCampaignParam("id") before every route in it.
			if !strings.HasPrefix(path, "/v1/campaigns/:id") {
				t.Errorf("%q claims the campaign check but is outside /v1/campaigns/:id", route)
			}
		case middleware.ScopeCheckMailbox, middleware.ScopeCheckOwnRows:
			if !strings.HasPrefix(path, "/v1/unibox/") {
				t.Errorf("%q claims a unibox check but is outside /v1/unibox", route)
			}
		default:
			t.Errorf("%q names no grant check", route)
		}
	}
}

// A read-only POST that is renamed falls out of the list and forgets the overview again; this keeps the list honest.
func TestReadOnlyPostRoutesExist(t *testing.T) {
	r := Run(&handler.Handler{}, &middleware.Handler{}, &middleware.OidcHandler{}, "", gin.TestMode, nil)
	registered := map[string]bool{}
	for _, rt := range r.Routes() {
		registered[rt.Method+" "+rt.Path] = true
	}
	for _, route := range handler.ReadOnlyPostRoutes() {
		if !registered[route] {
			t.Errorf("read-only POST %q is not registered", route)
		}
	}
}

// A list parameter's route that is renamed would start refusing its repeated values; this keeps the allowlist honest.
func TestRepeatableQueryRoutesExist(t *testing.T) {
	r := Run(&handler.Handler{}, &middleware.Handler{}, &middleware.OidcHandler{}, "", gin.TestMode, nil)
	registered := map[string]bool{}
	for _, rt := range r.Routes() {
		registered[rt.Path] = true
	}
	for path := range middleware.RepeatableQuery {
		if !registered[path] {
			t.Errorf("repeatable-query route %q is not registered", path)
		}
	}
}
