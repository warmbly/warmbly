package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// mailboxLoad is one mailbox being loaded; removed is set when a REMOVE_EMAIL arrives meanwhile.
type mailboxLoad struct {
	removed atomic.Bool
	done    chan struct{}
}

// HandleAddEmail loads a mailbox off the bus loop. Loading dials the mail
// server, and one that is slow or refuses would otherwise hold every command
// queued behind it, credential checks and sends included.
func (w *WorkerService) HandleAddEmail(ctx context.Context, e *models.AddWorkerEmail) error {
	if e == nil {
		return nil
	}

	if w.mailManager.Has(e.ID) {
		// Already loaded: keep the handler idempotent, but take a changed sync
		// budget so an operator's settings change reaches this mailbox.
		if mail := w.mailManager.Get(e.ID); mail != nil {
			mail.ApplySyncPolicy(e.Sync)
		}
		return nil
	}

	load := &mailboxLoad{done: make(chan struct{})}
	var prev *mailboxLoad
	for {
		v, busy := w.loads.LoadOrStore(e.ID, load)
		if !busy {
			break
		}
		old := v.(*mailboxLoad)
		if !old.removed.Load() {
			return nil
		}
		// A removal is pending on the load in flight: this add runs after it rather than being dropped.
		if w.loads.CompareAndSwap(e.ID, old, load) {
			prev = old
			break
		}
	}
	go func() {
		defer close(load.done)
		defer w.loads.CompareAndDelete(e.ID, load)
		defer func() {
			if r := recover(); r != nil {
				errs.Recover(r)
			}
		}()
		if prev != nil {
			<-prev.done
		}
		w.loadMailbox(context.WithoutCancel(ctx), e, load)
	}()
	return nil
}

// loadedMailbox returns a mailbox, waiting on a load of it still in flight so a
// command queued behind its ADD_EMAIL finds it as it did when loads were inline.
func (w *WorkerService) loadedMailbox(ctx context.Context, id uuid.UUID) (*wmail.WMail, bool) {
	if v, ok := w.loads.Load(id); ok {
		select {
		case <-v.(*mailboxLoad).done:
		case <-ctx.Done():
		}
	}
	mail := w.mailManager.Get(id)
	return mail, mail != nil
}

// loadMailbox connects one mailbox and starts its sync, or reports why it could not.
func (w *WorkerService) loadMailbox(ctx context.Context, e *models.AddWorkerEmail, load *mailboxLoad) {
	if err := w.mailManager.AddWMail(ctx, e); err != nil {
		log.Error().Err(err).Str("email_id", e.ID.String()).Msg("failed to add email account to worker")
		w.reportLoadFailure(e, err)
		return
	}
	mail := w.mailManager.Get(e.ID)
	if mail == nil {
		return
	}
	if load.removed.Load() {
		mail.Discard()
		w.mailManager.Terminate(e.ID)
		return
	}

	// Gmail (history) and Outlook/Graph (delta) always sync. Only generic
	// SMTP/IMAP mailboxes are opt-in via ImapSync. The sync worker runs on the
	// WMail's own context, cancelled when the account is removed or terminates.
	if e.Type == models.InboxProviderSMTPIMAP && !e.ImapSync {
		log.Info().Str("email_id", e.ID.String()).Str("email", e.Email).Msg("email account added (no sync)")
		return
	}
	go mail.StartSyncWorker(mail.Ctx)
	log.Info().Str("email_id", e.ID.String()).Str("email", e.Email).Msg("email account added to worker")
}

// reportLoadFailure raises the same account event a sync failure would, so a
// mailbox that cannot sign in shows the error and stops instead of looking connected.
func (w *WorkerService) reportLoadFailure(e *models.AddWorkerEmail, err error) {
	var mailErr *errx.MailError
	if !errors.As(err, &mailErr) {
		return
	}
	eventType := wmail.DetermineErrorEventType(mailErr)
	switch eventType {
	case models.JobEventTypeEmailAuthError, models.JobEventTypeEmailDisabled,
		models.JobEventTypeEmailRateLimited, models.JobEventTypeEmailServerError:
	default:
		return
	}
	info := mailErr.GetUserErrorInfo()
	event := models.EmailErrorEvent{
		EmailAccountID: e.ID.String(),
		UserID:         e.UserID.String(),
		ErrorCode:      string(mailErr.Code),
		ErrorType:      string(mailErr.Type),
		ResolveMethod:  string(mailErr.ResolveMethod),
		Message:        mailErr.Message,
		UserVisible:    mailErr.IsUserVisible(),
		UserTitle:      info.Title,
		UserMessage:    info.Message,
		ActionRequired: info.ActionRequired,
		Timestamp:      time.Now().Unix(),
	}
	if perr := w.mailManager.OnEvent(eventType, e.ID.String(), event); perr != nil {
		log.Error().Err(perr).Str("email_id", e.ID.String()).Msg("failed to report a mailbox that did not load")
	}
}

// dropMailbox removes a loaded mailbox and stops its work.
func (w *WorkerService) dropMailbox(id uuid.UUID, mail *wmail.WMail) {
	if mail.Cancel != nil {
		mail.Cancel()
	}
	w.mailManager.Terminate(id)
}
