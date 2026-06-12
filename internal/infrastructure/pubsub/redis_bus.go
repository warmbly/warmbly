package pubsub

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"
)

// DefaultRedisEventChannel is the Redis pub/sub channel the realtime service
// subscribes to. Events published here are decoded and rebroadcast to the
// matching org/user/entity Phoenix topics, exactly like the Google Pub/Sub path.
const DefaultRedisEventChannel = "realtime:events"

// RedisBus bridges realtime events to the Elixir realtime service over Redis
// pub/sub. It is the active transport whenever Google Pub/Sub is not configured
// (local dev and any environment without GCP). Routing is by event body fields
// (user_id, org_id, campaign_id, ...), so the topic and attributes are not
// needed on the wire.
type RedisBus struct {
	rdb     *redis.Client
	channel string
}

// NewRedisBus builds a Redis-backed event bus. channel defaults to
// DefaultRedisEventChannel when empty.
func NewRedisBus(rdb *redis.Client, channel string) *RedisBus {
	if channel == "" {
		channel = DefaultRedisEventChannel
	}
	return &RedisBus{rdb: rdb, channel: channel}
}

// Publish marshals the event and pushes it onto the Redis channel. topicID and
// attributes are intentionally ignored: the realtime subscriber routes on the
// event body, the same fields the Google Pub/Sub consumer reads.
func (b *RedisBus) Publish(ctx context.Context, _ string, data interface{}, _ map[string]string) error {
	if b == nil || b.rdb == nil {
		return nil
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, b.channel, payload).Err()
}
