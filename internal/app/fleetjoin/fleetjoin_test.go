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
	token       string
	expiresAt   *time.Time
	activated   int
	activateErr error
}

func (i *fakeIssuer) VerifyJoinToken(_ context.Context, token string) error {
	if token != i.token || i.token == "" {
		return fleetnode.ErrBadToken
	}
	if i.expiresAt == nil || !time.Now().Before(*i.expiresAt) {
		return fleetnode.ErrJoinTokenExpired
	}
	return nil
}
func (i *fakeIssuer) JoinTokenExpiresAt(context.Context) (*time.Time, error) { return i.expiresAt, nil }
func (i *fakeIssuer) ActivateJoinToken(_ context.Context, token string, expiresAt time.Time) error {
	if i.activateErr != nil {
		return i.activateErr
	}
	i.activated++
	i.token, i.expiresAt = token, &expiresAt
	return nil
}

func TestSyncKeepsTheStoredTokenJoinable(t *testing.T) {
	now := time.Now()
	in := func(d time.Duration) *time.Time { at := now.Add(d); return &at }
	fail := errors.New("unavailable")
	cases := []struct {
		name          string
		stored        string
		issuer        fakeIssuer
		getErr        error
		putErr        error
		wantRenewed   bool
		wantErr       bool
		wantActivated int
	}{
		{name: "nothing stored", wantRenewed: true, wantActivated: 1},
		{name: "valid for weeks", stored: "live", issuer: fakeIssuer{token: "live", expiresAt: in(20 * 24 * time.Hour)}},
		{name: "expiring soon", stored: "live", issuer: fakeIssuer{token: "live", expiresAt: in(5 * 24 * time.Hour)}, wantRenewed: true, wantActivated: 1},
		{name: "stored token was revoked", stored: "old", issuer: fakeIssuer{token: "live", expiresAt: in(20 * 24 * time.Hour)}, wantRenewed: true, wantActivated: 1},
		{name: "unreadable parameter changes nothing", stored: "live", issuer: fakeIssuer{token: "live", expiresAt: in(time.Hour)}, getErr: fail, wantErr: true},
		{name: "failed store keeps the stored token joining", stored: "live", issuer: fakeIssuer{token: "live", expiresAt: in(5 * 24 * time.Hour)}, putErr: fail, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{value: tc.stored, getErr: tc.getErr, putErr: tc.putErr}
			issuer := tc.issuer
			renewed, err := Sync(context.Background(), store, &issuer, now)
			if renewed != tc.wantRenewed || (err != nil) != tc.wantErr || issuer.activated != tc.wantActivated {
				t.Fatalf("renewed=%v err=%v activated=%d, want renewed=%v err=%v activated=%d", renewed, err, issuer.activated, tc.wantRenewed, tc.wantErr, tc.wantActivated)
			}
			if tc.getErr == nil && issuer.VerifyJoinToken(context.Background(), store.value) != nil {
				t.Fatalf("stored token %q does not join", store.value)
			}
		})
	}
}

func TestSyncRepairsAFailedActivation(t *testing.T) {
	store := &fakeStore{}
	issuer := &fakeIssuer{activateErr: errors.New("database unavailable")}
	if _, err := Sync(context.Background(), store, issuer, time.Now()); err == nil {
		t.Fatal("a failed activation must be reported")
	}
	issuer.activateErr = nil
	if renewed, err := Sync(context.Background(), store, issuer, time.Now()); err != nil || !renewed {
		t.Fatalf("renewed=%v err=%v, want the next sync to store and activate a token", renewed, err)
	}
	if err := issuer.VerifyJoinToken(context.Background(), store.value); err != nil {
		t.Fatalf("stored token does not join after repair: %v", err)
	}
}
