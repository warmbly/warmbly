package store

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

// memConnections is the stored connection in memory, and fixed live-purchase counts.
type memConnections struct {
	mu        sync.Mutex
	row       *repository.StoreConnection
	unsealed  bool
	mailboxes int
	domains   int
}

func (m *memConnections) CanSeal() bool { return !m.unsealed }

func (m *memConnections) GetConnection(context.Context) (*repository.StoreConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.row == nil {
		return nil, nil
	}
	c := *m.row
	return &c, nil
}

func (m *memConnections) PutConnection(_ context.Context, c repository.StoreConnection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.UpdatedAt = time.Now()
	m.row = &c
	return nil
}

func (m *memConnections) DeleteConnection(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.row = nil
	return nil
}

func (m *memConnections) CountLivePurchases(context.Context, *uuid.UUID) (int, int, error) {
	return m.mailboxes, m.domains, nil
}

// graphKitStub answers /partner/balance and /pricing, refusing any key but good.
func graphKitStub(t *testing.T, good string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+good {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"bad key"}}`))
			return
		}
		switch r.URL.Path {
		case "/partner/balance":
			_, _ = w.Write([]byte(`{"balance_micros":125000000,"currency":"USD","livemode":false}`))
		case "/pricing":
			_, _ = w.Write([]byte(`{"currency":"USD","domains":[{"tld":"com","register_micros":11000000,"renew_micros":12000000}],"mailboxes":[],"limits":{}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func offStore(t *testing.T, conns *memConnections, env config.GraphKit) *Service {
	t.Helper()
	s := New(Deps{Repo: newMemRepo()})
	s.WireConnectionSettings(conns, env)
	return s
}

func TestStatusSaysWhyTheStoreIsOff(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	conns := &memConnections{}
	s := offStore(t, conns, config.GraphKit{})
	st, err := s.Status(ctx, org)
	if err != nil || st.Enabled || st.Configured || st.Reason != ReasonNotConfigured || st.Disconnected != nil {
		t.Fatalf("not configured: %+v, %v", st, err)
	}

	conns.mailboxes, conns.domains = 2, 1
	st, _ = s.Status(ctx, org)
	if st.Disconnected == nil || st.Disconnected.Mailboxes != 2 || st.Disconnected.Domains != 1 {
		t.Fatalf("live purchases without GraphKit: %+v", st.Disconnected)
	}

	// A partner-managed workspace stays hidden whether or not the instance has a key.
	s.WireManaged(managed{org: org})
	if st, _ = s.Status(ctx, org); st.Reason != ReasonPartnerManaged || st.Disconnected != nil {
		t.Fatalf("managed, not configured: %+v", st)
	}
	h := newHarness(t)
	h.s.WireManaged(managed{org: h.org})
	if st, _ = h.s.Status(ctx, h.org); st.Enabled || !st.Configured || st.Reason != ReasonPartnerManaged {
		t.Fatalf("managed, configured: %+v", st)
	}
	if st, _ = h.s.Status(ctx, uuid.New()); !st.Enabled || !st.Configured || st.Reason != "" {
		t.Fatalf("on: %+v", st)
	}
}

func TestOnlyASelfHostShowsAnUnconfiguredStoreAsLocked(t *testing.T) {
	ctx := context.Background()
	for _, selfHosted := range []bool{false, true} {
		s := New(Deps{Repo: newMemRepo(), SelfHosted: selfHosted})
		s.WireConnectionSettings(&memConnections{}, config.GraphKit{})
		st, err := s.Status(ctx, uuid.New())
		if err != nil || st.Enabled || st.Reason != ReasonNotConfigured || st.Locked != selfHosted {
			t.Fatalf("self-hosted %v: %+v, %v", selfHosted, st, err)
		}
	}
	// A configured store and a partner-managed workspace are never locked.
	h := newHarness(t)
	h.s.SelfHosted = true
	if st, _ := h.s.Status(ctx, uuid.New()); !st.Enabled || st.Locked {
		t.Fatalf("on: %+v", st)
	}
	off := New(Deps{Repo: newMemRepo(), SelfHosted: true})
	off.WireConnectionSettings(&memConnections{}, config.GraphKit{})
	org := uuid.New()
	off.WireManaged(managed{org: org})
	if st, _ := off.Status(ctx, org); st.Reason != ReasonPartnerManaged || st.Locked {
		t.Fatalf("managed: %+v", st)
	}
}

func TestSavingAKeyTurnsTheStoreOnAndClearingItTurnsItOffWithoutARestart(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := graphKitStub(t, "gk_test_good")
	conns := &memConnections{}
	s := offStore(t, conns, config.GraphKit{})
	s.Start(ctx)

	if _, err := s.SaveConnection(ctx, ConnectionInput{APIURL: srv.URL, APIKey: "gk_test_bad"}, uuid.New()); code(err) != CodeGraphKitRejected {
		t.Fatalf("a rejected key: %v", err)
	}
	if conns.row != nil || s.Enabled() {
		t.Fatal("a key GraphKit refused was stored")
	}

	view, err := s.SaveConnection(ctx, ConnectionInput{APIURL: srv.URL, APIKey: "gk_test_good", WebhookSecret: "whsec_one"}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if !s.Enabled() || s.Source() != SourceSettings || !view.Configured || view.Source != SourceSettings || !view.APIKeySet || !view.WebhookSecretSet {
		t.Fatalf("after save: enabled %v, view %+v", s.Enabled(), view)
	}
	if _, err := s.Search(ctx, "acme", nil, false); code(err) == CodeDisabled {
		t.Fatal("the store still answers store_disabled after the key was saved")
	}

	// A blank secret keeps the stored one; a new webhook secret keeps the old verifying.
	if _, err := s.SaveConnection(ctx, ConnectionInput{APIURL: srv.URL, WebhookSecret: "whsec_two"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if conns.row.APIKey != "gk_test_good" || conns.row.WebhookSecret != "whsec_two" || conns.row.WebhookSecretPrevious != "whsec_one" {
		t.Fatalf("stored after a rotation: %+v", conns.row)
	}
	if got := s.webhookSecrets(); len(got) != 2 {
		t.Fatalf("webhook secrets in use: %d", len(got))
	}

	if _, err := s.ClearConnection(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Enabled() || conns.row != nil || s.Source() != SourceNone {
		t.Fatal("the store is still on after its key was removed")
	}
	if _, err := s.CreateQuote(ctx, uuid.New(), QuoteRequest{}); code(err) != CodeDisabled {
		t.Fatalf("a write after clearing: %v", err)
	}
}

func TestAnotherReplicasSaveIsPickedUp(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	ctx := context.Background()
	conns := &memConnections{row: &repository.StoreConnection{APIURL: "fake"}}
	s := offStore(t, conns, config.GraphKit{})
	if err := s.LoadConnection(ctx); err != nil || !s.Enabled() || s.Mode() != ModeSimulator {
		t.Fatalf("load: %v, enabled %v, mode %s", err, s.Enabled(), s.Mode())
	}
	before := s.current()
	if err := s.LoadConnection(ctx); err != nil || s.current() != before {
		t.Fatal("an unchanged connection was rebuilt")
	}
	conns.row = nil
	if err := s.LoadConnection(ctx); err != nil || s.Enabled() {
		t.Fatal("a removed connection left the store on")
	}
}

func TestTheEnvironmentWins(t *testing.T) {
	ctx := context.Background()
	t.Setenv("APP_ENV", "development")
	conns := &memConnections{row: &repository.StoreConnection{APIURL: "https://graphkit.invalid", APIKey: "gk_live_stored"}}
	env := config.GraphKit{APIURL: "fake"}
	s := offStore(t, conns, env)
	if err := s.Connect(env, SourceEnvironment); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadConnection(ctx); err != nil || s.Source() != SourceEnvironment {
		t.Fatalf("the stored key replaced the environment's: %v %s", err, s.Source())
	}
	view, err := s.ConnectionView(ctx)
	if err != nil || view.Source != SourceEnvironment || !view.Configured || view.APIURL != "fake" {
		t.Fatalf("view: %+v %v", view, err)
	}
	for name, err := range map[string]error{
		"save": func() error {
			_, err := s.SaveConnection(ctx, ConnectionInput{APIKey: "gk_live_x"}, uuid.New())
			return err
		}(),
		"test":  func() error { _, err := s.TestConnection(ctx, ConnectionInput{APIKey: "gk_live_x"}); return err }(),
		"clear": func() error { _, err := s.ClearConnection(ctx); return err }(),
	} {
		if code(err) != CodeConfiguredByEnv {
			t.Fatalf("%s with GRAPHKIT_* set: %v", name, err)
		}
	}
	if conns.row == nil {
		t.Fatal("clearing deleted the stored connection while the environment owns it")
	}
}

func TestSecretsNeverLeaveTheService(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	var logs bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&logs)
	t.Cleanup(func() { log.Logger = prev })

	ctx := context.Background()
	srv := graphKitStub(t, "gk_test_s3cret_key")
	conns := &memConnections{}
	s := offStore(t, conns, config.GraphKit{})
	_, _ = s.TestConnection(ctx, ConnectionInput{APIURL: srv.URL, APIKey: "gk_test_wrong_s3cret"})
	view, err := s.SaveConnection(ctx, ConnectionInput{APIURL: srv.URL, APIKey: "gk_test_s3cret_key", WebhookSecret: "whsec_s3cret"}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	test, err := s.TestConnection(ctx, ConnectionInput{APIURL: srv.URL})
	if err != nil || test.Mode != "test" || test.BalanceMicros != 125_000_000 || test.ComRegisterMicros == nil || *test.ComRegisterMicros != 11_000_000 {
		t.Fatalf("test with the stored key: %+v %v", test, err)
	}
	raw, _ := json.Marshal(view)
	for _, out := range []string{string(raw), logs.String()} {
		if strings.Contains(out, "s3cret") {
			t.Fatalf("a secret left the service: %s", out)
		}
	}
}

func TestSavingNeedsTheInstanceKey(t *testing.T) {
	s := offStore(t, &memConnections{unsealed: true}, config.GraphKit{})
	if _, err := s.SaveConnection(context.Background(), ConnectionInput{APIKey: "gk_live_x"}, uuid.New()); code(err) != CodeSealingMissing {
		t.Fatalf("save without CREDENTIALS_ENCRYPTION_KEY: %v", err)
	}
	t.Setenv("APP_ENV", "production")
	for _, u := range []string{"http://graphkit.example.com", "fake", "https://user:pw@graphkit.example.com", "not a url"} {
		if _, err := normalizeURL(u); err == nil {
			t.Fatalf("%q was accepted outside development", u)
		}
	}
}

func TestPurchasesWithoutConnectionOnlyWhileOff(t *testing.T) {
	ctx := context.Background()
	conns := &memConnections{mailboxes: 3, domains: 2}
	s := offStore(t, conns, config.GraphKit{})
	if m, d, err := s.UnreachablePurchases(ctx); err != nil || m != 3 || d != 2 {
		t.Fatalf("off: %d %d %v", m, d, err)
	}
	if err := s.Connect(config.GraphKit{APIURL: "fake"}, SourceEnvironment); err != nil {
		t.Fatal(err)
	}
	if m, d, _ := s.UnreachablePurchases(ctx); m+d != 0 {
		t.Fatal("purchases were reported unreachable while GraphKit is configured")
	}
	conns.mailboxes, conns.domains = 0, 0
	s.Disconnect()
	if got, _ := s.PurchasesWithoutConnection(ctx, nil); got != nil {
		t.Fatalf("nothing bought: %+v", got)
	}
}

func TestTheStoredKeyIsOnlySentToTheURLItWasSavedFor(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	ctx := context.Background()
	srv := graphKitStub(t, "gk_test_good")
	var other sync.Mutex
	var otherAuth []string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		other.Lock()
		otherAuth = append(otherAuth, r.Header.Get("Authorization"))
		other.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(elsewhere.Close)

	conns := &memConnections{}
	s := offStore(t, conns, config.GraphKit{})
	if _, err := s.SaveConnection(ctx, ConnectionInput{APIURL: srv.URL, APIKey: "gk_test_good"}, uuid.New()); err != nil {
		t.Fatal(err)
	}

	if _, err := s.TestConnection(ctx, ConnectionInput{APIURL: elsewhere.URL}); code(err) != CodeGraphKitKeyReentry {
		t.Fatalf("test against a new URL with a blank key: %v", err)
	}
	if _, err := s.SaveConnection(ctx, ConnectionInput{APIURL: elsewhere.URL}, uuid.New()); code(err) != CodeGraphKitKeyReentry {
		t.Fatalf("save against a new URL with a blank key: %v", err)
	}
	if len(otherAuth) != 0 {
		t.Fatalf("a new URL was called %d times", len(otherAuth))
	}
	if conns.row.APIURL != srv.URL {
		t.Fatalf("stored URL changed to %q", conns.row.APIURL)
	}

	// The same URL, however it is written, keeps the stored key.
	if _, err := s.TestConnection(ctx, ConnectionInput{APIURL: " " + srv.URL + "/ "}); err != nil {
		t.Fatalf("test against the saved URL with a blank key: %v", err)
	}
	if _, err := s.SaveConnection(ctx, ConnectionInput{APIURL: srv.URL}, uuid.New()); err != nil || conns.row.APIKey != "gk_test_good" {
		t.Fatalf("save against the saved URL with a blank key: %v", err)
	}

	// A new URL with the key entered again goes through.
	if _, err := s.TestConnection(ctx, ConnectionInput{APIURL: elsewhere.URL, APIKey: "gk_test_other"}); code(err) != CodeGraphKitRejected {
		t.Fatalf("test against a new URL with a key: %v", err)
	}
	if len(otherAuth) == 0 || otherAuth[0] != "Bearer gk_test_other" {
		t.Fatalf("the new URL saw %v", otherAuth)
	}
}

func TestAPreviousWebhookSecretVerifiesOnlyThroughTheOverlap(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	srv := graphKitStub(t, "gk_test_good")
	conns := &memConnections{}
	s := offStore(t, conns, config.GraphKit{})
	s.Now = func() time.Time { return now }
	body := []byte(`{"id":"evt_1","type":"order.updated"}`)
	verifies := func(secret string) bool {
		_, err := s.VerifyWebhook(graphkit.Sign(secret, now, body), body)
		return err == nil
	}

	if _, err := s.SaveConnection(ctx, ConnectionInput{APIURL: srv.URL, APIKey: "gk_test_good", WebhookSecret: "whsec_one"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConnection(ctx, ConnectionInput{APIURL: srv.URL, WebhookSecret: "whsec_two"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(webhookPreviousSecretTTL - time.Minute)
	if !verifies("whsec_one") || !verifies("whsec_two") {
		t.Fatal("inside the overlap both secrets verify")
	}
	// A save that changes nothing about the secret keeps the rotation's clock.
	if _, err := s.SaveConnection(ctx, ConnectionInput{APIURL: srv.URL}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if verifies("whsec_one") {
		t.Fatal("the previous secret verified after the overlap")
	}
	if !verifies("whsec_two") {
		t.Fatal("the current secret stopped verifying")
	}
	// Another replica loading the stored row reaches the same answer.
	other := offStore(t, conns, config.GraphKit{})
	other.Now = s.Now
	if err := other.LoadConnection(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := other.VerifyWebhook(graphkit.Sign("whsec_one", now, body), body); err == nil {
		t.Fatal("a replica accepted the previous secret after the overlap")
	}
	// The next save drops it from storage.
	if _, err := s.SaveConnection(ctx, ConnectionInput{APIURL: srv.URL}, uuid.New()); err != nil || conns.row.WebhookSecretPrevious != "" {
		t.Fatalf("an elapsed previous secret was kept: %v", err)
	}

	// GRAPHKIT_WEBHOOK_SECRET_PREVIOUS holds until the operator removes it.
	env := New(Deps{Repo: newMemRepo()})
	env.Now = s.Now
	if err := env.Connect(config.GraphKit{APIURL: "fake", WebhookSecret: "whsec_env", WebhookSecretPrevious: "whsec_env_old"}, SourceEnvironment); err != nil {
		t.Fatal(err)
	}
	now = now.Add(90 * 24 * time.Hour)
	if _, err := env.VerifyWebhook(graphkit.Sign("whsec_env_old", now, body), body); err != nil {
		t.Fatal("the environment's previous secret stopped verifying")
	}
}
