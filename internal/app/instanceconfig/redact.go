package instanceconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// sensitiveMarkers make redaction structural rather than per-field: a new
// variable is protected by its name, not by someone remembering to list it.
var sensitiveMarkers = []string{"SECRET", "PASSWORD", "KEY", "TOKEN", "DSN", "_PASS"}

// sensitiveKeys are connection strings: every one of them routinely carries
// user:password inline, and none of them matches a marker.
var sensitiveKeys = map[string]bool{
	"PRIMARY_DB":          true,
	"REDIS":               true,
	"NATS_URL":            true,
	"SCHEMA_REGISTRY_URL": true,
}

// Sensitive reports whether a variable's value must never be returned.
func Sensitive(key string) bool {
	if sensitiveKeys[key] {
		return true
	}
	upper := strings.ToUpper(key)
	for _, marker := range sensitiveMarkers {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

// Fingerprint is the first 4 hex characters of SHA-256 of a value, empty when
// the value is unset. It exists so an operator can confirm the backend and the
// realtime service hold the same AUTH_SECRET without either being disclosed.
func Fingerprint(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:4]
}
