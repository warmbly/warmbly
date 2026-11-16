package errx

import (
	"errors"
	"strings"
	"testing"
)

func TestProviderCallReadsAsTheCallNotASend(t *testing.T) {
	throttled := WithSendFailure(ErrMailSendingTooFast, SendFailure{Status: 429, Cause: "TooManyRequests", Disposition: SendThrottle})
	err := ProviderCall("graph subscription create", "", throttled)
	if got := err.Error(); got != "graph subscription create: HTTP 429 TooManyRequests" {
		t.Fatalf("message %q", got)
	}
	if strings.Contains(err.Error(), "sent too quickly") {
		t.Fatal("a push refusal reads as a send")
	}
	var mailErr *MailError
	if !errors.As(err, &mailErr) || mailErr.Failure.Disposition != SendThrottle {
		t.Fatal("the classified failure is not reachable")
	}

	refused := WithSendFailure(ErrMailSendRejected("The provider rejected the request"), SendFailure{Status: 400, Disposition: SendPermanent})
	if got := ProviderCall("gmail users.watch", "Invalid topicName", refused).Error(); got != "gmail users.watch: HTTP 400 Invalid topicName" {
		t.Fatalf("message %q", got)
	}

	plain := errors.New("dial tcp: timeout")
	if got := ProviderCall("gmail users.watch", "", plain); !errors.Is(got, plain) || got.Error() != "gmail users.watch: dial tcp: timeout" {
		t.Fatalf("an unclassified error lost its text: %v", got)
	}
	if ProviderCall("x", "", nil) != nil {
		t.Fatal("no error became one")
	}
}
