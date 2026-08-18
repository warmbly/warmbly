package wmail

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// SyncLane is which budget a new message draws from.
type SyncLane string

const (
	// LanePriority is mail in a conversation this mailbox owns (a reply to
	// its outreach). It has its own daily budget and skips the org budget, so
	// a bulky inbox or a sibling mailbox's import never starves a reply.
	LanePriority SyncLane = "priority"
	// LaneLive is new mail arriving after connect.
	LaneLive SyncLane = "live"
	// LaneBackfill is the initial import of history.
	LaneBackfill SyncLane = "backfill"
)

// Admission is the governor's answer for one message.
type Admission struct {
	OK bool
	// Reason names the exhausted budget when !OK (models.SyncThrottle*).
	Reason string
	// Until is when that budget's window rolls; the mailbox reports itself
	// throttled until then.
	Until time.Time
}

// governor is the per-mailbox fair-use engine. Counters live in Redis (shared
// across workers, so an organization budget holds even when its mailboxes sit
// on different machines) as fixed windows: cheap INCRs with a TTL, no sorted
// sets to trim. Redis errors fail open, as the old limiter did: a cache blip
// must not stall every mailbox on the fleet.
//
// It defers rather than drops. A denied message is left on the server with the
// provider cursor held, and is picked up again when the window rolls. Only two
// patterns deactivate a mailbox: a flood (more new live mail observed in one
// hour than any real mailbox produces) and chronic overage (the per-mailbox
// daily budget exhausted on SyncThrottleEscalationDays of the last seven).
type governor struct {
	emailID uuid.UUID
	orgID   *uuid.UUID
	cache   redisCmdable

	// mu guards policy: the sync goroutine reads it on every admission and
	// the worker's event loop replaces it when a republish carries a change.
	mu     sync.RWMutex
	policy models.SyncPolicy

	// warned de-duplicates the fail-open log so a Redis outage is one line
	// per mailbox, not one per message.
	warned bool
}

// redisCmdable is the slice of go-redis the governor uses; *cache.Cache
// embeds redis.Cmdable so it satisfies this directly.
type redisCmdable interface {
	Pipeline() redis.Pipeliner
	SMembers(ctx context.Context, key string) *redis.StringSliceCmd
}

func newGovernor(emailID uuid.UUID, orgID *uuid.UUID, cache redisCmdable, policy models.SyncPolicy) *governor {
	return &governor{emailID: emailID, orgID: orgID, cache: cache, policy: normalizePolicy(policy)}
}

// normalizePolicy fills zero fields with compiled defaults: a payload from a
// publisher older than the policy, or a partial one, never means "no budget".
func normalizePolicy(policy models.SyncPolicy) models.SyncPolicy {
	if policy.DailyMessages <= 0 {
		policy.DailyMessages = config.SyncDailyMessagesMailboxDefault
	}
	if policy.OrgDailyMessages <= 0 {
		policy.OrgDailyMessages = config.SyncDailyMessagesOrgDefault
	}
	if policy.BackfillMessages <= 0 {
		policy.BackfillMessages = config.SyncBackfillMessagesDefault
	}
	if policy.BackfillDays <= 0 {
		policy.BackfillDays = config.SyncBackfillDaysDefault
	}
	return policy
}

// Policy is the budget currently in force.
func (g *governor) Policy() models.SyncPolicy {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.policy
}

// SetPolicy replaces the budget. Called when a republished ADD_EMAIL carries
// a changed policy, so an operator's settings change reaches a mailbox that
// is already loaded without waiting for a worker restart.
func (g *governor) SetPolicy(policy models.SyncPolicy) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.policy = normalizePolicy(policy)
}

// Window keys. Every key carries its bucket in the name so expiry is the
// whole cleanup story.
func (g *governor) key(parts ...any) string {
	return "sync:v2:m:" + g.emailID.String() + ":" + fmt.Sprint(parts...)
}

func (g *governor) orgKey(day string) string {
	if g.orgID == nil {
		return ""
	}
	return "sync:v2:o:" + g.orgID.String() + ":1d:" + day
}

type window struct {
	key   string
	limit int
	ttl   time.Duration
	// reason is reported when this window is the one that denies.
	reason string
	// until is when the window rolls.
	until time.Time
}

func dayKey(t time.Time) string { return t.UTC().Format("20060102") }

func (g *governor) windows(lane SyncLane, now time.Time) []window {
	policy := g.Policy()
	now = now.UTC()
	day := dayKey(now)
	dayEnd := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	hourEnd := now.Truncate(time.Hour).Add(time.Hour)
	fiveEnd := now.Truncate(5 * time.Minute).Add(5 * time.Minute)
	minuteEnd := now.Truncate(time.Minute).Add(time.Minute)

	var ws []window
	switch lane {
	case LanePriority:
		ws = append(ws, window{g.key("pri:1d:", day), policy.DailyMessages, 48 * time.Hour, models.SyncThrottlePriorityFull, dayEnd})
	case LaneLive:
		ws = append(ws,
			window{g.key("5m:", now.Unix()/300), config.SyncBurstPer5Min, 10 * time.Minute, models.SyncThrottleBurst, fiveEnd},
			window{g.key("1h:", now.Unix()/3600), config.SyncHourlyPerMailbox, 2 * time.Hour, models.SyncThrottleHourly, hourEnd},
			window{g.key("1d:", day), policy.DailyMessages, 48 * time.Hour, models.SyncThrottleDaily, dayEnd},
		)
		if k := g.orgKey(day); k != "" {
			ws = append(ws, window{k, policy.OrgDailyMessages, 48 * time.Hour, models.SyncThrottleOrgDaily, dayEnd})
		}
	case LaneBackfill:
		ws = append(ws, window{g.key("bf:1m:", now.Unix()/60), config.SyncBackfillPerMinute, 3 * time.Minute, models.SyncThrottleBurst, minuteEnd})
		if k := g.orgKey(day); k != "" {
			ws = append(ws, window{k, policy.OrgDailyMessages, 48 * time.Hour, models.SyncThrottleOrgDaily, dayEnd})
		}
	}
	return ws
}

// Admit asks whether one more message may be stored on the lane and, when it
// may, charges every window the lane spans. Fail-open when the cache is
// unavailable.
func (g *governor) Admit(ctx context.Context, lane SyncLane) Admission {
	if g.cache == nil {
		return Admission{OK: true}
	}
	now := time.Now()
	ws := g.windows(lane, now)
	if len(ws) == 0 {
		return Admission{OK: true}
	}

	pipe := g.cache.Pipeline()
	gets := make([]*redis.StringCmd, len(ws))
	for i, w := range ws {
		gets[i] = pipe.Get(ctx, w.key)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		g.failOpen(err)
		return Admission{OK: true}
	}
	for i, w := range ws {
		n, _ := gets[i].Int()
		if n >= w.limit {
			return Admission{OK: false, Reason: w.reason, Until: w.until}
		}
	}

	pipe = g.cache.Pipeline()
	for _, w := range ws {
		pipe.Incr(ctx, w.key)
		pipe.Expire(ctx, w.key, w.ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		g.failOpen(err)
	}
	return Admission{OK: true}
}

// ObserveLive records that n new live messages were seen on the server,
// admitted or not, and reports whether the mailbox is flooding: more new mail
// in one clock hour than SyncFloodPerHour. Backfill and priority traffic are
// not observed; an import is expected to be bulky.
func (g *governor) ObserveLive(ctx context.Context, n int) (flooding bool) {
	if g.cache == nil || n <= 0 {
		return false
	}
	key := g.key("seen:1h:", time.Now().Unix()/3600)
	pipe := g.cache.Pipeline()
	incr := pipe.IncrBy(ctx, key, int64(n))
	pipe.Expire(ctx, key, 2*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		g.failOpen(err)
		return false
	}
	return incr.Val() > int64(config.SyncFloodPerHour)
}

// RecordThrottledDay notes that the per-mailbox daily budget ran out today
// and reports whether that has now happened on SyncThrottleEscalationDays of
// the last seven. Burst, hourly and organization denials do not count: the
// first two are short, and the org budget can be spent by a sibling mailbox.
func (g *governor) RecordThrottledDay(ctx context.Context) (chronic bool) {
	if g.cache == nil {
		return false
	}
	key := g.key("thr:days")
	now := time.Now().UTC()
	pipe := g.cache.Pipeline()
	pipe.SAdd(ctx, key, dayKey(now))
	pipe.Expire(ctx, key, 8*24*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		g.failOpen(err)
		return false
	}
	days, err := g.cache.SMembers(ctx, key).Result()
	if err != nil {
		g.failOpen(err)
		return false
	}
	// Count only the last seven days: the key's TTL is refreshed on every
	// add, so a long-held set can carry older dates than the rule reads.
	floor := dayKey(now.AddDate(0, 0, -6))
	recent := 0
	for _, d := range days {
		if d >= floor {
			recent++
		}
	}
	return recent >= config.SyncThrottleEscalationDays
}

func (g *governor) failOpen(err error) {
	if g.warned {
		return
	}
	g.warned = true
	log.Warn().Err(err).Str("email_id", g.emailID.String()).Msg("sync governor: cache unavailable, admitting without budget")
}

// escalate deactivates the mailbox for an abuse pattern. It rides the existing
// EMAIL_RATE_LIMITED path (the consumer records the error, flips the account
// inactive and marks warmup health) and then removes the mailbox from this
// worker, cancelling its loops so nothing keeps ticking against a dead entry.
func (w *WMail) escalate(mailErr *errx.MailError) {
	log.Error().
		Str("email_id", w.ID.String()).
		Str("user_id", w.UserID.String()).
		Str("error_code", string(mailErr.Code)).
		Msg("sync fair use: deactivating mailbox")

	userInfo := mailErr.GetUserErrorInfo()
	_ = w.onEvent(models.JobEventTypeEmailRateLimited, models.EmailErrorEvent{
		EmailAccountID: w.ID.String(),
		UserID:         w.UserID.String(),
		ErrorCode:      string(mailErr.Code),
		ErrorType:      string(mailErr.Type),
		ResolveMethod:  string(mailErr.ResolveMethod),
		Message:        mailErr.Message,
		UserVisible:    mailErr.IsUserVisible(),
		UserTitle:      userInfo.Title,
		UserMessage:    userInfo.Message,
		ActionRequired: userInfo.ActionRequired,
		Timestamp:      time.Now().Unix(),
	})
	if w.Cancel != nil {
		w.Cancel()
	}
	if w.TerminateFunc != nil {
		w.TerminateFunc()
	}
}
