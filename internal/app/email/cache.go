package email

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// OnboardingStateTTL bounds the time a user has to complete an OAuth round trip.
const OnboardingStateTTL = 10 * time.Minute

func onboardingStateKey(state string) string {
	return "email_onboarding:" + state
}

func (s *emailService) saveOnboardingState(ctx context.Context, state string, data *models.EmailOnboardingState) *errx.Error {
	if s.r == nil {
		return errx.InternalError()
	}
	raw, err := json.Marshal(data)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}
	if err := s.r.Set(ctx, onboardingStateKey(state), raw, OnboardingStateTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}
	return nil
}

func (s *emailService) takeOnboardingState(ctx context.Context, state string) (*models.EmailOnboardingState, *errx.Error) {
	if s.r == nil {
		return nil, errx.InternalError()
	}
	raw, err := s.r.Get(ctx, onboardingStateKey(state)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, errx.ErrEmailOnboardState
		}
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	// Single-use: remove immediately to prevent replay even on later errors.
	if err := s.r.Del(ctx, onboardingStateKey(state)).Err(); err != nil {
		sentry.CaptureException(err)
	}
	var out models.EmailOnboardingState
	if err := json.Unmarshal(raw, &out); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	return &out, nil
}
