package warmupramp

import "time"

const (
	// ColdRampIncrement is how much a graduating mailbox may add per clean day.
	ColdRampIncrement = 5

	// Graduation starting volumes, by how long the mailbox warmed. The bands
	// follow the documented cold posture: a mailbox connected in the last month
	// belongs near 10-20/day, not at the 50/day cap.
	coldStartUnproven = 5
	coldStartWarmed   = 10
	coldStartMature   = 20

	coldWarmedDays = 7
	coldMatureDays = 14
)

// ColdStart is the cold volume a mailbox graduates at, from how long it warmed.
func ColdStart(warmupDays int) int {
	switch {
	case warmupDays >= coldMatureDays:
		return coldStartMature
	case warmupDays >= coldWarmedDays:
		return coldStartWarmed
	default:
		return coldStartUnproven
	}
}

// ColdCeiling is a graduating mailbox's cold cap for today: its starting volume
// plus ColdRampIncrement per clean day since its first cold send, clamped to
// the mailbox's own cap.
//
// Placements freeze the climb through the same Days() union the warmup ramp
// uses, so a mailbox landing in junk stops adding cold volume for the same
// reason and by the same rule that it stops adding warmup volume.
//
// rampStart zero means the mailbox has not sent cold mail yet: it gets its
// starting volume, which is day zero of the ramp.
func ColdCeiling(warmupDays int, rampStart time.Time, placements []time.Time, now time.Time, mailboxCap int) int {
	ceiling := ColdStart(warmupDays)
	if !rampStart.IsZero() {
		ceiling += Days(rampStart, placements, now, FreezeWindow) * ColdRampIncrement
	}
	if ceiling > mailboxCap {
		return mailboxCap
	}
	return ceiling
}
