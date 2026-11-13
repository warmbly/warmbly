package fleetjoin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/app/fleetnode"
)

type fakeStore struct {
	value  string
	getErr error
	putErr error
}

func (s *fakeStore) Get(context.Context) (string, error) { return s.value, s.getErr }
func (s *fakeStore) Put(_ context.Context, token string) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.value = token
	return nil
}

type fakeIssuer struct {
	token     string
	expiresAt *time.Time
	issued    int
}

func (i *fakeIssuer) VerifyJoinToken(_ context.Context, token string) error {
	if token != i.token || i.token == "" {
		return fleetnode.ErrBadToken
	}
	return nil
}
func (i *fakeIssuer) JoinTokenExpiresAt(context.Context) (*time.Time, error) { return i.expiresAt, nil }
func (i *fakeIssuer) IssueJoinToken(_ context.Context, ttl time.Duration) (string, time.Time, error) {
	i.issued++
	i.token = "token-" + string(rune('a'+i.issued))
	at := time.Now().Add(ttl)
	i.expiresAt = &at
	return i.token, at, nil
}

func TestSyncKeepsTheStoredTokenJoinable(t *testing.T) {
	now := time.Now()
	in := func(d time.Duration) *time.Time { at := now.Add(d); return &at }
	storeFail := errors.New("store unavailable")
	cases := []struct {
		name        string
		stored      string
		issuer      fakeIssuer
		getErr      error
		putErr      error
		wantRenewed bool
		wantErr     bool
		wantIssued  int
	}{
		{name: "nothing stored", wantRenewed: true, wantIssued: 1},
		{name: "valid for weeks", stored: "live", issuer: fakeIssuer{token: "live", expiresAt: in(20 * 24 * time.Hour)}},
		{name: "expiring soon", stored: "live", issuer: fakeIssuer{token: "live", expiresAt: in(5 * 24 * time.Hour)}, wantRenewed: true, wantIssued: 1},
		{name: "stored token was revoked", stored: "old", issuer: fakeIssuer{token: "live", expiresAt: in(20 * 24 * time.Hour)}, wantRenewed: true, wantIssued: 1},
		{name: "unreadable parameter revokes nothing", stored: "live", issuer: fakeIssuer{token: "live", expiresAt: in(time.Hour)}, getErr: storeFail, wantErr: true},
		{name: "failed store is reported", putErr: storeFail, wantRenewed: true, wantErr: true, wantIssued: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{value: tc.stored, getErr: tc.getErr, putErr: tc.putErr}
			issuer := tc.issuer
			renewed, err := Sync(context.Background(), store, &issuer, now)
			if renewed != tc.wantRenewed || (err != nil) != tc.wantErr || issuer.issued != tc.wantIssued {
				t.Fatalf("renewed=%v err=%v issued=%d, want renewed=%v err=%v issued=%d", renewed, err, issuer.issued, tc.wantRenewed, tc.wantErr, tc.wantIssued)
			}
			if err == nil && issuer.VerifyJoinToken(context.Background(), store.value) != nil {
				t.Fatalf("stored token %q does not join", store.value)
			}
		})
	}
}
