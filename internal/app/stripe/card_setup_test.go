package stripe

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	stripeapi "github.com/stripe/stripe-go/v86"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// A setup in the page is an off-session card SetupIntent on the workspace's customer, created once when it has none.
func TestCardSetupIntentIsOffSessionOnTheWorkspaceCustomer(t *testing.T) {
	orgID := uuid.New()
	var customers atomic.Int32
	stripeAt(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/customers":
			customers.Add(1)
			if r.Header.Get("Idempotency-Key") != "warmbly-customer-"+orgID.String() || r.Form.Get("metadata[org_id]") != orgID.String() {
				t.Errorf("customer create: key %q form %v", r.Header.Get("Idempotency-Key"), r.Form)
			}
			_, _ = w.Write([]byte(`{"id":"cus_new"}`))
		case "/v1/setup_intents":
			if r.Form.Get("customer") != "cus_new" || r.Form.Get("usage") != "off_session" || r.Form.Get("allowed_payment_method_types[0]") != "card" ||
				r.Form.Get("metadata[purpose]") != purposeCardSetupInline || r.Form.Get("metadata[org_id]") != orgID.String() || r.Form.Has("metadata[user_id]") {
				t.Errorf("setup intent create: %v", r.Form)
			}
			_, _ = w.Write([]byte(`{"id":"seti_1","client_secret":"seti_1_secret_x"}`))
		default:
			t.Errorf("unexpected Stripe call %s %s", r.Method, r.URL.Path)
		}
	})
	repo := &billingSubRepo{sub: &models.Subscription{OrganizationID: orgID}}
	s := &stripeService{subRepo: repo, cfg: &config.StripeConfig{PublishableKey: "pk_test_x"}}
	setup, xerr := s.CreateCardSetupIntent(context.Background(), uuid.New(), orgID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if setup.ClientSecret != "seti_1_secret_x" || setup.PublishableKey != "pk_test_x" {
		t.Errorf("setup = %+v", setup)
	}
	if !repo.updated || repo.sub.StripeCustomerID != "cus_new" || customers.Load() != 1 {
		t.Errorf("customer not recorded: updated %v id %q creates %d", repo.updated, repo.sub.StripeCustomerID, customers.Load())
	}
}

// Only the workspace's own customer can make a card its default; metadata naming another workspace changes nothing.
func TestInlineCardSetupOnlyBindsTheWorkspacesOwnCustomer(t *testing.T) {
	orgID := uuid.New()
	var calls atomic.Int32
	stripeAt(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cus_mine","invoice_settings":{}}`))
	})
	event := func(customerID string) *stripeapi.Event {
		raw, _ := json.Marshal(map[string]any{
			"id": "seti_1", "customer": customerID, "payment_method": "pm_1",
			"metadata": map[string]string{"purpose": purposeCardSetupInline, "org_id": orgID.String()},
		})
		return &stripeapi.Event{Type: "setup_intent.succeeded", Data: &stripeapi.EventData{Raw: raw}}
	}
	saved := make(chan uuid.UUID, 1)
	s := &stripeService{subRepo: &billingSubRepo{sub: &models.Subscription{OrganizationID: orgID, StripeCustomerID: "cus_mine"}}}
	s.WireCardSaved(func(_ context.Context, id uuid.UUID) { saved <- id })

	if xerr := s.processWorkspaceEvent(context.Background(), event("cus_other")); xerr != nil {
		t.Fatal(xerr)
	}
	if calls.Load() != 0 || len(saved) != 0 {
		t.Fatalf("another customer's card reached the workspace: %d Stripe calls", calls.Load())
	}
	if xerr := s.processWorkspaceEvent(context.Background(), event("cus_mine")); xerr != nil {
		t.Fatal(xerr)
	}
	if got := <-saved; got != orgID {
		t.Errorf("card saved for %s, want %s", got, orgID)
	}
	if calls.Load() < 2 {
		t.Errorf("default payment method not set: %d Stripe calls", calls.Load())
	}
}

// A replacement card saved in the page becomes the default the next charge uses, even with an older default in place.
func TestAdoptCardSetupReplacesTheDefaultOnlyForTheWorkspacesConfirmedCard(t *testing.T) {
	orgID := uuid.New()
	intents := map[string]string{
		"seti_mine":    `{"id":"seti_mine","customer":"cus_mine","status":"succeeded","payment_method":"pm_new"}`,
		"seti_other":   `{"id":"seti_other","customer":"cus_other","status":"succeeded","payment_method":"pm_theirs"}`,
		"seti_pending": `{"id":"seti_pending","customer":"cus_mine","status":"requires_action","payment_method":"pm_new"}`,
	}
	var defaults []string
	stripeAt(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && len(r.URL.Path) > len("/v1/setup_intents/"):
			body, ok := intents[r.URL.Path[len("/v1/setup_intents/"):]]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"resource_missing"}}`))
				return
			}
			_, _ = w.Write([]byte(body))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/customers/cus_mine":
			defaults = append(defaults, r.Form.Get("invoice_settings[default_payment_method]"))
			_, _ = w.Write([]byte(`{"id":"cus_mine","invoice_settings":{"default_payment_method":"pm_new"}}`))
		default:
			t.Errorf("unexpected Stripe call %s %s", r.Method, r.URL.Path)
		}
	})
	saved := make(chan uuid.UUID, 1)
	s := &stripeService{subRepo: &billingSubRepo{sub: &models.Subscription{OrganizationID: orgID, StripeCustomerID: "cus_mine"}}}
	s.WireCardSaved(func(_ context.Context, id uuid.UUID) { saved <- id })

	for id, want := range map[string]errx.Code{"seti_other": errx.NotFound, "seti_missing": errx.NotFound, "seti_pending": errx.Conflict, "pi_wrong": errx.BadRequest} {
		if xerr := s.AdoptCardSetup(context.Background(), orgID, id); xerr == nil || xerr.Code != want {
			t.Errorf("%s: err = %v, want code %v", id, xerr, want)
		}
	}
	if len(defaults) != 0 {
		t.Fatalf("a card that is not the workspace's confirmed one became the default: %v", defaults)
	}
	if xerr := s.AdoptCardSetup(context.Background(), orgID, "seti_mine"); xerr != nil {
		t.Fatal(xerr)
	}
	if len(defaults) != 1 || defaults[0] != "pm_new" {
		t.Fatalf("default set to %v, want [pm_new]", defaults)
	}
	if got := <-saved; got != orgID {
		t.Errorf("card saved for %s, want %s", got, orgID)
	}
}
