package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

type interruptedManualSends interface {
	RecoverInterruptedManualSends(ctx context.Context, claimedBefore, retrySince time.Time, limit int) ([]repository.InterruptedManualSend, error)
}

// StartManualSendRecovery settles manual sends whose execution stopped between
// their claim and their reservation, so each one runs again or fails visibly.
func (s *tasksService) StartManualSendRecovery(ctx context.Context, interval time.Duration) {
	jobrun.Loop(ctx, "manual_send_recovery", interval, true, func(ctx context.Context) error {
		s.recoverInterruptedManualSends(ctx)
		return nil
	})
}

func (s *tasksService) recoverInterruptedManualSends(ctx context.Context) {
	repo, ok := s.taskRepo.(interruptedManualSends)
	if !ok {
		return
	}
	now := time.Now()
	sends, err := repo.RecoverInterruptedManualSends(ctx,
		now.Add(-config.ManualSendInterruptedAfterMinutes*time.Minute),
		now.Add(-config.ManualSendRetryWithinMinutes*time.Minute), 200)
	if err != nil {
		log.Warn().Err(err).Msg("interrupted manual send recovery deferred")
	}
	for _, send := range sends {
		if send.Retried {
			// The local scheduler fires the pending row on its own; Cloud Tasks needs a new callback.
			if s.tasksClient != nil {
				if _, err := s.tasksClient.CreateTask(ctx, &proto.ProcessTask{TaskId: send.TaskID.String()}, time.Now()); err != nil {
					log.Warn().Err(err).Str("task_id", send.TaskID.String()).Msg("interrupted manual send could not be re-enqueued")
				}
			}
			continue
		}
		account, xerr := s.emailRepo.GetByID(ctx, send.MailboxID)
		if xerr != nil || account == nil {
			continue
		}
		s.notifyUserEmailFailed(ctx, send.TaskID, account, "Email was not sent",
			fmt.Sprintf("%s did not send your email: sending was interrupted before it reached its worker. Send it again.", account.Email))
	}
	if len(sends) > 0 {
		log.Info().Int("settled", len(sends)).Msg("settled manual sends interrupted before reservation")
	}
}
