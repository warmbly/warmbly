package goog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
)

const tokenHost = "oauth2.test"

// refusingRT answers the token endpoint with a fixed refusal and fails the
// test if anything reaches Gmail: a refresh that cannot succeed must never
// turn into an API call.
type refusingRT struct {
	t      *testing.T
	status int
	body   string
	calls  int32
}

func (r *refusingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != tokenHost {
		r.t.Errorf("request escaped to %s: a failed token refresh must not reach the provider", req.URL)
		return nil, errors.New("unexpected network call")
	}
	atomic.AddInt32(&r.calls, 1)
	return &http.Response{
		StatusCode: r.status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Request:    req,
	}, nil
}

// refusingClient is a real Gmail client whose access token has expired and
// whose token endpoint refuses the refresh, so the first API call fails the
// way a revoked grant does in production.
func refusingClient(t *testing.T, status int, body string) (*Client, *refusingRT) {
	t.Helper()

	rt := &refusingRT{t: t, status: status, body: body}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: rt})

	c := &Client{Email: "sender@gmail.com"}
	cfg := oauth2.Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		// Pinned so the refusal is one request, not the auth-style probe's two.
		Endpoint: oauth2.Endpoint{TokenURL: "https://" + tokenHost + "/token", AuthStyle: oauth2.AuthStyleInParams},
	}
	expired := &oauth2.Token{
		AccessToken:  "stale",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(-time.Hour),
	}
	if merr := c.Init(ctx, expired, cfg); merr != nil {
		t.Fatalf("Init: %v", merr.Message)
	}
	return c, rt
}

func mailErrorOf(t *testing.T, err error) *errx.MailError {
	t.Helper()
	if err == nil {
		t.Fatal("call succeeded, want a mail error")
	}
	var mailErr *errx.MailError
	if !errors.As(err, &mailErr) {
		t.Fatalf("error %v (%T) is not a *errx.MailError, so the sync loop cannot classify it", err, err)
	}
	return mailErr
}

const revokedBody = `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`

// The Gmail half of the same incident: a grant the customer revoked in their
// Google account never reaches Gmail to become a 401, so it read as an
// offline mail server and retried forever.
func TestRevokedGrantIsAnAuthenticationErrorOnEveryCallPath(t *testing.T) {
	ctx := context.Background()

	t.Run("get message", func(t *testing.T) {
		c, rt := refusingClient(t, http.StatusBadRequest, revokedBody)
		_, err := c.GetMessage(ctx, "message-id")
		if got := mailErrorOf(t, err).Code; got != errx.MailErrorCodeGoogleAuth {
			t.Errorf("code = %s, want %s", got, errx.MailErrorCodeGoogleAuth)
		}
		if atomic.LoadInt32(&rt.calls) == 0 {
			t.Error("the token endpoint was never called, so no refresh was exercised")
		}
	})

	t.Run("list messages", func(t *testing.T) {
		c, _ := refusingClient(t, http.StatusBadRequest, revokedBody)
		_, _, err := c.ListMessages(ctx, "newer_than:1d", "", 10)
		if got := mailErrorOf(t, err).Code; got != errx.MailErrorCodeGoogleAuth {
			t.Errorf("code = %s, want %s", got, errx.MailErrorCodeGoogleAuth)
		}
	})

	t.Run("fetch history", func(t *testing.T) {
		c, _ := refusingClient(t, http.StatusBadRequest, revokedBody)
		_, err := c.FetchHistory(ctx, 12345)
		if got := mailErrorOf(t, err).Code; got != errx.MailErrorCodeGoogleAuth {
			t.Errorf("code = %s, want %s", got, errx.MailErrorCodeGoogleAuth)
		}
	})
}

// The other half: Google having a moment must not cost a customer their
// mailbox. Each of these must stay a retryable server error rather than the
// auth error the consumer deactivates on.
func TestTransientTokenRefusalsStayRetryable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"throttled", http.StatusTooManyRequests, `{"error":"rate_limit_exceeded"}`},
		{"token endpoint 500", http.StatusInternalServerError, `{}`},
		{"token endpoint 503", http.StatusServiceUnavailable, `{"error":"temporarily_unavailable"}`},
		{"unrecognised refusal", http.StatusBadRequest, `{"error":"something_new"}`},
		// Our own OAuth app, not the customer's grant: this answers for every
		// Gmail mailbox on the install at once.
		{"our app credentials are wrong", http.StatusUnauthorized, `{"error":"invalid_client","error_description":"The OAuth client was not found."}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := refusingClient(t, tc.status, tc.body)
			_, err := c.GetMessage(context.Background(), "message-id")
			mailErr := mailErrorOf(t, err)
			if mailErr.Code != errx.MailErrorCodeServerUnreachable {
				t.Fatalf("code = %s, want %s: this deactivates the mailbox", mailErr.Code, errx.MailErrorCodeServerUnreachable)
			}
			if mailErr.ResolveMethod != errx.MailErrorResolveMethodRetry {
				t.Errorf("resolve method = %s, want %s", mailErr.ResolveMethod, errx.MailErrorResolveMethodRetry)
			}
		})
	}
}

func TestHandleErrorClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want errx.MailErrorCode
	}{
		{"gmail 401", &googleapi.Error{Code: 401, Message: "invalid credentials"}, errx.MailErrorCodeGoogleAuth},
		{"gmail 402", &googleapi.Error{Code: 402, Message: "payment required"}, errx.MailErrorCodeGooglePayment},
		{"gmail 403", &googleapi.Error{Code: 403, Message: "gmail api disabled"}, errx.MailErrorCodeGoogleForbidden},
		// The API client wraps its error on some paths; an unwrapped type
		// assertion turned a real 403 into "the server may be offline".
		{"wrapped gmail 403", fmt.Errorf("gmail: %w", &googleapi.Error{Code: 403, Message: "gmail api disabled"}), errx.MailErrorCodeGoogleForbidden},
		{"wrapped gmail 401", fmt.Errorf("gmail: %w", &googleapi.Error{Code: 401, Message: "invalid credentials"}), errx.MailErrorCodeGoogleAuth},
		{"transport failure", errors.New("dial tcp: i/o timeout"), errx.MailErrorCodeServerUnreachable},
		{
			"revoked grant",
			&oauth2.RetrieveError{
				Response:  &http.Response{StatusCode: http.StatusBadRequest},
				ErrorCode: "invalid_grant",
			},
			errx.MailErrorCodeGoogleAuth,
		},
		{
			"throttled token endpoint",
			&oauth2.RetrieveError{
				Response:  &http.Response{StatusCode: http.StatusTooManyRequests},
				ErrorCode: "rate_limit_exceeded",
			},
			errx.MailErrorCodeServerUnreachable,
		},
		// A quota is a 403 too, and reading the status alone told the owner of
		// a mailbox that had gone too fast for a minute to re-authorize.
		{
			"gmail 403 quota, structured reason",
			&googleapi.Error{
				Code:    403,
				Message: "Quota exceeded for quota metric 'Total Query Cost'",
				Errors:  []googleapi.ErrorItem{{Reason: "rateLimitExceeded"}},
			},
			errx.MailErrorCodeSendingTooFast,
		},
		{
			"gmail 403 quota, message only",
			&googleapi.Error{Code: 403, Message: "Quota exceeded for quota metric 'Total Query Cost' and limit 'Units per minute per user'"},
			errx.MailErrorCodeSendingTooFast,
		},
		{"gmail 429", &googleapi.Error{Code: 429, Message: "too many requests"}, errx.MailErrorCodeSendingTooFast},
		{
			"wrapped gmail 403 quota",
			fmt.Errorf("gmail: %w", &googleapi.Error{Code: 403, Message: "Quota exceeded for quota metric"}),
			errx.MailErrorCodeSendingTooFast,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HandleError(tt.err)
			if got == nil {
				t.Fatal("HandleError returned nil for a real failure")
			}
			if got.Code != tt.want {
				t.Errorf("code = %s, want %s", got.Code, tt.want)
			}
		})
	}

	if HandleError(nil) != nil {
		t.Error("HandleError(nil) must stay nil so success is not reported as a failure")
	}
}
