package imap

import (
	"errors"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (c *Client) handleError(err error) *errx.MailError {
	if errors.Is(err, errSyncViewMoved) {
		// Handled like a folder gone mid-walk: the next pass re-baselines it.
		return errx.ErrMailResourceNotFound
	}
	var imapErr *imap.Error
	if errors.As(err, &imapErr) {
		switch imapErr.Code {
		case imap.ResponseCodeAuthenticationFailed, imap.ResponseCodeExpired:
			// RFC 5530 EXPIRED: the owner has to supply a new passphrase.
			return c.credentialsRefused()
		case imap.ResponseCodeAuthorizationFailed:
			return errx.ErrMailAuthorizationFailed
		case imap.ResponseCodeUnavailable, imap.ResponseCodeInUse, imap.ResponseCodeServerBug:
			// RFC 5530's "try again later" pair: the server is up and the
			// credentials are fine, it just refused this command for a few
			// minutes. Falling through to the unknown-IMAP error told every
			// mailbox on the provider to reconnect (resolve method RELOAD) over
			// a blip the next pass clears on its own. SERVERBUG is the server's own fault.
			return errx.ErrMailServerUnreachable
		case imap.ResponseCodeLimit:
			// A server limit (connections, commands per window) is a throttle to back off from.
			return errx.ErrMailSendingTooFast
		case imap.ResponseCodeNonExistent:
			// The folder or message asked for is gone, which the folder walk
			// recovers from by re-listing. Not a reason to reconnect a mailbox.
			return errx.ErrMailResourceNotFound
		case imap.ResponseCodeAlert:
			if mailErr := c.classifyAlert(imapErr.Text); mailErr != nil {
				return mailErr
			}
			return errx.ErrMailUnknownImapError(imapErrDetail(imapErr))
		case "":
			if imapErr.Type == imap.StatusResponseTypeNo && bareCommandFailure(imapErr.Text) {
				return errx.ErrMailServerUnreachable
			}
			return errx.ErrMailUnknownImapError(imapErrDetail(imapErr))
		default:
			return errx.ErrMailUnknownImapError(imapErrDetail(imapErr))
		}
	}

	if err == nil {
		return nil
	}

	if unreadableReply(err) {
		c.logUnreadable(err)
	}

	// Anything that is not a tagged IMAP response is the transport: a server
	// that dropped the session (net.ErrClosed once go-imap parks the client in
	// Logout), an EOF, a timeout. These used to map to nil, which turned a dead
	// connection into a "clean pass with no folders" — no log, no error record,
	// no new mail, forever. Retry-level, so the loop reconnects at the next
	// pass instead of deactivating the mailbox.
	return errx.ErrMailServerUnreachable
}

// unreadableReply is a server answer go-imap could not decode, which also
// closes the session. It is a dialect we do not read yet, not a dead network:
// an io failure keeps its own error and never reaches the grammar check.
func unreadableReply(err error) bool {
	var imapErr *imap.Error
	if err == nil || errors.As(err, &imapErr) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "imapwire: ") ||
		strings.Contains(msg, "in search-") ||
		strings.Contains(msg, "panic reading response")
}

// logUnreadable names the reply once per distinct message: it reads as
// SERVER_UNREACHABLE everywhere else, and this line is the only clue.
func (c *Client) logUnreadable(err error) {
	msg := err.Error()
	if prev := c.lastUnreadable.Swap(&msg); prev != nil && *prev == msg {
		return
	}
	log.Warn().Str("host", c.host()).Str("reply", msg).Msg("imap: server sent a reply that could not be read; the session was dropped")
}

func (c *Client) credentialsRefused() *errx.MailError {
	if c.AuthType == models.AuthOAuth2 {
		return errx.ErrMailAuthenticationFailed
	}
	return errx.ErrMailInvalidCredentials
}

// Gmail's [ALERT] texts for a throttle and for a sign-in the owner must complete.
var (
	alertThrottles = []string{
		"too many simultaneous connections",
		"exceeded command or bandwidth limits",
	}
	alertLoginRequired = []string{
		"application-specific password required",
		"log in via your web browser",
		"web login required",
	}
)

// classifyAlert returns nil for an [ALERT] text it does not recognise.
func (c *Client) classifyAlert(text string) *errx.MailError {
	lower := strings.ToLower(text)
	for _, phrase := range alertThrottles {
		if strings.Contains(lower, phrase) {
			return errx.ErrMailSendingTooFast
		}
	}
	for _, phrase := range alertLoginRequired {
		if strings.Contains(lower, phrase) {
			return c.credentialsRefused()
		}
	}
	return nil
}

// bareCommandFailure is a reasonless "LIST failed" / "UID FETCH failed" (OVH, Zoho); LOGIN stays out.
func bareCommandFailure(text string) bool {
	cmd, ok := strings.CutSuffix(strings.TrimSuffix(strings.TrimSpace(text), "."), " failed")
	if !ok {
		return false
	}
	_, ok = bareFailureCommands[strings.TrimPrefix(cmd, "UID ")]
	return ok
}

var bareFailureCommands = map[string]struct{}{
	"LIST": {}, "LSUB": {}, "STATUS": {}, "SELECT": {}, "EXAMINE": {},
	"FETCH": {}, "SEARCH": {},
}

// imapErrDetail is the part of the mailbox's error row that says what the
// server actually refused. The response code is optional in IMAP, and a
// codeless NO/BAD (IONOS, Gmail's "NO System Error") rendered as "Something
// went wrong: " with nothing after the colon, which is unactionable for the
// customer and undiagnosable from a bug report. Never returns "".
func imapErrDetail(err *imap.Error) string {
	if err.Code != "" {
		// Keep the text: an [ALERT] must be shown to a person (RFC 3501).
		if text := strings.TrimSpace(err.Text); text != "" {
			return fmt.Sprintf("[%s] %s", err.Code, text)
		}
		return string(err.Code)
	}
	// Keep the status: a BAD means we sent something the server does not
	// understand, a NO means it understood and declined.
	if detail := strings.TrimSpace(fmt.Sprintf("%s %s", err.Type, err.Text)); detail != "" {
		return detail
	}
	return "the mail server refused the command without saying why"
}
