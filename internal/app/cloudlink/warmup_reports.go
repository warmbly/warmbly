package cloudlink

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *service) reportMailboxes(ctx context.Context, orgID uuid.UUID, id *uuid.UUID) ([]models.CloudLinkMailbox, *errx.Error) {
	rows, err := s.repo.ListForOrg(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	return rows, nil
}

func reportRequest(mailboxes []models.CloudLinkMailbox, from, to time.Time) models.PoolLinkWarmupReportRequest {
	req := models.PoolLinkWarmupReportRequest{From: from.Format(time.DateOnly), To: to.Format(time.DateOnly)}
	for _, mailbox := range mailboxes {
		req.RemoteIDs = append(req.RemoteIDs, mailbox.RemoteID)
	}
	return req
}

func (s *service) WarmupStats(ctx context.Context, orgID uuid.UUID, id *uuid.UUID, from, to time.Time) ([]models.WarmupDailyStats, *errx.Error) {
	mailboxes, xerr := s.reportMailboxes(ctx, orgID, id)
	if xerr != nil || len(mailboxes) == 0 {
		return nil, xerr
	}
	var out []models.WarmupDailyStats
	for _, mailboxes := range mailboxGroups(mailboxes) {
		l, xerr := s.mailboxLink(ctx, &mailboxes[0])
		if xerr != nil {
			return nil, xerr
		}
		c := s.clientFor(l)
		for start := from; !start.After(to); {
			end := start.AddDate(0, 0, models.WarmupReportMaxDays-1)
			if end.After(to) {
				end = to
			}
			for i := 0; i < len(mailboxes); i += models.WarmupReportBatchSize {
				batch := mailboxes[i:min(i+models.WarmupReportBatchSize, len(mailboxes))]
				var rows []models.WarmupDailyStats
				if xerr := c.do(ctx, http.MethodPost, "/instance/analytics/warmup", reportRequest(batch, start, end), &rows); xerr != nil {
					return nil, xerr
				}
				if rows == nil {
					return nil, errx.NewWithIdentifier(errx.ServiceUnavailable, "cloud_link_bad_response", "Warmbly Cloud returned an unreadable report.")
				}
				out = models.MergeWarmupStats(out, rows)
			}
			start = end.AddDate(0, 0, 1)
		}
	}
	return out, nil
}

func (s *service) WarmupPlacementData(ctx context.Context, orgID uuid.UUID, id *uuid.UUID, from, to time.Time) (*models.WarmupPlacementData, *errx.Error) {
	mailboxes, xerr := s.reportMailboxes(ctx, orgID, id)
	if xerr != nil || len(mailboxes) == 0 {
		return nil, xerr
	}
	out := &models.WarmupPlacementData{}
	for _, mailboxes := range mailboxGroups(mailboxes) {
		l, xerr := s.mailboxLink(ctx, &mailboxes[0])
		if xerr != nil {
			return nil, xerr
		}
		c := s.clientFor(l)
		// Placement includes per-sender days; keep a year of rows within the client's response limit.
		const batchSize = 10
		for i := 0; i < len(mailboxes); i += batchSize {
			batch := mailboxes[i:min(i+batchSize, len(mailboxes))]
			var part *models.WarmupPlacementData
			if xerr := c.do(ctx, http.MethodPost, "/instance/analytics/warmup/placement", reportRequest(batch, from, to), &part); xerr != nil {
				return nil, xerr
			}
			if part == nil || part.Daily == nil || part.Hosts == nil || part.Sent == nil || part.Unconfirmed == nil || part.Windows == nil {
				return nil, errx.NewWithIdentifier(errx.ServiceUnavailable, "cloud_link_bad_response", "Warmbly Cloud returned an unreadable report.")
			}
			localIDs := make(map[uuid.UUID]uuid.UUID, len(batch))
			for _, mailbox := range batch {
				localIDs[mailbox.RemoteID] = mailbox.EmailAccountID
			}
			if err := part.RemapSenders(localIDs); err != nil {
				return nil, errx.NewWithIdentifier(errx.ServiceUnavailable, "cloud_link_bad_response", "Warmbly Cloud returned an unreadable report.")
			}
			out.Add(part)
		}
	}
	return out, nil
}
