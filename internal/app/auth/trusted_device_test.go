package auth

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/authrisk"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
)

// trustedSvc connects to WARMBLY_TEST_REDIS (for example redis://localhost:16379/15).
func trustedSvc(t *testing.T, mode string) *authService {
	t.Helper()
	url := os.Getenv("WARMBLY_TEST_REDIS")
	if url == "" {
		t.Skip("WARMBLY_TEST_REDIS not set")
	}
	c, err := cache.New(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	s := svc(exemptUsers{}, mode)
	s.cache = c
	return s
}

func TestTrustedDeviceSkipsTheCodeOnlyForItsOwner(t *testing.T) {
	s := trustedSvc(t, config.LoginCodeNewDevice)
	ctx := context.Background()
	owner, other := uuid.New(), uuid.New()
	t.Cleanup(func() { _ = s.ForgetTrustedDevices(ctx, owner) })

	tok := s.trustDevice(ctx, owner)
	if tok == "" {
		t.Fatal("no token issued")
	}
	if s.loginCodeRequired(ctx, owner, tok, authrisk.Verdict{}) {
		t.Error("a trusted device must skip the code")
	}
	if !s.loginCodeRequired(ctx, other, tok, authrisk.Verdict{}) {
		t.Error("a token must not work for another account")
	}
	if !s.loginCodeRequired(ctx, owner, tok+"x", authrisk.Verdict{}) {
		t.Error("an unknown token must not skip the code")
	}
	if !s.loginCodeRequired(ctx, owner, tok, authrisk.Verdict{Flagged: true, Reason: "impossible_travel"}) {
		t.Error("a flagged sign-in must still get the code")
	}
}

func TestTrustedDeviceDoesNotOverrideAlways(t *testing.T) {
	s := trustedSvc(t, config.LoginCodeAlways)
	ctx := context.Background()
	uid := uuid.New()
	t.Cleanup(func() { _ = s.ForgetTrustedDevices(ctx, uid) })

	if !s.loginCodeRequired(ctx, uid, s.trustDevice(ctx, uid), authrisk.Verdict{}) {
		t.Error("AUTH_LOGIN_CODE=always must ignore trusted devices")
	}
}

func TestForgetTrustedDevicesRevokesEveryToken(t *testing.T) {
	s := trustedSvc(t, config.LoginCodeNewDevice)
	ctx := context.Background()
	uid, other := uuid.New(), uuid.New()
	t.Cleanup(func() { _ = s.ForgetTrustedDevices(ctx, other) })

	a, b := s.trustDevice(ctx, uid), s.trustDevice(ctx, uid)
	keep := s.trustDevice(ctx, other)
	if err := s.ForgetTrustedDevices(ctx, uid); err != nil {
		t.Fatal(err)
	}

	if s.isTrustedDevice(ctx, uid, a) || s.isTrustedDevice(ctx, uid, b) {
		t.Error("a forgotten device still skips the code")
	}
	if !s.isTrustedDevice(ctx, other, keep) {
		t.Error("forgetting one account's devices touched another's")
	}
}

func TestForgetTrustedDeviceRevokesOnlyThatDevice(t *testing.T) {
	s := trustedSvc(t, config.LoginCodeNewDevice)
	ctx := context.Background()
	uid, other := uuid.New(), uuid.New()
	t.Cleanup(func() { _ = s.ForgetTrustedDevices(ctx, uid); _ = s.ForgetTrustedDevices(ctx, other) })

	signedOut, kept, othersTok := s.trustDevice(ctx, uid), s.trustDevice(ctx, uid), s.trustDevice(ctx, other)
	if err := s.ForgetTrustedDevice(ctx, uid, []string{signedOut, othersTok, "", "unknown"}); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if s.isTrustedDevice(ctx, uid, signedOut) {
		t.Error("the signed-out browser must ask for a code again")
	}
	if !s.isTrustedDevice(ctx, uid, kept) {
		t.Error("another remembered browser of the same account must stay remembered")
	}
	if !s.isTrustedDevice(ctx, other, othersTok) {
		t.Error("a token of another account must not be revoked through this one")
	}
}
