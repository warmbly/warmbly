package email

import (
	"context"
	"testing"
	"time"
)

// TestVerifySMTP_UnreachableHost guards against a nil-conn deref. A wrong SMTP
// host is ordinary onboarding input, and these probes run in bare goroutines
// inside the worker, so a panic here takes the worker down along with every
// mailbox assigned to it.
func TestVerifySMTP_UnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name string
		host string
		port int
	}{
		// 465/587 are the only ports onboarding accepts, and each takes a
		// different dial path (implicit TLS vs STARTTLS).
		{"closed port, 587", "127.0.0.1", 587},
		{"closed port, 465", "127.0.0.1", 465},
		{"unresolvable host", "no-such-mail-host.invalid", 587},
		{"unresolvable host, implicit tls", "no-such-mail-host.invalid", 465},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("VerifySMTP panicked: %v", r)
				}
			}()
			if VerifySMTP(ctx, tc.host, tc.port, "user", "pass") {
				t.Fatal("VerifySMTP returned true for an unreachable server")
			}
		})
	}
}

// TestVerifyImap_UnreachableHost is the same guard for the IMAP probe.
func TestVerifyImap_UnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("VerifyImap panicked: %v", r)
		}
	}()
	if VerifyImap(ctx, "no-such-mail-host.invalid", 993, "user", "pass") {
		t.Fatal("VerifyImap returned true for an unreachable server")
	}
}
