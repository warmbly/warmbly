package worker

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

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
		Strs("actions", action.Actions).
		Msg("Processing warmup email action")

	// Get the email account from MailManager
	w.mailManager.RLock()
	mail, exists := w.mailManager.Emails[action.EmailID]
	w.mailManager.RUnlock()

	if !exists {
		log.Warn().Str("email_id", action.EmailID.String()).Msg("Email account not found for warmup action")
		return fmt.Errorf("email account %s not found", action.EmailID.String())
	}

	for _, act := range action.Actions {
		switch act {
		case "move_to_warmbly":
			// Gmail: create/get "Warmbly" label, apply it
			// IMAP: move to "Warmbly" folder
			if mail.GoogleData != nil && mail.GoogleData.Client != nil {
				if err := mail.GoogleData.Client.ApplyLabel(ctx, action.GmailID, "Warmbly"); err != nil {
					log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to apply Warmbly label")
				}
			}

		case "mark_read":
			// Gmail: remove UNREAD label
			// IMAP: set \\Seen flag
			if mail.GoogleData != nil && mail.GoogleData.Client != nil {
				if err := mail.GoogleData.Client.MarkAsRead(ctx, action.GmailID); err != nil {
					log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to mark as read")
				}
			}

		case "remove_from_spam":
			// Gmail: remove SPAM label
			// IMAP: move from Junk
			if mail.GoogleData != nil && mail.GoogleData.Client != nil {
				if err := mail.GoogleData.Client.RemoveFromSpam(ctx, action.GmailID); err != nil {
					log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to remove from spam")
				}
			}

		case "mark_important":
			// Gmail: add IMPORTANT label
			// IMAP: set \\Flagged
			if mail.GoogleData != nil && mail.GoogleData.Client != nil {
				if err := mail.GoogleData.Client.MarkImportant(ctx, action.GmailID); err != nil {
					log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to mark important")
				}
			}

		default:
			log.Warn().Str("action", act).Msg("Unknown warmup action")
		}
	}

	log.Info().
		Str("email_id", action.EmailID.String()).
		Msg("Warmup email actions completed")

	return nil
}
