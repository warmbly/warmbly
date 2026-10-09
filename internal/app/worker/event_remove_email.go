package worker

import (
	"context"
	"time"

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
		for load := v.(*mailboxLoad); load != nil; load = load.prev {
			load.removed.Store(true)
		}
	}

	mail := w.mailManager.Get(id)
	if mail == nil {
		// Already gone - idempotent
		return nil
	}
	w.fileWarmupBeforeDisconnect(ctx, e)
	w.dropMailbox(id, mail)

	log.Info().Str("email_id", e.EmailID).Msg("email account removed from worker")
	return nil
}

func (w *WorkerService) fileWarmupBeforeDisconnect(ctx context.Context, e *models.RemoveWorkerEmail) {
	if len(e.WarmupMessageIDs) == 0 || e.WarmupPlacement == models.WarmupPlacementInbox {
		return
	}
	accountID, err := uuid.Parse(e.EmailID)
	if err != nil {
		return
	}
	userID, err := uuid.Parse(e.UserID)
	if err != nil {
		return
	}
	// Removal must not be held hostage by a broken provider connection.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for _, messageID := range e.WarmupMessageIDs {
		if ctx.Err() != nil {
			break
		}
		// Provider removals use the normal mailbox-presence verification path.
		if err := w.HandleWarmupAction(ctx, models.WarmupEmailAction{
			UserID: userID, EmailID: accountID, RFCMessageID: messageID,
			Placement: e.WarmupPlacement, TargetFolder: e.WarmupFolder,
			Actions: []string{models.WarmupActionFile},
		}); err != nil {
			log.Warn().Err(err).Str("email_id", e.EmailID).Msg("pre-disconnect warmup filing failed")
		}
	}
}

// Compile-time assertion that RemoveWorkerEmail is the expected type
var _ = (*models.RemoveWorkerEmail)(nil)
