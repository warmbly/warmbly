package analytics

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *analyticsService) GetClientReportAnalytics(ctx context.Context, orgID uuid.UUID, q models.ClientReportQuery) (*models.ClientReportAnalytics, *errx.Error) {
	out := &models.ClientReportAnalytics{Daily: []models.DashboardDailyStats{}, Campaigns: []models.ClientReportCampaign{}, Replies: []models.ClientReportReply{}}
	sec := q.Sections
	if sec.Overview || sec.Campaigns || sec.PositiveReplies {
		overall, xerr := s.analyticsRepo.GetDashboardOverallStats(ctx, orgID, q.From, q.To, q.Campaigns, q.Mailboxes)
		if xerr != nil {
			return nil, xerr
		}
		out.Overall = *overall
		table, xerr := s.analyticsRepo.GetReportCampaignTable(ctx, orgID, q.From, q.To, q.Campaigns, models.ClientReportCampaignLimit)
		if xerr != nil {
			return nil, xerr
		}
		out.Campaigns = table
		for _, c := range table {
			if c.Positive != nil {
				out.Positive += *c.Positive
			}
		}
	}
	if sec.DailyChart {
		daily, xerr := s.analyticsRepo.GetDashboardDailyTrend(ctx, orgID, q.From, q.To, q.Campaigns)
		if xerr != nil {
			return nil, xerr
		}
		out.Daily = daily
	}
	if sec.Replies || sec.PositiveReplies {
		replies, xerr := s.analyticsRepo.GetReportReplies(ctx, orgID, q.From, q.To, q.Campaigns, !sec.Replies, q.ReplySnippets, models.ClientReportReplyLimit)
		if xerr != nil {
			return nil, xerr
		}
		out.Replies = replies
	}
	if sec.Mailboxes {
		health, xerr := s.analyticsRepo.GetAccountHealthSummary(ctx, orgID, q.Mailboxes)
		if xerr != nil {
			return nil, xerr
		}
		out.Health = health
		stats, xerr := s.analyticsRepo.GetWarmupStatsForAccounts(ctx, orgID, q.Mailboxes, q.From, q.To)
		if xerr != nil {
			return nil, xerr
		}
		var sum models.WarmupSummary
		for _, d := range stats {
			sum.TotalSent += d.EmailsSent
			sum.TotalReceived += d.EmailsReceived
			sum.TotalReplied += d.EmailsReplied
		}
		out.Warmup = &sum
	}
	return out, nil
}
