package worker

import (
	"context"
	"errors"
	"strings"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// errGmailIMAPCannot is an action IMAP cannot do on this mailbox (a hidden folder); the API does it instead.
var errGmailIMAPCannot = errors.New("gmail imap: the folder this action needs is not visible over IMAP")

// errGmailIMAPNotFound is a Message-ID no searched folder holds.
var errGmailIMAPNotFound = errors.New("gmail imap: message not found")

// gmailIMAPAction is the recipient-side engagement a Google mailbox performs over IMAP.
func gmailIMAPAction(act string) bool {
	switch act {
	case models.WarmupActionRescueFromSpam, models.WarmupActionMarkRead, models.WarmupActionMarkImportant, models.WarmupActionStar:
		return true
	}
	return false
}

// unsettledEngagement reports an engagement action IMAP left for the API.
func unsettledEngagement(acts []string, settled map[string]bool) bool {
	for _, act := range acts {
		if gmailIMAPAction(act) && !settled[act] {
			return true
		}
	}
	return false
}

// gmailIMAPRefusal names why IMAP can never serve this mailbox, or "" for a failure worth retrying.
func gmailIMAPRefusal(err error) string {
	if errors.Is(err, wmail.ErrGmailIMAPScope) {
		return err.Error()
	}
	if imap.IMAPDisabled(err) {
		return "IMAP is turned off for this account or its domain"
	}
	var mailErr *errx.MailError
	if errors.As(err, &mailErr) && (mailErr.Code == errx.MailErrorCodeAuthenticationFailed || mailErr.Code == errx.MailErrorCodeInvalidCredentials) {
		return "Gmail refused the IMAP sign-in for this token"
	}
	return ""
}

// runGmailIMAPEngagement performs the leg's engagement over IMAP and returns the actions it settled; the API runs the rest.
func (w *WorkerService) runGmailIMAPEngagement(ctx context.Context, mail *wmail.WMail, action models.WarmupEmailAction, rescue *string) map[string]bool {
	g := mail.GoogleData.IMAP
	if g == nil || g.Refused() != "" || strings.TrimSpace(action.RFCMessageID) == "" {
		return nil
	}
	var acts []string
	for _, act := range action.Actions {
		if gmailIMAPAction(act) {
			acts = append(acts, act)
		}
	}
	if len(acts) == 0 {
		return nil
	}

	settled := make(map[string]bool, len(acts))
	err := g.With(ctx, func(s wmail.GmailIMAPSession) error {
		boxes, err := s.AllFolders(ctx)
		if err != nil {
			return err
		}
		e := newGmailEngagement(s, boxes, action)
		for _, act := range acts {
			err := e.do(ctx, act, rescue)
			switch {
			case err == nil:
				settled[act] = true
			case gmailIMAPRefusal(err) != "":
				return err
			case errors.Is(err, errGmailIMAPNotFound) && action.GmailID == "":
				log.Debug().Str("email_id", action.EmailID.String()).Str("action", act).Msg("Warmup message not found over IMAP and no Gmail id to fall back on")
				settled[act] = true
			case errors.Is(err, errGmailIMAPCannot), errors.Is(err, errGmailIMAPNotFound), imap.Refused(err):
				log.Debug().Err(err).Str("email_id", action.EmailID.String()).Str("action", act).Msg("Warmup action handed to the Gmail API")
			default:
				// The API gets the next try; it records the failure if it cannot do it either.
				log.Warn().Err(err).Str("email_id", action.EmailID.String()).Str("action", act).Msg("Warmup action failed over Gmail IMAP; trying the Gmail API")
			}
		}
		return nil
	})
	if err == nil {
		return settled
	}
	if reason := gmailIMAPRefusal(err); reason != "" {
		if g.Refuse(reason) {
			log.Info().Str("email_id", action.EmailID.String()).Str("reason", reason).
				Msg("Gmail warmup engagement falls back to the Gmail API for this mailbox")
		}
		return settled
	}
	// A dial or listing that failed on the network leaves the rest to the API.
	log.Warn().Err(err).Str("email_id", action.EmailID.String()).Strs("actions", acts).Msg("Warmup engagement failed over Gmail IMAP; trying the Gmail API")
	return settled
}

// gmailEngagement is one leg's view of a Gmail mailbox over IMAP.
type gmailEngagement struct {
	s                             wmail.GmailIMAPSession
	id                            string
	junk, all, important          string
	placement, folder, filingDest string
	// at is where the message was last found, so a leg's later actions skip the search.
	at struct {
		folder string
		uid    uint32
	}
}

func newGmailEngagement(s wmail.GmailIMAPSession, boxes []models.Mailbox, action models.WarmupEmailAction) *gmailEngagement {
	name := func(attr goimap.MailboxAttr) string {
		if b := imap.FindSpecialUse(boxes, attr); b != nil {
			return b.Name
		}
		return ""
	}
	e := &gmailEngagement{s: s, id: action.RFCMessageID, junk: name(goimap.MailboxAttrJunk), all: name(goimap.MailboxAttrAll), important: name(goimap.MailboxAttrImportant)}
	e.placement, e.folder = warmupFiling(action)
	// Where a rescued message goes: the end state the API's RescueFromSpam leaves.
	switch e.placement {
	case models.WarmupPlacementInbox:
		e.filingDest = "INBOX"
	case models.WarmupPlacementArchive:
		e.filingDest = e.all
	default:
		e.filingDest = e.folder
	}
	return e
}

func (e *gmailEngagement) do(ctx context.Context, act string, rescue *string) error {
	if act == models.WarmupActionRescueFromSpam {
		return e.rescue(ctx, rescue)
	}
	folder, uid, err := e.locate(ctx)
	if err != nil {
		return err
	}
	switch act {
	case models.WarmupActionMarkRead:
		return e.s.MarkAsRead(ctx, folder, uid)
	case models.WarmupActionStar:
		// \Flagged is Gmail's star.
		return e.s.MarkImportant(ctx, folder, uid)
	case models.WarmupActionMarkImportant:
		if e.important == "" {
			return errGmailIMAPCannot
		}
		if folder == e.important {
			return nil
		}
		return e.s.CopyToFolder(ctx, folder, e.important, uid)
	}
	return nil
}

// rescue moves the message out of Spam to where the mailbox's placement files warmup.
func (e *gmailEngagement) rescue(ctx context.Context, rescue *string) error {
	if e.junk == "" || e.filingDest == "" {
		return errGmailIMAPCannot
	}
	uid, err := e.s.FindUIDInFolder(ctx, e.junk, e.id)
	if err != nil {
		return err
	}
	if uid == 0 {
		_, _, err := e.locate(ctx)
		switch {
		case err == nil:
			setRescue(rescue, models.WarmupRescueAlreadyOut)
		case errors.Is(err, errGmailIMAPNotFound):
			setRescue(rescue, models.WarmupRescueGone)
		default:
			return err
		}
		return nil
	}
	moved, err := e.s.MoveToFolder(ctx, e.junk, e.filingDest, uid)
	if err != nil {
		return err
	}
	if !moved {
		return errGmailIMAPCannot
	}
	e.at.folder, e.at.uid = "", 0
	setRescue(rescue, models.WarmupRescueMoved)
	return nil
}

// locate finds the message by Message-ID, All Mail first since filing may have moved it out of the inbox.
func (e *gmailEngagement) locate(ctx context.Context) (string, uint32, error) {
	if e.at.uid != 0 {
		return e.at.folder, e.at.uid, nil
	}
	candidates := []string{e.all}
	if e.placement == models.WarmupPlacementFolder {
		candidates = append(candidates, e.folder)
	}
	candidates = append(candidates, "INBOX", e.junk)
	seen := make(map[string]bool, len(candidates))
	for _, name := range candidates {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		uid, err := e.s.FindUIDInFolder(ctx, name, e.id)
		if err != nil {
			return "", 0, err
		}
		if uid != 0 {
			e.at.folder, e.at.uid = name, uid
			return name, uid, nil
		}
	}
	return "", 0, errGmailIMAPNotFound
}
