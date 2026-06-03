package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/models"
)

// maxWarmupDwell bounds how long the worker will hold a warmup action timer, so
// a misconfigured dwell can't pin goroutines/memory indefinitely.
const maxWarmupDwell = 15 * time.Minute

func (w *WorkerService) HandleWarmupAction(ctx context.Context, body any) error {
	action, ok := body.(models.WarmupEmailAction)
	if !ok {
		log.Debug().Msg("Invalid HandleWarmupAction body type")
		return fmt.Errorf("invalid body type")
	}

	log.Info().
		Str("email_id", action.EmailID.String()).
		Str("gmail_id", action.GmailID).
		Uint32("uid", action.UID).
		Uint32("mailbox_uid_validity", action.MailboxUIDValidity).
		Strs("actions", action.Actions).
		Int("delay_seconds", action.DelaySeconds).
		Msg("Processing warmup email action")

	runActions := func(runCtx context.Context, acts []string) {
		if len(acts) == 0 {
			return
		}
		w.mailManager.RLock()
		mail, exists := w.mailManager.Emails[action.EmailID]
		w.mailManager.RUnlock()

		if !exists {
			log.Warn().Str("email_id", action.EmailID.String()).Msg("Email account not found for warmup action")
			return
		}

		a := action
		a.Actions = acts
		switch {
		case mail.GoogleData != nil && mail.GoogleData.Client != nil:
			w.runGoogleWarmupActions(runCtx, mail, a)
		case mail.SmtpImapData != nil && mail.SmtpImapData.ImapClient != nil:
			w.runImapWarmupActions(runCtx, mail, a)
		default:
			log.Warn().
				Str("email_id", action.EmailID.String()).
				Msg("No mail client available for warmup actions; skipping")
		}
	}

	// Foldering (moving the warmup email into the dedicated "Warmbly"
	// folder/label, auto-created if missing) keeps the recipient's inbox clean
	// and should happen promptly. The engagement actions (read / mark-important
	// / spam-rescue) get the recipient-side dwell delay so they don't all fire
	// milliseconds after delivery — a bot signature.
	immediate, delayed := splitWarmupActions(action.Actions)
	runActions(ctx, immediate)

	if len(delayed) == 0 {
		return nil
	}
	if delay := warmupDwellDelay(action.DelaySeconds); delay > 0 {
		// Detached timer so the consume loop isn't blocked. Losing the timer on
		// restart is fine for best-effort warmup engagement.
		time.AfterFunc(delay, func() {
			runCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			runActions(runCtx, delayed)
		})
		return nil
	}
	runActions(ctx, delayed)
	return nil
}

// splitWarmupActions separates foldering (done promptly) from engagement
// actions (delayed by the recipient-side dwell).
func splitWarmupActions(actions []string) (immediate, delayed []string) {
	for _, a := range actions {
		if a == "move_to_warmbly" {
			immediate = append(immediate, a)
		} else {
			delayed = append(delayed, a)
		}
	}
	return immediate, delayed
}

// warmupDwellDelay clamps the requested dwell into a sane bound.
func warmupDwellDelay(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	d := time.Duration(seconds) * time.Second
	if d > maxWarmupDwell {
		d = maxWarmupDwell
	}
	return d
}

func (w *WorkerService) runGoogleWarmupActions(ctx context.Context, mail *wmail.WMail, action models.WarmupEmailAction) {
	for _, act := range action.Actions {
		switch act {
		case "move_to_warmbly":
			if err := mail.GoogleData.Client.ApplyLabel(ctx, action.GmailID, imap.WarmupFolderName); err != nil {
				log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to apply Warmbly label")
			}
		case "mark_read":
			if err := mail.GoogleData.Client.MarkAsRead(ctx, action.GmailID); err != nil {
				log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to mark as read")
			}
		case "remove_from_spam":
			if err := mail.GoogleData.Client.RemoveFromSpam(ctx, action.GmailID); err != nil {
				log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to remove from spam")
			}
		case "mark_important":
			if err := mail.GoogleData.Client.MarkImportant(ctx, action.GmailID); err != nil {
				log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to mark important")
			}
		case "star":
			if err := mail.GoogleData.Client.AddStar(ctx, action.GmailID); err != nil {
				log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to star warmup message")
			}
		default:
			log.Warn().Str("action", act).Msg("Unknown warmup action")
		}
	}
}

func (w *WorkerService) runImapWarmupActions(ctx context.Context, mail *wmail.WMail, action models.WarmupEmailAction) {
	sourceBox := lookupMailboxByUIDValidity(mail.SmtpImapData.Mailboxes, action.MailboxUIDValidity)
	if sourceBox == nil {
		log.Warn().
			Uint32("uid_validity", action.MailboxUIDValidity).
			Str("email_id", action.EmailID.String()).
			Msg("Source mailbox for warmup action not found; skipping")
		return
	}

	inboxBox := lookupInbox(mail.SmtpImapData.Mailboxes)
	inboxName := "INBOX"
	if inboxBox != nil {
		inboxName = inboxBox.Name
	}

	imapClient := mail.SmtpImapData.ImapClient
	uid := action.UID

	for _, act := range action.Actions {
		switch act {
		case "move_to_warmbly":
			if err := imapClient.MoveToFolder(ctx, sourceBox.Name, imap.WarmupFolderName, uid); err != nil {
				log.Error().Err(err).Uint32("uid", uid).Msg("Failed to move to Warmbly folder")
			}
		case "mark_read":
			if err := imapClient.MarkAsRead(ctx, sourceBox.Name, uid); err != nil {
				log.Error().Err(err).Uint32("uid", uid).Msg("Failed to mark as read (IMAP)")
			}
		case "remove_from_spam":
			if !imap.IsSpamMailbox(sourceBox.Name, sourceBox.Attrs) {
				continue
			}
			if err := imapClient.RemoveFromSpam(ctx, sourceBox.Name, inboxName, uid); err != nil {
				log.Error().Err(err).Uint32("uid", uid).Msg("Failed to remove from spam (IMAP)")
			}
		case "mark_important":
			if err := imapClient.MarkImportant(ctx, sourceBox.Name, uid); err != nil {
				log.Error().Err(err).Uint32("uid", uid).Msg("Failed to mark important (IMAP)")
			}
		case "star":
			// No-op on IMAP: \Flagged is already set by mark_important, so
			// starring here would just re-flag the same message. Star is a
			// Gmail-only distinct signal.
			continue
		default:
			log.Warn().Str("action", act).Msg("Unknown warmup action")
		}
	}
}

func lookupMailboxByUIDValidity(boxes []*models.Mailbox, uidValidity uint32) *models.Mailbox {
	for _, b := range boxes {
		if b != nil && b.UIDValidity == uidValidity {
			return b
		}
	}
	return nil
}

func lookupInbox(boxes []*models.Mailbox) *models.Mailbox {
	for _, b := range boxes {
		if b != nil && imap.IsInboxMailbox(b.Name, b.Attrs) {
			return b
		}
	}
	return nil
}
