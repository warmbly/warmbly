package analytics

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type warmupAnalyticsRepoStub struct {
	repository.AnalyticsRepository
	stats []models.WarmupDailyStats
}

func (s warmupAnalyticsRepoStub) GetWarmupStats(context.Context, uuid.UUID, *uuid.UUID, time.Time, time.Time) ([]models.WarmupDailyStats, *errx.Error) {
	return s.stats, nil
}

type accountStatusEmailRepoStub struct {
	repository.EmailRepository
	emailID uuid.UUID
}

func (s accountStatusEmailRepoStub) Get(context.Context, string, string) (*models.Email, *errx.Error) {
	return &models.Email{ID: s.emailID}, nil
}

func (s accountStatusEmailRepoStub) Search(context.Context, string, string, *string, *string, int32, []uuid.UUID) (*models.EmailsResult, *errx.Error) {
	return &models.EmailsResult{Data: []models.Email{{ID: s.emailID}}}, nil
}

type accountStatusAnalyticsRepoStub struct {
	repository.AnalyticsRepository
}

func (accountStatusAnalyticsRepoStub) GetAccountDailyUsage(context.Context, uuid.UUID, time.Time) (*models.AccountDailyUsage, *errx.Error) {
	return nil, errx.InternalError()
}

func (accountStatusAnalyticsRepoStub) GetAccountDailyUsageBatch(context.Context, []uuid.UUID, time.Time) (map[uuid.UUID]*models.AccountDailyUsage, *errx.Error) {
	return nil, errx.InternalError()
}

type dashboardAnalyticsRepoStub struct {
	repository.AnalyticsRepository
	overallFrom time.Time
	overallTo   time.Time
	trendFrom   time.Time
	trendTo     time.Time
	// resolved is what ResolveDashboardScope answers; scopes records what
	// each section was then asked for, keyed by section.
	resolved *models.CampaignScope
	resolves int
	scopes   map[string]*models.CampaignScope
}

func (s *dashboardAnalyticsRepoStub) record(section string, scope *models.CampaignScope) {
	if s.scopes == nil {
		s.scopes = map[string]*models.CampaignScope{}
	}
	s.scopes[section] = scope
}

func (s *dashboardAnalyticsRepoStub) ResolveDashboardScope(_ context.Context, _ uuid.UUID, _ models.DashboardFilter) (*models.DashboardScope, *models.CampaignScope, *errx.Error) {
	s.resolves++
	return &models.DashboardScope{CampaignCount: len(s.resolved.CampaignIDs)}, s.resolved, nil
}

func (s *dashboardAnalyticsRepoStub) GetDashboardOverallStats(_ context.Context, _ uuid.UUID, from, to time.Time, scope *models.CampaignScope) (*models.DashboardOverallStats, *errx.Error) {
	s.overallFrom, s.overallTo = from, to
	s.record("overall", scope)
	return &models.DashboardOverallStats{}, nil
}

func (s *dashboardAnalyticsRepoStub) GetRecentActivity(_ context.Context, _ uuid.UUID, _ int, scope *models.CampaignScope) ([]models.RecentActivityItem, *errx.Error) {
	s.record("recent", scope)
	return []models.RecentActivityItem{}, nil
}

func (s *dashboardAnalyticsRepoStub) GetTopCampaigns(_ context.Context, _ uuid.UUID, _, _ time.Time, _ int, _ string, scope *models.CampaignScope) ([]models.TopCampaignStats, *errx.Error) {
	s.record("top", scope)
	return []models.TopCampaignStats{}, nil
}

func (*dashboardAnalyticsRepoStub) GetAccountHealthSummary(context.Context, uuid.UUID) (*models.AccountHealthSummary, *errx.Error) {
	return &models.AccountHealthSummary{}, nil
}

func (s *dashboardAnalyticsRepoStub) GetDashboardDailyTrend(_ context.Context, _ uuid.UUID, from, to time.Time, scope *models.CampaignScope) ([]models.DashboardDailyStats, *errx.Error) {
	s.trendFrom, s.trendTo = from, to
	s.record("trend", scope)
	return []models.DashboardDailyStats{}, nil
}

func TestWarmupSummaryReportsEveryDisplayedMetric(t *testing.T) {
	repo := warmupAnalyticsRepoStub{stats: []models.WarmupDailyStats{
		{Date: "2026-09-14", EmailsSent: 5, EmailsReplied: 2, EmailsReceived: 4, TargetVolume: 10, Active: true},
		{Date: "2026-09-15", EmailsSent: 10, EmailsReplied: 3, EmailsReceived: 7, TargetVolume: 10, Active: true},
		// A day the mailbox was written to but had no plan: it lists, and is
		// not a day active.
		{Date: "2026-09-16", EmailsReceived: 2},
	}}
	svc := &analyticsService{analyticsRepo: repo}

	got, xerr := svc.GetWarmupAnalytics(context.Background(), uuid.New(), nil, time.Time{}, time.Now())
	if xerr != nil {
		t.Fatalf("GetWarmupAnalytics: %v", xerr)
	}
	if got.Summary.TotalSent != 15 || got.Summary.TotalReplied != 5 || got.Summary.DaysActive != 2 {
		t.Errorf("summary totals = sent %d, replied %d, days %d; want 15/5/2",
			got.Summary.TotalSent, got.Summary.TotalReplied, got.Summary.DaysActive)
	}
	if got.Summary.TotalReceived != 13 {
		t.Errorf("total_received = %d, want 13", got.Summary.TotalReceived)
	}
	if math.Abs(got.Summary.AverageDaily-7.5) > 0.001 {
		t.Errorf("average_daily = %.3f, want 7.5", got.Summary.AverageDaily)
	}
	if math.Abs(got.Summary.ReplyRate-33.333) > 0.01 {
		t.Errorf("reply_rate = %.3f, want 33.333", got.Summary.ReplyRate)
	}
	if math.Abs(got.Summary.TargetProgress-75) > 0.001 {
		t.Errorf("target_progress = %.3f, want 75", got.Summary.TargetProgress)
	}
}

func TestAccountStatusDoesNotTurnUsageErrorsIntoZero(t *testing.T) {
	emailID := uuid.New()
	svc := &analyticsService{
		analyticsRepo: accountStatusAnalyticsRepoStub{},
		emailRepo:     accountStatusEmailRepoStub{emailID: emailID},
	}

	status, xerr := svc.GetAccountStatus(context.Background(), uuid.New(), uuid.New())
	if xerr == nil || status != nil {
		t.Fatalf("GetAccountStatus = (%+v, %v), want an error and no plausible zero usage", status, xerr)
	}
}

func TestAccountStatusListDoesNotHideUsageErrors(t *testing.T) {
	emailID := uuid.New()
	svc := &analyticsService{
		analyticsRepo: accountStatusAnalyticsRepoStub{},
		emailRepo:     accountStatusEmailRepoStub{emailID: emailID},
	}

	statuses, xerr := svc.GetAllAccountStatuses(context.Background(), uuid.New())
	if xerr == nil || statuses != nil {
		t.Fatalf("GetAllAccountStatuses = (%+v, %v), want an error instead of an incomplete list", statuses, xerr)
	}
}

func TestDashboardUsesTheSameSevenCalendarDaysForCardsAndTrend(t *testing.T) {
	repo := &dashboardAnalyticsRepoStub{}
	svc := &analyticsService{analyticsRepo: repo}

	got, xerr := svc.GetDashboardAnalytics(context.Background(), uuid.New(), "7d", models.DashboardFilter{})
	if xerr != nil {
		t.Fatalf("GetDashboardAnalytics: %v", xerr)
	}
	if got.Period != "7d" {
		t.Fatalf("period = %q, want 7d", got.Period)
	}
	if !repo.overallFrom.Equal(repo.trendFrom) || !repo.overallTo.Equal(repo.trendTo) {
		t.Fatalf("card range %s..%s differs from trend range %s..%s", repo.overallFrom, repo.overallTo, repo.trendFrom, repo.trendTo)
	}
	if repo.overallFrom.Location() != time.UTC || repo.overallFrom.Hour() != 0 || repo.overallFrom.Minute() != 0 {
		t.Fatalf("range starts at %s, want UTC midnight", repo.overallFrom)
	}
	if days := int(repo.overallTo.Sub(repo.overallFrom).Hours() / 24); days != 6 {
		t.Fatalf("range starts %d whole days before today, want 6", days)
	}
}

func TestDashboardWithoutFilterStaysWorkspaceWide(t *testing.T) {
	repo := &dashboardAnalyticsRepoStub{}
	svc := &analyticsService{analyticsRepo: repo}

	got, xerr := svc.GetDashboardAnalytics(context.Background(), uuid.New(), "30d", models.DashboardFilter{})
	if xerr != nil {
		t.Fatalf("GetDashboardAnalytics: %v", xerr)
	}
	if repo.resolves != 0 || got.Scope != nil {
		t.Fatalf("an empty filter resolved %d times and echoed %+v; want the workspace with no scope", repo.resolves, got.Scope)
	}
	for section, scope := range repo.scopes {
		if scope != nil {
			t.Errorf("%s narrowed to %+v without a filter", section, scope)
		}
	}
}

func TestDashboardFilterNarrowsEveryCampaignSectionToOneSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []uuid.UUID
	}{
		{"campaigns", []uuid.UUID{uuid.New(), uuid.New()}},
		// Foreign or deleted ids resolve to nothing, which must not read as no filter.
		{"nothing in the workspace", []uuid.UUID{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved := &models.CampaignScope{CampaignIDs: tc.ids}
			repo := &dashboardAnalyticsRepoStub{resolved: resolved}
			svc := &analyticsService{analyticsRepo: repo}

			filter := models.DashboardFilter{FolderIDs: []uuid.UUID{uuid.New()}}
			got, xerr := svc.GetDashboardAnalytics(context.Background(), uuid.New(), "7d", filter)
			if xerr != nil {
				t.Fatalf("GetDashboardAnalytics: %v", xerr)
			}
			if repo.resolves != 1 {
				t.Fatalf("resolved %d times, want once for every section", repo.resolves)
			}
			if got.Scope == nil || got.Scope.CampaignCount != len(tc.ids) {
				t.Fatalf("scope = %+v, want the resolved %d campaigns echoed", got.Scope, len(tc.ids))
			}
			for _, section := range []string{"overall", "recent", "top", "trend"} {
				if repo.scopes[section] != resolved {
					t.Errorf("%s read %+v, want the resolved scope", section, repo.scopes[section])
				}
			}
		})
	}
}
