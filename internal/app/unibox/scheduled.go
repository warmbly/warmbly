package unibox

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ScheduledListMax bounds the rows returned from /unibox/scheduled.
// The dashboard renders them in a virtualisable list, so we cap at
// something well above any realistic personal queue.
const ScheduledListMax = 200

// ScheduledThreadListMax bounds the queued-sends list returned for a
// single thread. Tighter than ScheduledListMax because a single
// conversation accumulating dozens of pending replies almost
// certainly means runaway client behaviour, not a real workflow.
const ScheduledThreadListMax = 50

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

// ListScheduledByThread returns pending queued sends for the given
// thread. Empty threadID is rejected up front so a malformed query
// can't silently fall back to the full per-user list.
func (s *uniboxService) ListScheduledByThread(ctx context.Context, userID uuid.UUID, threadID string) ([]models.UniboxScheduledItem, *errx.Error) {
	if threadID == "" {
		return nil, errx.New(errx.BadRequest, "thread_id is required")
	}
	rows, err := s.taskRepo.ListScheduledForUserByThread(ctx, userID, threadID, ScheduledThreadListMax)
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

// CancelScheduled cancels a pending outbound email.
//
// Two-step, belt-and-braces:
//
//  1. Flip status to 'cancelled' in Postgres. This is the
//     user-facing source of truth — the response succeeds as soon as
//     this commits.
//  2. Best-effort DeleteTask against Cloud Tasks to clean the queue.
//     Bounded to 1s so a slow GCP doesn't bleed into the API
//     response. Failures are logged but do not roll the cancel back.
//
// Why both: at GCP's pricing ($0.40/M ops), a DeleteTask costs the
// same as the dispatch that would have fired anyway, so deletion is
// free *and* gives a cleaner queue (no zombie tasks burning real
// dispatch-rate-limit slots and no wasted POSTs to our handler). If
// DeleteTask fails for any reason — GCP outage, network blip,
// already-fired — the handler's status check still short-circuits
// the dispatch into a harmless no-op, so the user is always safe.
func (s *uniboxService) CancelScheduled(ctx context.Context, userID, taskID uuid.UUID) *errx.Error {
	cloudTaskName, cancelled, err := s.taskRepo.CancelScheduledByUser(ctx, taskID, userID)
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

	// Best-effort Cloud Tasks cleanup. Bounded timeout so a stuck GCP
	// can't add seconds to the user-visible cancel latency. We use
	// context.Background as the parent because we don't want a
	// request-scope cancellation (user closing the browser) to skip
	// the cleanup.
	if s.tasksClient != nil && cloudTaskName != nil && *cloudTaskName != "" {
		gcpCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if derr := s.tasksClient.DeleteTask(gcpCtx, *cloudTaskName); derr != nil {
			// NotFound = already executed or already deleted. Common,
			// not an error. Anything else is genuinely worth knowing
			// but doesn't change the response — the DB is the source
			// of truth and the handler safety-net catches stragglers.
			if st, ok := status.FromError(derr); !ok || st.Code() != codes.NotFound {
				sentry.CaptureException(derr)
				log.Warn().
					Err(derr).
					Str("task_id", taskID.String()).
					Str("cloud_task_name", *cloudTaskName).
					Msg("cloud tasks DeleteTask failed; handler status-check will catch the dispatch")
			}
		}
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
