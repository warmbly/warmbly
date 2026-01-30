package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache struct {
	*redis.Client
}

func New(RedisURL string) (*Cache, error) {
	rOpts, err := redis.ParseURL(RedisURL)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(rOpts)

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("Failed to test redis connection: %v", err)
	}

	return &Cache{
		Client: rdb,
	}, nil
}

// SetJSON stores a value as JSON with TTL
func (c *Cache) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, data, ttl).Err()
}

// GetJSON retrieves a JSON value and unmarshals it into the provided interface
func (c *Cache) GetJSON(ctx context.Context, key string, dest interface{}) error {
	data, err := c.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}
