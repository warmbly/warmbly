package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
)

const unreachableSweepBatch = 200

// StartUnreachableMailboxSweep switches off custom-server mailboxes whose server has refused every connection for
// config.MailboxUnreachableSwitchOffHours, so a mailbox deleted at its host stops being retried and reads as Off.
func (s *JobsService) StartUnreachableMailboxSweep(ctx context.Context, interval time.Duration) {
	if s.EmailAccountErrorRepository == nil {
		return
	}
	jobrun.Loop(ctx, "unreachable_mailbox_sweep", interval, false, func(ctx context.Context) error {
		sweepCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		s.switchOffUnreachable(sweepCtx)
		return nil
	})
}

func (s *JobsService) switchOffUnreachable(ctx context.Context) {
	silentFor := time.Duration(config.MailboxUnreachableSwitchOffHours) * time.Hour
	off, xerr := s.EmailAccountErrorRepository.SwitchOffUnreachable(ctx, silentFor, unreachableSweepBatch)
	if xerr != nil {
		log.Warn().Str("error", xerr.Message).Msg("unreachable mailbox sweep: could not switch off unreachable mailboxes")
		return
	}
	notice := errx.ErrMailMailboxUnreachable(silentFor)
	info := notice.GetUserErrorInfo()
	for _, m := range off {
		log.Info().Str("email_account_id", m.ID.String()).Msg("unreachable mailbox sweep: switched off a mailbox whose server refused every connection")
		if m.WorkerID != nil && s.Publisher != nil {
			if err := s.Publisher.PublishRemoveEmail(ctx, *m.WorkerID, &models.RemoveWorkerEmail{UserID: m.UserID.String(), EmailID: m.ID.String()}); err != nil {
				log.Warn().Err(err).Str("email_account_id", m.ID.String()).Msg("unreachable mailbox sweep: could not tell the worker to drop the mailbox")
			}
		}
		s.emitMailboxEvent(ctx, m.ID, models.WebhookEventEmailAccountDisconnected, map[string]any{
			"reason":     "unreachable",
			"error_code": string(notice.Code),
		})
		if s.StreamingPublisher != nil {
			s.StreamingPublisher.PublishEmailError(ctx, m.UserID.String(), m.ID, uuid.Nil, info.Title, notice.Message)
		}
	}
}
