package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/warmbly/warmbly/internal/infrastructure/apns"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// PushSender delivers one alert to a device token. Satisfied by *apns.Client.
type PushSender interface {
	Push(ctx context.Context, deviceToken, environment string, n apns.Notification) error
}

// Push batching: the first notification in a quiet period is pushed
// immediately; everything else that arrives inside the window (default 5h,
// NOTIFICATION_PUSH_WINDOW) is coalesced into one digest push sent when the
// window closes ("3 new replies"). State lives in Redis so backend + consumer
// share one window per (user, category), and a ZSET claim keeps replicas from
// double-sending the digest.
const (
	defaultPushWindow = 5 * time.Hour
	pushKeyPrefix     = "notifpush"
	digestPollEvery   = 30 * time.Second
)

type pendingPush struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Link  string `json:"link,omitempty"`
}

func pushWindow() time.Duration {
	if raw := os.Getenv("NOTIFICATION_PUSH_WINDOW"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return defaultPushWindow
}

// WirePush attaches the APNs sender, device-token storage, and the Redis
// client backing the digest window, then starts the digest loop. Token storage
// works with just the repository (devices can register before APNs is
// configured); delivery needs all three.
func (s *service) WirePush(sender PushSender, tokens repository.DeviceTokenRepository, rdb *redis.Client) {
	if tokens != nil {
		s.deviceTokens = tokens
	}
	if sender == nil || tokens == nil || rdb == nil {
		return
	}
	s.push = sender
	s.pushRedis = rdb
	go s.digestLoop()
}

func (s *service) RegisterDevice(ctx context.Context, userID uuid.UUID, platform, token, environment string) error {
	if s.deviceTokens == nil {
		return errors.New("push not configured")
	}
	return s.deviceTokens.Upsert(ctx, userID, platform, token, environment)
}

func (s *service) UnregisterDevice(ctx context.Context, userID uuid.UUID, token string) error {
	if s.deviceTokens == nil {
		return errors.New("push not configured")
	}
	return s.deviceTokens.Delete(ctx, userID, token)
}

func pushMember(userID uuid.UUID, category models.NotificationCategory) string {
	return userID.String() + "|" + string(category)
}

func lastKey(member string) string    { return pushKeyPrefix + ":last:{" + member + "}" }
func pendingKey(member string) string { return pushKeyPrefix + ":pending:{" + member + "}" }
func dueKey() string                  { return pushKeyPrefix + ":due" }

// deliverPush is the per-notification ingress (detached, best-effort). First
// event in a quiet window pushes right away; the rest queue for the digest.
func (s *service) deliverPush(userID uuid.UUID, category models.NotificationCategory, title, body, link string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	member := pushMember(userID, category)
	window := pushWindow()

	ok, err := s.pushRedis.SetNX(ctx, lastKey(member), "1", window).Result()
	if err != nil {
		return
	}
	if ok {
		s.sendPush(ctx, userID, category, apnsAlert(category, title, body, link, 1))
		return
	}

	// Inside the window: queue and make sure a digest fires when it closes.
	item, merr := json.Marshal(pendingPush{Title: title, Body: body, Link: link})
	if merr != nil {
		return
	}
	if err := s.pushRedis.RPush(ctx, pendingKey(member), item).Err(); err != nil {
		return
	}
	due := time.Now().Add(window)
	if ttl, terr := s.pushRedis.PTTL(ctx, lastKey(member)).Result(); terr == nil && ttl > 0 {
		due = time.Now().Add(ttl)
	}
	s.pushRedis.ZAddNX(ctx, dueKey(), redis.Z{Score: float64(due.Unix()), Member: member})
}

// digestLoop drains due digest windows. ZRem is the cross-replica claim: only
// the process that removes the member sends its digest.
func (s *service) digestLoop() {
	ticker := time.NewTicker(digestPollEvery)
	defer ticker.Stop()
	for range ticker.C {
		s.flushDueDigests()
	}
}

func (s *service) flushDueDigests() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	members, err := s.pushRedis.ZRangeByScore(ctx, dueKey(), &redis.ZRangeBy{
		Min: "-inf",
		Max: fmt.Sprintf("%d", time.Now().Unix()),
	}).Result()
	if err != nil {
		return
	}
	for _, member := range members {
		removed, rerr := s.pushRedis.ZRem(ctx, dueKey(), member).Result()
		if rerr != nil || removed == 0 {
			continue // another replica claimed it
		}
		s.sendDigest(ctx, member)
	}
}

func (s *service) sendDigest(ctx context.Context, member string) {
	parts := strings.SplitN(member, "|", 2)
	if len(parts) != 2 {
		return
	}
	userID, err := uuid.Parse(parts[0])
	if err != nil {
		return
	}
	category := models.NotificationCategory(parts[1])

	raw, lerr := s.pushRedis.LRange(ctx, pendingKey(member), 0, -1).Result()
	if lerr != nil {
		return
	}
	s.pushRedis.Del(ctx, pendingKey(member))
	if len(raw) == 0 {
		return
	}
	// Restart the window so the next burst digests again instead of streaming.
	s.pushRedis.Set(ctx, lastKey(member), "1", pushWindow())

	items := make([]pendingPush, 0, len(raw))
	for _, r := range raw {
		var p pendingPush
		if json.Unmarshal([]byte(r), &p) == nil {
			items = append(items, p)
		}
	}
	if len(items) == 0 {
		return
	}
	if len(items) == 1 {
		s.sendPush(ctx, userID, category, apnsAlert(category, items[0].Title, items[0].Body, items[0].Link, 1))
		return
	}
	last := items[len(items)-1]
	n := apnsAlert(category, digestTitle(category, len(items)), "Latest: "+last.Title, last.Link, len(items))
	n.CollapseID = "digest:" + string(category)
	s.sendPush(ctx, userID, category, n)
}

func apnsAlert(category models.NotificationCategory, title, body, link string, count int) apns.Notification {
	custom := map[string]any{"category": string(category), "count": count}
	if link != "" {
		custom["link"] = link
	}
	return apns.Notification{
		Title:    title,
		Body:     body,
		ThreadID: string(category),
		Custom:   custom,
	}
}

func digestTitle(category models.NotificationCategory, n int) string {
	switch category {
	case models.NotifInboundReply:
		return fmt.Sprintf("%d new replies", n)
	case models.NotifInboundOOO:
		return fmt.Sprintf("%d out-of-office replies", n)
	case models.NotifHealthBounce:
		return fmt.Sprintf("%d new bounces", n)
	case models.NotifHealthComplaint:
		return fmt.Sprintf("%d new spam complaints", n)
	case models.NotifWorkerDowntime:
		return fmt.Sprintf("%d worker alerts", n)
	case models.NotifSecuritySignIn:
		return fmt.Sprintf("%d new sign-ins", n)
	default:
		return fmt.Sprintf("%d new notifications", n)
	}
}

// sendPush fans one alert out to every device the user registered, dropping
// tokens APNs reports as gone. The badge is the user's unread feed count.
func (s *service) sendPush(ctx context.Context, userID uuid.UUID, category models.NotificationCategory, n apns.Notification) {
	tokens, err := s.deviceTokens.ListByUser(ctx, userID)
	if err != nil || len(tokens) == 0 {
		return
	}
	if unread, cerr := s.repo.CountUnread(ctx, userID); cerr == nil {
		n.Badge = &unread
	}
	for _, t := range tokens {
		if perr := s.push.Push(ctx, t.Token, t.Environment, n); errors.Is(perr, apns.ErrUnregistered) {
			_ = s.deviceTokens.DeleteToken(ctx, t.Token)
		}
	}
}
