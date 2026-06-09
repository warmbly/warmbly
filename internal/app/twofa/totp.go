// Package twofa implements TOTP (RFC 6238) two-factor auth. The TOTP secret is
// sealed at rest with a SERVER-WIDE key (not the per-user DEK, which is
// unreachable during login — verification happens before full auth), and
// recovery codes are argon2-hashed. Hand-rolled TOTP (HMAC-SHA1, 6 digits,
// 30s period) keeps it dependency-free and auditable.
package twofa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpPeriod = 30
	totpDigits = 6
	totpSkew   = 1 // accept the adjacent 30s step on each side (±30s clock drift)
)

// GenerateSecret returns a fresh 160-bit base32 (no padding) TOTP secret.
func GenerateSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(buf), "="), nil
}

// padBase32 restores '=' padding so a stored (unpadded) secret decodes.
func padBase32(s string) string {
	if m := len(s) % 8; m != 0 {
		s += strings.Repeat("=", 8-m)
	}
	return s
}

func hotp(secret string, counter uint64) (string, error) {
	key, err := base32.StdEncoding.DecodeString(padBase32(strings.ToUpper(strings.TrimSpace(secret))))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset]&0x7f) << 24) | (uint32(sum[offset+1]) << 16) | (uint32(sum[offset+2]) << 8) | uint32(sum[offset+3])
	return fmt.Sprintf("%0*d", totpDigits, code%1_000_000), nil
}

// ValidateCode checks a 6-digit code against the secret, allowing ±1 step of
// clock skew. Constant-time compare on each candidate.
func ValidateCode(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	step := uint64(time.Now().Unix()) / totpPeriod
	for i := -totpSkew; i <= totpSkew; i++ {
		expected, err := hotp(secret, uint64(int64(step)+int64(i)))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// OtpauthURI builds the otpauth://totp provisioning URI an authenticator scans.
func OtpauthURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", totpDigits))
	q.Set("period", fmt.Sprintf("%d", totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
