package unibox

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// Overview rolls up the counts the dashboard's scope rail and top
// metric strip need into one request, so the dashboard never has to
// fan out N+M follow-up queries to render those panels. The counts are
// the organization's whoever asks, so its callers share a computation.
func (s *uniboxService) Overview(ctx context.Context, orgID, _ uuid.UUID) (*models.UniboxOverview, *errx.Error) {
	scope := overviewScope{OrgID: orgID}
	var (
		o   *models.UniboxOverview
		err error
	)
	if s.overview != nil {
		o, err = s.overview.get(ctx, scope)
	} else {
		o, err = s.computeOverview(ctx, scope)
	}
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return o, nil
}

// OverviewForMailboxes counts only the named mailboxes. It is never shared.
func (s *uniboxService) OverviewForMailboxes(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID) (*models.UniboxOverview, *errx.Error) {
	o, err := s.uniboxRepository.OverviewForMailboxes(ctx, orgID, accountIDs)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	o.ScheduledPendingMax = int64(config.MaxPendingScheduledSendsPerOrg)
	return o, nil
}

// UnseenCountForMailboxes is GetUnseenCount over a set of mailboxes.
func (s *uniboxService) UnseenCountForMailboxes(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID) (int64, *errx.Error) {
	n, err := s.uniboxRepository.UnseenCountForMailboxes(ctx, orgID, accountIDs)
	if err != nil {
		errs.CaptureException(err)
		return 0, errx.InternalError()
	}
	return n, nil
}

// MessageMailboxes lists the mailboxes the named messages and conversations sit in.
func (s *uniboxService) MessageMailboxes(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, threadIDs []string) ([]uuid.UUID, *errx.Error) {
	out, err := s.uniboxRepository.MessageMailboxes(ctx, orgID, ids, nonEmpty(threadIDs))
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return out, nil
}

// ForgetOverview drops the organization's shared overview after a unibox write.
func (s *uniboxService) ForgetOverview(orgID uuid.UUID) {
	if s.overview != nil {
		s.overview.forget(orgID)
	}
}

// computeOverview reads only what scope names; the result is shared and read-only.
func (s *uniboxService) computeOverview(ctx context.Context, scope overviewScope) (*models.UniboxOverview, error) {
	o, err := s.uniboxRepository.Overview(ctx, scope.OrgID)
	if err != nil {
		return nil, err
	}
	// Every mailbox and tag is listed; the rail searches and collapses them.
	// Scheduled-pending count comes from the tasks table; folded into
	// the same response so the scope rail shows it without a second
	// round-trip. A failure here is non-fatal — the rest of the
	// overview is more important than the badge — so we log via
	// the error reporter and continue with a zero count.
	if s.taskRepo != nil {
		if n, err := s.taskRepo.CountScheduledInOrg(ctx, scope.OrgID); err == nil {
			o.ScheduledPending = n
		} else {
			errs.CaptureException(err)
		}
	}
	// Static-for-now cap. Surfacing it in the overview lets the
	// dashboard render "N / max" so the user sees where they are
	// before they hit the wall. When this moves to per-plan, this is
	// the only line that needs a feature-gate lookup.
	o.ScheduledPendingMax = int64(config.MaxPendingScheduledSendsPerOrg)
	return o, nil
}
