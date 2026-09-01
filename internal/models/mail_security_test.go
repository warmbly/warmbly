package models

import "testing"

// The resolvers are what keep mailboxes connected across the rollout: a stored
// mode wins, and an empty one has to reproduce the port-inferred behaviour the
// clients had before the column existed.
func TestResolveSMTPSecurity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		security string
		port     int
		want     string
	}{
		{"stored tls wins over port", MailSecurityTLS, 587, MailSecurityTLS},
		{"stored starttls wins over port", MailSecurityStartTLS, 465, MailSecurityStartTLS},
		{"empty infers implicit tls on 465", "", 465, MailSecurityTLS},
		{"empty infers starttls on 587", "", 587, MailSecurityStartTLS},
		{"empty infers starttls on 2525", "", 2525, MailSecurityStartTLS},
		{"garbage falls back to the port", "banana", 465, MailSecurityTLS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveSMTPSecurity(tc.security, tc.port); got != tc.want {
				t.Fatalf("ResolveSMTPSecurity(%q, %d) = %q, want %q", tc.security, tc.port, got, tc.want)
			}
		})
	}
}

func TestResolveIMAPSecurity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		security string
		port     int
		want     string
	}{
		{"stored starttls wins over port", MailSecurityStartTLS, 993, MailSecurityStartTLS},
		{"stored tls wins over port", MailSecurityTLS, 143, MailSecurityTLS},
		// Implicit TLS on anything but 143 reproduces the old always-TLS dial.
		{"empty infers implicit tls on 993", "", 993, MailSecurityTLS},
		{"empty infers implicit tls on a custom port", "", 9993, MailSecurityTLS},
		{"empty infers starttls on 143", "", 143, MailSecurityStartTLS},
		{"garbage falls back to the port", "banana", 143, MailSecurityStartTLS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveIMAPSecurity(tc.security, tc.port); got != tc.want {
				t.Fatalf("ResolveIMAPSecurity(%q, %d) = %q, want %q", tc.security, tc.port, got, tc.want)
			}
		})
	}
}

func TestValidMailSecurity(t *testing.T) {
	for _, s := range []string{MailSecurityTLS, MailSecurityStartTLS} {
		if !ValidMailSecurity(s) {
			t.Fatalf("ValidMailSecurity(%q) = false, want true", s)
		}
	}
	// "none" is deliberately not a mode: TLS is mandatory for mailboxes, and
	// the cleartext escape hatch is the instance-level MAIL_TLS_INSECURE knob.
	for _, s := range []string{"", "none", "ssl", "TLS"} {
		if ValidMailSecurity(s) {
			t.Fatalf("ValidMailSecurity(%q) = true, want false", s)
		}
	}
}
