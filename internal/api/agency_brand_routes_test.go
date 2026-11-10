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
)

// The portal theme's image routes sit in the agency group beside the logo's, so a session held to one agency reaches
// its own and no other agency's.
func TestAgencyBrandImageRoutesAreAgencyRoutes(t *testing.T) {
	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	agency := uuid.New()
	r := Run(&handler.Handler{}, &middleware.Handler{TokenService: portalTokens{agency: &agency}}, &middleware.OidcHandler{}, "", gin.TestMode, nil)
	registered := map[string]bool{}
	for _, rt := range r.Routes() {
		registered[rt.Method+" "+rt.Path] = true
	}
	call := func(method, path string) (int, string) {
		req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer t")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body.Code
	}
	for _, image := range []string{"logo", "icon", "sign-in-image"} {
		for _, method := range []string{http.MethodPost, http.MethodDelete} {
			if !registered[method+" /v1/agency/:agencyId/brand/"+image] {
				t.Errorf("%s /v1/agency/:agencyId/brand/%s is not registered", method, image)
				continue
			}
			if status, code := call(method, "/v1/agency/"+uuid.NewString()+"/brand/"+image); status != http.StatusForbidden || code != middleware.CodePortalSessionRestricted {
				t.Errorf("%s %s on another agency = %d %q, want 403 %s", method, image, status, code, middleware.CodePortalSessionRestricted)
			}
			// On its own agency the request reaches the handler, which has no brand service in this router.
			if status, _ := call(method, "/v1/agency/"+agency.String()+"/brand/"+image); status != http.StatusServiceUnavailable {
				t.Errorf("%s %s on its own agency = %d, want the handler's 503", method, image, status)
			}
		}
	}
}
