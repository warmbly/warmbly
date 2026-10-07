package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

const DiagnosticDKIMVerifier = "go-msgauth/dkim-v0.7.0"

type DiagnosticDKIMResult struct {
	DKIM          string    `json:"dkim"`
	Alignment     string    `json:"alignment"`
	SigningDomain string    `json:"signing_domain,omitempty"`
	Verifier      string    `json:"verifier"`
	ObservedAt    time.Time `json:"observed_at"`
}

func (r DiagnosticDKIMResult) Valid(now time.Time) bool {
	if r.Verifier != DiagnosticDKIMVerifier || r.ObservedAt.Before(now.Add(-5*time.Minute)) || r.ObservedAt.After(now.Add(time.Minute)) {
		return false
	}
	if r.DKIM != "unknown" && r.DKIM != "pass" && r.DKIM != "fail" {
		return false
	}
	if r.Alignment != "unknown" && r.Alignment != "pass" {
		return false
	}
	if r.DKIM != "pass" {
		return r.SigningDomain == "" && r.Alignment == "unknown"
	}
	if len(r.SigningDomain) == 0 || len(r.SigningDomain) > 253 || strings.ContainsAny(r.SigningDomain, "\r\n") {
		return false
	}
	for _, c := range r.SigningDomain {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

type DiagnosticAuthRequest struct {
	Token     uuid.UUID             `json:"token"`
	MailboxID uuid.UUID             `json:"mailbox_id"`
	WorkerID  uuid.UUID             `json:"worker_id"`
	MessageID string                `json:"message_id"`
	Nonce     uuid.UUID             `json:"nonce"`
	Result    *DiagnosticDKIMResult `json:"result,omitempty"`
}

type DiagnosticAuthGrant struct {
	Nonce     uuid.UUID `json:"nonce"`
	MessageID string    `json:"message_id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
}
