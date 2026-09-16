package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
)

// StartWarmupInboxCleanup repairs old leaks in bounded batches, retrying verification failures.
func (s *JobsService) StartWarmupInboxCleanup(ctx context.Context) {
	if s.UniboxRepository == nil {
		return
	}
	var afterID uuid.UUID
	var nextPass time.Time
	jobrun.Loop(ctx, "warmup_inbox_cleanup", time.Minute, true, func(ctx context.Context) error {
		if time.Now().Before(nextPass) {
			return nil
		}
		batchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		next, done, err := s.cleanWarmupInboxBatch(batchCtx, afterID)
		afterID = next
		if err == nil && done {
			afterID = uuid.Nil
			nextPass = time.Now().Add(24 * time.Hour)
		}
		return err
	})
}

// StartPendingWarmupVerification drains arrivals held during verification outages.
func (s *JobsService) StartPendingWarmupVerification(ctx context.Context) {
	jobrun.Loop(ctx, "pending_warmup_verification", time.Minute, true, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		return s.retryPendingWarmupVerification(ctx)
	})
}

func (s *JobsService) retryPendingWarmupVerification(ctx context.Context) error {
	events, err := s.UniboxRepository.ClaimPendingWarmupVerification(ctx, 25)
	if err != nil {
		return err
	}
	var failures []error
	for _, e := range events {
		if err := s.UniboxRepository.ProcessPendingWarmupVerification(ctx, e.Message.ID, func(current *models.JobEventNewEmail) error {
			return s.ingestNewEmail(ctx, current)
		}); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *JobsService) cleanWarmupInboxBatch(ctx context.Context, afterID uuid.UUID) (uuid.UUID, bool, error) {
	const batchSize = 100
	events, err := s.UniboxRepository.ListWarmupReviewCandidates(ctx, afterID, batchSize)
	if err != nil {
		return afterID, false, err
	}
	for _, e := range events {
		// Historical cleanup requires an exact identifier, never a reused subject.
		candidate, message := e, *e.Message
		message.Subject = ""
		candidate.Message = &message
		warmup, err := s.isKnownWarmupEmail(ctx, &candidate)
		if err != nil {
			return afterID, false, err
		}
		if warmup {
			if err := s.UniboxRepository.Delete(ctx, e.UserID, e.Message.ID); err != nil {
				return afterID, false, err
			}
			if s.StreamingPublisher != nil {
				s.StreamingPublisher.PublishEmailDeleted(ctx, s.emailInboxEvent(ctx, e.UserID, e.Message))
			}
		}
		afterID = e.Message.ID
	}
	return afterID, len(events) < batchSize, nil
}
