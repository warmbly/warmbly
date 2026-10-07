package warmupramp

import (
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	// ColdRampIncrement is how much a graduating mailbox may add per clean day.
	ColdRampIncrement = 5

	coldStart = 5
)

// ColdStart is conservative pacing, not a synthetic-age readiness verdict.
func ColdStart(_ int) int {
	return coldStart
}

// ColdCeiling is a graduating mailbox's cold cap for today: its starting volume
// plus ColdRampIncrement per clean day since its first cold send, clamped to
// the mailbox's own cap. Placements freeze the climb through the same Days()
// union the warmup ramp uses. A zero rampStart means it has not sent cold mail
// yet, so it gets its starting volume.
func ColdCeiling(warmupDays int, rampStart time.Time, placements []time.Time, now time.Time, mailboxCap int, confirmedReplies ...int) int {
	ceiling := ColdStart(warmupDays)
	replies := 0
	if len(confirmedReplies) > 0 {
		replies = max(0, confirmedReplies[0])
	}
	if !rampStart.IsZero() {
		ceiling += min(replies, Days(rampStart, placements, now, FreezeWindow)*ColdRampIncrement)
	}
	if ceiling > mailboxCap {
		return mailboxCap
	}
	return ceiling
}

// Notice is the graduation notice the mailbox drawer and a campaign's send
// plan both show: today's ceiling, the configured cap, the clean days left
// before the two meet, and whether a placement is pausing the climb. One
// builder, so the two surfaces cannot round differently. Nil when the
// mailbox never warmed.
func Notice(warmupStartedAt, coldRampStartedAt *time.Time, placements []time.Time, mailboxCap int, now time.Time, confirmedReplies ...int) *models.ColdRampInfo {
	if warmupStartedAt == nil {
		return nil
	}
	warmupDays := int(now.Sub(*warmupStartedAt).Hours() / 24)
	if warmupDays < 0 {
		warmupDays = 0
	}
	var rampStart time.Time
	if coldRampStartedAt != nil {
		rampStart = *coldRampStartedAt
	}
	ceiling := ColdCeiling(warmupDays, rampStart, placements, now, mailboxCap, confirmedReplies...)
	days := 0
	if ceiling < mailboxCap {
		days = -1
	}
	return &models.ColdRampInfo{
		Ceiling:       ceiling,
		MailboxCap:    mailboxCap,
		DaysToFullCap: days,
		Held:          ColdHeldUntil(rampStart, placements, now, FreezeWindow) != nil,
	}
}

// ColdHeldUntil is when the cold ramp resumes climbing, or nil when it already
// is. It filters placements the same way ColdCeiling does, so the number the
// dashboard shows and the reason it gives cannot disagree.
func ColdHeldUntil(rampStart time.Time, placements []time.Time, now time.Time, freeze time.Duration) *time.Time {
	if rampStart.IsZero() {
		return nil
	}
	counted := make([]time.Time, 0, len(placements))
	for _, p := range placements {
		if !p.Before(rampStart) {
			counted = append(counted, p)
		}
	}
	return FrozenUntil(counted, now, freeze)
}
