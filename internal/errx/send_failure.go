package errx

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SendFailure contains protocol evidence, never provider response bodies.
type SendFailure struct {
	Provider       string     `json:"provider,omitempty" avro:"provider"`
	Protocol       string     `json:"protocol,omitempty" avro:"protocol"`
	Status         int        `json:"status,omitempty" avro:"status"`
	EnhancedStatus string     `json:"enhanced_status,omitempty" avro:"enhanced_status"`
	Cause          string     `json:"cause,omitempty" avro:"cause"`
	Stage          string     `json:"stage,omitempty" avro:"stage"`
	Disposition    string     `json:"disposition,omitempty" avro:"disposition"`
	Scope          string     `json:"scope,omitempty" avro:"scope"`
	ObservedAt     time.Time  `json:"observed_at" avro:"observed_at"`
	RetryAt        *time.Time `json:"retry_at,omitempty" avro:"retry_at"`
}

const (
	SendRetry     = "retry"
	SendPermanent = "permanent"
	SendAuth      = "authentication"
	SendThrottle  = "throttle"
	SendAmbiguous = "ambiguous"
)

func RetryAfterAt(value string, now time.Time) *time.Time {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 || seconds > int64((1<<63-1)/time.Second) {
			return nil
		}
		at := now.Add(time.Duration(seconds) * time.Second)
		return &at
	}
	if at, err := http.ParseTime(value); err == nil {
		if at.Before(now) {
			at = now
		}
		return &at
	}
	return nil
}

// SafeProviderCode permits identifiers, not arbitrary messages or addresses.
func SafeProviderCode(value string) string {
	if len(value) == 0 || len(value) > 96 {
		return ""
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
			return ""
		}
	}
	return value
}

func WithSendFailure(base *MailError, failure SendFailure) *MailError {
	result := *base
	result.Failure = &failure
	if failure.RetryAt != nil {
		result.RetryAfter = failure.RetryAt.Sub(failure.ObservedAt)
	}
	return &result
}
