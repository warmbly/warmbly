package imap

import (
	"errors"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (c *Client) handleError(err error) *errx.MailError {
	var imapErr *imap.Error
	if errors.As(err, &imapErr) {
		switch imapErr.Code {
		case imap.ResponseCodeAuthenticationFailed:
			if c.AuthType == models.AuthOAuth2 {
				return errx.ErrMailAuthenticationFailed
			} else {
				return errx.ErrMailInvalidCredentials
			}
		case imap.ResponseCodeAuthorizationFailed:
			return errx.ErrMailAuthorizationFailed
		case imap.ResponseCodeUnavailable, imap.ResponseCodeInUse:
			// RFC 5530's "try again later" pair: the server is up and the
			// credentials are fine, it just refused this command for a few
			// minutes. Falling through to the unknown-IMAP error told every
			// mailbox on the provider to reconnect (resolve method RELOAD) over
			// a blip the next pass clears on its own.
			return errx.ErrMailServerUnreachable
		case imap.ResponseCodeNonExistent:
			// The folder or message asked for is gone, which the folder walk
			// recovers from by re-listing. Not a reason to reconnect a mailbox.
			return errx.ErrMailResourceNotFound
		default:
			return errx.ErrMailUnknownImapError(imapErrDetail(imapErr))
		}
	}

	if err == nil {
		return nil
	}

	// Anything that is not a tagged IMAP response is the transport: a server
	// that dropped the session (net.ErrClosed once go-imap parks the client in
	// Logout), an EOF, a timeout. These used to map to nil, which turned a dead
	// connection into a "clean pass with no folders" — no log, no error record,
	// no new mail, forever. Retry-level, so the loop reconnects at the next
	// pass instead of deactivating the mailbox.
	return errx.ErrMailServerUnreachable
}

// imapErrDetail is the part of the mailbox's error row that says what the
// server actually refused. The response code is optional in IMAP, and a
// codeless NO/BAD (IONOS, Gmail's "NO System Error") rendered as "Something
// went wrong: " with nothing after the colon, which is unactionable for the
// customer and undiagnosable from a bug report. Never returns "".
func imapErrDetail(err *imap.Error) string {
	if err.Code != "" {
		return string(err.Code)
	}
	// Keep the status: a BAD means we sent something the server does not
	// understand, a NO means it understood and declined.
	if detail := strings.TrimSpace(fmt.Sprintf("%s %s", err.Type, err.Text)); detail != "" {
		return detail
	}
	return "the mail server refused the command without saying why"
}
