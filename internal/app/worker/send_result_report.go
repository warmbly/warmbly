package worker

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type pendingSendResult struct {
	result                     models.SendEmailResult
	mailboxID, orgID, workerID uuid.UUID
	dispatch                   repository.WorkerWarmupDispatch
	nonce                      string
	recipients                 []string
}

func (w *WorkerService) replayPendingSend(ctx context.Context, command models.SendEmail, worker uuid.UUID, p pendingSendResult) error {
	recipients := append(append(append([]string{}, command.To...), command.Cc...), command.Bcc...)
	if p.mailboxID != command.EmailID || p.orgID != command.OrgID || p.workerID != worker || !repository.MatchOutboundRecipients(p.recipients, recipients) {
		return errors.New("pending send outcome ownership mismatch")
	}
	if p.nonce != "" {
		body, err := w.fetchEmailBody(ctx, command.OrgID, command.BodyS3Key)
		if err != nil {
			return err
		}
		if body.DispatchNonce != p.nonce {
			return errors.New("pending send outcome nonce mismatch")
		}
	}
	return w.reportSendResult(ctx, p)
}

// Provider execution spends the handler budget; recording gets its own bounded budget.
func (w *WorkerService) reportSendResult(parent context.Context, p pendingSendResult) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 45*time.Second)
	defer cancel()
	var err error
	for attempt := range 4 {
		pass, stop := context.WithTimeout(ctx, 10*time.Second)
		if p.dispatch != nil {
			_, err = p.dispatch.WarmupDispatch(pass, p.result.TaskID, p.mailboxID, p.workerID, uuid.Nil, false, &p.result)
		} else {
			err = nil
		}
		if err == nil {
			err = w.publishSendResult(pass, p.result)
		}
		stop()
		if err == nil {
			w.pendingSendResults.Delete(p.result.TaskID)
			return nil
		}
		if attempt < 3 {
			timer := time.NewTimer(time.Duration(attempt+1) * 250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return err
}

func (w *WorkerService) publishSendResult(ctx context.Context, result models.SendEmailResult) error {
	kind := models.JobEventTypeEmailFailed
	if result.Success {
		kind = models.JobEventTypeEmailSent
	}
	payload, err := w.Codec.Serialize(ctx, kafka.TopicWorkerEvents, &models.JobEvent{Type: kind, Body: result})
	if err != nil {
		return err
	}
	return w.Bus.Publish(ctx, kafka.TopicWorkerEvents, result.TaskID.String(), payload)
}
