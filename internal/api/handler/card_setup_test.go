package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/stripe"
	"github.com/warmbly/warmbly/internal/errx"
)

type idleCredits struct{ credits.CreditService }

type cardSetupStripe struct {
	stripe.StripeService
	org     uuid.UUID
	adopted string
}

func (s *cardSetupStripe) AdoptCardSetup(_ context.Context, orgID uuid.UUID, setupIntentID string) *errx.Error {
	s.org, s.adopted = orgID, setupIntentID
	return nil
}

func (s *cardSetupStripe) CreateCardSetupIntent(_ context.Context, _, orgID uuid.UUID) (*stripe.CardSetup, *errx.Error) {
	s.org = orgID
	return &stripe.CardSetup{ClientSecret: "seti_secret", PublishableKey: "pk_test"}, nil
}

// The setup is opened for the caller's workspace, and never without one.
func TestCardSetupIntentIsScopedToTheWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &cardSetupStripe{}
	h := &Handler{StripeService: fake, CreditService: idleCredits{}}
	call := func(org *uuid.UUID) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c.Set("user_id", uuid.New().String())
		if org != nil {
			c.Set(middleware.OrganizationIDKey, *org)
		}
		h.CreateCardSetupIntent(c)
		return w
	}

	if w := call(nil); w.Code != http.StatusBadRequest || fake.org != uuid.Nil {
		t.Fatalf("no workspace: status %d, Stripe reached for %s", w.Code, fake.org)
	}
	org := uuid.New()
	w := call(&org)
	var body stripe.CardSetup
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusOK || body.ClientSecret != "seti_secret" || body.PublishableKey != "pk_test" || fake.org != org {
		t.Errorf("status %d body %+v org %s, want 200 for %s", w.Code, body, fake.org, org)
	}
}

// Making a saved card the default is asked for the caller's workspace, and only with the setup it names.
func TestAdoptCardSetupIsScopedToTheWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &cardSetupStripe{}
	h := &Handler{StripeService: fake, CreditService: idleCredits{}}
	call := func(org *uuid.UUID, body string) int {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		if org != nil {
			c.Set(middleware.OrganizationIDKey, *org)
		}
		h.AdoptCardSetup(c)
		return c.Writer.Status()
	}
	if code := call(nil, `{"setup_intent_id":"seti_1"}`); code != http.StatusBadRequest || fake.adopted != "" {
		t.Fatalf("no workspace: status %d, adopted %q", code, fake.adopted)
	}
	org := uuid.New()
	if code := call(&org, `{}`); code != http.StatusBadRequest || fake.adopted != "" {
		t.Fatalf("no setup id: status %d, adopted %q", code, fake.adopted)
	}
	if code := call(&org, `{"setup_intent_id":"seti_1"}`); code != http.StatusNoContent || fake.org != org || fake.adopted != "seti_1" {
		t.Errorf("status %d org %s adopted %q, want 204 for %s", code, fake.org, fake.adopted, org)
	}
}
