package imap

import (
	"io"
	"net"
	"strings"
	"testing"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/warmbly/warmbly/internal/errx"
)

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
			name: "a response code still wins over the text",
			err:  &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeServerBug, Text: "oops"},
			want: "SERVERBUG",
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
