package email

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// swapTransport points the package's shared client at h for one test.
func swapTransport(t *testing.T, h http.Handler) {
	t.Helper()
	srv := httptest.NewServer(h)
	prev := httpClient.Transport
	httpClient.Transport = rewriteTo(srv.URL)
	t.Cleanup(func() {
		httpClient.Transport = prev
		srv.Close()
	})
}

type rewriteTo string

func (r rewriteTo) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	target := strings.TrimPrefix(string(r), "http://")
	out.URL.Scheme = "http"
	out.URL.Host = target
	return http.DefaultTransport.RoundTrip(out)
}

// The failure this exists for: Google answers 403 because the Gmail API was
// never enabled on the project, and the old code discarded the status and the
// body, so the operator saw one opaque sentence and error tracking saw
// nothing at all. The user-facing message stays vague on purpose; what must
// not be lost is that the call fails and is reported.
func TestGmailOwnerLookupFailuresAreDistinguished(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"api not enabled", http.StatusForbidden, `{"error":{"code":403,"message":"Gmail API has not been used in project 1010273043313 before or it is disabled."}}`},
		{"token revoked", http.StatusUnauthorized, `{"error":{"code":401,"message":"Invalid Credentials"}}`},
		{"unparseable body", http.StatusOK, `not json at all`},
		{"no address in payload", http.StatusOK, `{}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			swapTransport(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))

			owner, xerr := fetchGmailOwner(context.Background(), "token")
			if owner != nil {
				t.Fatalf("expected no owner, got %+v", owner)
			}
			if xerr == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// The success path must keep working: a 200 with an address returns it.
func TestGmailOwnerLookupSucceeds(t *testing.T) {
	swapTransport(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"emailAddress":"someone@example.com"}`))
	}))

	owner, xerr := fetchGmailOwner(context.Background(), "token")
	if xerr != nil {
		t.Fatalf("unexpected error: %v", xerr)
	}
	if owner.Email != "someone@example.com" {
		t.Fatalf("got %q", owner.Email)
	}
}

// ownerLookupFailed truncates the provider's body rather than reporting it
// whole, because a provider error payload has no length contract.
func TestOwnerLookupTruncatesLongBodies(t *testing.T) {
	long := strings.Repeat("x", 2000)
	if xerr := ownerLookupFailed(context.Background(), "gmail", "status", 500, []byte(long), nil); xerr == nil {
		t.Fatal("expected an error")
	}
}
