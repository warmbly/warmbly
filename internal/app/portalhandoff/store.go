package portalhandoff

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/warmbly/warmbly/internal/infrastructure/cache"
)

// RedisStore keeps codes in the shared cache, so any replica can exchange one.
type RedisStore struct{ Cache *cache.Cache }

func (r RedisStore) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.Cache.SetEx(ctx, key, value, ttl).Err()
}

func (r RedisStore) Take(ctx context.Context, key string) ([]byte, error) {
	b, err := r.Cache.GetDel(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	return b, err
}
