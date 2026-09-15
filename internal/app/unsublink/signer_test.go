package unsublink

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRoundTrip(t *testing.T) {
	s := New("secret", "https://api.example.com/")
	org, camp, contact := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	u := s.URL(org, camp, contact, now)
	if !strings.HasPrefix(u, "https://api.example.com/unsubscribe/") {
		t.Fatalf("unexpected url %q", u)
	}
	tok := strings.TrimPrefix(u, "https://api.example.com/unsubscribe/")
	c, err := s.Verify(tok, now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.OrgID != org || c.CampaignID != camp || c.ContactID != contact {
		t.Fatalf("claims mismatch: %+v", c)
	}
	if !c.ExpiresAt.Equal(now.Add(Validity)) {
		t.Fatalf("expiry %v", c.ExpiresAt)
	}
	if _, err := s.Verify(tok, now.Add(Validity)); err != ErrExpired {
		t.Fatalf("want expired, got %v", err)
	}
}

func TestTamperAndWrongKey(t *testing.T) {
	s := New("secret", "https://api.example.com")
	tok := s.Token(uuid.New(), uuid.New(), uuid.New(), time.Now())

	if _, err := New("other", "https://api.example.com").Verify(tok, time.Now()); err != ErrInvalid {
		t.Fatalf("wrong key: want invalid, got %v", err)
	}
	flipped := []byte(tok)
	flipped[3] ^= 1
	if _, err := s.Verify(string(flipped), time.Now()); err != ErrInvalid {
		t.Fatalf("tampered: want invalid, got %v", err)
	}
	if _, err := s.Verify("", time.Now()); err != ErrInvalid {
		t.Fatalf("empty: want invalid, got %v", err)
	}
}

func TestDisabledWithoutBase(t *testing.T) {
	s := New("secret", "")
	if s.Enabled() {
		t.Fatal("expected disabled")
	}
	if s.URL(uuid.New(), uuid.New(), uuid.New(), time.Now()) != "" {
		t.Fatal("expected empty url")
	}
}

// A workspace with its own verified tracking domain mints the link there, so
// the address the recipient reads is on the sender's domain. The token is the
// same one the API origin verifies, because the host is not signed.
func TestURLOnCustomOrigin(t *testing.T) {
	s := New("secret", "https://api.example.com")
	org, camp, contact := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()

	u := s.URLOn("https://t.acme.com/", org, camp, contact, now)
	if !strings.HasPrefix(u, "https://t.acme.com/unsubscribe/") {
		t.Fatalf("unexpected url %q", u)
	}
	if _, err := s.Verify(strings.TrimPrefix(u, "https://t.acme.com/unsubscribe/"), now); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got := s.URLOn("", org, camp, contact, now); !strings.HasPrefix(got, "https://api.example.com/unsubscribe/") {
		t.Fatalf("empty origin should fall back to the API origin, got %q", got)
	}
}

func TestTicketsAreShortAndTellThemselvesApartFromSignedTokens(t *testing.T) {
	s := New("secret", "https://api.example.com")

	ticket, err := NewTicket()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if len(ticket) != TicketLen {
		t.Fatalf("ticket %q is %d characters, want %d", ticket, len(ticket), TicketLen)
	}
	// The size is the security property, so it is asserted rather than left
	// to a comment: at least as much entropy as a random UUID (122 bits),
	// and fewer characters than one (36) to carry it.
	if bits := TicketBytes * 8; bits < 122 {
		t.Errorf("a ticket carries %d bits, less than a random UUID's 122", bits)
	}
	if TicketLen >= len("00000000-0000-4000-8000-000000000000") {
		t.Errorf("a ticket is %d characters, no shorter than the UUID it beats on entropy", TicketLen)
	}
	if !IsTicket(ticket) {
		t.Fatalf("a freshly minted ticket %q is not recognised as one", ticket)
	}

	// The whole point: the address a recipient reads in the text/plain part
	// fits on one line. The signed form does not.
	short := s.TicketURL("", ticket)
	long := s.URL(uuid.New(), uuid.New(), uuid.New(), time.Now())
	if len(short) >= len(long)-60 {
		t.Errorf("ticket link is not meaningfully shorter:\n%s (%d)\n%s (%d)", short, len(short), long, len(long))
	}
	if !strings.HasPrefix(short, "https://api.example.com/unsubscribe/") {
		t.Errorf("unexpected ticket url %q", short)
	}
	// Same path and same origin rules as a signed link, so the
	// tracking-domain proxy and the click-tracking skip need no new case.
	if got := s.TicketURL("https://t.customer.com/", ticket); got != "https://t.customer.com/unsubscribe/"+ticket {
		t.Errorf("ticket on a tracking domain: %q", got)
	}

	// A signed token must never be mistaken for a ticket: it would cost a
	// pointless lookup and then answer "invalid" for a link that is valid.
	if IsTicket(s.Token(uuid.New(), uuid.New(), uuid.New(), time.Now())) {
		t.Error("a signed token looks like a ticket")
	}
	for _, bad := range []string{"", "short", ExampleTicket + "x", "abcdefgh/../", "abcdefghij=="} {
		if IsTicket(bad) {
			t.Errorf("%q is not a ticket shape", bad)
		}
	}
	if !IsTicket(ExampleTicket) {
		t.Errorf("the preview example %q must have the real shape", ExampleTicket)
	}
}

// Tickets are minted per send; a repeat is a second recipient reading someone
// else's opt-out link.
func TestTicketsDoNotRepeat(t *testing.T) {
	seen := make(map[string]bool, 2000)
	for i := 0; i < 2000; i++ {
		tok, err := NewTicket()
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		if seen[tok] {
			t.Fatalf("duplicate ticket %q after %d mints", tok, i)
		}
		seen[tok] = true
	}
}

// Enabled is nil-safe on purpose, and every minting call answers a signer that
// cannot mint with the empty string rather than a panic. Asserted because the
// guard is easy to lose: Token signs through s.key, and Go evaluates it as an
// argument before the function it is passed to can check anything.
func TestAnUnusableSignerMintsNothingAndNeverPanics(t *testing.T) {
	org, camp, contact := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()

	for name, s := range map[string]*Signer{
		"nil":        nil,
		"no origin":  New("secret", ""),
		"zero value": {},
	} {
		if s.Enabled() {
			t.Errorf("%s: Enabled() should be false", name)
		}
		if got := s.URLOn("https://t.customer.com", org, camp, contact, now); got != "" {
			t.Errorf("%s: URLOn = %q, want empty", name, got)
		}
		if got := s.URL(org, camp, contact, now); got != "" {
			t.Errorf("%s: URL = %q, want empty", name, got)
		}
		if got := s.TicketURL("https://t.customer.com", ExampleTicket); got != "" {
			t.Errorf("%s: TicketURL = %q, want empty", name, got)
		}
		if _, err := s.Verify("whatever", now); err == nil {
			t.Errorf("%s: Verify accepted a token", name)
		}
	}
}
