package tasks

import "github.com/warmbly/warmbly/internal/pkg/warmlint"

// lintWarmupContent rejects content that would raise the sending mailbox's own
// spam score (ALL-CAPS subjects, stacked punctuation, stacked spam triggers, a
// fabricated Re:/Fwd: on a non-reply). Shared with the offline AI generator via
// the warmlint package so static and AI content are held to the same bar.
func lintWarmupContent(subject, body string, isReply bool) error {
	return warmlint.Check(subject, body, isReply)
}
