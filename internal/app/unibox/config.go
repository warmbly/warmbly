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

	// Overview is hot per-page-load. Cap defensively in case a future
	// caller iterates it.
	OverviewMaxMailboxes = 200
	OverviewMaxTags      = 100

	// SnoozeMaxHorizon is the hardest a thread can be snoozed for.
	// Anything longer is functionally "mute" — we'd rather expose a
	// dedicated mute action than let snooze drift into archive.
	SnoozeMaxHorizon = 90 * 24 * time.Hour
)
