package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// HandleEmailAuthError handles authentication errors that require user re-authorization
func (s *JobsService) HandleEmailAuthError(ctx context.Context, event models.EmailErrorEvent) error {
	log.Info().
		Str("email_account_id", event.EmailAccountID).
		Str("error_code", event.ErrorCode).
		Msg("Handling email auth error")

	emailAccountID, err := uuid.Parse(event.EmailAccountID)
	if err != nil {
		log.Error().Err(err).Str("email_account_id", event.EmailAccountID).Msg("Invalid email account ID")
		return err
	}

	userID, err := uuid.Parse(event.UserID)
	if err != nil {
		log.Error().Err(err).Str("user_id", event.UserID).Msg("Invalid user ID")
		return err
	}

	var taskID *uuid.UUID
	if event.TaskID != "" {
		tid, err := uuid.Parse(event.TaskID)
		if err == nil {
			taskID = &tid
		}
	}

	// Store error in database
	if s.EmailAccountErrorRepository != nil {
		errorRecord := &repository.CreateEmailAccountError{
			EmailAccountID: emailAccountID,
			UserID:         userID,
			ErrorCode:      event.ErrorCode,
			Severity:       event.ErrorType,
			ResolveMethod:  event.ResolveMethod,
			Title:          event.UserTitle,
			Message:        event.Message,
			UserMessage:    ptrString(event.UserMessage),
			ActionRequired: ptrString(event.ActionRequired),
			TaskID:         taskID,
		}

		if _, xerr := s.EmailAccountErrorRepository.CreateOnce(ctx, errorRecord); xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to store email auth error")
		}
	}

	// Mark email account as needing re-auth (set status to inactive)
	s.deactivateAccount(ctx, userID, emailAccountID)

	// Send Pub/Sub notification to user
	if s.StreamingPublisher != nil && event.UserVisible {
		s.StreamingPublisher.PublishEmailError(
			ctx,
			event.UserID,
			emailAccountID,
			uuid.Nil,
			event.UserTitle,
			event.UserMessage,
		)
	}

	return nil
}

// HandleEmailDisabled handles errors indicating the account has been disabled
func (s *JobsService) HandleEmailDisabled(ctx context.Context, event models.EmailErrorEvent) error {
	log.Info().
		Str("email_account_id", event.EmailAccountID).
		Str("error_code", event.ErrorCode).
		Msg("Handling email disabled error")

	emailAccountID, err := uuid.Parse(event.EmailAccountID)
	if err != nil {
		log.Error().Err(err).Str("email_account_id", event.EmailAccountID).Msg("Invalid email account ID")
		return err
	}

	userID, err := uuid.Parse(event.UserID)
	if err != nil {
		log.Error().Err(err).Str("user_id", event.UserID).Msg("Invalid user ID")
		return err
	}

	var taskID *uuid.UUID
	if event.TaskID != "" {
		tid, err := uuid.Parse(event.TaskID)
		if err == nil {
			taskID = &tid
		}
	}

	// Store error in database
	if s.EmailAccountErrorRepository != nil {
		errorRecord := &repository.CreateEmailAccountError{
			EmailAccountID: emailAccountID,
			UserID:         userID,
			ErrorCode:      event.ErrorCode,
			Severity:       event.ErrorType,
			ResolveMethod:  event.ResolveMethod,
			Title:          event.UserTitle,
			Message:        event.Message,
			UserMessage:    ptrString(event.UserMessage),
			ActionRequired: ptrString(event.ActionRequired),
			TaskID:         taskID,
		}

		if _, xerr := s.EmailAccountErrorRepository.CreateOnce(ctx, errorRecord); xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to store email disabled error")
		}
	}

	// Mark email account as inactive
	s.deactivateAccount(ctx, userID, emailAccountID)

	// Send Pub/Sub notification to user
	if s.StreamingPublisher != nil && event.UserVisible {
		s.StreamingPublisher.PublishEmailError(
			ctx,
			event.UserID,
			emailAccountID,
			uuid.Nil,
			event.UserTitle,
			event.UserMessage,
		)
	}

	return nil
}

// HandleEmailRateLimited handles rate limit exceeded errors (anti-abuse)
func (s *JobsService) HandleEmailRateLimited(ctx context.Context, event models.EmailErrorEvent) error {
	// Older workers grouped provider 429/quota responses onto this topic. Only
	// Warmbly's own abuse and fair-use decisions may deactivate a mailbox.
	switch errx.MailErrorCode(event.ErrorCode) {
	case errx.MailErrorCodeRateLimitExceeded,
		errx.MailErrorCodeSyncFlood,
		errx.MailErrorCodeSyncFairUse:
		// Continue with deactivation below.
	default:
		log.Info().
			Str("email_account_id", event.EmailAccountID).
			Str("error_code", event.ErrorCode).
			Msg("Treating provider rate limit as a temporary mailbox error")
		return s.HandleEmailServerError(ctx, event)
	}

	log.Warn().
		Str("email_account_id", event.EmailAccountID).
		Str("error_code", event.ErrorCode).
		Msg("Handling email rate limit exceeded")

	emailAccountID, err := uuid.Parse(event.EmailAccountID)
	if err != nil {
		log.Error().Err(err).Str("email_account_id", event.EmailAccountID).Msg("Invalid email account ID")
		return err
	}

	userID, err := uuid.Parse(event.UserID)
	if err != nil {
		log.Error().Err(err).Str("user_id", event.UserID).Msg("Invalid user ID")
		return err
	}

	var taskID *uuid.UUID
	if event.TaskID != "" {
		tid, err := uuid.Parse(event.TaskID)
		if err == nil {
			taskID = &tid
		}
	}

	// Store error in database
	if s.EmailAccountErrorRepository != nil {
		errorRecord := &repository.CreateEmailAccountError{
			EmailAccountID: emailAccountID,
			UserID:         userID,
			ErrorCode:      event.ErrorCode,
			Severity:       event.ErrorType,
			ResolveMethod:  event.ResolveMethod,
			Title:          event.UserTitle,
			Message:        event.Message,
			UserMessage:    ptrString(event.UserMessage),
			ActionRequired: ptrString(event.ActionRequired),
			TaskID:         taskID,
		}

		if _, xerr := s.EmailAccountErrorRepository.CreateOnce(ctx, errorRecord); xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to store rate limit error")
		}
	}

	// Mark email account as inactive (terminated due to abuse)
	s.deactivateAccount(ctx, userID, emailAccountID)

	if s.WarmupService != nil {
		health, _ := s.WarmupService.ApplyRateLimitExceeded(ctx, emailAccountID, "worker sync/email rate limit exceeded")
		s.markRiskBandFromWarmupHealth(ctx, emailAccountID, health)
	}

	// Send Pub/Sub notification to user (this is a warning notification)
	if s.StreamingPublisher != nil && event.UserVisible {
		s.StreamingPublisher.PublishEmailWarning(
			ctx,
			event.UserID,
			emailAccountID,
			event.UserTitle,
			event.UserMessage,
		)
	}

	return nil
}

// HandleEmailServerError handles temporary server errors (may auto-resolve)
func (s *JobsService) HandleEmailServerError(ctx context.Context, event models.EmailErrorEvent) error {
	log.Info().
		Str("email_account_id", event.EmailAccountID).
		Str("error_code", event.ErrorCode).
		Msg("Handling email server error")

	emailAccountID, err := uuid.Parse(event.EmailAccountID)
	if err != nil {
		log.Error().Err(err).Str("email_account_id", event.EmailAccountID).Msg("Invalid email account ID")
		return err
	}

	userID, err := uuid.Parse(event.UserID)
	if err != nil {
		log.Error().Err(err).Str("user_id", event.UserID).Msg("Invalid user ID")
		return err
	}

	// A mailbox whose row is gone reports here and nowhere else. Its provider
	// failures are classified as server errors (wmail.err maps NotFound into
	// that bucket), and the server-error path deliberately changes no status,
	// so the eviction that deactivateAccount performs was unreachable for the
	// one case that needs it most: a worker holding a mailbox that no longer
	// exists calls the provider once a sync interval, forever, and burns the
	// account's API quota doing it.
	if s.Publisher != nil && s.EmailRepository != nil {
		if _, xerr := s.EmailRepository.GetWorkerID(ctx, emailAccountID); errors.Is(xerr, errx.ErrNotFound) {
			s.evictDeletedMailbox(ctx, userID, emailAccountID)
			return nil
		}
	}

	var taskID *uuid.UUID
	if event.TaskID != "" {
		tid, err := uuid.Parse(event.TaskID)
		if err == nil {
			taskID = &tid
		}
	}

	// Store error in database (as warning, not critical).
	//
	// CreateOnce, not Create: the sync loop retries a refused server about
	// once a minute and relays what it got each time, so the same unresolved
	// failure would otherwise fill the mailbox's error list with a row a
	// minute for as long as the server keeps refusing (issue #405).
	notify := true
	warningMessage := event.UserMessage
	if s.EmailAccountErrorRepository != nil {
		errorRecord := &repository.CreateEmailAccountError{
			EmailAccountID:      emailAccountID,
			UserID:              userID,
			ErrorCode:           event.ErrorCode,
			Severity:            "WARNING",
			ResolveMethod:       event.ResolveMethod,
			Title:               event.UserTitle,
			Message:             event.Message,
			UserMessage:         ptrString(event.UserMessage),
			ActionRequired:      ptrString(event.ActionRequired),
			TaskID:              taskID,
			ObservedAt:          serverErrorObservedAt(event.Timestamp),
			Operation:           serverErrorOperation(event.Operation, taskID, event.TaskID != ""),
			TransportGeneration: event.TransportGeneration,
		}

		row, xerr := s.EmailAccountErrorRepository.CreateOnce(ctx, errorRecord)
		if xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to store server error")
			return xerr
		}
		notify = xerr == nil && row != nil && !s.transientBlip(ctx, emailAccountID, row)
		if row != nil && row.UserMessage != nil {
			warningMessage = *row.UserMessage
		} else if row != nil && row.Message == errx.ErrMailServerUnreachable.Message {
			warningMessage = row.Message
		}
	}

	// Server errors are temporary - don't change account status
	// The error will be auto-resolved when connectivity is restored

	// Only notify for an error we just recorded on a mailbox that is not still working: a repeat of one already in
	// the drawer must not toast the reader again every pass.
	if s.StreamingPublisher != nil && event.UserVisible && notify {
		s.StreamingPublisher.PublishEmailWarning(
			ctx,
			event.UserID,
			emailAccountID,
			event.UserTitle,
			warningMessage,
		)
	}

	return nil
}

// transientBlip is a temporary failure on a mailbox that still worked within the grace window, which is not worth a notification.
func (s *JobsService) transientBlip(ctx context.Context, accountID uuid.UUID, row *repository.EmailAccountError) bool {
	reader, ok := s.EmailAccountErrorRepository.(interface {
		LastTransportSuccess(context.Context, []uuid.UUID) (map[uuid.UUID]map[models.MailTransportOperation]time.Time, *errx.Error)
	})
	if !ok {
		return false
	}
	success, xerr := reader.LastTransportSuccess(ctx, []uuid.UUID{accountID})
	if xerr != nil {
		return false
	}
	return models.MailTransientBlip(row.Severity, row.ErrorCode, row.Operation, row.ObservationSource, success[accountID], time.Now())
}

func serverErrorObservedAt(timestamp int64) *time.Time {
	if timestamp <= 0 || timestamp > time.Now().Unix() {
		return nil
	}
	observed := time.Unix(timestamp, 0).UTC()
	return &observed
}

func serverErrorOperation(operation models.MailTransportOperation, taskID *uuid.UUID, taskProvided bool) models.MailTransportOperation {
	if operation == models.MailTransportSync && taskProvided || operation == models.MailTransportSend && (taskID == nil || *taskID == uuid.Nil) {
		return models.MailTransportUnknown
	}
	return operation.Conservative()
}

// ptrString returns a pointer to a string, or nil if empty
func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// deactivateAccount marks a mailbox inactive AND tells the worker holding it
// to drop it. The per-worker load filters on status, but that only decides what
// a worker picks up when it starts, and the send path relays an account-level
// failure without stopping the mailbox's own sync loop: without this the worker
// keeps syncing a mailbox that has just been deactivated, failing every pass
// and writing an email_account_errors row each time, until someone restarts it.
func (s *JobsService) deactivateAccount(ctx context.Context, userID, emailAccountID uuid.UUID) {
	if s.EmailRepository == nil {
		return
	}

	// The assignment is read on its own rather than off the row Update
	// returns, because that row is the mailbox as the dashboard sees it and
	// carries no assignment; and before the write rather than after, because
	// that is what separates a mailbox that no longer exists from one whose
	// update merely failed. Update cannot tell those apart, and treating the
	// first as the second is what left workers syncing deleted mailboxes.
	// Only worth doing when there is a publisher to act on the answer.
	var workerID *uuid.UUID
	if s.Publisher != nil {
		id, xerr := s.EmailRepository.GetWorkerID(ctx, emailAccountID)
		switch {
		case xerr == nil:
			workerID = id
		case errors.Is(xerr, errx.ErrNotFound):
			// The mailbox is gone, so there is no status left to write and
			// nothing holds its assignment. All that remains is to stop
			// whichever worker is still executing it.
			s.evictDeletedMailbox(ctx, userID, emailAccountID)
			return
		default:
			log.Warn().
				Str("error", xerr.Message).
				Str("email_account_id", emailAccountID.String()).
				Msg("Cannot tell the worker to drop the deactivated mailbox: assignment lookup failed")
		}
	}

	// By id, not through the organization-scoped Update: this path was handed
	// the owner's USER id for that tenant argument, so the write matched no
	// row and every deactivation failed with "Resource not found" while the
	// worker carried on. A mailbox with dead credentials retried its provider
	// every few minutes, indefinitely, and filed a report each time.
	if xerr := s.EmailRepository.SetStatus(ctx, emailAccountID, "inactive"); xerr != nil {
		log.Error().
			Str("error", xerr.Message).
			Str("email_account_id", emailAccountID.String()).
			Msg("Failed to deactivate email account; the worker will keep syncing it")
		return
	}
	if workerID == nil {
		return
	}
	if err := s.Publisher.PublishRemoveEmail(ctx, *workerID, &models.RemoveWorkerEmail{
		UserID:  userID.String(),
		EmailID: emailAccountID.String(),
	}); err != nil {
		log.Warn().Err(err).
			Str("email_account_id", emailAccountID.String()).
			Str("worker_id", workerID.String()).
			Msg("Failed to tell the worker to drop the deactivated mailbox")
	}
}

// evictDeletedMailbox stops a worker still executing a mailbox whose row is
// gone. It is addressed to every live worker because the assignment died with
// the row, so there is nothing left that says which one holds it; the worker
// handler is idempotent and a worker that does not hold the id does nothing.
//
// This is the backstop, not the mechanism. Deleting a mailbox publishes the
// removal while the assignment still exists (emailService.Delete and the
// scheduled-deletion path both do). It exists because a removal that is
// published exactly once can be missed — a worker restarting, a bus hiccup —
// and the cost of missing it is unbounded: the worker authenticates against a
// provider on behalf of an account that no longer exists, once per sync
// interval, forever, and every failure is reported.
func (s *JobsService) evictDeletedMailbox(ctx context.Context, userID, emailAccountID uuid.UUID) {
	if s.Publisher == nil || s.WorkerRepo == nil {
		return
	}
	workers, err := s.WorkerRepo.ListPlaceableWorkers(ctx)
	if err != nil {
		log.Warn().Err(err).
			Str("email_account_id", emailAccountID.String()).
			Msg("Cannot evict a deleted mailbox: worker list unavailable")
		return
	}
	log.Info().
		Str("email_account_id", emailAccountID.String()).
		Int("workers", len(workers)).
		Msg("Mailbox no longer exists; telling every live worker to drop it")
	for _, w := range workers {
		if err := s.Publisher.PublishRemoveEmail(ctx, w.ID, &models.RemoveWorkerEmail{
			UserID:  userID.String(),
			EmailID: emailAccountID.String(),
		}); err != nil {
			log.Warn().Err(err).
				Str("email_account_id", emailAccountID.String()).
				Str("worker_id", w.ID.String()).
				Msg("Failed to tell a worker to drop a deleted mailbox")
		}
	}
}

// dropForDeletedMailbox reports whether a worker event's write was refused
// because its mailbox row is gone, and if so evicts the mailbox from every
// worker. The event is then done: no retry can bring the row back.
func (s *JobsService) dropForDeletedMailbox(ctx context.Context, userID, emailAccountID uuid.UUID, err error) bool {
	if !repository.IsForeignKeyViolation(err) || s.EmailRepository == nil {
		return false
	}
	// Confirmed against the row, not inferred from the refusal: the write may
	// have tripped a different foreign key, such as the owner's user_id.
	if _, xerr := s.EmailRepository.GetWorkerID(ctx, emailAccountID); xerr == nil || !errors.Is(xerr, errx.ErrNotFound) {
		return false
	}
	s.evictDeletedMailbox(ctx, userID, emailAccountID)
	return true
}
