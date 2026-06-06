package user

import (
	"context"

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
