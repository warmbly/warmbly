package jobs

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (s *JobsService) HandleFlagsAdd(ctx context.Context, e *models.JobEventFlags) error {
	email, err := s.UniboxRepository.GetByID(ctx, e.UserID, e.ID)
	if err != nil {
		CaptureError(e.UserID, e.EmailID, fmt.Errorf("Email (%s): %w", e.ID.String(), err))
		return err
	}

	// Check if a warmup email is being flagged as spam
	if s.WarmupRepo != nil && containsSpamFlag(e.Flags) {
		if tokenStr := warmupTokenFromFlags(email.Flags); tokenStr != "" {
			tokenID, parseErr := uuid.Parse(tokenStr)
			if parseErr == nil {
				token, tokenErr := s.WarmupRepo.FindWarmupToken(ctx, tokenID)
				if tokenErr == nil && token != nil {
					if s.WarmupService != nil {
						health, _ := s.WarmupService.ApplySpamReport(ctx, e.EmailID, token.SenderAccountID, email.MessageID, "user_complaint")
						s.markRiskBandFromWarmupHealth(ctx, token.SenderAccountID, health)
					} else {
						_, _ = s.WarmupRepo.IncrementSpamScore(ctx, token.SenderAccountID, 10)
						s.checkAndAutoBlock(ctx, token.SenderAccountID)
						s.markRiskBandFromWarmupHealth(ctx, token.SenderAccountID, nil)
					}
				}
			}
		}
	}

	var updated bool

	for i := range e.Flags {
		if !slices.Contains(email.Flags, e.Flags[i]) {
			email.Flags = append(email.Flags, e.Flags[i])
			updated = true
		}
	}

	if !updated {
		return nil
	}

	if err := s.UniboxRepository.UpdateEntry(
		ctx,
		e.UserID,
		e.EmailID,
		e.ID,
		&repository.UpdateUniboxEntry{
			Flags: email.Flags,
		},
	); err != nil {
		return err
	}

	s.publishEmailUpdated(ctx, e.UserID, email)
	return nil
}

func warmupTokenFromFlags(flags []string) string {
	// Try current header name first, then legacy "X-Warmbly-Token" so messages
	// sent before the header rename continue to verify until they age out.
	prefixes := []string{config.WarmupVerifyHeader + ":", "X-Warmbly-Token:"}
	for _, flag := range flags {
		for _, p := range prefixes {
			if strings.HasPrefix(flag, p) {
				return strings.TrimPrefix(flag, p)
			}
		}
	}
	return ""
}

func (s *JobsService) HandleFlagsRemove(ctx context.Context, e *models.JobEventFlags) error {
	email, err := s.UniboxRepository.GetByID(ctx, e.UserID, e.ID)
	if err != nil {
		CaptureError(e.UserID, e.EmailID, fmt.Errorf("Email (%s): %w", e.ID.String(), err))
		return err
	}

	if len(email.Flags) == 0 {
		return nil
	}

	// Build a set of flags to remove
	removeSet := make(map[string]struct{}, len(e.Flags))
	for _, f := range e.Flags {
		removeSet[f] = struct{}{}
	}

	// Filter out flags that should be removed
	newFlags := make([]string, 0, len(email.Flags))
	for _, f := range email.Flags {
		if _, toRemove := removeSet[f]; !toRemove {
			newFlags = append(newFlags, f)
		}
	}

	// No change → skip DB update
	if len(newFlags) == len(email.Flags) {
		return nil
	}

	if err := s.UniboxRepository.UpdateEntry(
		ctx,
		e.UserID,
		e.EmailID,
		e.ID,
		&repository.UpdateUniboxEntry{
			Flags: newFlags,
		},
	); err != nil {
		return err
	}

	email.Flags = newFlags
	s.publishEmailUpdated(ctx, e.UserID, email)
	return nil
}
