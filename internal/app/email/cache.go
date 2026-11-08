package email

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
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
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if err := s.r.Set(ctx, onboardingStateKey(state), raw, OnboardingStateTTL).Err(); err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	return nil
}

func (s *emailService) takeOnboardingState(ctx context.Context, state string) (*models.EmailOnboardingState, *errx.Error) {
	if s.r == nil {
		return nil, errx.InternalError()
	}
	// GetDel, not Get-then-Del: with two statements, two callbacks arriving at
	// once both read the state before either deletes it, and "single use"
	// stops being true. The SSO path already uses this; this one did not.
	raw, err := s.r.GetDel(ctx, onboardingStateKey(state)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, errx.ErrEmailOnboardState
		}
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	var out models.EmailOnboardingState
	if err := json.Unmarshal(raw, &out); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return &out, nil
}

// OAuthReturnOrigin reads routing metadata without consuming the single-use state.
func (s *emailService) OAuthReturnOrigin(ctx context.Context, state string) string {
	if s.r == nil || !IsWebState(state) {
		return ""
	}
	raw, err := s.r.Get(ctx, onboardingStateKey(state)).Bytes()
	if err != nil {
		return ""
	}
	var data models.EmailOnboardingState
	if json.Unmarshal(raw, &data) != nil || data.Nonce != state ||
		(data.Provider != string(models.InboxProviderGoogle) && data.Provider != string(models.InboxProviderOutlook)) {
		return ""
	}
	if data.ReturnOrigin == "" {
		return config.PrimaryDashboardOrigin()
	}
	return config.TrustedOrigin(data.ReturnOrigin)
}
