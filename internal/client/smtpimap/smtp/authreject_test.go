package smtp

import (
	"errors"
	"testing"
)

func TestIsDomainAuthRejection(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{
			"Microsoft 5.7.515",
			errors.New("550 5.7.515 Access denied, sending domain [acme.com] does not meet the required authentication level"),
			true,
		},
		{"Microsoft 5.7.509", errors.New("550 5.7.509 Access denied, sending domain does not pass DMARC verification"), true},
		{
			"Google 5.7.26",
			errors.New("550-5.7.26 Unauthenticated email from acme.com is not accepted due to domain's DMARC policy"),
			true,
		},
		{"lowercase prose", errors.New("550 unauthenticated mail is not accepted from this domain"), true},

		// A bad mailbox is the recipient's problem, not our domain's. Reading
		// it as a domain-auth failure would blame the wrong side and skip
		// suppressing an address that really is dead.
		{"unknown recipient", errors.New("550 5.1.1 User unknown"), false},
		{"mailbox full", errors.New("552 5.2.2 Mailbox full"), false},
		{"greylisting", errors.New("450 4.7.1 Try again later"), false},
		// Bare 5.7.1 is an ordinary relay denial and far too generic to act on.
		{"bare 5.7.1 relay denial", errors.New("550 5.7.1 Relaying denied"), false},
		{"5.7.1 that explains itself", errors.New("550 5.7.1 Message rejected due to DMARC policy"), true},
		{"ordinary outage", errors.New("dial tcp: connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDomainAuthRejection(tt.err); got != tt.want {
				t.Errorf("isDomainAuthRejection(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
