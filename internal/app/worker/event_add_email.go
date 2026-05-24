package worker

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WorkerService) HandleAddEmail(ctx context.Context, e *models.AddWorkerEmail) error {
	if e == nil {
		return nil
	}

	if w.mailManager.Has(e.ID) {
		// Already loaded - skip silently to keep handler idempotent
		return nil
	}

	if err := w.mailManager.AddWMail(ctx, e); err != nil {
		log.Error().Err(err).Str("email_id", e.ID.String()).Msg("failed to add email account to worker")
		return err
	}

	// Start the periodic mail sync worker. Uses the WMail's own context which is
	// cancelled when the account is removed or terminates, so we don't leak goroutines.
	mail := w.mailManager.Get(e.ID)
	if mail != nil {
		// Skip IMAP if not requested for SMTP/IMAP providers
		if e.Type != models.InboxProviderGoogle && !e.ImapSync {
			log.Info().Str("email_id", e.ID.String()).Str("email", e.Email).Msg("email account added (no sync)")
			return nil
		}
		go mail.StartSyncWorker(mail.Ctx)
	}

	log.Info().Str("email_id", e.ID.String()).Str("email", e.Email).Msg("email account added to worker")
	return nil
}
