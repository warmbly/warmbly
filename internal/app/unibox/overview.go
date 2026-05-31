package unibox

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Overview rolls up the counts the dashboard's scope rail and top
// metric strip need into one request, so the dashboard never has to
// fan out N+M follow-up queries to render those panels.
func (s *uniboxService) Overview(ctx context.Context, userID uuid.UUID) (*models.UniboxOverview, *errx.Error) {
	o, err := s.uniboxRepository.Overview(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	if len(o.Mailboxes) > OverviewMaxMailboxes {
		o.Mailboxes = o.Mailboxes[:OverviewMaxMailboxes]
	}
	if len(o.Tags) > OverviewMaxTags {
		o.Tags = o.Tags[:OverviewMaxTags]
	}
	// Scheduled-pending count comes from the tasks table; folded into
	// the same response so the scope rail shows it without a second
	// round-trip. A failure here is non-fatal — the rest of the
	// overview is more important than the badge — so we log via
	// sentry and continue with a zero count.
	if s.taskRepo != nil {
		if n, err := s.taskRepo.CountScheduledForUser(ctx, userID); err == nil {
			o.ScheduledPending = n
		} else {
			sentry.CaptureException(err)
		}
	}
	// Static-for-now cap. Surfacing it in the overview lets the
	// dashboard render "N / max" so the user sees where they are
	// before they hit the wall. When this moves to per-plan, this is
	// the only line that needs a feature-gate lookup.
	o.ScheduledPendingMax = int64(config.MaxPendingScheduledSendsPerUser)
	return o, nil
}
