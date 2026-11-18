package worker

import (
	"errors"
	"fmt"
	"testing"

	"github.com/warmbly/warmbly/internal/errx"
)

func TestUnreachableServerIsNotRedelivered(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("locate warmup message for deletion: %w", &errx.MailError{Code: errx.MailErrorCodeServerUnreachable}), true},
		{&errx.MailError{Code: errx.MailErrorCodeConnectionLost}, true},
		{&errx.MailError{Code: errx.MailErrorCodeRateLimitExceeded}, false},
		{errors.New("folder busy"), false},
	} {
		if got := mailServerUnreachable(tc.err); got != tc.want {
			t.Fatalf("%v: got %v want %v", tc.err, got, tc.want)
		}
	}
}
