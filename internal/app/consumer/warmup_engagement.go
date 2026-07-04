package jobs

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
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
	actions = append(actions, "move_to_warmbly")

	// Spam-rescue is the reputation-critical "not spam" signal warmup exists for,
	// so it fires regardless of neglect (and the worker only acts if it's really
	// in spam).
	if rollPct(e.SpamRescueRate, p.Bias("rescue", 0.8, 1.2)) {
		actions = append(actions, "remove_from_spam")
	}

	// Occasionally a mailbox files a message but never engages with it — real
	// inboxes are full of read-later-and-forgotten mail. Skip the positive
	// engagement signals on those so the pool never shows perfect, every-message
	// engagement. Spam-rescue and foldering above still happen.
	neglect := rand.Float64() < 0.07*p.Bias("neglect", 0.6, 1.4)
	if !neglect {
		if rollPct(e.MarkReadRate, p.Bias("read", 0.85, 1.15)) {
			actions = append(actions, "mark_read")
		}
		if rollPct(e.MarkImportantRate, p.Bias("important", 0.7, 1.3)) {
			actions = append(actions, "mark_important")
		}
		// Starring is a separate, lower-rate positive signal (Gmail STARRED). On
		// IMAP the worker no-ops it because \Flagged is already covered by
		// mark_important — so it never double-flags the same message.
		if rollPct(e.StarRate, p.Bias("star", 0.6, 1.4)) {
			actions = append(actions, "star")
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
		if a == "move_to_warmbly" || a == "remove_from_spam" {
			immediate = append(immediate, a)
		} else {
			delayed = append(delayed, a)
		}
	}
	return immediate, delayed
}

// rollPct rolls a biased percentage chance. The persona bias nudges a given
// mailbox consistently above/below the configured rate so mailboxes differ.
func rollPct(rate int, bias float64) bool {
	if rate <= 0 {
		return false
	}
	if rate >= 100 {
		return true
	}
	effective := float64(rate) * bias
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
