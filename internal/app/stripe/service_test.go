package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	stripeapi "github.com/stripe/stripe-go/v86"
	workerapp "github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type billingWorkerAssignment struct {
	workerapp.WorkerAssignmentService
	org    uuid.UUID
	pool   models.WarmupPoolType
	called bool
}

func (a *billingWorkerAssignment) SetOrganizationWarmupPool(_ context.Context, orgID uuid.UUID, pool models.WarmupPoolType) error {
	a.org, a.pool, a.called = orgID, pool, true
	return nil
}

func TestSubscriptionPaymentRecoveryDoesNotDemoteMailboxes(t *testing.T) {
	orgID := uuid.New()
	repo := &billingSubRepo{sub: &models.Subscription{OrganizationID: orgID}}
	assignment := &billingWorkerAssignment{}
	s := &stripeService{subRepo: repo, planRepo: &billingPlanRepo{}, workerAssignment: assignment}
	raw, err := json.Marshal(map[string]any{
		"id":       "sub_pending",
		"status":   "past_due",
		"metadata": map[string]string{"org_id": orgID.String()},
	})
	if err != nil {
		t.Fatal(err)
	}

	if xerr := s.handleSubscriptionUpdated(context.Background(), &stripeapi.Event{Data: &stripeapi.EventData{Raw: raw}}); xerr != nil {
		t.Fatal(xerr)
	}
	if assignment.called {
		t.Fatal("a non-active subscription update changed mailbox pool membership")
	}
}

type billingSubRepo struct {
	repository.SubscriptionRepository
	sub       *models.Subscription
	lookupErr error
	updated   bool
	recorded  bool
}

func (r *billingSubRepo) GetByOrganizationID(context.Context, uuid.UUID) (*models.Subscription, error) {
	return r.sub, r.lookupErr
}
func (r *billingSubRepo) GetByStripeSubscriptionID(context.Context, string) (*models.Subscription, error) {
	return nil, r.lookupErr
}
func (r *billingSubRepo) Update(_ context.Context, sub *models.Subscription) error {
	r.sub, r.updated = sub, true
	return nil
}

type billingPlanRepo struct {
	repository.PlanRepository
	plan *models.Plan
}

func (r *billingPlanRepo) GetByID(context.Context, uuid.UUID) (*models.Plan, error) {
	return r.plan, nil
}
func (r *billingPlanRepo) GetByStripePriceID(context.Context, string) (*models.Plan, error) {
	return r.plan, nil
}

func TestSubscriptionCheckoutCustomerParameters(t *testing.T) {
	for _, customerID := range []string{"", "cus_existing"} {
		t.Run("customer_"+customerID, func(t *testing.T) {
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Form.Get("mode") != "subscription" || r.Form.Has("customer_creation") || r.Form.Get("customer") != customerID {
					t.Errorf("invalid checkout parameters: %v", r.Form)
				}
				if r.Form.Get("automatic_tax[enabled]") != "false" || r.Form.Get("billing_address_collection") != "required" || r.Form.Get("tax_id_collection[enabled]") != "true" {
					t.Errorf("checkout tax parameters are wrong: %v", r.Form)
				}
				if customerID == "" && (r.Form.Has("customer_update[address]") || r.Form.Has("customer_update[name]")) {
					t.Errorf("new-customer checkout included customer_update: %v", r.Form)
				}
				if customerID != "" && (r.Form.Get("customer_update[address]") != "auto" || r.Form.Get("customer_update[name]") != "auto") {
					t.Errorf("existing-customer checkout does not save business details: %v", r.Form)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"cs_test","url":"https://checkout.stripe.com/test"}`))
			}))
			defer server.Close()
			old := stripeapi.GetBackend(stripeapi.APIBackend)
			stripeapi.SetBackend(stripeapi.APIBackend, stripeapi.GetBackendWithConfig(stripeapi.APIBackend, &stripeapi.BackendConfig{URL: stripeapi.String(server.URL), HTTPClient: server.Client()}))
			t.Cleanup(func() { stripeapi.SetBackend(stripeapi.APIBackend, old) })
			s := &stripeService{subRepo: &billingSubRepo{sub: &models.Subscription{StripeCustomerID: customerID}}}
			_, err := s.CreateCheckoutSession(context.Background(), uuid.New(), uuid.New(), "price_test", "https://example.com/success", "https://example.com/cancel", "")
			if err != nil || !called {
				t.Fatalf("checkout called=%v error=%v", called, err)
			}
		})
	}
}

func TestBalanceCheckoutCollectsTaxAndSavesTheCard(t *testing.T) {
	for _, customerID := range []string{"", "cus_existing"} {
		t.Run("customer_"+customerID, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Form.Get("mode") != "payment" || r.Form.Get("automatic_tax[enabled]") != "false" || r.Form.Get("billing_address_collection") != "required" || r.Form.Get("tax_id_collection[enabled]") != "true" {
					t.Errorf("invalid tax-aware balance checkout: %v", r.Form)
				}
				if r.Form.Get("payment_intent_data[setup_future_usage]") != "off_session" || r.Form.Get("line_items[0][price_data][unit_amount]") != "2500" || r.Form.Get("metadata[amount_micro]") != "25000000" {
					t.Errorf("balance checkout does not save the card or charge the amount: %v", r.Form)
				}
				if r.Form.Has("metadata[user_id]") || r.Form.Get("metadata[actor_user_id]") == "" {
					t.Errorf("balance checkout session metadata must name the actor without user_id: %v", r.Form)
				}
				if customerID == "" && r.Form.Get("customer_creation") != "always" {
					t.Errorf("credit checkout will not save the new customer: %v", r.Form)
				}
				if customerID != "" && (r.Form.Get("customer") != customerID || r.Form.Get("customer_update[address]") != "auto" || r.Form.Get("customer_update[name]") != "auto") {
					t.Errorf("credit checkout does not update the existing customer: %v", r.Form)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"cs_credit","url":"https://checkout.stripe.com/test"}`))
			}))
			defer server.Close()
			old := stripeapi.GetBackend(stripeapi.APIBackend)
			stripeapi.SetBackend(stripeapi.APIBackend, stripeapi.GetBackendWithConfig(stripeapi.APIBackend, &stripeapi.BackendConfig{URL: stripeapi.String(server.URL), HTTPClient: server.Client()}))
			t.Cleanup(func() { stripeapi.SetBackend(stripeapi.APIBackend, old) })

			s := &stripeService{subRepo: &billingSubRepo{sub: &models.Subscription{StripeCustomerID: customerID}}}
			if _, xerr := s.CreateBalanceCheckout(context.Background(), uuid.New(), uuid.New(), 25*models.MicroPerUSD, "https://example.com/success", "https://example.com/cancel"); xerr != nil {
				t.Fatal(xerr)
			}
		})
	}
}

func TestCardSetupCheckoutSessionCarriesNoUserID(t *testing.T) {
	stripeAt(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("mode") != "setup" || r.Form.Has("metadata[user_id]") || r.Form.Get("metadata[actor_user_id]") == "" || r.Form.Get("metadata[purpose]") != purposeCardSetup {
			t.Errorf("card setup checkout session metadata: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cs_setup","url":"https://checkout.stripe.com/test"}`))
	})
	s := &stripeService{subRepo: &billingSubRepo{sub: &models.Subscription{StripeCustomerID: "cus_existing"}}}
	if _, xerr := s.CreateCardSetupCheckout(context.Background(), uuid.New(), uuid.New(), "https://example.com/success", "https://example.com/cancel"); xerr != nil {
		t.Fatal(xerr)
	}
}

func TestPortalRejectsMissingCustomer(t *testing.T) {
	for _, customerID := range []string{"", " \t"} {
		url, err := (&stripeService{}).CreatePortalSession(context.Background(), customerID, "https://example.com")
		if err == nil || err.Code != errx.BadRequest || url != "" {
			t.Fatalf("url=%q error=%v", url, err)
		}
	}
}

func TestSubscriptionEventBeforeCheckout(t *testing.T) {
	orgID, planID := uuid.New(), uuid.New()
	repo := &billingSubRepo{sub: &models.Subscription{OrganizationID: orgID}}
	assignment := &billingWorkerAssignment{}
	s := &stripeService{subRepo: repo, planRepo: &billingPlanRepo{plan: &models.Plan{ID: planID}}, workerAssignment: assignment}
	raw, err := json.Marshal(map[string]any{"id": "sub_new", "customer": "cus_new", "status": "active", "metadata": map[string]string{"org_id": orgID.String()}, "items": map[string]any{"data": []any{map[string]any{"price": map[string]string{"id": "price_new"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if xerr := s.handleSubscriptionUpdated(context.Background(), &stripeapi.Event{Data: &stripeapi.EventData{Raw: raw}}); xerr != nil {
		t.Fatal(xerr)
	}
	if !repo.updated || repo.sub.PlanID != planID || repo.sub.StripeCustomerID != "cus_new" || repo.sub.StripeSubscriptionID == nil || *repo.sub.StripeSubscriptionID != "sub_new" || repo.sub.Status != models.SubscriptionStatusActive {
		t.Fatalf("subscription event did not activate the workspace: %+v", repo.sub)
	}
	if assignment.org != orgID || assignment.pool != "premium" {
		t.Fatalf("mailboxes moved to %q for %s, want premium for %s", assignment.pool, assignment.org, orgID)
	}
}

func TestSubscriptionLookupFailureRemainsRetryable(t *testing.T) {
	s := &stripeService{subRepo: &billingSubRepo{lookupErr: errors.New("database unavailable")}}
	err := s.handleSubscriptionUpdated(context.Background(), &stripeapi.Event{Data: &stripeapi.EventData{Raw: json.RawMessage(`{"id":"sub_new"}`)}})
	if err == nil || err.Code != errx.Internal {
		t.Fatalf("expected retryable failure, got %v", err)
	}
}

func TestAnnualChangeDoesNotFallBackToMonthly(t *testing.T) {
	s := &stripeService{subRepo: &billingSubRepo{sub: &models.Subscription{StripeSubscriptionID: stripeapi.String("sub_existing")}}, planRepo: &billingPlanRepo{plan: &models.Plan{StripePriceID: stripeapi.String("price_monthly")}}}
	_, err := s.ChangePlan(context.Background(), uuid.New(), uuid.New(), "", "", "year")
	if err == nil || err.Code != errx.BadRequest {
		t.Fatalf("expected unavailable annual price, got %v", err)
	}
}

type billingCredits struct {
	resetErr error
	granted  models.Micro
	reset    models.Micro
}

// billingCharges records the invoice a saved-card charge gets.
type billingCharges struct {
	repository.SavedCardChargeRepository
	invoiceID string
}

func (r *billingCharges) SetInvoice(_ context.Context, _ uuid.UUID, id string) error {
	r.invoiceID = id
	return nil
}

func (c *billingCredits) ResetMonthlyAllowance(_ context.Context, _ uuid.UUID, amount models.Micro, _ string) error {
	c.reset = amount
	return c.resetErr
}
func (c *billingCredits) GrantPurchased(_ context.Context, _ uuid.UUID, amount models.Micro, _, _ string) (models.Micro, error) {
	c.granted += amount
	return c.granted, nil
}
func (r *billingSubRepo) WebhookEventExists(context.Context, string) (bool, error) { return false, nil }
func (r *billingSubRepo) RecordWebhookEvent(context.Context, *models.StripeWebhookEvent) error {
	r.recorded = true
	return nil
}

// stripeAt points the Stripe client at a test server for one test.
func stripeAt(t *testing.T, h http.HandlerFunc) {
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	old := stripeapi.GetBackend(stripeapi.APIBackend)
	stripeapi.SetBackend(stripeapi.APIBackend, stripeapi.GetBackendWithConfig(stripeapi.APIBackend, &stripeapi.BackendConfig{URL: stripeapi.String(server.URL), HTTPClient: server.Client()}))
	t.Cleanup(func() { stripeapi.SetBackend(stripeapi.APIBackend, old) })
}

func TestSavedCardChargeIsOneKeyedInvoice(t *testing.T) {
	charge := &models.SavedCardCharge{
		ID: uuid.New(), OrgID: uuid.New(), Kind: models.SavedCardAutoReload, Amount: 25 * models.MicroPerUSD,
		AmountCents: 2500, Currency: "usd", CustomerID: "cus_test", PaymentMethodID: "pm_test",
	}
	key := "warmbly-balance-" + charge.ID.String()
	stripeAt(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		idem := r.Header.Get("Idempotency-Key")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/invoices":
			if idem != key+"-invoice" || r.Form.Get("auto_advance") != "false" || r.Form.Get("metadata[charge_id]") != charge.ID.String() {
				t.Errorf("invoice create: key %q form %v", idem, r.Form)
			}
			_, _ = w.Write([]byte(`{"id":"in_1","status":"draft","total":0}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/invoiceitems":
			if idem != key+"-item" || r.Form.Get("amount") != "2500" || r.Form.Get("invoice") != "in_1" {
				t.Errorf("invoice item: key %q form %v", idem, r.Form)
			}
			_, _ = w.Write([]byte(`{"id":"ii_1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/invoices/in_1/finalize":
			_, _ = w.Write([]byte(`{"id":"in_1","status":"open","total":2500}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/invoices/in_1/pay":
			if idem != key+"-pay-1" || r.Form.Get("off_session") != "true" || r.Form.Get("payment_method") != "pm_test" {
				t.Errorf("pay: key %q form %v", idem, r.Form)
			}
			_, _ = w.Write([]byte(`{"id":"in_1","status":"paid","total":2500}`))
		default:
			http.NotFound(w, r)
		}
	})
	charges := &billingCharges{}
	s := &stripeService{balanceCharges: charges}
	out, err := s.ChargeSavedCard(context.Background(), charge, 1, true)
	if err != nil || !out.Paid || out.InvoiceID != "in_1" || charges.invoiceID != "in_1" {
		t.Fatalf("charge = %+v, %v; recorded invoice %q", out, err, charges.invoiceID)
	}
}

func TestSavedCardRefusalsAreClassified(t *testing.T) {
	cases := []struct {
		body     string
		code     string
		soft     bool
		declined bool
	}{
		{`{"error":{"type":"card_error","code":"card_declined","decline_code":"insufficient_funds"}}`, models.ReloadCardDeclined, true, true},
		{`{"error":{"type":"card_error","code":"card_declined","decline_code":"stolen_card"}}`, models.ReloadCardDeclined, false, true},
		{`{"error":{"type":"card_error","code":"authentication_required","decline_code":"authentication_required"}}`, models.ReloadAuthenticationRequired, false, true},
	}
	for _, c := range cases {
		stripeAt(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/v1/invoices/in_open":
				_, _ = w.Write([]byte(`{"id":"in_open","status":"open","total":1000}`))
			case "/v1/invoices/in_open/pay":
				w.WriteHeader(http.StatusPaymentRequired)
				_, _ = w.Write([]byte(c.body))
			default:
				http.NotFound(w, r)
			}
		})
		s := &stripeService{}
		out, err := s.ChargeSavedCard(context.Background(), &models.SavedCardCharge{ID: uuid.New(), InvoiceID: "in_open", PaymentMethodID: "pm"}, 2, true)
		if err != nil || out.Paid || out.Declined != c.code || out.Soft != c.soft {
			t.Errorf("%s: got %+v, %v", c.body, out, err)
		}
	}
}

func TestInvoiceBeforeCheckoutAndRetryAfterCreditFailure(t *testing.T) {
	orgID := uuid.New()
	repo := &billingSubRepo{sub: &models.Subscription{OrganizationID: orgID}}
	credits := &billingCredits{resetErr: errors.New("temporary ledger failure")}
	s := &stripeService{subRepo: repo, planRepo: &billingPlanRepo{plan: &models.Plan{MonthlyAllowanceUSD: 10}}, credits: credits}
	raw := json.RawMessage(`{"subscription":"sub_new","billing_reason":"subscription_create","subscription_details":{"metadata":{"org_id":"` + orgID.String() + `"}},"lines":{"data":[{"price":{"id":"price_new"}}]}}`)
	event := &stripeapi.Event{ID: "evt_invoice", Type: "invoice.paid", Data: &stripeapi.EventData{Raw: raw}}
	if err := s.ProcessWebhookEvent(context.Background(), event); err == nil || err.Code != errx.Internal {
		t.Fatalf("expected retryable error, got %v", err)
	}
	if repo.recorded {
		t.Fatal("failed credit grant marked processed")
	}
	credits.resetErr = nil
	if err := s.ProcessWebhookEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if !repo.recorded || credits.reset != 10*models.MicroPerUSD {
		t.Fatal("retried invoice did not grant monthly allowance")
	}
}

func TestAsyncCreditPurchaseStoresCustomer(t *testing.T) {
	orgID := uuid.New()
	repo := &billingSubRepo{sub: &models.Subscription{OrganizationID: orgID}}
	credits := &billingCredits{}
	s := &stripeService{subRepo: repo, credits: credits}
	raw := json.RawMessage(`{"id":"cs_credit","customer":"cus_credit","payment_status":"paid","metadata":{"purpose":"credit_topup","credits":"500","org_id":"` + orgID.String() + `"}}`)
	event := &stripeapi.Event{ID: "evt_credit", Type: "checkout.session.async_payment_succeeded", Data: &stripeapi.EventData{Raw: raw}}
	if err := s.ProcessWebhookEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if !repo.updated || repo.sub.StripeCustomerID != "cus_credit" || credits.granted != 500*models.LegacyCreditMicro || !repo.recorded {
		t.Fatal("purchase did not persist the billing customer and credits")
	}
}

type recordingOps struct{ keys []string }

func (r *recordingOps) NotifyOperator(key, _, _ string, _ map[string]string) {
	r.keys = append(r.keys, key)
}

func TestDeclinedBalanceChargeDoesNotPageTheOperator(t *testing.T) {
	ops := &recordingOps{}
	s := &stripeService{subRepo: &billingSubRepo{}, opsNotify: ops}
	raw := json.RawMessage(`{"customer":"cus_1","amount_due":2500,"currency":"usd","metadata":{"purpose":"balance_charge"}}`)
	if err := s.ProcessWebhookEvent(context.Background(), &stripeapi.Event{ID: "evt_bal", Type: "invoice.payment_failed", Data: &stripeapi.EventData{Raw: raw}}); err != nil {
		t.Fatal(err)
	}
	raw = json.RawMessage(`{"customer":"cus_1","amount_due":8900,"currency":"usd","subscription":"sub_1"}`)
	if err := s.ProcessWebhookEvent(context.Background(), &stripeapi.Event{ID: "evt_sub", Type: "invoice.payment_failed", Data: &stripeapi.EventData{Raw: raw}}); err != nil {
		t.Fatal(err)
	}
	if len(ops.keys) != 1 {
		t.Fatalf("operator alerts = %v; want only the subscription's", ops.keys)
	}
}
