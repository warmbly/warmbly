package clientreport

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"github.com/google/uuid"
)

// tokenBytes is the entropy behind every report link.
const tokenBytes = 32

// hintLength is how much of a token the dashboard shows to tell links apart.
const hintLength = 6

// newShareToken is a random share-link token in its URL form.
func newShareToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// emailToken is a delivery's link token: derived, so a retried send can rebuild the link it never stored.
func emailToken(key []byte, deliveryID uuid.UUID) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("client-report-delivery:"))
	mac.Write(deliveryID[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// EmailLinkKey derives the delivery-token key from the instance's auth secret.
func EmailLinkKey(authSecret string) []byte {
	sum := sha256.Sum256([]byte(authSecret + ":client-report-email"))
	return sum[:]
}

// hashToken is what the database stores and looks a token up by.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// validTokenShape refuses anything that could not be a minted token before it reaches the database.
func validTokenShape(token string) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(tokenBytes) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == tokenBytes
}

func tokenHint(token string) string {
	if len(token) < hintLength {
		return token
	}
	return token[:hintLength]
}
