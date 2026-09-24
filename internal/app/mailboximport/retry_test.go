package mailboximport

import (
	"context"
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
