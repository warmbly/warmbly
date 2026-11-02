package jobs

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/warmpersona"
)

// engagementSettingsCache is a process-wide TTL cache over the warmup
// generation settings so performWarmupActions doesn't read Postgres on every
// incoming warmup email.
var (
	engSettingsMu      sync.RWMutex
	engSettingsVal     models.WarmupGenerationSettings
	engSettingsFetched time.Time
)

// getGenerationSettings returns the warmup generation settings (engagement
// rates live here), cached for 60s. Falls back to defaults when unconfigured.
func (s *JobsService) getGenerationSettings(ctx context.Context) models.WarmupGenerationSettings {
	if s.WarmupContentRepo == nil {
		return models.DefaultWarmupGenerationSettings()
	}
	engSettingsMu.RLock()
	fresh := !engSettingsFetched.IsZero() && time.Since(engSettingsFetched) < 60*time.Second
	val := engSettingsVal
	engSettingsMu.RUnlock()
	if fresh {
		return val
	}
	set, err := s.WarmupContentRepo.GetGenerationSettings(ctx)
	if err != nil || set == nil {
		return models.DefaultWarmupGenerationSettings()
	}
	engSettingsMu.Lock()
	engSettingsVal = *set
	engSettingsFetched = time.Now()
	engSettingsMu.Unlock()
	return *set
}

// Floors for mail from a sender in watch or throttled, on a consumer host, or backing a live campaign: the pool replies to and rescues more of it.
const (
	recoveryReplyRate     = warmupramp.BoostedReplyRate
	recoveryImportantRate = 60
	// Below 100 so the persona bias still varies it: no mailbox rescues every message.
	recoveryRescueRate = 95
)

// senderGetsRecoveryEngagement reports whether the sender's mail gets recovery engagement:
// its standing is watch or throttled, it is a consumer Gmail or Outlook.com mailbox,
// or it backs a live campaign, whose cold mail draws few replies.
func (s *JobsService) senderGetsRecoveryEngagement(ctx context.Context, senderID uuid.UUID) bool {
	signals := warmupramp.RecoverySignals{}
	if s.WarmupRepo != nil {
		signals.Health = s.WarmupRepo
	}
	if s.EmailRepository != nil {
		signals.Mailboxes = s.EmailRepository
	}
	if s.CampaignRepo != nil {
		signals.Campaigns = s.CampaignRepo
	}
	return signals.Recovering(ctx, senderID)
}

// recoveryEngagement raises the positive signals for a recovering sender's mail and never lowers them.
func recoveryEngagement(e models.WarmupEngagementSettings) models.WarmupEngagementSettings {
	e.SpamRescueRate = max(e.SpamRescueRate, recoveryRescueRate)
	e.MarkReadRate = max(e.MarkReadRate, 95)
	e.MarkImportantRate = max(e.MarkImportantRate, recoveryImportantRate)
	return e
}

// recoveryReplyChance is the arrival chance of a reply, raised for a recovering sender unless the owner turned replies off.
func recoveryReplyChance(rate int, recovering bool) int {
	if recovering && rate > 0 {
		return max(rate, recoveryReplyRate)
	}
	return rate
}

// engagementPlan decides which recipient-side warmup actions to perform and
// how long to wait first ("dwell"). Two goals:
//   - break the uniform "every account marks important, instantly" bot
//     signature: rates are probabilistic and biased per-mailbox by persona, so
//     the pool shows a natural distribution rather than lockstep behaviour;
//   - keep the strong positive signals: foldering always happens, reads happen
//     most of the time, and spam-rescue (the "not spam" signal warmup exists
//     for) fires at the configured rate but only actually executes when the
//     message really landed in spam (the worker guards this).
//
// The dwell delay is applied recipient-side by the worker so opens/reads don't
// all happen milliseconds after delivery.
func engagementPlan(accountID uuid.UUID, e models.WarmupEngagementSettings) (actions []string, delaySeconds int) {
	p := warmpersona.For(accountID)

	// Foldering is organisational, not an engagement fingerprint — always do it.
	actions = append(actions, models.WarmupActionFile)

	// Spam-rescue is the reputation-critical "not spam" signal warmup exists for,
	// so it fires regardless of neglect (and the worker only acts if it's really
	// in spam).
	if rollPct(e.SpamRescueRate, p.Bias("rescue", 0.8, 1.2)) {
		actions = append(actions, models.WarmupActionRescueFromSpam)
	}

	// Occasionally a mailbox files a message but never engages with it — real
	// inboxes are full of read-later-and-forgotten mail. Skip the positive
	// engagement signals on those so the pool never shows perfect, every-message
	// engagement. Spam-rescue and foldering above still happen.
	neglect := rand.Float64() < 0.07*p.Bias("neglect", 0.6, 1.4)
	if !neglect {
		if rollPct(e.MarkReadRate, p.Bias("read", 0.85, 1.15)) {
			actions = append(actions, models.WarmupActionMarkRead)
		}
		if rollPct(e.MarkImportantRate, p.Bias("important", 0.7, 1.3)) {
			actions = append(actions, models.WarmupActionMarkImportant)
		}
		// Starring is a separate, lower-rate positive signal (Gmail STARRED). On
		// IMAP the worker no-ops it because \Flagged is already covered by
		// mark_important — so it never double-flags the same message.
		if rollPct(e.StarRate, p.Bias("star", 0.6, 1.4)) {
			actions = append(actions, models.WarmupActionStar)
		}
	}

	delaySeconds = dwellSeconds(e.MinDwellSeconds, e.MaxDwellSeconds, p.Bias("dwell", 0.7, 1.3))
	return actions, delaySeconds
}

// splitEngagementLegs separates the reputation-critical, durable actions
// (foldering + spam-rescue, published immediately) from the low-stakes
// engagement-timing signals (read / important / star) that carry the
// recipient-side dwell. The dwell is now applied durably in the control plane
// (a fire_at row drained by the poller), not by an in-process worker timer, so
// a worker restart can no longer drop the delayed leg.
func splitEngagementLegs(actions []string) (immediate, delayed []string) {
	for _, a := range actions {
		if a == models.WarmupActionFile || a == models.WarmupActionRescueFromSpam {
			immediate = append(immediate, a)
		} else {
			delayed = append(delayed, a)
		}
	}
	return immediate, delayed
}

// biasedRateCeiling caps a persona-biased rate, so no mailbox performs an action every time.
const biasedRateCeiling = 98.0

// rollPct rolls a biased percentage chance. The persona bias nudges a given
// mailbox consistently above/below the configured rate so mailboxes differ.
func rollPct(rate int, bias float64) bool {
	if rate <= 0 {
		return false
	}
	if rate >= 100 {
		return true
	}
	// A bias never turns a configured chance into a certainty; only an explicit 100 is one.
	effective := math.Min(float64(rate)*bias, biasedRateCeiling)
	return rand.Float64()*100 < effective
}

// dwellSeconds returns a randomised delay within [min,max], nudged by persona.
// The sample is heavy-tailed (u^2.2 curve), not uniform: most mail gets opened
// within the first stretch of the range, a long tail waits much longer — the
// shape of real inbox-checking, where a uniform "always read within N minutes
// of delivery, around the clock" is a lockstep signature.
func dwellSeconds(minS, maxS int, bias float64) int {
	if maxS <= 0 || maxS < minS {
		return 0
	}
	span := maxS - minS
	base := minS
	if span > 0 {
		u := rand.Float64()
		base += int(float64(span) * math.Pow(u, 2.2))
	}
	out := int(float64(base) * bias)
	if out < minS {
		out = minS
	}
	if out > maxS {
		out = maxS
	}
	return out
}

// humanizeFireAt keeps recipient-side engagement inside plausible waking hours.
// A read that fires at 3am local is a bot signature; anything landing in the
// night window (22:30–07:30) is deferred to the next morning at a randomised
// 07:30–09:30 moment in the recipient's timezone.
func humanizeFireAt(fireAt time.Time, timezone string) time.Time {
	loc, err := time.LoadLocation(timezone)
	if err != nil || timezone == "" {
		return fireAt
	}
	local := fireAt.In(loc)
	minutes := local.Hour()*60 + local.Minute()

	const nightStart = 22*60 + 30
	const morningEnd = 7*60 + 30
	if minutes < nightStart && minutes >= morningEnd {
		return fireAt
	}

	morning := time.Date(local.Year(), local.Month(), local.Day(), 7, 30, 0, 0, loc)
	if minutes >= nightStart {
		morning = morning.Add(24 * time.Hour)
	}
	offset := time.Duration(rand.Intn(120))*time.Minute + time.Duration(rand.Intn(60))*time.Second
	return morning.Add(offset)
}

// selfMoveTTL bounds how long a pending self-move marker is trusted. The move
// is published immediately and the provider delta reports it within a sync
// tick, so this only has to outlive that gap; keeping it short means a real
// deletion later on is still counted.
const selfMoveTTL = 30 * time.Minute

func selfMoveKey(accountID uuid.UUID, rfcMessageID string) string {
	return fmt.Sprintf("warmup:selfmoved:%s:%s", accountID, rfcMessageID)
}

// markSelfMove records that OUR OWN engagement is about to move this warmup
// message out of the folder it was delivered to. Graph reports a move as a
// removal exactly like a delete, so without this the recipient is banned from
// the pool for foldering the platform itself asked for.
func (s *JobsService) markSelfMove(ctx context.Context, accountID uuid.UUID, rfcMessageID string) {
	if s.Cache == nil || rfcMessageID == "" {
		return
	}
	_ = s.Cache.Set(ctx, selfMoveKey(accountID, rfcMessageID), "1", selfMoveTTL).Err()
}

// consumeSelfMove reports whether this removal is the one our own move caused,
// clearing the marker so only the FIRST removal is excused and a later real
// deletion still counts. A cache error answers true on purpose: a permanent
// warmup ban is not something to hand out on a Redis hiccup.
func (s *JobsService) consumeSelfMove(ctx context.Context, accountID uuid.UUID, rfcMessageID string) bool {
	if s.Cache == nil || rfcMessageID == "" {
		return false
	}
	n, err := s.Cache.Del(ctx, selfMoveKey(accountID, rfcMessageID)).Result()
	if err != nil {
		return true
	}
	return n > 0
}

// hasAction reports whether the leg contains a given warmup action.
func hasAction(actions []string, want string) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}
