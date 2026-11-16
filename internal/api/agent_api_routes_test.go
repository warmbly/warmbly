package api

import (
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/apicatalog"
)

var specParam = regexp.MustCompile(`\{[^}]+\}`)
var ginParam = regexp.MustCompile(`[:*][^/]+`)

// Every operation the assistant may call through the API tools has to be a real route, or its calls 404.
func TestAgentCallableOperationsAreRoutes(t *testing.T) {
	h := &handler.Handler{}
	r := Run(h, &middleware.Handler{}, &middleware.OidcHandler{}, "", gin.TestMode, nil)
	if h.Router == nil {
		t.Fatal("Run must hand the router to the handler for the API tools")
	}
	registered := map[string]bool{}
	for _, rt := range r.Routes() {
		registered[rt.Method+" "+ginParam.ReplaceAllString(rt.Path, "{}")] = true
	}
	cat, err := apicatalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range cat.Operations() {
		if o.Blocked != "" {
			continue
		}
		if key := o.Method + " /v1" + specParam.ReplaceAllString(o.Path, "{}"); !registered[key] {
			t.Errorf("%s is callable by the assistant but no route serves it", o.Key())
		}
	}
}
