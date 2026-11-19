package graphkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Error codes GraphKit answers with.
const (
	CodeInvalidRequest             = "invalid_request"
	CodeIdempotencyConflict        = "idempotency_conflict"
	CodeNotFound                   = "not_found"
	CodeAccountNotFound            = "account_not_found"
	CodeDomainUnavailable          = "domain_unavailable"
	CodeDomainPremium              = "domain_premium"
	CodeTLDUnsupported             = "tld_unsupported"
	CodeQuoteExpired               = "quote_expired"
	CodeQuoteMismatch              = "quote_mismatch"
	CodeContactInvalid             = "contact_invalid"
	CodeProviderMixed              = "provider_mixed"
	CodeMailboxExists              = "mailbox_exists"
	CodeMailboxLimit               = "mailbox_limit"
	CodeLocalPartInvalid           = "local_part_invalid"
	CodeDomainNotReady             = "domain_not_ready"
	CodeRenewalNotDue              = "renewal_not_due"
	CodeRenewalWindowClosed        = "renewal_window_closed"
	CodeExpectedExpiryMismatch     = "expected_expiry_mismatch"
	CodeTransferLocked             = "transfer_locked"
	CodePartnerBalanceInsufficient = "partner_balance_insufficient"
	CodeUpstreamUnavailable        = "upstream_unavailable"
	CodeRateLimited                = "rate_limited"
	CodeInternal                   = "internal"
	CodeUnauthorized               = "unauthorized"
	CodeProvisioningFailed         = "provisioning_failed"
	CodeCredentialsUnavailable     = "credentials_unavailable"
	CodeDNSRecordConflict          = "dns_record_conflict"
	CodeDNSRecordManaged           = "dns_record_managed"
	CodeRenewalInProgress          = "renewal_in_progress"
	CodeStateConflict              = "state_conflict"
	// CodeNotRegisteredHere is a registrar operation on a domain registered elsewhere.
	CodeNotRegisteredHere = "not_registered_here"
	// CodePartnerSuspended refuses every key of a partner GraphKit suspended.
	CodePartnerSuspended   = "partner_suspended"
	CodeSendingUnavailable = "sending_unavailable"
	// CodeTransport is client-side: no answer at all (a timeout, a refused connection).
	CodeTransport = "transport"
)

// Error is a refusal from GraphKit, or a transport failure (Status 0).
type Error struct {
	Status     int             `json:"-"`
	Code       string          `json:"code"`
	Message    string          `json:"message"`
	RequestID  string          `json:"request_id"`
	Details    json.RawMessage `json:"details,omitempty"`
	RetryAfter time.Duration   `json:"-"`
	cause      error
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("graphkit: %s: %v", e.Code, e.cause)
	}
	return fmt.Sprintf("graphkit: %d %s: %s", e.Status, e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// Retryable reports a failure the same request may succeed on later.
// A provider that is not offered right now is not retried.
func (e *Error) Retryable() bool {
	if e.UnavailableProvider() != "" {
		return false
	}
	return e.Status == 0 || e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// UnavailableProvider is the mailbox provider an upstream_unavailable refusal names as not offered right now.
func (e *Error) UnavailableProvider() string {
	if e.Code != CodeUpstreamUnavailable || len(e.Details) == 0 {
		return ""
	}
	var d struct {
		Provider string `json:"provider"`
	}
	_ = json.Unmarshal(e.Details, &d)
	return d.Provider
}

// ContactFields names the registrant fields a contact_invalid refusal rejected.
func (e *Error) ContactFields() []string {
	var d struct {
		Fields []string `json:"fields"`
	}
	if len(e.Details) > 0 {
		_ = json.Unmarshal(e.Details, &d)
	}
	return d.Fields
}

// ExistingOrderID is the order a quote_mismatch says already used the quote.
func (e *Error) ExistingOrderID() string {
	var d struct {
		OrderID string `json:"order_id"`
	}
	if len(e.Details) > 0 {
		_ = json.Unmarshal(e.Details, &d)
	}
	return d.OrderID
}

// CurrentDomain is the domain an expected_expiry_mismatch carries.
func (e *Error) CurrentDomain() *Domain {
	if len(e.Details) == 0 {
		return nil
	}
	var d struct {
		Domain *Domain `json:"domain"`
	}
	if err := json.Unmarshal(e.Details, &d); err == nil && d.Domain != nil {
		return d.Domain
	}
	var bare Domain
	if err := json.Unmarshal(e.Details, &bare); err == nil && bare.ID != "" {
		return &bare
	}
	return nil
}

// DetailReason is details.reason, as on a domain_unavailable refusal.
func (e *Error) DetailReason() string {
	var d struct {
		Reason string `json:"reason"`
	}
	if len(e.Details) > 0 {
		_ = json.Unmarshal(e.Details, &d)
	}
	return d.Reason
}

// Conflicts are the records a dns_record_conflict refusal names.
func (e *Error) Conflicts() []DNSConflict {
	var d struct {
		Conflicts []DNSConflict `json:"conflicts"`
	}
	if len(e.Details) > 0 {
		_ = json.Unmarshal(e.Details, &d)
	}
	return d.Conflicts
}

// AccountLevel reports a refusal of the instance's partner account itself
// (a bad or revoked key, a suspension, an empty balance), which an operator
// has to fix and no customer can.
func (e *Error) AccountLevel() bool {
	switch e.Code {
	case CodeUnauthorized, CodePartnerSuspended, CodePartnerBalanceInsufficient:
		return true
	}
	return false
}

// AsError unwraps a GraphKit error.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// CodeOf is the GraphKit code behind err, "" when it is not a GraphKit error.
func CodeOf(err error) string {
	if e, ok := AsError(err); ok {
		return e.Code
	}
	return ""
}

// Definitive reports a refusal that will not change on retry: anything but a
// transport failure, a rate limit or a server error.
func Definitive(err error) bool {
	e, ok := AsError(err)
	return ok && !e.Retryable()
}
