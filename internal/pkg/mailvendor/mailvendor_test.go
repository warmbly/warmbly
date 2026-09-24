package mailvendor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const testKey = "sk_live_SECRET_KEY_do_not_leak_42"

type seenRequest struct {
	Method string
	Path   string
	Query  map[string][]string
	Header http.Header
	Body   string
}

// recorder is an httptest server that keeps every request it served.
type recorder struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []seenRequest
}

func newRecorder(t *testing.T, h http.HandlerFunc) *recorder {
	t.Helper()
	rec := &recorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, seenRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: string(body)})
		rec.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		h(w, r)
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (s seenRequest) q(name string) string {
	if v := s.Query[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func (r *recorder) requests() []seenRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]seenRequest(nil), r.reqs...)
}

// sleeps records the waits a client asked for without waiting.
type sleeps struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *sleeps) sleep(_ context.Context, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d = append(s.d, d)
	return nil
}

func (s *sleeps) all() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.d...)
}

func newTestClient(t *testing.T, vendor string, fields map[string]string, base string, s *sleeps) Client {
	t.Helper()
	if s == nil {
		s = &sleeps{}
	}
	c, err := New(vendor, fields, WithBaseURL(base), withSleep(s.sleep))
	if err != nil {
		t.Fatalf("New(%s): %v", vendor, err)
	}
	return c
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// fieldsFor returns a complete, valid field set for a vendor.
func fieldsFor(vendor string) map[string]string {
	f := map[string]string{FieldAPIKey: testKey}
	if vendor == VendorScaledMail {
		// A legacy organization id keeps ScaledMail usable against a server that lists none.
		f[FieldOrganizationID] = "recORG000000001"
	}
	return f
}

func TestDescriptorsStableAndComplete(t *testing.T) {
	want := []string{VendorInboxKit, VendorZapmail, VendorMailforge, VendorInfraforge, VendorMaildoso, VendorCheapInboxes, VendorScaledMail}
	var got []string
	for _, d := range Descriptors() {
		got = append(got, d.ID)
		if d.Label == "" || d.Website == "" || d.KeyHelpURL == "" {
			t.Errorf("%s: descriptor is missing a label or URL", d.ID)
		}
		if len(d.Fields) == 0 || d.Fields[0].Key != FieldAPIKey || !d.Fields[0].Secret || !d.Fields[0].Required {
			t.Errorf("%s: first field must be the required, secret api_key", d.ID)
		}
		// Anything a vendor's API can discover is never asked for.
		for _, f := range d.Fields[1:] {
			if !f.Legacy {
				t.Errorf("%s: asks for %s beyond the API key", d.ID, f.Key)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Descriptors order = %v, want %v", got, want)
	}
	d, ok := Lookup(VendorScaledMail)
	if !ok || d.Label != "ScaledMail" {
		t.Fatalf("Lookup(scaledmail) = %+v, %v", d, ok)
	}
	d.Fields[0].Key = "mutated"
	if again, _ := Lookup(VendorScaledMail); again.Fields[0].Key != FieldAPIKey {
		t.Fatal("Lookup returned a descriptor sharing the package's slice")
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("Lookup found an unknown vendor")
	}
}

func TestNewValidatesFields(t *testing.T) {
	if _, err := New("nope", nil); !errors.Is(err, ErrUnknownVendor) {
		t.Fatalf("unknown vendor: %v", err)
	}
	if _, err := New(VendorInboxKit, map[string]string{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing key: %v", err)
	}
	if _, err := New(VendorScaledMail, map[string]string{FieldAPIKey: "  "}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("blank key: %v", err)
	}
	_, err := New(VendorMaildoso, map[string]string{FieldAPIKey: testKey + "\r\nX-Evil: 1"})
	if !errors.Is(err, ErrInvalidConfig) || strings.Contains(err.Error(), testKey) {
		t.Fatalf("control characters: %v", err)
	}
	for _, d := range Descriptors() {
		c, err := New(d.ID, fieldsFor(d.ID))
		if err != nil {
			t.Fatalf("New(%s): %v", d.ID, err)
		}
		if c.Vendor() != d.ID {
			t.Fatalf("Vendor() = %q, want %q", c.Vendor(), d.ID)
		}
	}
}

func TestNormalizeProvider(t *testing.T) {
	cases := map[string]string{
		"GOOGLE": ProviderGoogle, "gsuite": ProviderGoogle, "Google Workspace": ProviderGoogle,
		"MICROSOFT": ProviderMicrosoft, "outlook": ProviderMicrosoft, "office365": ProviderMicrosoft,
		"M365": ProviderMicrosoft, "azure": ProviderMicrosoft,
		"smtp": ProviderSMTP, "maildoso": ProviderSMTP,
		"": "", "yahoo": "",
	}
	for in, want := range cases {
		if got := normalizeProvider(in); got != want {
			t.Errorf("normalizeProvider(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecurityForPort(t *testing.T) {
	cases := map[int]string{465: SecurityTLS, 993: SecurityTLS, 587: SecurityStartTLS, 143: SecurityStartTLS, 2525: ""}
	for port, want := range cases {
		if got := securityForPort(port); got != want {
			t.Errorf("securityForPort(%d) = %q, want %q", port, got, want)
		}
	}
}

// Every vendor maps a rejected key to ErrUnauthorized and never echoes the key.
func TestUnauthorizedAllVendors(t *testing.T) {
	for _, d := range Descriptors() {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
			srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, status, `{"message":"invalid api key `+testKey+`"}`)
			})
			c := newTestClient(t, d.ID, fieldsFor(d.ID), srv.URL, nil)
			for name, run := range map[string]func() error{
				"verify": func() error { return c.Verify(context.Background()) },
				"list":   func() error { _, err := c.List(context.Background()); return err },
				"credentials": func() error {
					_, err := c.Credentials(context.Background(), Mailbox{ID: "mb1", Email: "a@x.com"})
					return err
				},
			} {
				err := run()
				if !errors.Is(err, ErrUnauthorized) {
					t.Errorf("%s %s HTTP %d: err = %v, want ErrUnauthorized", d.ID, name, status, err)
					continue
				}
				if strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), srv.URL) {
					t.Errorf("%s %s: error leaks the key or URL: %q", d.ID, name, err)
				}
				var ve *Error
				if !errors.As(err, &ve) || ve.Vendor != d.ID || ve.Status != status {
					t.Errorf("%s %s: error = %#v", d.ID, name, err)
				}
			}
		}
	}
}

// Every vendor retries a 429 after Retry-After and succeeds.
func TestRateLimitRetryAllVendors(t *testing.T) {
	for _, d := range Descriptors() {
		var n int
		var mu sync.Mutex
		srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			n++
			first := n == 1
			mu.Unlock()
			if first {
				w.Header().Set("Retry-After", "2")
				writeJSON(w, http.StatusTooManyRequests, `{"status":429,"message":"Too many requests"}`)
				return
			}
			writeJSON(w, http.StatusOK, `{}`)
		})
		s := &sleeps{}
		c := newTestClient(t, d.ID, fieldsFor(d.ID), srv.URL, s)
		if err := c.Verify(context.Background()); err != nil {
			t.Errorf("%s: Verify after one 429: %v", d.ID, err)
			continue
		}
		if got := len(srv.requests()); got != 2 {
			t.Errorf("%s: %d requests, want 2", d.ID, got)
		}
		found := false
		for _, w := range s.all() {
			if w == 2*time.Second {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: waits %v do not include Retry-After 2s", d.ID, s.all())
		}
	}
}

func TestRateLimitGivesUpAfterThreeTries(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusTooManyRequests, `{}`)
	})
	c := newTestClient(t, VendorMaildoso, fieldsFor(VendorMaildoso), srv.URL, nil)
	err := c.Verify(context.Background())
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if got := len(srv.requests()); got != maxAttempts {
		t.Fatalf("%d requests, want %d", got, maxAttempts)
	}
}

func TestRateLimitRefusesLongRetryAfter(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, http.StatusTooManyRequests, `{}`)
	})
	c := newTestClient(t, VendorMaildoso, fieldsFor(VendorMaildoso), srv.URL, nil)
	if err := c.Verify(context.Background()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if got := len(srv.requests()); got != 1 {
		t.Fatalf("%d requests, want 1", got)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", time.Now().Add(5*time.Second).UTC().Format(http.TimeFormat))
	d, ok := retryAfter(h, 1)
	if !ok || d <= 0 || d > 6*time.Second {
		t.Fatalf("retryAfter(date) = %v, %v", d, ok)
	}
	if d, ok := retryAfter(http.Header{}, 2); !ok || d != 2*time.Second {
		t.Fatalf("retryAfter(none, 2) = %v, %v", d, ok)
	}
}

func TestServerErrorAndNotFound(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/credentials") {
			writeJSON(w, http.StatusNotFound, `{"error":{"code":"NOT_FOUND","message":"Mailbox not found"}}`)
			return
		}
		writeJSON(w, http.StatusInternalServerError, `{"error":{"code":"INTERNAL_ERROR","message":"boom"}}`)
	})
	c := newTestClient(t, VendorCheapInboxes, fieldsFor(VendorCheapInboxes), srv.URL, nil)
	err := c.Verify(context.Background())
	var ve *Error
	if !errors.As(err, &ve) || ve.Status != 500 || errors.Is(err, ErrUnauthorized) || strings.Contains(err.Error(), "boom") {
		t.Fatalf("500: %v", err)
	}
	if _, err := c.Credentials(context.Background(), Mailbox{ID: "mb_gone"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("404: %v", err)
	}
}

func TestBodyLimit(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"items":[`+strings.Repeat(" ", defaultBodyLimit)+`]}`)
	})
	c := newTestClient(t, VendorMaildoso, fieldsFor(VendorMaildoso), srv.URL, nil)
	err := c.Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want response too large", err)
	}
}

func TestMalformedResponse(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{"items": "`+testKey+`"`)
	})
	c := newTestClient(t, VendorMaildoso, fieldsFor(VendorMaildoso), srv.URL, nil)
	_, err := c.List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "malformed") || strings.Contains(err.Error(), testKey) {
		t.Fatalf("err = %v", err)
	}
}

// The default client never follows a redirect, which would replay auth headers.
func TestDefaultClientRefusesRedirect(t *testing.T) {
	other := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, `{}`) })
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	})
	c, err := New(VendorZapmail, fieldsFor(VendorZapmail), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(context.Background()); err == nil {
		t.Fatal("Verify followed a redirect to success")
	}
	if n := len(other.requests()); n != 0 {
		t.Fatalf("redirect target received %d requests", n)
	}
}

func TestTransportErrorIsGeneric(t *testing.T) {
	srv := newRecorder(t, func(http.ResponseWriter, *http.Request) {})
	base := srv.URL
	srv.Close()
	c := newTestClient(t, VendorScaledMail, fieldsFor(VendorScaledMail), base, nil)
	err := c.Verify(context.Background())
	if err == nil || strings.Contains(err.Error(), base) || strings.Contains(err.Error(), testKey) {
		t.Fatalf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Verify(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestThrottleSpacesRequests(t *testing.T) {
	th := &throttle{every: 200 * time.Millisecond}
	s := &sleeps{}
	for range 3 {
		if err := th.wait(context.Background(), s.sleep); err != nil {
			t.Fatal(err)
		}
	}
	got := s.all()
	if len(got) != 2 || got[0] < 150*time.Millisecond || got[1] < 350*time.Millisecond {
		t.Fatalf("waits = %v, want about 200ms then 400ms", got)
	}
}
