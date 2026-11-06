package errx

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type MailErrorLogType string

const (
	MailErrorLogTypeExternal MailErrorLogType = "EXTERNAL"
	MailErrorLogTypeInternal MailErrorLogType = "INTERNAL"
)

func (l MailErrorLogType) IsExternal() bool {
	return l == MailErrorLogTypeExternal
}

type MailErrorType string

const (
	MailErrorCritical      MailErrorType = "CRITICAL"
	MailErrorWarning       MailErrorType = "WARNING"
	MailErrorInformational MailErrorType = "INFORMATIONAL"
)

type MailErrorCode string

const (
	MailErrorCodeFolderLimit     MailErrorCode = "MAX_FOLDERS_REACHED"
	MailErrorCodeUpdateLimit     MailErrorCode = "MAX_FOLDERS_REACHED"
	MailErrorCodeGoogleAuth      MailErrorCode = "GOOGLE_AUTHENTICATION_FAILED"
	MailErrorCodeGooglePayment   MailErrorCode = "GOOGLE_PAYMENT_REQUIRED"
	MailErrorCodeGoogleForbidden MailErrorCode = "GOOGLE_FORBIDDEN"

	MailErrorCodeServerUnreachable MailErrorCode = "SERVER_UNREACHABLE"
	// MailErrorCodeNotFound is the provider saying the addressed resource is
	// absent, kept apart from SERVER_UNREACHABLE so "this folder does not
	// exist" is never inferred from a catch-all that also covers a 503.
	MailErrorCodeNotFound             MailErrorCode = "RESOURCE_NOT_FOUND"
	MailErrorCodeUnsupported          MailErrorCode = "UNSUPPORTED"
	MailErrorCodeInvalidCredentials   MailErrorCode = "INVALID_CREDENTIALS"   // e.g. invalid username or password
	MailErrorCodeAuthorizationFailed  MailErrorCode = "AUTHORIZATION_FAILED"  // e.g. imap disabled
	MailErrorCodeAuthenticationFailed MailErrorCode = "AUTHENTICATION_FAILED" // e.g. invalid token
	MailErrorCodeConnectionLost       MailErrorCode = "CONNECTION_LOST"
	MailErrorCodeImapUnknown          MailErrorCode = "IMAP_UNKNOWN"

	// Rate limiting and abuse detection
	MailErrorCodeRateLimitExceeded MailErrorCode = "RATE_LIMIT_EXCEEDED"
	// Sync fair use (internal/app/worker/wmail/governor.go). SYNC_FLOOD is
	// an hourly volume no real mailbox produces; SYNC_FAIR_USE is repeated
	// daily overage. Both deactivate the mailbox until someone reactivates it.
	MailErrorCodeSyncFlood         MailErrorCode = "SYNC_FLOOD"
	MailErrorCodeSyncFairUse       MailErrorCode = "SYNC_FAIR_USE"
	MailErrorCodeSendingTooFast    MailErrorCode = "SENDING_TOO_FAST"
	MailErrorCodeRecipientRejected MailErrorCode = "RECIPIENT_REJECTED"
	// MailErrorCodeSendRejected is the receiving server refusing the message
	// or the sender for good (a 5xx on MAIL FROM or at the end of DATA).
	// Distinct from RECIPIENT_REJECTED, which is one address, and from
	// SERVER_UNREACHABLE, which is worth retrying.
	MailErrorCodeSendRejected MailErrorCode = "SEND_REJECTED"
	// MailErrorCodeAuthUnsupported is a server whose advertised
	// authentication mechanisms we do not implement.
	MailErrorCodeAuthUnsupported MailErrorCode = "AUTH_UNSUPPORTED"
	// MailErrorCodeInsecureRemoteHost is the unencrypted mailbox mode aimed
	// at something that is not this machine. Kept apart from a credentials
	// or reachability failure because the server is fine and the password is
	// fine: the connection is one we refuse to make.
	MailErrorCodeInsecureRemoteHost MailErrorCode = "INSECURE_REMOTE_HOST"
	// MailErrorCodeDomainAuthRejected is the receiving side refusing the mail
	// because the SENDING DOMAIN failed its authentication bar (Outlook's
	// 5.7.515, Gmail's 5.7.26). Not a dead server and not a bad recipient:
	// retrying from the same domain fails identically until DNS is fixed.
	MailErrorCodeDomainAuthRejected MailErrorCode = "DOMAIN_AUTH_REJECTED"
	MailErrorCodeQuotaExceeded      MailErrorCode = "QUOTA_EXCEEDED"
	MailErrorCodeAccountSuspended   MailErrorCode = "ACCOUNT_SUSPENDED"
	// MailErrorCodeTLSCertificateChanged is a server trusted on its pinned
	// certificate presenting a different one. Not a credentials failure: the
	// owner re-confirms the certificate in the mailbox's settings.
	MailErrorCodeTLSCertificateChanged MailErrorCode = "TLS_CERTIFICATE_CHANGED"
	// MailErrorCodeTLSCertificateUnconfirmed is a server the owner chose to
	// trust without a certificate authority whose certificate was never
	// confirmed, so no connection is made until it is.
	MailErrorCodeTLSCertificateUnconfirmed MailErrorCode = "TLS_CERTIFICATE_UNCONFIRMED"
)

// TLSCertificateMailErrorCodes are resolved by confirming the server's
// certificate in the mailbox's settings.
var TLSCertificateMailErrorCodes = []MailErrorCode{
	MailErrorCodeTLSCertificateChanged,
	MailErrorCodeTLSCertificateUnconfirmed,
}

// CredentialMailErrorCodes are the credential-class errors a successful
// mailbox re-authorization or SMTP/IMAP credential update fixes.
var CredentialMailErrorCodes = []MailErrorCode{
	MailErrorCodeGoogleAuth,
	MailErrorCodeAuthenticationFailed,
	MailErrorCodeAuthorizationFailed,
	MailErrorCodeInvalidCredentials,
}

var MailErrorCodeGoogleUnknown = func(code int) MailErrorCode {
	return MailErrorCode(fmt.Sprintf("Unknown (%d)", code))
}

type MailErrorResolveMethod string

const (
	MailErrorResolveMethodNone   MailErrorResolveMethod = ""
	MailErrorResolveMethodAuth   MailErrorResolveMethod = "OAUTH"
	MailErrorResolveMethodRetry  MailErrorResolveMethod = "RETRY"
	MailErrorResolveMethodReload MailErrorResolveMethod = "RELOAD"
)

type MailError struct {
	ID string `json:"id"`

	Type          MailErrorType          `json:"type"`
	Code          MailErrorCode          `json:"code"`
	ResolveMethod MailErrorResolveMethod `json:"resolve_method"`
	ResolvedAt    *time.Time             `json:"resolved_at"`

	Message string `json:"message"`
	// Recipient is the one address a per-recipient refusal was about.
	Recipient string `json:"recipient,omitempty"`

	// RetryAfter is provider guidance for transient throttles. It stays local
	// to the worker; persisted error records should not depend on a stale delay.
	RetryAfter time.Duration `json:"-"`
	Failure    *SendFailure  `json:"-"`

	CreatedAt time.Time `json:"created_at"`
}

// Error names the failure, not the error object.
//
// It used to read "Email (<uuid>): ...", where the uuid was this struct's own
// ID and not a mailbox at all. Every other "Email (%s)" in the codebase is a
// mailbox id, so the message invited exactly one reading and it was wrong; the
// sentinels are package-level, so their ID is minted once at init and looks
// reassuringly stable while naming nothing. An hour went into looking up a
// mailbox that had never existed. The code is what a reader actually wants,
// and it groups these usefully in error tracking. The ID stays on the struct
// for anyone who needs it.
func (e *MailError) Error() string {
	return fmt.Sprintf("mail %s: %s", e.Code, e.Message)
}

func (e *MailError) Unwrap() error {
	return fmt.Errorf("mail %s: %s", e.Code, e.Message)
}

func MError(eType MailErrorType, code MailErrorCode, message string, resolveMethod MailErrorResolveMethod) *MailError {
	return &MailError{
		ID:            uuid.NewString(),
		Type:          eType,
		Code:          code,
		Message:       message,
		ResolveMethod: resolveMethod,
	}
}

const (
	MailConnectionReasonDNSFailure    = "DNS lookup failed"
	MailConnectionReasonDNSTimeout    = "DNS lookup timed out"
	MailConnectionReasonCertificate   = "TLS certificate validation failed"
	MailConnectionReasonWorkerAddress = "worker network address unavailable"
	MailConnectionReasonRefused       = "connection refused"
	MailConnectionReasonRoute         = "network route unavailable"
	MailConnectionReasonClosed        = "connection closed"
	MailConnectionReasonTimeout       = "connection timed out"
	MailConnectionReasonFailed        = "connection failed"
	MailConnectionReasonPrivateHost   = "private network address"
	MailConnectionReasonNoSTARTTLS    = "no STARTTLS offered"
	MailConnectionStageIMAPDial       = "IMAP dial"
	MailConnectionStageIMAPTLS        = "IMAP TLS"
	MailConnectionStageIMAPSTARTTLS   = "IMAP STARTTLS"
	mailServerUnreachablePrefix       = "The connection to the mail server could not be established (%s: %s). "
	mailServerUnreachableHint         = "The server may be offline or blocking the connection."
	// MailPrivateHostHint is what a mailbox owner reads when its server resolves to a private address.
	MailPrivateHostHint = "The server's host name points to a private network address, and Warmbly only connects mailboxes to public addresses. Use the host name your provider gives for connecting from outside your network."
	// MailCertificateHint is what a mailbox owner reads when its server's certificate did not verify.
	MailCertificateHint = "The server's certificate is not one a recognised certificate authority vouches for, has expired, or is for another host name. Use the host name the certificate is issued for. For a server with its own certificate, choose Update credentials on the mailbox and trust the certificate when the test offers it."
	// MailCleartextAuthMessage is the refusal to sign in over a link that is not encrypted.
	MailCleartextAuthMessage = "This mail server only offers sign-in over an unencrypted connection, and Warmbly never sends a mailbox password unencrypted. Use the server's SSL/TLS port (usually 465) or a port that offers STARTTLS (usually 587)."
	// MailServerThrottlingMessage replaces the generic connection text when the server limited this worker's address.
	MailServerThrottlingMessage = "The mail server temporarily refused new connections or sign-ins from Warmbly's sending address, which providers do when many mailboxes they host connect from the same address. Warmbly now spaces its connections to this server and retries on its own; nothing needs to change on the mailbox."
	// MailServerPacedMessage is a send held back without contacting a server that is limiting Warmbly's address.
	MailServerPacedMessage = "Not sent yet: this mailbox's mail server is limiting connections from Warmbly's sending address, so Warmbly is holding the send briefly instead of adding to them. It is retried automatically."
	// MailWrongServerHint opens the message for a provider saying the account lives on another of its servers.
	MailWrongServerHint = "The mail server refused the sign-in because this mailbox's account is hosted on a different server of the same provider."
	// MailNoSTARTTLSHint is what a mailbox owner reads when the port offers no encryption.
	MailNoSTARTTLSHint = "The server offers no STARTTLS on this port, and Warmbly only signs in over an encrypted connection. Use the server's SSL/TLS port (usually 465) or a port that offers STARTTLS (usually 587)."
)

// connectionHints are the reasons with something more specific to say than the default.
var connectionHints = map[string]string{
	MailConnectionReasonPrivateHost: MailPrivateHostHint,
	MailConnectionReasonCertificate: MailCertificateHint,
	MailConnectionReasonNoSTARTTLS:  MailNoSTARTTLSHint,
}

// mailServerUnreachableMessage is the stored message for a connection failure at stage.
func mailServerUnreachableMessage(stage, reason string) string {
	hint, ok := connectionHints[reason]
	if !ok {
		hint = mailServerUnreachableHint
	}
	return fmt.Sprintf(mailServerUnreachablePrefix, stage, reason) + hint
}

// IsKnownIMAPConnectionMessage accepts each fixed IMAP connection message,
// in its current wording and the generic one earlier workers still send.
func IsKnownIMAPConnectionMessage(message string) bool {
	for _, stage := range [...]string{MailConnectionStageIMAPDial, MailConnectionStageIMAPTLS, MailConnectionStageIMAPSTARTTLS} {
		for _, reason := range [...]string{MailConnectionReasonDNSFailure, MailConnectionReasonDNSTimeout, MailConnectionReasonCertificate,
			MailConnectionReasonWorkerAddress, MailConnectionReasonRefused, MailConnectionReasonRoute, MailConnectionReasonClosed,
			MailConnectionReasonTimeout, MailConnectionReasonFailed, MailConnectionReasonPrivateHost} {
			if message == mailServerUnreachableMessage(stage, reason) ||
				message == fmt.Sprintf(mailServerUnreachablePrefix, stage, reason)+mailServerUnreachableHint {
				return true
			}
		}
	}
	return false
}

var (
	ErrMailUpdateLimit     = MError(MailErrorCritical, MailErrorCodeUpdateLimit, "Your inbox has received an unusually large number of updates. Please reactivate your inbox once the issue is resolved.", MailErrorResolveMethodReload)
	ErrMailGoogleAuth      = MError(MailErrorCritical, MailErrorCodeGoogleAuth, "Cannot access your Gmail account. Please re-authorize your account to restore mailbox access.", MailErrorResolveMethodReload)
	ErrMailGooglePayment   = MError(MailErrorCritical, MailErrorCodeGooglePayment, "Gmail access blocked due to unpaid invoices. Please resolve the payment with Google.", MailErrorResolveMethodReload)
	ErrMailGoogleForbidden = func(message string) *MailError {
		return MError(MailErrorWarning, MailErrorCodeGoogleForbidden, fmt.Sprintf("Gmail access blocked: %s", message), MailErrorResolveMethodReload)
	}
	ErrMailGoogleUnknown = func(code int, message string) *MailError {
		return MError(MailErrorWarning, MailErrorCodeGoogleUnknown(code), message, MailErrorResolveMethodRetry)
	}
	ErrMailServerUnreachable = MError(MailErrorWarning, MailErrorCodeServerUnreachable, "The connection to the mail server could not be established. The server may be offline or blocking the connection.", MailErrorResolveMethodRetry)
	// ErrMailServerUnreachableAt preserves the stable code while exposing the failed stage.
	ErrMailServerUnreachableAt = func(stage string, cause error) *MailError {
		if stage == "" || cause == nil {
			return ErrMailServerUnreachable
		}
		return MError(
			MailErrorWarning,
			MailErrorCodeServerUnreachable,
			mailServerUnreachableMessage(stage, cause.Error()),
			MailErrorResolveMethodRetry,
		)
	}
	// ErrMailSMTPSEgressBlocked keeps the retryable code but says the block is
	// the worker's network, so the customer is not sent to debug a healthy server.
	ErrMailSMTPSEgressBlocked = func(addr string) *MailError {
		return MError(
			MailErrorWarning,
			MailErrorCodeServerUnreachable,
			fmt.Sprintf("Warmbly's sending network currently blocks outbound port 465, so it could not reach %s, and the server did not answer on port 587 either. The block is on the sending side, not on your mail server. Sending resumes on its own once the port is open; if your server can also accept submission on port 587 with STARTTLS, turning that on gets this mailbox sending now.", addr),
			MailErrorResolveMethodRetry,
		)
	}
	ErrMailResourceNotFound      = MError(MailErrorWarning, MailErrorCodeNotFound, "The mail server does not have the folder or message that was requested.", MailErrorResolveMethodRetry)
	ErrMailCondStoreNotSupported = MError(MailErrorCritical, MailErrorCodeUnsupported, "The mail server does not support the required CONDSTORE extension. Synchronization cannot continue.", MailErrorResolveMethodReload)
	ErrMailInvalidCredentials    = MError(
		MailErrorCritical,
		MailErrorCodeInvalidCredentials,
		"The email address or password is incorrect. Please check your credentials and try again.",
		MailErrorResolveMethodReload,
	)
	// ErrMailWrongServer is credentials-class because Update credentials is where the host is fixed; detail is the server's own words.
	ErrMailWrongServer = func(detail string) *MailError {
		return MError(MailErrorCritical, MailErrorCodeInvalidCredentials, fmt.Sprintf("%s The server said: %s", MailWrongServerHint, detail), MailErrorResolveMethodReload)
	}
	ErrMailAuthenticationFailed = MError(
		MailErrorCritical,
		MailErrorCodeAuthenticationFailed,
		"Authentication failed. This often happens with OAuth2 providers (like Google for Gmail or Microsoft for Outlook). Possible causes: invalid or expired token, wrong permissions/scope, or two-factor authentication requiring an app password. Please re-authenticate or check your account security settings.",
		MailErrorResolveMethodAuth,
	)
	ErrMailAuthorizationFailed = MError(
		MailErrorCritical,
		MailErrorCodeAuthorizationFailed,
		"This account lacks permission to access certain required resources.",
		MailErrorResolveMethodReload,
	)
	ErrMailUnknownImapError = func(errStatus string) *MailError {
		return MError(
			MailErrorCritical,
			MailErrorCodeImapUnknown,
			fmt.Sprintf("Something went wrong: %s", errStatus),
			MailErrorResolveMethodReload,
		)
	}

	// Rate limiting and abuse errors
	ErrMailRateLimitExceeded = MError(
		MailErrorWarning,
		MailErrorCodeRateLimitExceeded,
		"This email account has exceeded the sync rate limit. This may indicate suspicious activity.",
		MailErrorResolveMethodNone,
	)
	ErrMailSyncFlood = MError(
		MailErrorWarning,
		MailErrorCodeSyncFlood,
		"This mailbox received far more new mail in one hour than sync fair use allows, so syncing was stopped to protect the platform.",
		MailErrorResolveMethodNone,
	)
	ErrMailSyncFairUse = MError(
		MailErrorWarning,
		MailErrorCodeSyncFairUse,
		"This mailbox exceeded its daily sync budget on several recent days, so syncing was stopped. Reactivate it once the volume is back to normal.",
		MailErrorResolveMethodNone,
	)
	ErrMailSendingTooFast = MError(
		MailErrorWarning,
		MailErrorCodeSendingTooFast,
		"Emails are being sent too quickly. Please wait before sending more emails.",
		MailErrorResolveMethodRetry,
	)
	// ErrMailRecipientRejected carries the server's own refusal, because
	// "the address was rejected" alone leaves the user nothing to act on: a
	// mailbox that no longer exists and one blocked by a policy read
	// identically without it.
	ErrMailRecipientRejected = func(detail string) *MailError {
		if detail == "" {
			return MError(MailErrorWarning, MailErrorCodeRecipientRejected, "The recipient email address was rejected by the mail server.", MailErrorResolveMethodNone)
		}
		return MError(MailErrorWarning, MailErrorCodeRecipientRejected, fmt.Sprintf("The mail server rejected the recipient: %s", detail), MailErrorResolveMethodNone)
	}
	// ErrMailSendRejected is a permanent refusal of the message itself. Not
	// retried: a 5xx means the server will answer the same way next time, so
	// another attempt only spends the mailbox's daily budget.
	ErrMailSendRejected = func(detail string) *MailError {
		return MError(MailErrorWarning, MailErrorCodeSendRejected, fmt.Sprintf("The receiving mail server refused this message: %s", detail), MailErrorResolveMethodNone)
	}
	// ErrMailInsecureRemoteHost is the dial-time half of the loopback rule
	// for the unencrypted mailbox mode. The connect form enforces it too,
	// but the worker is the thing holding the socket, so it decides: an
	// instance-local edit to the stored host cannot talk a worker into
	// putting a password on a wire.
	ErrMailInsecureRemoteHost = MError(
		MailErrorCritical,
		MailErrorCodeInsecureRemoteHost,
		"This mailbox is set to connect without encryption, which Warmbly only does to a mail server on the same machine as the worker. Point it at localhost or choose SSL/TLS or STARTTLS.",
		MailErrorResolveMethodReload,
	)
	// ErrMailCleartextAuth is our own refusal to put a password on an
	// unencrypted wire, raised before anything is sent. Not retryable: no
	// number of attempts encrypts the link, and reporting it as an outage
	// sent the operator looking at a server that is answering fine.
	ErrMailCleartextAuth = MError(
		MailErrorCritical,
		MailErrorCodeAuthUnsupported,
		MailCleartextAuthMessage,
		MailErrorResolveMethodReload,
	)
	ErrMailAuthUnsupported = MError(
		MailErrorCritical,
		MailErrorCodeAuthUnsupported,
		"This mail server offers no sign-in method allowed by this instance. Use the provider's documented SMTP submission host with LOGIN or PLAIN over TLS, or check the server's documentation for an app password.",
		MailErrorResolveMethodReload,
	)
	ErrMailDomainAuthRejected = MError(
		MailErrorCritical,
		MailErrorCodeDomainAuthRejected,
		"The receiving mail server refused this message because your sending domain failed its authentication checks. Fix the domain's SPF, DKIM and DMARC records.",
		MailErrorResolveMethodNone,
	)
	ErrMailQuotaExceeded = MError(
		MailErrorCritical,
		MailErrorCodeQuotaExceeded,
		"Your email sending quota has been exceeded. Please try again later.",
		MailErrorResolveMethodRetry,
	)
	ErrMailTLSCertificateChanged = func(server string) *MailError {
		return MError(
			MailErrorWarning,
			MailErrorCodeTLSCertificateChanged,
			fmt.Sprintf("The %s server's certificate changed since it was confirmed for this mailbox, so Warmbly did not connect. If the server's owner replaced it, re-confirm the certificate under Settings > Server certificates on the mailbox.", server),
			MailErrorResolveMethodReload,
		)
	}
	ErrMailTLSCertificateUnconfirmed = func(server string) *MailError {
		return MError(
			MailErrorWarning,
			MailErrorCodeTLSCertificateUnconfirmed,
			fmt.Sprintf("This mailbox trusts its %s server without a certificate authority, but the server's certificate has not been confirmed yet, so Warmbly did not connect. Confirm it under Settings > Server certificates on the mailbox.", server),
			MailErrorResolveMethodReload,
		)
	}
	ErrMailAccountSuspended = MError(
		MailErrorCritical,
		MailErrorCodeAccountSuspended,
		"This email account has been suspended. Please contact your email provider.",
		MailErrorResolveMethodReload,
	)
)

// UserErrorInfo contains user-visible error information
type UserErrorInfo struct {
	Title          string
	Message        string
	ActionRequired string
}

// GetUserErrorInfo returns user-friendly error information for display
func (e *MailError) GetUserErrorInfo() UserErrorInfo {
	info := UserErrorInfo{
		Title:   "Email Error",
		Message: e.Message,
	}

	switch e.Code {
	case MailErrorCodeGoogleAuth, MailErrorCodeAuthenticationFailed:
		info.Title = "Authentication Required"
		info.ActionRequired = "Please re-authorize your email account"
	case MailErrorCodeInvalidCredentials:
		info.Title = "Invalid Credentials"
		info.ActionRequired = "Please update your email credentials"
		if strings.HasPrefix(e.Message, MailWrongServerHint) {
			info.Title = "Wrong mail server"
			info.ActionRequired = "Choose Update credentials on the mailbox and enter the IMAP and SMTP hosts for the data centre your account is in"
		}
	case MailErrorCodeServerUnreachable:
		info.Title = "Connection Error"
		info.ActionRequired = "The email server is temporarily unavailable. We'll retry automatically."
		switch {
		case strings.Contains(e.Message, MailPrivateHostHint):
			info.Title = "Server address is not public"
			info.ActionRequired = "Choose Update credentials on the mailbox and enter the server's public host name"
		case strings.Contains(e.Message, MailCertificateHint):
			info.Title = "Server certificate not verified"
			info.ActionRequired = "Choose Update credentials on the mailbox to correct the host name or trust the server's own certificate"
		case strings.Contains(e.Message, MailNoSTARTTLSHint):
			info.Title = "No encrypted connection offered"
			info.ActionRequired = "Choose Update credentials on the mailbox and use the server's SSL/TLS port or a port that offers STARTTLS"
		}
	case MailErrorCodeNotFound:
		info.Title = "Mailbox Item Missing"
		info.ActionRequired = "The folder or message is no longer on the mail server. Nothing to do; we'll skip it."
	case MailErrorCodeRateLimitExceeded:
		info.Title = "Rate Limit Exceeded"
		info.ActionRequired = "Your account has been temporarily limited due to unusual activity"
	case MailErrorCodeSyncFlood:
		info.Title = "Sync stopped: unusual volume"
		info.ActionRequired = "Check what is delivering mail into this mailbox, then reactivate it under Mailboxes"
	case MailErrorCodeSyncFairUse:
		info.Title = "Sync stopped: fair use"
		info.ActionRequired = "Reduce the volume landing in this mailbox or ask your administrator to raise the sync budget, then reactivate it"
	case MailErrorCodeSendingTooFast:
		info.Title = "Sending Too Fast"
		info.ActionRequired = "Please wait before sending more emails"
	case MailErrorCodeQuotaExceeded:
		info.Title = "Quota Exceeded"
		info.ActionRequired = "Your daily sending limit has been reached"
	case MailErrorCodeAccountSuspended:
		info.Title = "Account Suspended"
		info.ActionRequired = "Contact your email provider to resolve this issue"
	case MailErrorCodeSendRejected:
		info.Title = "Message refused"
		info.ActionRequired = "The receiving server rejected this message outright. The reason it gave is in the message above."
	case MailErrorCodeAuthUnsupported:
		info.Title = "Sign-in method not supported"
		info.ActionRequired = "This server asks for an authentication method Warmbly does not support. An app password, or the provider's documented SMTP host, usually works."
		if strings.HasPrefix(e.Message, MailCleartextAuthMessage) {
			info.Title = "Encrypted sign-in not offered"
			info.ActionRequired = "Choose Update credentials on the mailbox and use the server's SSL/TLS port or a port that offers STARTTLS"
		}
	case MailErrorCodeRecipientRejected:
		info.Title = "Recipient Rejected"
		info.ActionRequired = "The recipient address was not accepted"
	case MailErrorCodeTLSCertificateChanged:
		info.Title = "Server certificate changed"
		info.ActionRequired = "Check with the server's owner, then re-confirm the certificate under Settings > Server certificates on the mailbox"
	case MailErrorCodeTLSCertificateUnconfirmed:
		info.Title = "Server certificate not confirmed"
		info.ActionRequired = "Confirm the server's certificate under Settings > Server certificates on the mailbox"
	}

	return info
}

// IsUserVisible returns true if this error should be shown to users
func (e *MailError) IsUserVisible() bool {
	return e.Type == MailErrorCritical || e.Type == MailErrorWarning
}
