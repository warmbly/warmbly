// Package partner is the reseller side of Warmbly: partners, their keys, the workspaces they create
// and manage through /v1/partner, the people they sign in, and the events they receive. It is
// unrelated to package partners, which reports partner-program referrals to Dub.
package partner

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	LiveKeyPrefix = "wmbp_live_"
	TestKeyPrefix = "wmbp_test_"
	keyBytes      = 32
	displayChars  = 4
)

// KeyMode is the mode a partner key belongs to, read from its prefix.
func KeyMode(raw string) (models.PartnerMode, bool) {
	switch {
	case strings.HasPrefix(raw, LiveKeyPrefix):
		return models.PartnerModeLive, true
	case strings.HasPrefix(raw, TestKeyPrefix):
		return models.PartnerModeTest, true
	}
	return "", false
}

func keyPrefix(mode models.PartnerMode) string {
	if mode == models.PartnerModeLive {
		return LiveKeyPrefix
	}
	return TestKeyPrefix
}

// HashKey is what is stored for a key; the key has 256 bits of entropy, so no salt is needed.
func HashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// GenerateKey mints a key for mode with the parts shown in lists and the hash to store.
func GenerateKey(mode models.PartnerMode) (raw, prefix, suffix, hash string, err error) {
	b := make([]byte, keyBytes)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", "", err
	}
	brand := keyPrefix(mode)
	raw = brand + base64.RawURLEncoding.EncodeToString(b)
	return raw, raw[:len(brand)+displayChars], raw[len(raw)-displayChars:], HashKey(raw), nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
