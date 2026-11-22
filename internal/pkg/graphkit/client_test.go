package graphkit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, h http.HandlerFunc) (*Client, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(srv.URL, "gk_test_key")
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	c.jitterFn = func(d time.Duration) time.Duration { return d }
	return c, &waits
}

func TestClientRetriesRateLimitHonouringRetryAfter(t *testing.T) {
	var calls atomic.Int32
	c, waits := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer gk_test_key" {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("Idempotency-Key") != "k1" {
			t.Errorf("Idempotency-Key = %q, want k1 on every attempt", r.Header.Get("Idempotency-Key"))
		}
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow down","request_id":"req_1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"acct_1","external_id":"org"}`))
	})
	acct, err := c.UpsertAccount(context.Background(), "org", AccountInput{Name: "Acme"}, "k1")
	if err != nil || acct.ID != "acct_1" {
		t.Fatalf("UpsertAccount = %+v, %v", acct, err)
	}
	if calls.Load() != 2 || len(*waits) != 1 || (*waits)[0] != 7*time.Second {
		t.Fatalf("calls=%d waits=%v, want one retry after 7s", calls.Load(), *waits)
	}
}

func TestClientRetriesServerErrorsWithBackoff(t *testing.T) {
	var calls atomic.Int32
	c, waits := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	_, err := c.Pricing(context.Background())
	ge, ok := AsError(err)
	if !ok || ge.Code != CodeUpstreamUnavailable || ge.Status != http.StatusBadGateway {
		t.Fatalf("err = %v, want upstream_unavailable 502", err)
	}
	if calls.Load() != 4 {
		t.Fatalf("calls = %d, want 1 + 3 retries", calls.Load())
	}
	want := []time.Duration{300 * time.Millisecond, 600 * time.Millisecond, 1200 * time.Millisecond}
	for i, w := range want {
		if (*waits)[i] != w {
			t.Fatalf("waits = %v, want %v", *waits, want)
		}
	}
}

func TestClientDoesNotRetryRefusals(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"expected_expiry_mismatch","message":"moved","request_id":"req_2",
			"details":{"domain":{"id":"dom_1","expires_at":"2028-11-16T00:00:00Z"}}}}`))
	})
	_, err := c.RenewDomain(context.Background(), "dom_1", 1, time.Date(2027, 11, 16, 0, 0, 0, 0, time.UTC), "renew-key")
	ge, ok := AsError(err)
	if !ok || ge.Code != CodeExpectedExpiryMismatch || calls.Load() != 1 || !Definitive(err) {
		t.Fatalf("err = %v calls = %d, want one definitive expected_expiry_mismatch", err, calls.Load())
	}
	d := ge.CurrentDomain()
	if d == nil || d.ID != "dom_1" || d.ExpiresAt == nil || d.ExpiresAt.Year() != 2028 {
		t.Fatalf("CurrentDomain = %+v", d)
	}
}

func TestClientRefusesWriteWithoutKey(t *testing.T) {
	c, _ := testClient(t, func(http.ResponseWriter, *http.Request) { t.Fatal("no request should be sent") })
	if _, err := c.CancelMailbox(context.Background(), "mbx_1", ""); err == nil {
		t.Fatal("a write without an Idempotency-Key was sent")
	}
}

func TestRetryAfterDate(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	if got := retryAfter(now.Add(30*time.Second).Format(http.TimeFormat), now); got != 30*time.Second {
		t.Fatalf("retryAfter(date) = %v", got)
	}
	if got := retryAfter("garbage", now); got != 0 {
		t.Fatalf("retryAfter(garbage) = %v", got)
	}
}

func TestClientSuspendedPartnerIsAccountLevelAndNotRetried(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"partner_suspended","message":"suspended by GraphKit","request_id":"req_2"}}`))
	})
	_, err := c.Pricing(context.Background())
	ge, ok := AsError(err)
	if !ok || ge.Code != CodePartnerSuspended || !ge.AccountLevel() || calls.Load() != 1 {
		t.Fatalf("err = %v calls = %d, want one partner_suspended", err, calls.Load())
	}
}

func TestClientDNSCheckReadsRecordsUnderData(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/domains/dom_1/dns/check" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"dns_1","type":"A","name":"@","content":"192.0.2.1","ttl":300,"priority":null,"managed":false,"purpose":"imported","proxied":false,"status":"missing"}],"checked_at":"2026-11-18T05:39:57Z","nameservers":["a.ns.example","b.ns.example"]}`))
	})
	got, err := c.CheckDNS(context.Background(), "dom_1", "k")
	if err != nil || len(got.Data) != 1 || got.Data[0].Status != "missing" || len(got.Nameservers) != 2 {
		t.Fatalf("check = %+v, %v", got, err)
	}
}

func TestClientDeleteWebhookSendsAKeyAndReadsNoContent(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/partner/webhook" || r.Header.Get("Idempotency-Key") != "k" {
			t.Errorf("%s %s key %q", r.Method, r.URL.Path, r.Header.Get("Idempotency-Key"))
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.DeleteWebhook(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
}
