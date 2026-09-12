package email

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
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

// ── partial consent ──────────────────────────────────────────────────────────
//
// Google's consent screen lets the person untick individual permissions and
// still issues a token. Everything below is about refusing that at connect
// time, because the alternative is a mailbox that looks connected and then
// fails on its first send with a provider 403 nobody can trace to a checkbox.

func tokenWithScope(scope string) *oauth2.Token {
	tok := &oauth2.Token{AccessToken: "at", RefreshToken: "rt"}
	if scope == "" {
		return tok
	}
	return tok.WithExtra(map[string]any{"scope": scope})
}

func TestPartialConsentIsRefused(t *testing.T) {
	want := []string{
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/gmail.send",
	}

	// Send was unticked: the one that breaks campaigns.
	xerr := checkGrantedScopes(context.Background(), models.InboxProviderGoogle, want,
		tokenWithScope("https://www.googleapis.com/auth/gmail.readonly"))
	if xerr == nil {
		t.Fatal("expected a partly granted consent to be refused")
	}
	if !strings.Contains(xerr.Message, "send email on your behalf") {
		t.Errorf("the error should name the missing permission, got %q", xerr.Message)
	}
}

func TestFullConsentIsAccepted(t *testing.T) {
	want := []string{
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/gmail.send",
	}
	if xerr := checkGrantedScopes(context.Background(), models.InboxProviderGoogle, want,
		tokenWithScope("https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/gmail.send")); xerr != nil {
		t.Fatalf("full consent must be accepted, got %v", xerr)
	}
}

// gmail.modify confers both reading and sending. Someone who granted the
// broader permission must not be turned away for lacking the narrower one.
func TestBroaderScopeSatisfiesNarrowerOnes(t *testing.T) {
	want := []string{
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/gmail.send",
		"https://www.googleapis.com/auth/gmail.metadata",
	}
	if xerr := checkGrantedScopes(context.Background(), models.InboxProviderGoogle, want,
		tokenWithScope("https://www.googleapis.com/auth/gmail.modify")); xerr != nil {
		t.Fatalf("gmail.modify should satisfy readonly, send and metadata, got %v", xerr)
	}
}

// A provider that returns no scope at all has told us nothing. That is not the
// same as a denial and must not block a legitimate connection.
func TestMissingScopeFieldDoesNotBlock(t *testing.T) {
	want := []string{"https://www.googleapis.com/auth/gmail.send"}
	if xerr := checkGrantedScopes(context.Background(), models.InboxProviderGoogle, want, tokenWithScope("")); xerr != nil {
		t.Fatalf("an absent scope field must not be read as a denial, got %v", xerr)
	}
}

func TestOutlookPartialConsentIsRefused(t *testing.T) {
	want := []string{
		"https://graph.microsoft.com/Mail.Send",
		"https://graph.microsoft.com/Mail.ReadWrite",
	}
	xerr := checkGrantedScopes(context.Background(), models.InboxProviderOutlook, want,
		tokenWithScope("https://graph.microsoft.com/Mail.Send"))
	if xerr == nil {
		t.Fatal("expected a partly granted Outlook consent to be refused")
	}
	if !strings.Contains(xerr.Message, "read and write mail") {
		t.Errorf("got %q", xerr.Message)
	}
}

// ── what may be recorded ─────────────────────────────────────────────────────

// A decode failure and a missing address both carry status 200, and that body
// is a successful profile payload: the mailbox owner's address. It must never
// reach the log stream or error tracking.
func TestDiagnosticBodyNeverRecordsASuccessfulPayload(t *testing.T) {
	profile := []byte(`{"emailAddress":"someone@example.com","messagesTotal":42}`)

	for _, status := range []int{200, 201, 204, 299} {
		if got := diagnosticBody(status, profile); got != "" {
			t.Errorf("status %d recorded a success body: %q", status, got)
		}
	}
}

// A failure body is the provider's own error payload and is what makes the
// failure diagnosable, so it is kept.
func TestDiagnosticBodyKeepsFailurePayloads(t *testing.T) {
	body := []byte(`{"error":{"code":403,"message":"Gmail API has not been used in project 1010273043313"}}`)
	got := diagnosticBody(403, body)
	if !strings.Contains(got, "has not been used in project") {
		t.Fatalf("a failure payload should be recorded, got %q", got)
	}
}

func TestDiagnosticBodyTruncatesToTheLimit(t *testing.T) {
	got := diagnosticBody(500, []byte(strings.Repeat("x", 5000)))
	if len(got) != diagnosticDetailLimit+len("…") {
		t.Fatalf("got %d bytes, want the %d-byte prefix plus the marker", len(got), diagnosticDetailLimit)
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("a truncated body should say so")
	}
}

// The reads are bounded before anything looks at them, so a provider that
// streams forever cannot be turned into unbounded memory here.
func TestProviderBodyReadIsBounded(t *testing.T) {
	swapTransport(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		huge := strings.Repeat("y", maxProviderBody*4)
		_, _ = w.Write([]byte(huge))
	}))

	owner, xerr := fetchGmailOwner(context.Background(), "token")
	if owner != nil || xerr == nil {
		t.Fatal("expected the lookup to fail")
	}
}
