package user

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func getUserKey(id uuid.UUID) string {
	return "user:" + id.String()
}

func (s *userService) SaveUser(ctx context.Context, user *models.User) *errx.Error {
	raw, err := json.Marshal(user)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	key := getUserKey(user.ID)
	if err := s.cache.SetEx(ctx, key, raw, UserTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}

func (s *userService) getUser(ctx context.Context, userID uuid.UUID) (*models.User, *errx.Error) {
	data, err := s.cache.Get(ctx, getUserKey(userID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &user, nil
}
