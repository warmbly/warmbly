package repository

import (
	"context"
	"net/mail"
	"testing"

	"github.com/google/uuid"
)

// Issue #142: RegistrationStart received the source address and dropped it
// after the CAPTCHA check, so nothing could correlate accounts by origin.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveSignupMetadata -v

func newSignupUser(t *testing.T) (UserRepository, uuid.UUID) {
	t.Helper()
	handle, pool := liveContactDB(t)
	addr, err := mail.ParseAddress("signup-" + uuid.New().String()[:8] + "@test.local")
	if err != nil {
		t.Fatalf("parse address: %v", err)
	}
	repo := NewUserRepostory(handle, nil)
	u, err := repo.CreateUser(context.Background(), addr, "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})
	return repo, u.ID
}

func readSignup(t *testing.T, id uuid.UUID) (ip *string, ua *string, risk int, norm *string) {
	t.Helper()
	_, pool := liveContactDB(t)
	if err := pool.QueryRow(context.Background(),
		`SELECT host(signup_ip), signup_user_agent, signup_email_risk, signup_email_normalized
		   FROM users WHERE id = $1`, id).Scan(&ip, &ua, &risk, &norm); err != nil {
		t.Fatalf("read signup metadata: %v", err)
	}
	return
}

func TestLiveSignupMetadataRoundTrips(t *testing.T) {
	repo, id := newSignupUser(t)

	if err := repo.RecordSignupMetadata(context.Background(), id,
		"203.0.113.5", "Mozilla/5.0 (test)", 35, "adalovelace@gmail.com"); err != nil {
		t.Fatalf("RecordSignupMetadata: %v", err)
	}

	ip, ua, risk, norm := readSignup(t, id)
	if ip == nil || *ip != "203.0.113.5" {
		t.Errorf("signup_ip = %v, want 203.0.113.5", ip)
	}
	if ua == nil || *ua != "Mozilla/5.0 (test)" {
		t.Errorf("signup_user_agent = %v", ua)
	}
	if risk != 35 {
		t.Errorf("signup_email_risk = %d, want 35", risk)
	}
	if norm == nil || *norm != "adalovelace@gmail.com" {
		t.Errorf("signup_email_normalized = %v", norm)
	}
}

// A signup with no usable address must still record everything else rather
// than failing the write and losing the whole record.
func TestLiveSignupMetadataToleratesAMissingAddress(t *testing.T) {
	repo, id := newSignupUser(t)

	for _, bad := range []string{"", "not-an-ip", "999.999.999.999"} {
		if err := repo.RecordSignupMetadata(context.Background(), id, bad, "agent", 10, "ada@acme.com"); err != nil {
			t.Fatalf("RecordSignupMetadata(%q): %v", bad, err)
		}
		ip, _, risk, norm := readSignup(t, id)
		if ip != nil {
			t.Errorf("signup_ip = %v for input %q, want NULL", *ip, bad)
		}
		if risk != 10 || norm == nil || *norm != "ada@acme.com" {
			t.Errorf("the rest of the record was lost for input %q", bad)
		}
	}
}

// A hostile user agent must not be able to write an unbounded string.
func TestLiveSignupMetadataBoundsTheUserAgent(t *testing.T) {
	repo, id := newSignupUser(t)
	huge := make([]byte, 8000)
	for i := range huge {
		huge[i] = 'x'
	}
	if err := repo.RecordSignupMetadata(context.Background(), id, "203.0.113.5", string(huge), 0, "a@b.com"); err != nil {
		t.Fatalf("RecordSignupMetadata: %v", err)
	}
	_, ua, _, _ := readSignup(t, id)
	if ua == nil || len(*ua) > 512 {
		t.Errorf("stored user agent is %d bytes, want it bounded", len(*ua))
	}
}
