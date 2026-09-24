package mailboximport

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
)

func TestRetryUnansweredRetriesOnlyASilentFleet(t *testing.T) {
	unansweredBackoff, unansweredReserve = time.Millisecond, 0
	t.Cleanup(func() { unansweredBackoff, unansweredReserve = 5*time.Second, 48*time.Second })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	calls := 0
	xerr := retryUnanswered(ctx, func() *errx.Error {
		calls++
		if calls < 3 {
			return errx.ErrEmailValidation
		}
		return nil
	})
	if xerr != nil || calls != 3 {
		t.Fatalf("silent workers: err = %v after %d calls, want success on the third", xerr, calls)
	}

	calls = 0
	refused := errx.NewWithIdentifier(errx.BadRequest, errx.ErrEmailValidation.Identifier, "IMAP did not answer")
	refused.Cause = "imap_timeout"
	if xerr := retryUnanswered(ctx, func() *errx.Error { calls++; return refused }); xerr != refused || calls != 1 {
		t.Fatalf("a verdict about the mail server was retried: %d calls", calls)
	}

	calls = 0
	if xerr := retryUnanswered(ctx, func() *errx.Error { calls++; return errx.ErrEmailCredentials }); xerr != errx.ErrEmailCredentials || calls != 1 {
		t.Fatalf("refused credentials were retried: %d calls", calls)
	}

	calls = 0
	short, cancelShort := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancelShort()
	unansweredReserve = time.Hour
	if xerr := retryUnanswered(short, func() *errx.Error { calls++; return errx.ErrEmailOnboardNoWorker }); xerr != errx.ErrEmailOnboardNoWorker || calls != 1 {
		t.Fatalf("retried past the lease: %d calls", calls)
	}
}

func TestAuthorizingMessageSaysWhoWhatAndHowLong(t *testing.T) {
	ms := authorizingMessage(VendorAuthorization{Pending: true, Vendor: "InboxKit", Stage: "processing"}, causeMicrosoftSignin, "acme.io")
	for _, want := range []string{"InboxKit is authorizing Warmbly on acme.io", "InboxKit status: processing", "Microsoft usually takes a few minutes", "within 2 hours"} {
		if !strings.Contains(ms, want) {
			t.Fatalf("microsoft message %q lacks %q", ms, want)
		}
	}
	g := authorizingMessage(VendorAuthorization{Pending: true}, causeGoogleSignin, "acme.io")
	if !strings.HasPrefix(g, "Your inbox vendor is authorizing") || !strings.Contains(g, "Google can take up to an hour") || strings.Contains(g, "status:") {
		t.Fatalf("google message %q", g)
	}
}
