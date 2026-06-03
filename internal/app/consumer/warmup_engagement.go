package jobs

import (
	"context"
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

	if rollPct(e.MarkReadRate, p.Bias("read", 0.85, 1.15)) {
		actions = append(actions, "mark_read")
	}
	// Spam-rescue: the worker only actually moves it if it's in spam.
	if rollPct(e.SpamRescueRate, p.Bias("rescue", 0.8, 1.2)) {
		actions = append(actions, "remove_from_spam")
	}
	if rollPct(e.MarkImportantRate, p.Bias("important", 0.7, 1.3)) {
		actions = append(actions, "mark_important")
	}

	delaySeconds = dwellSeconds(e.MinDwellSeconds, e.MaxDwellSeconds, p.Bias("dwell", 0.7, 1.3))
	return actions, delaySeconds
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
func dwellSeconds(minS, maxS int, bias float64) int {
	if maxS <= 0 || maxS < minS {
		return 0
	}
	span := maxS - minS
	base := minS
	if span > 0 {
		base += rand.Intn(span + 1)
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
