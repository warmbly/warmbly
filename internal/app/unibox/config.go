package unibox

import "time"

const (
	LimitMin     = 1
	LimitMax     = 100
	DefaultLimit = 50

	// Threads are bounded by what a real human conversation can hold;
	// 500 messages is well above anything organic. We surface the
	// whole thread in one request so the reader doesn't need to
	// paginate inside a single conversation.
	ThreadLimitMax     = 500
	DefaultThreadLimit = 500

	// SnoozeMaxHorizon is the hardest a thread can be snoozed for.
	// Anything longer is functionally "mute" — we'd rather expose a
	// dedicated mute action than let snooze drift into archive.
	SnoozeMaxHorizon = 90 * 24 * time.Hour

	// SnoozeMaxThreads bounds one selection-bar press, matching the id cap
	// the read-state and filing endpoints apply.
	SnoozeMaxThreads = 500
)
