package jobs

import (
	"context"
	"fmt"
	"slices"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (s *JobsService) HandleFlagsAdd(ctx context.Context, e *models.JobEventFlags) error {
	email, err := s.UniboxRepository.GetByID(ctx, e.UserID, e.ID)
	if err != nil {
		CaptureError(e.UserID, e.EmailID, fmt.Errorf("Email (%s): %w", e.ID.String(), err))
		return err
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

	return s.UniboxRepository.UpdateEntry(
		ctx,
		e.UserID,
		e.EmailID,
		e.ID,
		&repository.UpdateUniboxEntry{
			Flags: email.Flags,
		},
	)
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

	return s.UniboxRepository.UpdateEntry(
		ctx,
		e.UserID,
		e.EmailID,
		e.ID,
		&repository.UpdateUniboxEntry{
			Flags: newFlags,
		},
	)
}
