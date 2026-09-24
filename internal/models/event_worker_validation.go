package models

import (
	"strings"

	"github.com/google/uuid"
)

type EventWorkerEmailValidation struct {
	OrgID       uuid.UUID `json:"org_id" avro:"org_id"`
	ProcessID   uuid.UUID `json:"process_id" avro:"process_id"`
	Credentials *SmtpImap `json:"credentials" avro:"credentials"`
}

// EmailValidationRequestChannel is the Redis channel a worker takes credential
// checks on, so a check never waits behind the commands queued on its topic.
func EmailValidationRequestChannel(workerID uuid.UUID) string {
	return "email_validation_request:" + workerID.String()
}

// EmailValidationReplyChannel is where the worker answers one check.
func EmailValidationReplyChannel(processID uuid.UUID) string {
	return "email_validation:" + processID.String()
}

// Mail probe reasons: why one leg of a credential check did not pass. They
// travel from the worker to the backend inside EmailValidationVerdict and are
// what lets the connect form say what the server said instead of "invalid
// credentials" for everything from a typo to a firewall.
const (
	// MailProbeAuthRefused: the server answered and refused the sign-in.
	MailProbeAuthRefused = "auth_refused"
	// MailProbeUnreachable: no connection at all (DNS, refused, filtered).
	MailProbeUnreachable = "unreachable"
	// MailProbeTLS: the handshake or certificate failed, or no STARTTLS.
	MailProbeTLS = "tls"
	// MailProbeTemporary: a 4xx-class refusal the server says to retry.
	MailProbeTemporary = "temporary"
	// MailProbeProtocol: the server spoke, but not something we can sign in to.
	MailProbeProtocol = "protocol"
	// MailProbeCleartext: the unencrypted mode is not allowed for this host.
	MailProbeCleartext = "cleartext"
	// MailProbeTimeout: the budget ran out before the server answered.
	MailProbeTimeout = "timeout"
)

// EmailValidationLeg is one side (SMTP or IMAP) of a worker's verdict.
type EmailValidationLeg struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
	// Detail is the server's own reply or the dial error, never anything of
	// ours and never the password: it is shown to the person at the form.
	Detail string `json:"detail,omitempty"`
	// Port and Security are set when the worker reached the server on a port
	// other than the one asked for: a 465 that never answered, taken on 587
	// with STARTTLS. On a passing verdict the mailbox is stored with them.
	Port     int    `json:"port,omitempty"`
	Security string `json:"security,omitempty"`
}

// EmailValidationVerdict is the worker's answer to an EventWorkerEmailValidation,
// published as JSON on the process's Redis channel ahead of the legacy "1"/"0".
type EmailValidationVerdict struct {
	OK   bool               `json:"ok"`
	SMTP EmailValidationLeg `json:"smtp"`
	IMAP EmailValidationLeg `json:"imap"`
	// Error is set when the worker could not run the probes at all (it could
	// not unseal the credentials, say), in closed words with the cause logged
	// on the worker. The backend answers with a server error rather than
	// letting an untested mailbox read as a mail server that never replied.
	Error string `json:"error,omitempty"`
}

// GoogleMailHost reports whether host is one of Google's mail servers, where
// the password can only be an app password. Google prints those as four groups
// of four letters and accepts them without the spaces.
func GoogleMailHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(NormalizeMailHost(host), "."))
	for _, d := range []string{"gmail.com", "googlemail.com", "google.com"} {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}
