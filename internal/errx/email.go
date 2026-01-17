package errx

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
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

	MailErrorCodeServerUnreachable    MailErrorCode = "SERVER_UNREACHABLE"
	MailErrorCodeUnsupported          MailErrorCode = "UNSUPPORTED"
	MailErrorCodeInvalidCredentials   MailErrorCode = "INVALID_CREDENTIALS"   // e.g. invalid username or password
	MailErrorCodeAuthorizationFailed  MailErrorCode = "AUTHORIZATION_FAILED"  // e.g. imap disabled
	MailErrorCodeAuthenticationFailed MailErrorCode = "AUTHENTICATION_FAILED" // e.g. invalid token
	MailErrorCodeConnectionLost       MailErrorCode = "CONNECTION_LOST"
	MailErrorCodeImapUnknown          MailErrorCode = "IMAP_UNKNOWN"
)

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

	CreatedAt time.Time `json:"created_at"`
}

func (e *MailError) Error() string {
	return fmt.Sprintf("Email (%s): %s", e.ID, e.Message)
}

func (e *MailError) Unwrap() error {
	return fmt.Errorf("Email (%s): %s", e.ID, e.Message)
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

var (
	ErrMailFoldersMax      = MError(MailErrorCritical, MailErrorCodeFolderLimit, fmt.Sprintf("You reached the maximum limit of %d folders reached.", config.MaxEmailFolders), MailErrorResolveMethodReload)
	ErrMailUpdateLimit     = MError(MailErrorCritical, MailErrorCodeUpdateLimit, "Your inbox has received an unusually large number of updates. Please reactivate your inbox once the issue is resolved.", MailErrorResolveMethodReload)
	ErrMailGoogleAuth      = MError(MailErrorCritical, MailErrorCodeGoogleAuth, "Cannot access your Gmail account. Please re-authorize your account to restore mailbox access.", MailErrorResolveMethodReload)
	ErrMailGooglePayment   = MError(MailErrorCritical, MailErrorCodeGooglePayment, "Gmail access blocked due to unpaid invoices. Please resolve the payment with Google.", MailErrorResolveMethodReload)
	ErrMailGoogleForbidden = func(message string) *MailError {
		return MError(MailErrorWarning, MailErrorCodeGoogleForbidden, fmt.Sprintf("Gmail access blocked: %s", message), MailErrorResolveMethodReload)
	}
	ErrMailGoogleUnknown = func(code int, message string) *MailError {
		return MError(MailErrorWarning, MailErrorCodeGoogleUnknown(code), message, MailErrorResolveMethodRetry)
	}
	ErrMailServerUnreachable     = MError(MailErrorWarning, MailErrorCodeServerUnreachable, "The connection to the mail server could not be established. The server may be offline or blocking the connection.", MailErrorResolveMethodRetry)
	ErrMailCondStoreNotSupported = MError(MailErrorCritical, MailErrorCodeUnsupported, "The mail server does not support the required CONDSTORE extension. Synchronization cannot continue.", MailErrorResolveMethodReload)
	ErrMailInvalidCredentials    = MError(
		MailErrorCritical,
		MailErrorCodeInvalidCredentials,
		"The email address or password is incorrect. Please check your credentials and try again.",
		MailErrorResolveMethodReload,
	)
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
)
