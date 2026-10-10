package nodelogs

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
)

const Retention = time.Hour
const MaxEvents = 1000
const MaxBatch = 32
const MaxRead = 200
const StaleAfter = 2 * time.Minute

var ErrInvalid = errors.New("invalid node evidence")
var ErrUnauthorized = errors.New("node evidence credential does not match enrollment")
var ErrRateLimited = errors.New("node evidence request rate exceeded")

type Service struct{ cache *cache.Cache }

func New(c *cache.Cache) *Service  { return &Service{cache: c} }
func prefix(node uuid.UUID) string { return "node-evidence:" + node.String() + ":" }

// Enroll requires the fleet join authority, not a shared worker broker credential.
func (s *Service) Enroll(ctx context.Context, node uuid.UUID) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(b)
	if s == nil || s.cache == nil {
		return "", ErrInvalid
	}
	if err := s.cache.Set(ctx, prefix(node)+"credential", crypt.SHA256(secret), 0).Err(); err != nil {
		return "", err
	}
	return secret, nil
}

func (s *Service) Authenticate(ctx context.Context, node uuid.UUID, secret string) error {
	if s == nil || s.cache == nil || len(secret) != 43 || node == uuid.Nil {
		return ErrUnauthorized
	}
	hash, err := s.cache.Get(ctx, prefix(node)+"credential").Result()
	if errors.Is(err, redis.Nil) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(crypt.SHA256(secret))) != 1 {
		return ErrUnauthorized
	}
	allowed, err := s.cache.ReserveAttempt(ctx, prefix(node)+"ingest-budget", 30, time.Minute)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrRateLimited
	}
	return nil
}

func (s *Service) Revoke(ctx context.Context, node uuid.UUID) error {
	if s == nil || s.cache == nil {
		return nil
	}
	return s.cache.Del(ctx, prefix(node)+"credential", prefix(node)+"events", prefix(node)+"status", prefix(node)+"status-time", prefix(node)+"ingest-budget").Err()
}

type Status struct {
	Protocol   int       `json:"protocol"`
	RunID      uuid.UUID `json:"run_id"`
	StartedAt  time.Time `json:"started_at"`
	ObservedAt time.Time `json:"observed_at"`
	ReceivedAt time.Time `json:"received_at"`
	Dropped    uint64    `json:"dropped"`
}

var ingest = redis.NewScript(`
if redis.call('GET', KEYS[4]) ~= ARGV[3] then return -1 end
if not redis.call('SET', KEYS[3], '1', 'NX', 'EX', 3600) then return 0 end
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
for i=5,#ARGV,2 do redis.call('ZADD', KEYS[1], ARGV[i], ARGV[i+1]) end
local n = redis.call('ZCARD', KEYS[1])
if n > 1000 then redis.call('ZREMRANGEBYRANK', KEYS[1], 0, n-1001) end
redis.call('EXPIRE', KEYS[1], 3600)
local previous = tonumber(redis.call('GET', KEYS[5]) or '0')
if tonumber(ARGV[4]) >= previous then
  redis.call('SET', KEYS[2], ARGV[2], 'EX', 7200)
  redis.call('SET', KEYS[5], ARGV[4], 'EX', 7200)
end
return 1`)

func (s *Service) Ingest(ctx context.Context, node uuid.UUID, secret string, b nodeevidence.Batch) error {
	now := time.Now().UTC()
	if b.Protocol != 1 || b.BatchID == uuid.Nil || b.RunID == uuid.Nil || len(b.Events) > MaxBatch || b.ObservedAt.Before(now.Add(-StaleAfter)) || b.ObservedAt.After(now.Add(time.Minute)) || b.StartedAt.IsZero() || b.StartedAt.After(b.ObservedAt) {
		return ErrInvalid
	}
	status := Status{Protocol: b.Protocol, RunID: b.RunID, StartedAt: b.StartedAt, ObservedAt: b.ObservedAt, ReceivedAt: now, Dropped: b.Dropped}
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	args := []any{now.Add(-Retention).UnixMilli(), string(raw), crypt.SHA256(secret), b.ObservedAt.UnixMilli()}
	for _, e := range b.Events {
		safe, ok := nodeevidence.Sanitize(e)
		if !ok || e.ID == uuid.Nil || e.ObservedAt.Before(now.Add(-Retention)) || e.ObservedAt.After(now.Add(time.Minute)) {
			return ErrInvalid
		}
		raw, err := json.Marshal(safe)
		if err != nil {
			return err
		}
		args = append(args, e.ObservedAt.UnixMilli(), string(raw))
	}
	n, err := ingest.Run(ctx, s.cache.Client, []string{prefix(node) + "events", prefix(node) + "status", prefix(node) + "batch:" + b.BatchID.String(), prefix(node) + "credential", prefix(node) + "status-time"}, args...).Int()
	if err != nil {
		return err
	}
	if n == -1 {
		return ErrUnauthorized
	}
	return nil
}

type Filter struct {
	After, Before time.Time
	Level         string
	MailboxID     uuid.UUID
	Limit         int
}
type History struct {
	NodeID           uuid.UUID            `json:"node_id"`
	Availability     string               `json:"availability"`
	Coverage         string               `json:"coverage"`
	Reason           string               `json:"reason,omitempty"`
	Status           *Status              `json:"capture,omitempty"`
	Events           []nodeevidence.Event `json:"events"`
	Oldest           *time.Time           `json:"oldest_retained_at,omitempty"`
	Newest           *time.Time           `json:"newest_retained_at,omitempty"`
	RetainedCount    int                  `json:"retained_count"`
	Truncated        bool                 `json:"truncated"`
	RetentionSeconds int                  `json:"retention_seconds"`
	MaxEvents        int                  `json:"max_events"`
	ObservedAt       time.Time            `json:"observed_at"`
}

func (s *Service) History(ctx context.Context, node uuid.UUID, f Filter) (*History, error) {
	now := time.Now().UTC()
	out := &History{NodeID: node, Availability: "unavailable", Coverage: "unavailable", Reason: "capture_not_observed", Events: []nodeevidence.Event{}, ObservedAt: now, RetentionSeconds: int(Retention.Seconds()), MaxEvents: MaxEvents}
	if s == nil || s.cache == nil {
		out.Reason = "cache_unavailable"
		return out, nil
	}
	var status Status
	err := s.cache.GetJSON(ctx, prefix(node)+"status", &status)
	if errors.Is(err, redis.Nil) {
		exists, err := s.cache.Exists(ctx, prefix(node)+"credential").Result()
		if err != nil {
			out.Reason = "cache_unavailable"
		} else if exists == 0 {
			out.Reason = "log_credential_unavailable"
		}
		return out, nil
	}
	if err != nil {
		out.Reason = "cache_unavailable"
		return out, nil
	}
	out.Status = &status
	out.Availability, out.Coverage, out.Reason = "fresh", "partial", "allowlisted_evidence_only"
	if status.ObservedAt.Before(now.Add(-StaleAfter)) {
		out.Availability, out.Coverage, out.Reason = "stale", "partial", "no_recent_capture"
	}
	if f.Limit <= 0 || f.Limit > MaxRead {
		f.Limit = MaxRead
	}
	rows, err := s.cache.ZRevRangeByScore(ctx, prefix(node)+"events", &redis.ZRangeBy{Max: "+inf", Min: strconv.FormatInt(now.Add(-Retention).UnixMilli(), 10), Count: MaxEvents}).Result()
	if err != nil {
		out.Availability, out.Coverage, out.Reason = "unavailable", "unavailable", "cache_unavailable"
		return out, nil
	}
	for _, row := range rows {
		var e nodeevidence.Event
		if err := json.Unmarshal([]byte(row), &e); err != nil {
			continue
		}
		safe, ok := nodeevidence.Sanitize(e)
		if !ok {
			continue
		}
		out.RetainedCount++
		at := safe.ObservedAt
		if out.Oldest == nil || at.Before(*out.Oldest) {
			out.Oldest = &at
		}
		if out.Newest == nil || at.After(*out.Newest) {
			out.Newest = &at
		}
		if !f.After.IsZero() && at.Before(f.After) || !f.Before.IsZero() && at.After(f.Before) || f.Level != "" && safe.Level != f.Level || f.MailboxID != uuid.Nil && safe.MailboxID != f.MailboxID {
			continue
		}
		if len(out.Events) >= f.Limit {
			out.Truncated = true
			continue
		}
		out.Events = append(out.Events, safe)
	}
	if out.RetainedCount == 0 && out.Availability == "fresh" {
		out.Reason = "no_recent_evidence"
	}
	return out, nil
}
