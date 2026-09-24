package worker

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WorkerService) HandleRemoveEmail(ctx context.Context, e *models.RemoveWorkerEmail) error {
	if e == nil || e.EmailID == "" {
		return nil
	}

	id, err := uuid.Parse(e.EmailID)
	if err != nil {
		log.Warn().Str("email_id", e.EmailID).Err(err).Msg("invalid email id in remove event")
		return nil
	}

	// A load still dialing drops the mailbox when it finishes.
	if v, ok := w.loads.Load(id); ok {
		v.(*mailboxLoad).removed.Store(true)
	}

	mail := w.mailManager.Get(id)
	if mail == nil {
		// Already gone - idempotent
		return nil
	}
	w.dropMailbox(id, mail)

	log.Info().Str("email_id", e.EmailID).Msg("email account removed from worker")
	return nil
}

// Compile-time assertion that RemoveWorkerEmail is the expected type
var _ = (*models.RemoveWorkerEmail)(nil)
