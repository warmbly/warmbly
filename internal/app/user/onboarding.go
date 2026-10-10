package user

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
)

func (s *userService) CompleteOnboarding(ctx context.Context, userID uuid.UUID, firstName, lastName, referralSource, role, teamSize string) *errx.Error {
	if err := s.userRepository.UpdateOnboarding(ctx, userID, firstName, lastName, referralSource, role, teamSize); err != nil {
		return errx.InternalError()
	}

	s.cache.Del(ctx, getUserKey(userID))

	return nil
}

// UpdateProfile persists the user's display name (first/last) from the profile
// settings page. Distinct from CompleteOnboarding, which also captures the
// one-time questionnaire answers; this is the editable, repeatable name update.
func (s *userService) UpdateProfile(ctx context.Context, userID uuid.UUID, firstName, lastName string) *errx.Error {
	if err := s.userRepository.UpdateProfile(ctx, userID, firstName, lastName); err != nil {
		return errx.InternalError()
	}

	s.cache.Del(ctx, getUserKey(userID))

	return nil
}

// CompleteProductTour records that the dashboard tour was finished or skipped,
// so it is not offered again on any device.
func (s *userService) CompleteProductTour(ctx context.Context, userID uuid.UUID) (time.Time, *errx.Error) {
	at, err := s.userRepository.MarkProductTourCompleted(ctx, userID)
	if err != nil {
		return time.Time{}, errx.InternalError()
	}

	s.cache.Del(ctx, getUserKey(userID))

	return at, nil
}

// UpdateUndoSendSeconds persists the user's undo-send window. Bounds are
// validated at the handler; the DB CHECK backstops.
func (s *userService) UpdateUndoSendSeconds(ctx context.Context, userID uuid.UUID, seconds int) *errx.Error {
	if err := s.userRepository.SetUndoSendSeconds(ctx, userID, seconds); err != nil {
		return errx.InternalError()
	}

	s.cache.Del(ctx, getUserKey(userID))

	return nil
}

// UpdateAvatar sets (or clears, with nil) the user's avatar URL. It must go
// through the service so the cached /auth/me copy is dropped; writing the
// repository directly leaves the old avatar served until UserTTL expires.
func (s *userService) UpdateAvatar(ctx context.Context, userID uuid.UUID, avatarURL *string) *errx.Error {
	if err := s.userRepository.UpdateAvatar(ctx, userID, avatarURL); err != nil {
		return errx.InternalError()
	}

	s.cache.Del(ctx, getUserKey(userID))

	return nil
}
