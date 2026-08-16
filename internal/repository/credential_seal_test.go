package repository

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

func testRepo(t *testing.T) *emailRepository {
	t.Helper()
	enc, err := encrypt.NewEncrypterFromHex(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("build encrypter: %v", err)
	}
	return &emailRepository{Encrypt: enc}
}

func TestSealCredentialRoundTrip(t *testing.T) {
	r := testRepo(t)

	for _, plain := range []string{
		"ya29.a0ARGnu0-fake-google-access-token",
		"1//0gFAKE_google_refresh_token",
		"eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.fake.graph-token",
		"",
	} {
		sealed, err := r.sealCredential(plain)
		if err != nil {
			t.Fatalf("seal %q: %v", plain, err)
		}
		if plain != "" && sealed == plain {
			t.Fatalf("seal %q returned the plaintext unchanged", plain)
		}

		opened, legacy, err := r.openCredential(sealed)
		if err != nil {
			t.Fatalf("open %q: %v", plain, err)
		}
		if legacy {
			t.Errorf("open %q reported a sealed value as legacy plaintext", plain)
		}
		if opened != plain {
			t.Errorf("round trip of %q returned %q", plain, opened)
		}
	}
}

// Rows written before OAuth tokens were sealed must still be readable, and must
// be reported as legacy so the caller re-seals them.
func TestOpenCredentialDetectsLegacyPlaintext(t *testing.T) {
	r := testRepo(t)

	for _, stored := range []string{
		"ya29.a0ARGnu0-fake-google-access-token",
		"1//0gFAKE_google_refresh_token",
		"eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.fake.graph-token",
		"seed-fake-access-token",
	} {
		opened, legacy, err := r.openCredential(stored)
		if err != nil {
			t.Fatalf("open %q: %v", stored, err)
		}
		if !legacy {
			t.Errorf("open %q was not flagged as legacy plaintext", stored)
		}
		if opened != stored {
			t.Errorf("open %q returned %q, want the stored value verbatim", stored, opened)
		}
	}
}

// Without CREDENTIALS_ENCRYPTION_KEY the repository must fail closed rather
// than fall back to writing provider tokens in the clear.
func TestSealCredentialFailsClosedWithoutEncrypter(t *testing.T) {
	r := &emailRepository{}

	if _, err := r.sealCredential("ya29.token"); err == nil {
		t.Fatal("sealCredential succeeded with no encrypter configured")
	}
	if _, _, err := r.openCredential("ya29.token"); err == nil {
		t.Fatal("openCredential succeeded with no encrypter configured")
	}
}
