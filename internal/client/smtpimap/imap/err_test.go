package imap

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/warmbly/warmbly/internal/client/netbind"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestConnectionFailureReasonDoesNotExposeTransportDetails(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"dns", &net.DNSError{Err: "secret.invalid", Name: "secret.invalid"}, "DNS lookup failed"},
		{"dns_timeout", &net.DNSError{Err: "secret.invalid", Name: "secret.invalid", IsTimeout: true}, "DNS lookup timed out"},
		{"certificate", x509.UnknownAuthorityError{}, "TLS certificate validation failed"},
		{"certificate_expired", x509.CertificateInvalidError{Reason: x509.Expired}, "TLS certificate validation failed"},
		{"certificate_wrong_host", x509.HostnameError{Host: "secret.invalid"}, "TLS certificate validation failed"},
		{"worker_bind", syscall.EADDRNOTAVAIL, "worker network address unavailable"},
		{"refused", syscall.ECONNREFUSED, "connection refused"},
		{"route", syscall.ENETUNREACH, "network route unavailable"},
		{"host_unreachable", syscall.EHOSTUNREACH, "network route unavailable"},
		{"closed", io.EOF, "connection closed"},
		{"closed_socket", net.ErrClosed, "connection closed"},
		{"timeout", context.DeadlineExceeded, "connection timed out"},
		{"op_timeout", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ETIMEDOUT}, "connection timed out"},
		{"private_host", &net.OpError{Op: "dial", Net: "tcp", Err: netbind.ErrPrivateMailHost}, errx.MailConnectionReasonPrivateHost},
		{"other", errors.New("secret.invalid password=secret"), "connection failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("secret.invalid password=secret: %w", tc.err)
			got := connectionFailureReason(err)
			if got != tc.want {
				t.Fatalf("connectionFailureReason = %q, want %q", got, tc.want)
			}
			message := errx.ErrMailServerUnreachableAt(errx.MailConnectionStageIMAPDial, errors.New(got)).Message
			if !errx.IsKnownIMAPConnectionMessage(message) {
				t.Fatal("fixed producer category not recognized by incoming validator")
			}
			if strings.Contains(message, "secret") || strings.Contains(message, "secret.invalid") {
				t.Fatalf("warning disclosed connection details: %q", message)
			}
		})
	}
}

// A transport error must never read as success: mapping net.ErrClosed to nil
// is what let a dropped Gmail session run as a clean "no folders" pass every
// minute for days, with nothing logged and no mail synced.
func TestHandleErrorTransportIsNotNil(t *testing.T) {
	c := &Client{}
	for _, err := range []error{net.ErrClosed, io.EOF, io.ErrUnexpectedEOF} {
		got := c.handleError(err)
		if got == nil {
			t.Fatalf("handleError(%v) = nil, want a retryable mail error", err)
		}
		if got.Code != errx.MailErrorCodeServerUnreachable {
			t.Errorf("handleError(%v).Code = %q, want %q", err, got.Code, errx.MailErrorCodeServerUnreachable)
		}
	}
	if c.handleError(nil) != nil {
		t.Error("handleError(nil) must stay nil")
	}
}

// A "try again later" response code is not a broken mailbox. UNAVAILABLE used
// to fall through to the unknown-IMAP error, which is CRITICAL with resolve
// method RELOAD, so a few minutes of provider maintenance told every mailbox
// on that provider to reconnect.
func TestHandleErrorTransientResponseCodes(t *testing.T) {
	c := &Client{}

	for _, tc := range []struct {
		code goimap.ResponseCode
		want errx.MailErrorCode
	}{
		{goimap.ResponseCodeUnavailable, errx.MailErrorCodeServerUnreachable},
		{goimap.ResponseCodeInUse, errx.MailErrorCodeServerUnreachable},
		{goimap.ResponseCodeServerBug, errx.MailErrorCodeServerUnreachable},
		{goimap.ResponseCodeLimit, errx.MailErrorCodeSendingTooFast},
		{goimap.ResponseCodeNonExistent, errx.MailErrorCodeNotFound},
	} {
		t.Run(string(tc.code), func(t *testing.T) {
			got := c.handleError(&goimap.Error{Type: goimap.StatusResponseTypeNo, Code: tc.code, Text: "try later"})
			if got == nil {
				t.Fatalf("handleError(%s) = nil, want a mail error", tc.code)
			}
			if got.Code != tc.want {
				t.Errorf("Code = %q, want %q", got.Code, tc.want)
			}
			if got.Type == errx.MailErrorCritical {
				t.Errorf("%s is a transient refusal; Type = CRITICAL parks a mailbox error the user has to clear", tc.code)
			}
			if got.ResolveMethod != errx.MailErrorResolveMethodRetry {
				t.Errorf("ResolveMethod = %q, want %q", got.ResolveMethod, errx.MailErrorResolveMethodRetry)
			}
		})
	}
}

// A NO/BAD carries a response code only when the server chooses to send one.
// A codeless one used to render as "Something went wrong: " with nothing after
// the colon (issue #405, IONOS), which tells the customer nothing and leaves a
// bug report with no way to identify the refused command.
func TestHandleErrorCodelessImapErrorKeepsServerText(t *testing.T) {
	c := &Client{}

	for _, tc := range []struct {
		name string
		err  *goimap.Error
		want string
	}{
		{
			name: "codeless NO keeps the server's text",
			err:  &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "System Error"},
			want: "NO System Error",
		},
		{
			name: "codeless BAD is distinguishable from a NO",
			err:  &goimap.Error{Type: goimap.StatusResponseTypeBad, Text: "Command unrecognized"},
			want: "BAD Command unrecognized",
		},
		{
			name: "a response code leads and keeps the text",
			err:  &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeContactAdmin, Text: "oops"},
			want: "[CONTACTADMIN] oops",
		},
		{
			name: "an unrecognised ALERT keeps the text it must show",
			err:  &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: "Your account is locked"},
			want: "[ALERT] Your account is locked",
		},
		{
			name: "a codeless LOGIN failure is not read as a server fault",
			err:  &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "LOGIN failed."},
			want: "NO LOGIN failed.",
		},
		{
			name: "no code and no text still says something",
			err:  &goimap.Error{Type: goimap.StatusResponseTypeNo},
			want: "NO",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := c.handleError(tc.err)
			if got == nil {
				t.Fatalf("handleError(%v) = nil, want a mail error", tc.err)
			}
			if got.Code != errx.MailErrorCodeImapUnknown {
				t.Fatalf("Code = %q, want %q", got.Code, errx.MailErrorCodeImapUnknown)
			}
			if !strings.Contains(got.Message, tc.want) {
				t.Errorf("Message = %q, want it to contain %q", got.Message, tc.want)
			}
		})
	}
}

// The whole point of the fallback: the detail is never empty, so the row can
// never read as a bare "Something went wrong: " again.
func TestHandleErrorImapDetailIsNeverEmpty(t *testing.T) {
	for _, err := range []*goimap.Error{
		{},
		{Type: goimap.StatusResponseTypeNo},
		{Type: goimap.StatusResponseTypeBad, Text: "   "},
	} {
		if detail := imapErrDetail(err); strings.TrimSpace(detail) == "" {
			t.Errorf("imapErrDetail(%+v) = %q, want a non-empty detail", err, detail)
		}
	}
}

// Provider throttles back off, sign-ins the owner must finish ask for
// credentials, and a reasonless server failure retries without a report.
func TestHandleErrorProviderConditions(t *testing.T) {
	for _, tc := range []struct {
		name string
		auth models.AuthType
		err  *goimap.Error
		want errx.MailErrorCode
	}{
		{"gmail too many connections", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: "Too many simultaneous connections. (Failure)"}, errx.MailErrorCodeSendingTooFast},
		{"gmail bandwidth", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: "Account exceeded command or bandwidth limits. (Failure)"}, errx.MailErrorCodeSendingTooFast},
		{"gmail app password", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: "Application-specific password required: https://support.google.com/accounts/answer/185833 (Failure)"}, errx.MailErrorCodeInvalidCredentials},
		{"gmail web login", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: "Please log in via your web browser: https://support.google.com/mail/accounts/answer/78754 (Failure)"}, errx.MailErrorCodeInvalidCredentials},
		{"web login on oauth", models.AuthOAuth2, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: "Web login required"}, errx.MailErrorCodeAuthenticationFailed},
		{"zoho wrong region", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: zohoWrongServerAlert}, errx.MailErrorCodeInvalidCredentials},
		{"gmail invalid app password", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: "Invalid credentials (Failure)"}, errx.MailErrorCodeInvalidCredentials},
		{"gmail invalid credentials on oauth", models.AuthOAuth2, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: "Invalid credentials (Failure)"}, errx.MailErrorCodeAuthenticationFailed},
		{"TRYCREATE on a missing folder", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeTryCreate, Text: "No such mailbox."}, errx.MailErrorCodeNotFound},
		{"older dovecot codeless missing folder", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "Mailbox doesn't exist: Warmbly (0.001 + 0.000 secs)."}, errx.MailErrorCodeNotFound},
		{"codeless does not exist", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "Mailbox does not exist"}, errx.MailErrorCodeNotFound},
		{"BAD missing folder stays unknown", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeBad, Text: "Mailbox doesn't exist: Warmbly"}, errx.MailErrorCodeImapUnknown},
		{"unknown alert", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: "Maintenance tonight"}, errx.MailErrorCodeImapUnknown},
		{"expired passphrase", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeExpired, Text: "Password expired"}, errx.MailErrorCodeInvalidCredentials},
		{"ovh LIST failed", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "LIST failed"}, errx.MailErrorCodeServerUnreachable},
		{"EXAMINE failed", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "EXAMINE failed"}, errx.MailErrorCodeServerUnreachable},
		{"zoho UID FETCH failed", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "UID FETCH failed."}, errx.MailErrorCodeServerUnreachable},
		{"BAD LIST failed stays unknown", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeBad, Text: "LIST failed"}, errx.MailErrorCodeImapUnknown},
		{"LOGIN failed stays unknown", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "LOGIN failed"}, errx.MailErrorCodeImapUnknown},
		{"LIST with a reason stays unknown", models.AuthPlain, &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "LIST failed: permission denied"}, errx.MailErrorCodeImapUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{AuthType: tc.auth}
			got := c.handleError(tc.err)
			if got == nil {
				t.Fatal("handleError returned nil for a refusal")
			}
			if got.Code != tc.want {
				t.Errorf("Code = %q, want %q", got.Code, tc.want)
			}
		})
	}
}

const zohoWrongServerAlert = "The server details you are using seems incorrect. For more details, check this announcement: https://help.zoho.com/portal/en/community/topic/zoho-mail-server-details"

// Zoho's wrong-region alert is the owner's to fix, so it must reach them in the server's words with the fix named.
func TestHandleErrorZohoWrongServerKeepsAlert(t *testing.T) {
	got := (&Client{AuthType: models.AuthPlain}).handleError(&goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlert, Text: zohoWrongServerAlert})
	if got == nil || got.Code != errx.MailErrorCodeInvalidCredentials {
		t.Fatalf("handleError = %+v, want %s", got, errx.MailErrorCodeInvalidCredentials)
	}
	if !strings.Contains(got.Message, "[ALERT] "+zohoWrongServerAlert) {
		t.Errorf("Message = %q, want the server's alert verbatim", got.Message)
	}
	if info := got.GetUserErrorInfo(); info.Title != "Wrong mail server" || !strings.Contains(info.ActionRequired, "Update credentials") {
		t.Errorf("GetUserErrorInfo = %+v, want the wrong-server title and the Update credentials fix", info)
	}
}

// A sign-in refusal worded like a missing mailbox stays unknown: no folder is selected at login.
func TestLoginErrorKeepsMissingMailboxTextOutOfFolderClasses(t *testing.T) {
	c := &Client{AuthType: models.AuthPlain}
	reply := &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "Mailbox doesn't exist: user@example.test"}
	if got := c.loginError(reply); got.Code != errx.MailErrorCodeImapUnknown {
		t.Errorf("login reply = %q, want %q", got.Code, errx.MailErrorCodeImapUnknown)
	}
	if got := c.handleError(reply); got.Code != errx.MailErrorCodeNotFound {
		t.Errorf("folder reply = %q, want %q", got.Code, errx.MailErrorCodeNotFound)
	}
	throttled := &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "Mailbox doesn't exist or too many login attempts"}
	if got := c.loginError(throttled); got.Code == errx.MailErrorCodeImapUnknown || got.Code == errx.MailErrorCodeNotFound {
		t.Errorf("throttled login = %q, want the login cooldown", got.Code)
	}
	refused := &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAuthenticationFailed, Text: "Authentication failed."}
	if got := c.loginError(refused); got.Code != errx.MailErrorCodeInvalidCredentials {
		t.Errorf("credential refusal = %q, want %q", got.Code, errx.MailErrorCodeInvalidCredentials)
	}
}
