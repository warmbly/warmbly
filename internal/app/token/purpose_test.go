package token

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// A token minted for one flow must not be spendable on another. This is the
// regression for the realtime socket accepting a password-reset link token,
// and for anything else that shares the signing key.
func TestTokenPurposeIsEnforced(t *testing.T) {
	s := &tokenService{AuthSecret: "test-secret-at-least-32-characters-long"}

	userID, sessionID := uuid.New(), uuid.New()
	now := time.Now()
	exp := now.Add(10 * time.Minute)

	purposes := []string{
		PurposeAccess,
		PurposeRefresh,
		PurposeWebSocket,
		PurposeWebSocketProxy,
		PurposeLoginCode,
		PurposeRegistration,
		PurposePasswordReset,
		PurposeTwoFAPending,
		PurposeSSOLink,
	}

	for _, minted := range purposes {
		tok, err := s.GenerateTokenFor(minted, userID, sessionID, "", "nonce", now, exp)
		if err != nil {
			t.Fatalf("mint %s: %v", minted, err)
		}

		if _, xerr := s.VerifyTokenFor(minted, tok); xerr != nil {
			t.Errorf("a %s token should verify as %s, got %v", minted, minted, xerr)
		}

		for _, other := range purposes {
			if other == minted {
				continue
			}
			if _, xerr := s.VerifyTokenFor(other, tok); xerr == nil {
				t.Errorf("a %s token must not be accepted as %s", minted, other)
			}
		}
	}
}

// A token with no purpose claim verifies for no flow at all.
func TestTokenWithoutPurposeIsRefused(t *testing.T) {
	s := &tokenService{AuthSecret: "test-secret-at-least-32-characters-long"}
	now := time.Now()

	bare, err := s.GenerateTokenFor("", uuid.New(), uuid.New(), "", "nonce", now, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	for _, p := range []string{PurposeAccess, PurposeRefresh, PurposeWebSocket, PurposePasswordReset, PurposeSSOLink} {
		if _, xerr := s.VerifyTokenFor(p, bare); xerr == nil {
			t.Errorf("a token with no purpose must not pass as %s", p)
		}
	}
}

// The verifier pins HS256 rather than accepting the HMAC family, and requires
// an expiry.
func TestVerifierRejectsUnsignedAndUnexpiring(t *testing.T) {
	s := &tokenService{AuthSecret: "test-secret-at-least-32-characters-long"}

	// alg=none, the classic.
	const none = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAifQ."
	if _, xerr := s.VerifyToken(none); xerr == nil {
		t.Error("an unsigned token must be refused")
	}

	expired, err := s.GenerateTokenFor(PurposeAccess, uuid.New(), uuid.New(), "", "n",
		time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, xerr := s.VerifyToken(expired); xerr == nil {
		t.Error("an expired token must be refused")
	}
}

// Rotation has to mint a refresh token this same function will accept next
// time. Minting it through GenerateToken, which defaults to PurposeAccess,
// produced a session that survived exactly one refresh and then signed the
// person out for good about a day after every login.
func TestRotatedRefreshTokenVerifiesAsRefresh(t *testing.T) {
	s := &tokenService{AuthSecret: "test-secret-at-least-32-characters-long"}

	userID, sessionID := uuid.New(), uuid.New()
	now := time.Now()

	// What refresh.go mints on rotation, for both halves of the pair.
	access, err := s.GenerateTokenFor(PurposeAccess, userID, sessionID, "", "a", now, now.Add(AccessTokenLifeTime))
	if err != nil {
		t.Fatalf("mint access: %v", err)
	}
	refresh, err := s.GenerateTokenFor(PurposeRefresh, userID, sessionID, "", "r", now, now.Add(RefreshTokenLifeTime))
	if err != nil {
		t.Fatalf("mint refresh: %v", err)
	}

	// The rotated refresh token must satisfy the check RefreshToken performs.
	if _, xerr := s.VerifyTokenFor(PurposeRefresh, refresh); xerr != nil {
		t.Errorf("a rotated refresh token must verify as a refresh token: %v", xerr)
	}
	// And the rotated access token must satisfy ValidateAccessToken's check.
	if _, xerr := s.VerifyTokenFor(PurposeAccess, access); xerr != nil {
		t.Errorf("a rotated access token must verify as an access token: %v", xerr)
	}
	// They are still not interchangeable.
	if _, xerr := s.VerifyTokenFor(PurposeAccess, refresh); xerr == nil {
		t.Error("a refresh token must not pass as an access token")
	}
	if _, xerr := s.VerifyTokenFor(PurposeRefresh, access); xerr == nil {
		t.Error("an access token must not pass as a refresh token")
	}
}
