package climit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

func Limit(ctx context.Context, r *redis.Client, key string, ttl time.Duration) (int64, error) {
	val, err := r.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}

	if val == 1 {
		if err := r.Expire(ctx, key, ttl).Err(); err != nil {
			return 0, err
		}
	}

	return val, nil
}
