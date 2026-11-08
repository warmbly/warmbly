package connectlink

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/formwire"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// tokenBytes is the size of the secret in a connect link's URL.
const tokenBytes = 32

// newToken returns the URL token and the hash stored in its place.
func newToken() (string, []byte, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(raw), sum[:], nil
}

// hashToken is the stored form of a URL token; ok is false for anything newToken could not have produced.
func hashToken(token string) ([]byte, bool) {
	if !formwire.ConnectTokenShape(token) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != tokenBytes {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	return sum[:], true
}

// URL is a connect page on base for token.
func URL(base, token string) string {
	return strings.TrimRight(base, "/") + PagePath + token
}

// PagePath is where the portal service serves a connect page, followed by the token.
const PagePath = "/connect/"

// ipHash records where a connect came from without keeping the address; salted per link so uses cannot be joined across links.
func ipHash(linkID uuid.UUID, ip string) string {
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("connect-link:" + linkID.String() + ":" + ip))
	return hex.EncodeToString(sum[:16])
}

// OAuthStatePrefix marks a sign-in a client started from a connect link; no other flow mints it.
const OAuthStatePrefix = "cl."

// IsOAuthState reports whether a provider callback belongs to a connect link sign-in.
func IsOAuthState(state string) bool {
	return strings.HasPrefix(state, OAuthStatePrefix)
}

func newOAuthState() (string, error) {
	n, err := crypt.Nonce()
	if err != nil {
		return "", err
	}
	return OAuthStatePrefix + n, nil
}
