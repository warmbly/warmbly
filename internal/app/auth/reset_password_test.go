package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// A reset link is bound to the password it was requested against: once any
// path writes a new one, every link issued at or before that write is dead,
// whatever its own expiry says.
func TestResetLinkPredatesPassword(t *testing.T) {
	changed := time.Date(2026, 9, 21, 12, 0, 0, 500_000_000, time.UTC)
	at := func(t time.Time) *jwt.NumericDate { return jwt.NewNumericDate(t) }

	cases := []struct {
		name      string
		issuedAt  *jwt.NumericDate
		changedAt *time.Time
		stale     bool
	}{
		{"never changed", at(changed.Add(-time.Hour)), nil, false},
		{"issued before the change", at(changed.Add(-time.Hour)), &changed, true},
		{"issued after the change", at(changed.Add(time.Hour)), &changed, false},
		// iat is whole seconds, so a token minted in the same second as the
		// change cannot prove it came after it.
		{"issued in the same second", at(changed), &changed, true},
		{"issued the next second", at(changed.Add(time.Second)), &changed, false},
		{"no issue time at all", nil, &changed, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resetLinkPredatesPassword(tc.issuedAt, tc.changedAt); got != tc.stale {
				t.Fatalf("stale = %v, want %v", got, tc.stale)
			}
		})
	}
}

// The confirm step tells a reset session handle from the emailed link token,
// so links opened by older clients keep working while new ones send a handle.
func TestResetSessionIsTellableFromLinkToken(t *testing.T) {
	a, _ := crypt.Nonce()
	b, _ := crypt.Nonce()
	if isResetLinkToken(a + b) {
		t.Fatal("a reset session handle was read as a link token")
	}
	if !isResetLinkToken("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln") {
		t.Fatal("a link token was read as a reset session handle")
	}
}

type fakeRevoker struct {
	err   error
	calls int
}

func (f *fakeRevoker) RevokeUserCredentials(context.Context, uuid.UUID) error {
	f.calls++
	return f.err
}

// Signing out apps and API keys either ends every kind or reports failure, so
// the password write that asked for it never goes ahead on a partial revoke.
func TestRevokeAPICredentials(t *testing.T) {
	ctx, uid := context.Background(), uuid.New()

	if err := (&authService{}).revokeAPICredentials(ctx, uid); err == nil {
		t.Fatal("no revokers wired, want an error")
	}

	grants, keys := &fakeRevoker{}, &fakeRevoker{}
	s := &authService{}
	s.WireCredentialRevokers(grants, keys)
	if err := s.revokeAPICredentials(ctx, uid); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if grants.calls != 1 || keys.calls != 1 {
		t.Fatalf("calls = %d, %d; want 1, 1", grants.calls, keys.calls)
	}

	failing := &fakeRevoker{err: errors.New("down")}
	s = &authService{}
	s.WireCredentialRevokers(failing, keys)
	if err := s.revokeAPICredentials(ctx, uid); err == nil {
		t.Fatal("a failing revoker must fail the revoke")
	}
}

// A deployment with password sign-in off sends no set-password link.
func TestSendPasswordLinkRefusedWhenPasswordLoginDisabled(t *testing.T) {
	s := &authService{policy: &config.AuthPolicy{DisablePasswordLogin: true}}
	if err := s.SendPasswordLink(context.Background(), uuid.New()); err != ErrPasswordLoginDisabled {
		t.Fatalf("err = %v, want ErrPasswordLoginDisabled", err)
	}
}
