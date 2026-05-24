package jobs

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
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

		if _, xerr := s.EmailAccountErrorRepository.Create(ctx, errorRecord); xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to store email auth error")
		}
	}

	// Mark email account as needing re-auth (set status to inactive)
	if s.EmailRepository != nil {
		inactive := "inactive"
		if _, xerr := s.EmailRepository.Update(ctx, event.UserID, event.EmailAccountID, &models.UpdateEmail{
			Status: &inactive,
		}); xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to update email account status")
		}
	}

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

		if _, xerr := s.EmailAccountErrorRepository.Create(ctx, errorRecord); xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to store email disabled error")
		}
	}

	// Mark email account as inactive
	if s.EmailRepository != nil {
		inactive := "inactive"
		if _, xerr := s.EmailRepository.Update(ctx, event.UserID, event.EmailAccountID, &models.UpdateEmail{
			Status: &inactive,
		}); xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to update email account status")
		}
	}

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

		if _, xerr := s.EmailAccountErrorRepository.Create(ctx, errorRecord); xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to store rate limit error")
		}
	}

	// Mark email account as inactive (terminated due to abuse)
	if s.EmailRepository != nil {
		inactive := "inactive"
		if _, xerr := s.EmailRepository.Update(ctx, event.UserID, event.EmailAccountID, &models.UpdateEmail{
			Status: &inactive,
		}); xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to update email account status")
		}
	}

	if s.WarmupService != nil {
		_, _ = s.WarmupService.ApplyRateLimitExceeded(ctx, emailAccountID, "worker sync/email rate limit exceeded")
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

	var taskID *uuid.UUID
	if event.TaskID != "" {
		tid, err := uuid.Parse(event.TaskID)
		if err == nil {
			taskID = &tid
		}
	}

	// Store error in database (as warning, not critical)
	if s.EmailAccountErrorRepository != nil {
		errorRecord := &repository.CreateEmailAccountError{
			EmailAccountID: emailAccountID,
			UserID:         userID,
			ErrorCode:      event.ErrorCode,
			Severity:       "WARNING",
			ResolveMethod:  event.ResolveMethod,
			Title:          event.UserTitle,
			Message:        event.Message,
			UserMessage:    ptrString(event.UserMessage),
			ActionRequired: ptrString(event.ActionRequired),
			TaskID:         taskID,
		}

		if _, xerr := s.EmailAccountErrorRepository.Create(ctx, errorRecord); xerr != nil {
			log.Error().Str("error", xerr.Message).Msg("Failed to store server error")
		}
	}

	// Server errors are temporary - don't change account status
	// The error will be auto-resolved when connectivity is restored

	// Only send notification if error persists (based on user visibility flag)
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

// ptrString returns a pointer to a string, or nil if empty
func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
