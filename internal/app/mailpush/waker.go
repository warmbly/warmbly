package mailpush

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// WakePublisher sends SYNC_WAKE to a worker.
type WakePublisher interface {
	PublishSyncWake(ctx context.Context, workerID uuid.UUID, wake *models.SyncWake) error
}

// Waker turns provider notifications into SYNC_WAKE for the worker holding each mailbox.
type Waker struct {
	targets   repository.MailboxPushRepository
	publisher WakePublisher
	// coalesce drops repeat wakes for one mailbox inside config.SyncWakeCoalesce; nil wakes every time.
	coalesce redis.Cmdable
}

func NewWaker(targets repository.MailboxPushRepository, publisher WakePublisher, coalesce *cache.Cache) *Waker {
	w := &Waker{targets: targets, publisher: publisher}
	if coalesce != nil {
		w.coalesce = coalesce
	}
	return w
}

// WakeMailboxes wakes each mailbox; renew also asks its worker to refresh the push channel.
func (w *Waker) WakeMailboxes(ctx context.Context, emailIDs []uuid.UUID, renew bool) error {
	targets, err := w.targets.WakeTargets(ctx, emailIDs)
	if err != nil {
		return err
	}
	return w.wake(ctx, targets, renew)
}

// WakeGmail wakes every watched Gmail mailbox at address.
func (w *Waker) WakeGmail(ctx context.Context, address string) error {
	targets, err := w.targets.GmailWakeTargets(ctx, address)
	if err != nil {
		return err
	}
	return w.wake(ctx, targets, false)
}

func (w *Waker) wake(ctx context.Context, targets []repository.SyncWakeTarget, renew bool) error {
	var errs []error
	for _, t := range targets {
		if !renew && w.coalesced(ctx, t.EmailID) {
			continue
		}
		if err := w.publisher.PublishSyncWake(ctx, t.WorkerID, &models.SyncWake{UserID: t.UserID, EmailID: t.EmailID, Renew: renew}); err != nil {
			// Free the slot so the provider's retry is not dropped as a duplicate.
			w.release(ctx, t.EmailID)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// coalesced reports a wake already sent for the mailbox moments ago. A cache
// outage wakes every time: an extra pass is cheap, a lost one is not.
func (w *Waker) coalesced(ctx context.Context, emailID uuid.UUID) bool {
	if w.coalesce == nil {
		return false
	}
	fresh, err := w.coalesce.SetNX(ctx, wakeKey(emailID), 1, config.SyncWakeCoalesce).Result()
	return err == nil && !fresh
}

func (w *Waker) release(ctx context.Context, emailID uuid.UUID) {
	if w.coalesce != nil {
		w.coalesce.Del(ctx, wakeKey(emailID))
	}
}

func wakeKey(emailID uuid.UUID) string { return "mailpush:wake:" + emailID.String() }
