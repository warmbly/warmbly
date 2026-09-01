package email

import (
	"context"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// TestVerifySMTP_UnreachableHost guards against a nil-conn deref. A wrong SMTP
// host is ordinary onboarding input, and these probes run in bare goroutines
// inside the worker, so a panic here takes the worker down along with every
// mailbox assigned to it.
func TestVerifySMTP_UnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name     string
		host     string
		port     int
		security string
	}{
		// Each security mode takes a different dial path (implicit TLS vs
		// plaintext + STARTTLS), and an empty mode falls back to the port.
		{"closed port, 587", "127.0.0.1", 587, ""},
		{"closed port, 465", "127.0.0.1", 465, ""},
		{"unresolvable host", "no-such-mail-host.invalid", 587, ""},
		{"unresolvable host, implicit tls", "no-such-mail-host.invalid", 465, ""},
		// Non-standard ports are accepted now, so the mode carries the choice.
		{"closed nonstandard port, starttls", "127.0.0.1", 2525, models.MailSecurityStartTLS},
		{"closed nonstandard port, implicit tls", "127.0.0.1", 8465, models.MailSecurityTLS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("VerifySMTP panicked: %v", r)
				}
			}()
			if VerifySMTP(ctx, tc.host, tc.port, "user", "pass", tc.security) {
				t.Fatal("VerifySMTP returned true for an unreachable server")
			}
		})
	}
}

// TestVerifyImap_UnreachableHost is the same guard for the IMAP probe, across
// both security modes.
func TestVerifyImap_UnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name     string
		host     string
		port     int
		security string
	}{
		{"unresolvable host, implicit tls", "no-such-mail-host.invalid", 993, ""},
		{"unresolvable host, starttls", "no-such-mail-host.invalid", 143, ""},
		{"closed port, starttls", "127.0.0.1", 143, models.MailSecurityStartTLS},
		{"closed nonstandard port, implicit tls", "127.0.0.1", 9993, models.MailSecurityTLS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("VerifyImap panicked: %v", r)
				}
			}()
			if VerifyImap(ctx, tc.host, tc.port, "user", "pass", tc.security) {
				t.Fatal("VerifyImap returned true for an unreachable server")
			}
		})
	}
}
