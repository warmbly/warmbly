package notification

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type digestDevices struct {
	repository.DeviceTokenRepository
	lists int
}

type digestTimeoutMembers struct {
	first []models.OrganizationMember
}

func (m *digestTimeoutMembers) GetMembers(ctx context.Context, _ uuid.UUID) ([]models.OrganizationMember, error) {
	if len(m.first) > 0 {
		first := m.first
		m.first = nil
		return first, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (d *digestDevices) ListByUser(context.Context, uuid.UUID) ([]models.DeviceToken, error) {
	d.lists++
	return nil, nil
}

func TestLivePushDigestAudienceRetries(t *testing.T) {
	url := os.Getenv("WARMBLY_TEST_REDIS")
	if url == "" {
		t.Skip("set WARMBLY_TEST_REDIS for real Redis digest regressions")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"membership", "message", "deadline", "mixed deadline"} {
		for _, recovery := range []string{"authorized", "revoked"} {
			t.Run(failure+"/"+recovery, func(t *testing.T) {
				user, org, message := uuid.New(), uuid.New(), uuid.New()
				category := models.NotifInboundReply
				member := pushMember(user, category)
				t.Cleanup(func() {
					_ = rdb.Del(context.Background(), pendingKey(member), lastKey(member)).Err()
					_ = rdb.ZRem(context.Background(), dueKey(), member).Err()
				})
				members := acceptedMember(user)
				repo := &messageNotificationRepo{allowed: true}
				devices := &digestDevices{}
				s := &service{repo: repo, members: members, pushRedis: rdb, deviceTokens: devices}
				deliveryCtx := ctx
				if failure == "membership" {
					members.err = errors.New("temporary membership failure")
				} else if failure == "message" {
					repo.err = errors.New("temporary message failure")
				} else {
					timeoutMembers := &digestTimeoutMembers{}
					if failure == "mixed deadline" {
						timeoutMembers.first = members.members
					}
					s.members = timeoutMembers
					var cancel context.CancelFunc
					deliveryCtx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
					defer cancel()
				}
				p := pendingPush{OrganizationID: &org, MessageID: &message, Title: "private", Body: "Human reply"}
				data, err := json.Marshal(p)
				if err != nil {
					t.Fatal(err)
				}
				values := []any{string(data), `{"title":"legacy workspace notice"}`, "invalid-json"}
				wantQueued := 1
				if failure == "mixed deadline" {
					values = append([]any{string(data)}, values...)
					wantQueued = 2
				}
				if err := rdb.RPush(ctx, pendingKey(member), values...).Err(); err != nil {
					t.Fatal(err)
				}
				s.sendDigest(deliveryCtx, member)
				queued, err := rdb.LRange(ctx, pendingKey(member), 0, -1).Result()
				if err != nil || len(queued) != wantQueued || queued[0] != string(data) || devices.lists != 0 {
					t.Fatalf("lookup failure lost or delivered pending item: queued=%v device lookups=%d err=%v", queued, devices.lists, err)
				}
				due, err := rdb.ZScore(ctx, dueKey(), member).Result()
				if err != nil || due < float64(time.Now().Unix()) || due > float64(time.Now().Add(time.Minute).Unix()) {
					t.Fatalf("retry not scheduled: due=%v err=%v", due, err)
				}
				members.err, repo.err = nil, nil
				s.members = members
				if recovery == "revoked" {
					members.members = nil
				}
				if err := rdb.ZAdd(ctx, dueKey(), redis.Z{Score: 0, Member: member}).Err(); err != nil {
					t.Fatal(err)
				}
				s.flushDueDigests()
				if n, err := rdb.LLen(ctx, pendingKey(member)).Result(); err != nil || n != 0 {
					t.Fatalf("resolved digest still queued: count=%v err=%v", n, err)
				}
				if _, err := rdb.ZScore(ctx, dueKey(), member).Result(); !errors.Is(err, redis.Nil) {
					t.Fatalf("resolved digest still scheduled: %v", err)
				}
				want := 0
				if recovery == "authorized" {
					want = 1
				}
				if devices.lists != want {
					t.Fatalf("delivery after recovery: device lookups=%d want=%d", devices.lists, want)
				}
			})
		}
	}
}
