package partners

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

type memStore struct {
	mu        sync.Mutex
	refs      map[uuid.UUID]*models.PartnerReferral
	attempted map[uuid.UUID]time.Time
}

func (m *memStore) RecordPartnerReferral(_ context.Context, orgID uuid.UUID, clickID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.refs[orgID]; !ok {
		m.refs[orgID] = &models.PartnerReferral{OrganizationID: orgID, DubClickID: clickID, CreatedAt: time.Now()}
	}
	return nil
}

func (m *memStore) GetPartnerReferral(_ context.Context, orgID uuid.UUID) (*models.PartnerReferral, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref, ok := m.refs[orgID]
	if !ok {
		return nil, nil
	}
	cp := *ref
	return &cp, nil
}

func (m *memStore) MarkPartnerLeadReported(_ context.Context, orgID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.refs[orgID].LeadReportedAt = &now
	return nil
}

func (m *memStore) MarkPartnerLeadAttempted(_ context.Context, orgID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempted[orgID] = time.Now()
	return nil
}

// ListUnreportedPartnerReferrals mirrors the SQL: never-tried first, then least recently tried, then oldest.
func (m *memStore) ListUnreportedPartnerReferrals(_ context.Context, after, before time.Time, limit int) ([]models.PartnerReferral, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.PartnerReferral
	for _, ref := range m.refs {
		if ref.LeadReportedAt == nil && !ref.CreatedAt.Before(after) && ref.CreatedAt.Before(before) {
			out = append(out, *ref)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aok := m.attempted[out[i].OrganizationID]
		aj, bok := m.attempted[out[j].OrganizationID]
		if aok != bok {
			return !aok
		}
		if aok && !ai.Equal(aj) {
			return ai.Before(aj)
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type fakeDub struct {
	mu     sync.Mutex
	status int
	// failClicks answers 404 for these click ids whatever status says.
	failClicks map[string]bool
	leads      []map[string]any
	auth       []string
	got        chan struct{}
}

func newFakeDub(t *testing.T, status int) (*fakeDub, *httptest.Server) {
	f := &fakeDub{status: status, got: make(chan struct{}, 8)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/track/lead" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.leads = append(f.leads, body)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		status := f.status
		if click, _ := body["clickId"].(string); f.failClicks[click] {
			status = http.StatusNotFound
		}
		f.mu.Unlock()
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Click not found: sk_secret_echo"}}`))
		} else {
			_, _ = w.Write([]byte(`{"click":{"id":"x"},"customer":{}}`))
		}
		f.got <- struct{}{}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func newService(t *testing.T, srv *httptest.Server) (*Service, *memStore) {
	store := &memStore{refs: map[uuid.UUID]*models.PartnerReferral{}, attempted: map[uuid.UUID]time.Time{}}
	s := New("dub_test_key", store)
	s.baseURL = srv.URL
	return s, store
}

func waitFor(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("Dub was never called")
	}
}

func TestNewIsOffWithoutAKey(t *testing.T) {
	if New("  ", &memStore{}) != nil {
		t.Fatal("an empty DUB_API_KEY must leave the program off")
	}
}

func TestSignupReportsTheLeadAndCheckoutCarriesTheCustomer(t *testing.T) {
	f, srv := newFakeDub(t, http.StatusOK)
	s, store := newService(t, srv)
	org := uuid.New()

	s.RecordSignup(context.Background(), org, "cl1ck_ID-9", Customer{Email: "owner@example.com", Name: "Ada Lovelace"})
	waitFor(t, f.got)
	// The background report stamps the row after Dub answers.
	deadline := time.Now().Add(2 * time.Second)
	for {
		ref, _ := store.GetPartnerReferral(context.Background(), org)
		if ref.LeadReportedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lead was never marked reported")
		}
		time.Sleep(10 * time.Millisecond)
	}

	f.mu.Lock()
	lead, auth := f.leads[0], f.auth[0]
	f.mu.Unlock()
	if auth != "Bearer dub_test_key" {
		t.Fatalf("authorization = %q", auth)
	}
	want := map[string]any{
		"clickId": "cl1ck_ID-9", "eventName": "Sign up", "customerExternalId": org.String(),
		"customerEmail": "owner@example.com", "customerName": "Ada Lovelace", "mode": "wait",
	}
	for k, v := range want {
		if lead[k] != v {
			t.Errorf("%s = %v, want %v", k, lead[k], v)
		}
	}

	md := s.CheckoutMetadata(context.Background(), org)
	if md["dubCustomerExternalId"] != org.String() || len(md) != 1 {
		t.Fatalf("checkout metadata = %v", md)
	}
	f.mu.Lock()
	calls := len(f.leads)
	f.mu.Unlock()
	if calls != 1 {
		t.Fatalf("a reported lead must not be sent again at checkout, got %d calls", calls)
	}
}

func TestCheckoutRetriesALeadDubNeverAccepted(t *testing.T) {
	f, srv := newFakeDub(t, http.StatusServiceUnavailable)
	s, store := newService(t, srv)
	org := uuid.New()

	s.RecordSignup(context.Background(), org, "abc123", Customer{Email: "owner@example.com"})
	waitFor(t, f.got)
	if ref, _ := store.GetPartnerReferral(context.Background(), org); ref.LeadReportedAt != nil {
		t.Fatal("a refused lead must not be marked reported")
	}

	f.mu.Lock()
	f.status = http.StatusOK
	f.mu.Unlock()
	md := s.CheckoutMetadata(context.Background(), org)
	waitFor(t, f.got)
	if md["dubCustomerExternalId"] != org.String() {
		t.Fatalf("checkout metadata = %v", md)
	}
	if ref, _ := store.GetPartnerReferral(context.Background(), org); ref.LeadReportedAt == nil {
		t.Fatal("the checkout retry must mark the lead reported")
	}
}

func TestUnreferredWorkspaceCheckoutCarriesNothing(t *testing.T) {
	_, srv := newFakeDub(t, http.StatusOK)
	s, _ := newService(t, srv)
	if md := s.CheckoutMetadata(context.Background(), uuid.New()); len(md) != 0 {
		t.Fatalf("got %v, want no metadata", md)
	}
}

func TestForgedClickIDIsNeitherStoredNorSent(t *testing.T) {
	f, srv := newFakeDub(t, http.StatusOK)
	s, store := newService(t, srv)
	org := uuid.New()
	s.RecordSignup(context.Background(), org, `x"><script>`, Customer{})
	if ref, _ := store.GetPartnerReferral(context.Background(), org); ref != nil {
		t.Fatal("a click id Dub could not have issued must not be stored")
	}
	select {
	case <-f.got:
		t.Fatal("Dub must not be called for a refused click id")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestReportErrorNamesTheCodeButNotTheKeyOrMessage(t *testing.T) {
	_, srv := newFakeDub(t, http.StatusNotFound)
	s, store := newService(t, srv)
	org := uuid.New()
	_ = store.RecordPartnerReferral(context.Background(), org, "abc")
	err := s.reportLead(context.Background(), org, "abc", Customer{})
	if err == nil {
		t.Fatal("want an error for a 404")
	}
	msg := err.Error()
	if !strings.Contains(msg, "404") || !strings.Contains(msg, "not_found") {
		t.Fatalf("error should name status and code: %q", msg)
	}
	if strings.Contains(msg, "dub_test_key") || strings.Contains(msg, "sk_secret_echo") {
		t.Fatalf("error leaks a secret or Dub's message: %q", msg)
	}
}

func TestCustomerFieldsDubWouldRefuseAreDropped(t *testing.T) {
	if dubEmail("Ada <ada@example.com>") != "" || dubEmail(strings.Repeat("a", 95)+"@x.com") != "" {
		t.Fatal("a display-name form or over-long address must be dropped")
	}
	if got := truncateRunes(strings.Repeat("é", 120), dubFieldMax); len([]rune(got)) != dubFieldMax {
		t.Fatalf("name not truncated to %d runes", dubFieldMax)
	}
}

func TestSweepReportsOnlySettledRecentUnreportedSignups(t *testing.T) {
	f, srv := newFakeDub(t, http.StatusOK)
	s, store := newService(t, srv)
	now := time.Now()
	stuck, fresh, stale := uuid.New(), uuid.New(), uuid.New()
	store.refs[stuck] = &models.PartnerReferral{OrganizationID: stuck, DubClickID: "stuck", CreatedAt: now.Add(-time.Hour)}
	store.refs[fresh] = &models.PartnerReferral{OrganizationID: fresh, DubClickID: "fresh", CreatedAt: now.Add(-time.Minute)}
	store.refs[stale] = &models.PartnerReferral{OrganizationID: stale, DubClickID: "stale", CreatedAt: now.Add(-8 * 24 * time.Hour)}

	s.retryUnreported(context.Background(), now)

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.leads) != 1 || f.leads[0]["clickId"] != "stuck" || f.leads[0]["customerExternalId"] != stuck.String() {
		t.Fatalf("sweep sent %v, want only the settled unreported signup", f.leads)
	}
	if ref, _ := store.GetPartnerReferral(context.Background(), stuck); ref.LeadReportedAt == nil {
		t.Fatal("a swept lead Dub accepted must be marked reported")
	}
}

// A full batch of clicks Dub keeps refusing must not starve a later signup.
func TestSweepRotatesPastReferralsThatKeepFailing(t *testing.T) {
	f, srv := newFakeDub(t, http.StatusOK)
	f.got = make(chan struct{}, 512)
	f.failClicks = map[string]bool{}
	s, store := newService(t, srv)
	now := time.Now()
	for i := 0; i < retryBatch; i++ {
		org := uuid.New()
		click := fmt.Sprintf("bad%d", i)
		f.failClicks[click] = true
		store.refs[org] = &models.PartnerReferral{OrganizationID: org, DubClickID: click, CreatedAt: now.Add(-2 * time.Hour)}
	}
	good := uuid.New()
	store.refs[good] = &models.PartnerReferral{OrganizationID: good, DubClickID: "good", CreatedAt: now.Add(-time.Hour)}

	s.retryUnreported(context.Background(), now)
	s.retryUnreported(context.Background(), now)

	if ref, _ := store.GetPartnerReferral(context.Background(), good); ref.LeadReportedAt == nil {
		t.Fatal("the valid referral was starved by a batch of failing ones")
	}
}
