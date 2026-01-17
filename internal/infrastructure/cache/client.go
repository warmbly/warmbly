package cache

import (
	"context"
	"fmt"

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
