package user

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
)

func (s *userService) CompleteOnboarding(ctx context.Context, userID uuid.UUID, firstName, lastName, referralSource string) *errx.Error {
	if err := s.userRepository.UpdateOnboarding(ctx, userID, firstName, lastName, referralSource); err != nil {
		return errx.InternalError()
	}

	s.cache.Del(ctx, getUserKey(userID))

	return nil
}
