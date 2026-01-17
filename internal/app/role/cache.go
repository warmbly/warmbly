package role

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/getsentry/sentry-go"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func GetRolesKey() string {
	return "roles"
}

func (s *roleService) saveRoles(ctx context.Context, roles []models.Role) *errx.Error {
	key := GetRolesKey()

	data, err := json.Marshal(roles)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.cache.SetNX(ctx, key, data, RolesTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}

func (s *roleService) getRoles(ctx context.Context) ([]models.Role, *errx.Error) {
	key := GetRolesKey()

	bytes, err := s.cache.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	var roles []models.Role
	if err := json.Unmarshal(bytes, &roles); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return roles, nil
}

func (s *roleService) delRoles(ctx context.Context) *errx.Error {
	key := GetRolesKey()

	if err := s.cache.Del(ctx, key).Err(); err != nil {
		if errors.Is(err, redis.Nil) {
			return nil
		}

		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}
