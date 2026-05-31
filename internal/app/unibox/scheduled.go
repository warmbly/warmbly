package unibox

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ScheduledListMax bounds the rows returned from /unibox/scheduled.
// The dashboard renders them in a virtualisable list, so we cap at
// something well above any realistic personal queue.
const ScheduledListMax = 200

// snippetMaxLen keeps the preview snippet small enough to stay cheap
// to ship across the wire and short enough to render as one line.
const snippetMaxLen = 240

// ListScheduled returns the user's pending email tasks: every queued
// outbound message that hasn't fired yet, ordered by next-to-fire.
// The view is read-only — cancel is a separate explicit action.
func (s *uniboxService) ListScheduled(ctx context.Context, userID uuid.UUID) ([]models.UniboxScheduledItem, *errx.Error) {
	rows, err := s.taskRepo.ListScheduledForUser(ctx, userID, ScheduledListMax)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	out := make([]models.UniboxScheduledItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.UniboxScheduledItem{
			TaskID:       r.TaskID,
			ScheduledAt:  r.ScheduledAt,
			CreatedAt:    r.CreatedAt,
			AccountID:    r.AccountID,
			AccountEmail: r.AccountEmail,
			AccountName:  r.AccountName,
			To:           r.To,
			CC:           r.CC,
			BCC:          r.BCC,
			Subject:      r.Subject,
			Snippet:      snippet(r.Body),
			ThreadID:     r.ThreadID,
		})
	}
	return out, nil
}

// CancelScheduled cancels a pending outbound email by flipping its
// status in the database. We DO NOT call Cloud Tasks DeleteTask —
// that would be a paid API round-trip per cancel. The queued task
// still fires; HandleUserEmailTask short-circuits when it sees a
// non-pending status and returns success so Cloud Tasks doesn't
// retry. Idempotent: cancelling an already-cancelled task returns
// the same "not pending" error (404-shaped) instead of double-acting.
func (s *uniboxService) CancelScheduled(ctx context.Context, userID, taskID uuid.UUID) *errx.Error {
	cancelled, err := s.taskRepo.CancelScheduledByUser(ctx, taskID, userID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}
	if !cancelled {
		// Either: task doesn't exist, isn't this user's, or already
		// left the pending state (fired / failed / cancelled). All
		// three look the same from the caller's perspective.
		return errx.New(errx.NotFound, "no pending scheduled send for this id")
	}
	return nil
}

// snippet trims a plaintext body to a single-line preview for the
// scheduled-list rows. Falls back to "(no body)" so the row never
// renders as a blank gap.
func snippet(body string) string {
	if body == "" {
		return ""
	}
	// Collapse whitespace into single spaces for the preview line.
	var b []rune
	prevSpace := false
	for _, r := range body {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		if r == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
		b = append(b, r)
	}
	if len(b) > snippetMaxLen {
		b = b[:snippetMaxLen]
		// Add an ellipsis so the truncation is visible.
		b = append(b, '…')
	}
	return string(b)
}
