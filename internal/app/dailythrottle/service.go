// Package dailythrottle stops abuse by capping per-day creation rates
// on resources that are otherwise "unlimited" by plan. The total cap
// (config.HardCap*) bounds the lifetime number; this service bounds
// the per-day rate so a fresh account can't pop 1000 campaigns at
// once.
//
// Mechanism: per-(scope, resource, UTC-day) Redis counters with a 25h
// TTL. CheckAndIncrement is the only entry point — atomic INCR plus a
// SETEX on the first hit so the key always expires even if the
// process crashes between calls. Resets at UTC midnight by the key
// design, not by a scheduled job.
package dailythrottle

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
)

// Resource enumerates the actions the throttle bounds. Keeping the
// list closed lets every key live under a single namespace and lets
// the rate ceiling live alongside the resource name in config.
type Resource string

const (
	ResourceCampaign      Resource = "campaign"
	ResourceMailbox       Resource = "mailbox"
	ResourceOrg           Resource = "org"
	ResourceScheduledSend Resource = "scheduled_send"
)

type Service interface {
	// CheckAndIncrement bumps the counter for (scope, resource, today)
	// and returns errx.New(TooManyRequests, ...) when the post-increment
	// value exceeds the ceiling.
	CheckAndIncrement(ctx context.Context, scope uuid.UUID, res Resource, ceiling int) *errx.Error
}

type service struct {
	cache *cache.Cache
}

func NewService(c *cache.Cache) Service {
	return &service{cache: c}
}

func (s *service) CheckAndIncrement(ctx context.Context, scope uuid.UUID, res Resource, ceiling int) *errx.Error {
	if ceiling <= 0 {
		return nil
	}
	if s == nil || s.cache == nil {
		// Fail-open: if Redis isn't wired we don't block creation.
		// The total caps still apply.
		return nil
	}

	key := fmt.Sprintf("dailythrottle:%s:%s:%s",
		res,
		scope.String(),
		time.Now().UTC().Format("2006-01-02"),
	)

	count, err := s.cache.Incr(ctx, key).Result()
	if err != nil {
		sentry.CaptureException(err)
		return nil // fail-open
	}
	// On the very first hit the TTL is unset; set a 25h floor so the
	// counter always expires after the day rolls over even if the
	// process restarts before midnight.
	if count == 1 {
		_ = s.cache.Expire(ctx, key, 25*time.Hour).Err()
	}

	if int(count) > ceiling {
		return errx.New(errx.TooManyRequests,
			fmt.Sprintf("daily creation limit reached for %s (%d / %d)", res, count, ceiling))
	}
	return nil
}
