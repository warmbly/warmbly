package plathealth

import (
	"encoding/json"
	"regexp"
	"strings"
)

var allowedReasons = map[string]struct{}{
	ReasonTimeout:              {},
	ReasonUnobserved:           {},
	ReasonFailed:               {},
	ReasonStale:                {},
	ReasonHTTPProcessUpOnly:    {},
	ReasonNoFreshHeartbeat:     {},
	ReasonNewestHeartbeatStale: {},
	ReasonRoundTripMismatch:    {},
	ReasonRoundTripTimeout:     {},
	ReasonPublishFailed:        {},
	ReasonSubscribeFailed:      {},
	ReasonContractMissingPlane: {},
	ReasonMailTransportMissing: {},
	ReasonTransportConfigured:  {},
	ReasonSMTPPreflightFailed:  {},
	ReasonHTTPScheme:           {},
	ReasonDNSFailed:            {},
	ReasonTLSFailed:            {},
	ReasonReadyNotReady:        {},
	ReasonDepsNotReady:         {},
	ReasonSelectFailed:         {},
	ReasonCacheMismatch:        {},
}

// AllowReason returns reason if it is on the allowlist, otherwise "failed".
// Empty stays empty.
func AllowReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	if _, ok := allowedReasons[reason]; ok {
		return reason
	}
	return ReasonFailed
}

var (
	emailLike   = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	secretLike  = regexp.MustCompile(`(?i)(password|passwd|secret|token|bearer |authorization:|api[_-]?key|BEGIN [A-Z ]+PRIVATE KEY)`)
	messageLike = regexp.MustCompile(`(?i)("body"|"message"|"subject"|"html"|"text"|"recipient"|"to_addr"|"from_addr")\s*:`)
)

// PIIFindings reports forbidden substrings in a health/probe JSON payload.
// Used by tests and by the probe CLI self-check. Empty means clean.
func PIIFindings(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	s := string(raw)
	var hits []string
	if emailLike.MatchString(s) {
		hits = append(hits, "recipient_or_email")
	}
	if secretLike.MatchString(s) {
		hits = append(hits, "secret_or_token")
	}
	if messageLike.MatchString(s) {
		hits = append(hits, "message_body_field")
	}
	return hits
}

// MarshalReport encodes a Report using the allowlisted struct tags only.
func MarshalReport(r Report) ([]byte, error) {
	return json.Marshal(r)
}

// MarshalProbe encodes a ProbeReport using the allowlisted struct tags only.
func MarshalProbe(r ProbeReport) ([]byte, error) {
	return json.Marshal(r)
}
