package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/delegation"
)

type onceStates struct {
	m map[string]delegation.ConsentState
}

func (s *onceStates) Put(_ context.Context, k string, v delegation.ConsentState, _ time.Duration) error {
	s.m[k] = v
	return nil
}
func (s *onceStates) Peek(_ context.Context, k string) (delegation.ConsentState, bool) {
	v, ok := s.m[k]
	return v, ok
}
func (s *onceStates) Take(_ context.Context, k string) (delegation.ConsentState, bool) {
	v, ok := s.m[k]
	delete(s.m, k)
	return v, ok
}

func purchasedConsentCallback(svc *delegation.Service, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/addresses/outlook/callback?"+query, nil)
	(&Handler{DelegationService: svc}).renderOAuthCallback(c, "outlook")
	return w
}

// A tenant consent goes back only to the URL its state stored, once; anything else gets the notice page.
func TestPurchasedConsentCallbackRedirectsOnlyToTheStoredURL(t *testing.T) {
	states := &onceStates{m: map[string]delegation.ConsentState{}}
	svc := delegation.NewService(delegation.Deps{States: states, MicrosoftClientID: "app", MicrosoftSecret: "secret"})
	states.m["mailbox_grant_state:mpc_known"] = delegation.ConsentState{OrgID: uuid.New(), Domain: "getacme.com",
		Tenant: "22222222-2222-2222-2222-222222222222", ReturnURL: "https://app.partner.test/back?x=1"}

	w := purchasedConsentCallback(svc, "state=mpc_known&error=access_denied&error_description=declined")
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://app.partner.test/back?domain=getacme.com&status=cancelled&x=1" {
		t.Fatalf("decline = %d %q", w.Code, w.Header().Get("Location"))
	}
	for _, q := range []string{"state=mpc_known&admin_consent=True&tenant=22222222-2222-2222-2222-222222222222", "state=mpc_forged&admin_consent=True"} {
		w = purchasedConsentCallback(svc, q)
		if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), "Link expired") {
			t.Fatalf("%s = %d %q", q, w.Code, w.Header().Get("Location"))
		}
	}
}
