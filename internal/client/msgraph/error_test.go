package msgraph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"golang.org/x/oauth2"
)

// graphStatus boots a client whose every Graph call answers with status. The
// token is live, so nothing is refreshed and the status under test is what the
// caller classifies.
func graphStatus(t *testing.T, status int, body string) *Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := &Client{Email: "sender@outlook.com"}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{
		Transport: rewriteTo(srv.URL),
	})
	token := &oauth2.Token{AccessToken: "live", Expiry: time.Now().Add(time.Hour)}
	if merr := c.Init(ctx, token, oauth2.Config{}); merr != nil {
		t.Fatalf("Init: %v", merr.Message)
	}
	return c
}

// rewriteTo sends every request to the test server instead of graph.microsoft.com,
// keeping the path and query the client actually built.
func rewriteTo(base string) http.RoundTripper {
	target, err := url.Parse(base)
	if err != nil {
		panic(err)
	}
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = target.Scheme
		clone.URL.Host = target.Host
		clone.Host = target.Host
		return http.DefaultTransport.RoundTrip(clone)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The classification the backfill's folder skip rests on. 404 is the only
// status that means "this folder is not on the tenant"; every other refusal,
// a 503 above all, has to stay an unreachable server so the caller retries
// instead of writing the folder off.
func TestHandleErrorSeparatesNotFoundFromUnreachable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   errx.MailErrorCode
	}{
		{"folder absent", http.StatusNotFound, `{"error":{"code":"ErrorItemNotFound","message":"The specified object was not found in the store."}}`, errx.MailErrorCodeNotFound},
		{"graph incident", http.StatusServiceUnavailable, `{"error":{"code":"ServiceUnavailable","message":"Server busy."}}`, errx.MailErrorCodeServerUnreachable},
		{"gateway", http.StatusBadGateway, `{}`, errx.MailErrorCodeServerUnreachable},
		{"internal", http.StatusInternalServerError, `{}`, errx.MailErrorCodeServerUnreachable},
		{"unrecognised", http.StatusTeapot, `{}`, errx.MailErrorCodeServerUnreachable},
		{"expired grant", http.StatusUnauthorized, `{}`, errx.MailErrorCodeAuthenticationFailed},
		{"missing scope", http.StatusForbidden, `{}`, errx.MailErrorCodeAuthorizationFailed},
		{"throttled", http.StatusTooManyRequests, `{}`, errx.MailErrorCodeSendingTooFast},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := graphStatus(t, tc.status, tc.body)
			_, _, err := c.ListMessagesSince(context.Background(), FolderArchive, time.Now().Add(-24*time.Hour), "", 10)
			if got := mailErrorOf(t, err).Code; got != tc.want {
				t.Errorf("status %d classified as %s, want %s", tc.status, got, tc.want)
			}
		})
	}
}

func TestHandleErrorPreservesGraphRetryAfter(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"90"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"TooManyRequests"}}`)),
	}

	mailErr := HandleError(resp)
	if mailErr.Code != errx.MailErrorCodeSendingTooFast {
		t.Fatalf("code = %s, want %s", mailErr.Code, errx.MailErrorCodeSendingTooFast)
	}
	if mailErr.RetryAfter != 90*time.Second {
		t.Fatalf("retry after = %s, want 90s", mailErr.RetryAfter)
	}
}

// FetchMessage keeps its own 404 handling: a message that vanished between the
// delta item and the hydration is a skip, not an error.
func TestFetchMessageStillTreatsNotFoundAsASkip(t *testing.T) {
	c := graphStatus(t, http.StatusNotFound, `{"error":{"code":"ErrorItemNotFound"}}`)
	msg, err := c.FetchMessage(context.Background(), FolderInbox, "gone")
	if err != nil {
		t.Fatalf("FetchMessage returned %v, want a silent skip", err)
	}
	if msg != nil {
		t.Fatalf("FetchMessage returned %+v, want nil", msg)
	}
}

// A success status with a body that is not JSON is named as such, not passed
// up as a bare decode error or dressed as an unreachable server.
func TestUnreadableSuccessBodyIsNamed(t *testing.T) {
	c := deltaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("\xe4\x9a\x01not json"))
	}, nil)
	var out listPage
	if err := c.doJSON(t.Context(), "GET", graphBase+"/me/mailFolders/inbox/messages", nil, &out); !errors.Is(err, ErrUnreadableResponse) {
		t.Fatalf("list: got %T %v", err, err)
	}
	if _, err := c.FetchMessage(t.Context(), "inbox", "m1"); !errors.Is(err, ErrUnreadableResponse) {
		t.Fatalf("fetch: got %T %v", err, err)
	}
}
