package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// Window durations
	WindowMinute = 60 * time.Second
	WindowDaily  = 24 * time.Hour

	// Key prefixes
	KeyPrefixMinute = "rl:min:"
	KeyPrefixDaily  = "rl:day:"

	// Cache TTL for user limits
	LimitsCacheTTL = 5 * time.Minute
)

type RateLimitService interface {
	// Get effective limits for user (considers plan defaults + overrides)
	GetUserLimits(ctx context.Context, userID uuid.UUID) (*models.UserRateLimits, *errx.Error)

	// Check if request is allowed (returns remaining, reset time, or error)
	CheckLimit(ctx context.Context, userID uuid.UUID, category models.RateLimitCategory) (*models.RateLimitStatus, error)

	// Record a request (increments counter)
	RecordRequest(ctx context.Context, userID uuid.UUID, category models.RateLimitCategory) error

	// Check and record in one operation (atomic)
	CheckAndRecord(ctx context.Context, userID uuid.UUID, category models.RateLimitCategory) (*models.RateLimitStatus, error)

	// Admin: Update user limits
	UpdateUserLimits(ctx context.Context, userID uuid.UUID, data *models.UpdateUserRateLimits, adminID uuid.UUID) (*models.UserRateLimits, *errx.Error)
}

type rateLimitService struct {
	repo  repository.RateLimitRepository
	cache *cache.Cache
}

func NewService(cache *cache.Cache, repo repository.RateLimitRepository) RateLimitService {
	return &rateLimitService{
		repo:  repo,
		cache: cache,
	}
}

func (s *rateLimitService) GetUserLimits(ctx context.Context, userID uuid.UUID) (*models.UserRateLimits, *errx.Error) {
	return s.repo.GetUserLimits(ctx, userID)
}

func (s *rateLimitService) CheckLimit(ctx context.Context, userID uuid.UUID, category models.RateLimitCategory) (*models.RateLimitStatus, error) {
	// Get user's limits
	limits, xerr := s.GetUserLimits(ctx, userID)
	if xerr != nil {
		// On error, return a permissive status (fail open)
		return &models.RateLimitStatus{
			Category:  category,
			Limit:     9999,
			Remaining: 9999,
			ResetAt:   time.Now().Add(WindowMinute),
		}, nil
	}

	limit := limits.GetLimitForCategory(category)

	// Get current count from Redis
	key := s.getMinuteKey(userID, category)
	count, err := s.cache.Get(ctx, key).Int()
	if err == redis.Nil {
		count = 0
	} else if err != nil {
		// Redis error - fail open
		return &models.RateLimitStatus{
			Category:  category,
			Limit:     limit,
			Remaining: limit,
			ResetAt:   time.Now().Add(WindowMinute),
		}, nil
	}

	// Calculate remaining and reset time
	remaining := limit - count
	if remaining < 0 {
		remaining = 0
	}

	// Get TTL for reset time
	ttl, _ := s.cache.TTL(ctx, key).Result()
	if ttl <= 0 {
		ttl = WindowMinute
	}
	resetAt := time.Now().Add(ttl)

	status := &models.RateLimitStatus{
		Category:  category,
		Limit:     limit,
		Remaining: remaining,
		ResetAt:   resetAt,
	}

	// If rate limited, calculate retry-after
	if remaining <= 0 {
		retryMs := ttl.Milliseconds()
		status.RetryAfterMs = &retryMs
	}

	return status, nil
}

func (s *rateLimitService) RecordRequest(ctx context.Context, userID uuid.UUID, category models.RateLimitCategory) error {
	key := s.getMinuteKey(userID, category)

	// Increment counter with expiry
	pipe := s.cache.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, WindowMinute)
	_, err := pipe.Exec(ctx)

	return err
}

func (s *rateLimitService) CheckAndRecord(ctx context.Context, userID uuid.UUID, category models.RateLimitCategory) (*models.RateLimitStatus, error) {
	// Get user's limits
	limits, xerr := s.GetUserLimits(ctx, userID)
	if xerr != nil {
		// On error, fail open but still record
		s.RecordRequest(ctx, userID, category)
		return &models.RateLimitStatus{
			Category:  category,
			Limit:     9999,
			Remaining: 9999,
			ResetAt:   time.Now().Add(WindowMinute),
		}, nil
	}

	limit := limits.GetLimitForCategory(category)

	key := s.getMinuteKey(userID, category)

	// Use Lua script for atomic check-and-increment
	script := redis.NewScript(`
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local window = tonumber(ARGV[2])

		local current = tonumber(redis.call('GET', key) or '0')

		if current >= limit then
			local ttl = redis.call('TTL', key)
			if ttl < 0 then ttl = window end
			return {current, ttl, 0}
		end

		current = redis.call('INCR', key)
		if current == 1 then
			redis.call('EXPIRE', key, window)
		end

		local ttl = redis.call('TTL', key)
		return {current, ttl, 1}
	`)

	result, err := script.Run(ctx, s.cache.Client, []string{key}, limit, int(WindowMinute.Seconds())).Slice()
	if err != nil {
		// Lua script failed, fall back to simple record
		s.RecordRequest(ctx, userID, category)
		return &models.RateLimitStatus{
			Category:  category,
			Limit:     limit,
			Remaining: limit,
			ResetAt:   time.Now().Add(WindowMinute),
		}, nil
	}

	current := int(result[0].(int64))
	ttl := time.Duration(result[1].(int64)) * time.Second
	allowed := result[2].(int64) == 1

	remaining := limit - current
	if remaining < 0 {
		remaining = 0
	}

	status := &models.RateLimitStatus{
		Category:  category,
		Limit:     limit,
		Remaining: remaining,
		ResetAt:   time.Now().Add(ttl),
	}

	if !allowed {
		retryMs := ttl.Milliseconds()
		status.RetryAfterMs = &retryMs
	}

	return status, nil
}

func (s *rateLimitService) UpdateUserLimits(ctx context.Context, userID uuid.UUID, data *models.UpdateUserRateLimits, adminID uuid.UUID) (*models.UserRateLimits, *errx.Error) {
	return s.repo.UpdateUserLimits(ctx, userID, adminID, data)
}

func (s *rateLimitService) getMinuteKey(userID uuid.UUID, category models.RateLimitCategory) string {
	// Key format: rl:min:{user_id}:{category}:{minute_bucket}
	bucket := time.Now().Unix() / 60
	return fmt.Sprintf("%s%s:%s:%d", KeyPrefixMinute, userID.String(), category, bucket)
}

func (s *rateLimitService) getDailyKey(userID uuid.UUID) string {
	// Key format: rl:day:{user_id}:{date}
	date := time.Now().Format("2006-01-02")
	return fmt.Sprintf("%s%s:%s", KeyPrefixDaily, userID.String(), date)
}
