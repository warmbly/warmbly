package analytics

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type AnalyticsService interface {
	// Warmup analytics
	GetWarmupAnalytics(ctx context.Context, userID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) (*models.WarmupAnalytics, *errx.Error)

	// Campaign analytics
	GetCampaignAnalytics(ctx context.Context, userID, campaignID uuid.UUID) (*models.CampaignAnalytics, *errx.Error)
	GetCampaignDailyStats(ctx context.Context, userID, campaignID uuid.UUID, from, to time.Time) ([]models.CampaignDailyStats, *errx.Error)

	// Email account status
	GetAccountStatus(ctx context.Context, userID, accountID uuid.UUID) (*models.EmailAccountStatus, *errx.Error)
	GetAllAccountStatuses(ctx context.Context, userID uuid.UUID) ([]models.EmailAccountStatus, *errx.Error)

	// Usage overview
	GetUsageOverview(ctx context.Context, userID uuid.UUID, period string) (*models.UsageOverview, *errx.Error)

	// Dashboard analytics
	GetDashboardAnalytics(ctx context.Context, userID uuid.UUID, period string) (*models.DashboardAnalytics, *errx.Error)
	GetCampaignHourlyStats(ctx context.Context, userID, campaignID uuid.UUID, date time.Time) ([]models.CampaignHourlyStats, *errx.Error)
	CompareCampaigns(ctx context.Context, userID uuid.UUID, campaignIDs []uuid.UUID, from, to time.Time) (*models.CampaignComparison, *errx.Error)
}

type analyticsService struct {
	analyticsRepo          repository.AnalyticsRepository
	emailRepo              repository.EmailRepository
	campaignRepo           repository.CampaignRepository
	emailAccountErrorsRepo repository.EmailAccountErrorRepository
}

func NewService(
	analyticsRepo repository.AnalyticsRepository,
	emailRepo repository.EmailRepository,
	campaignRepo repository.CampaignRepository,
	emailAccountErrorsRepo repository.EmailAccountErrorRepository,
) AnalyticsService {
	return &analyticsService{
		analyticsRepo:          analyticsRepo,
		emailRepo:              emailRepo,
		campaignRepo:           campaignRepo,
		emailAccountErrorsRepo: emailAccountErrorsRepo,
	}
}

func (s *analyticsService) GetWarmupAnalytics(ctx context.Context, userID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) (*models.WarmupAnalytics, *errx.Error) {
	// Get daily stats
	dailyStats, xerr := s.analyticsRepo.GetWarmupStats(ctx, userID, emailAccountID, from, to)
	if xerr != nil {
		return nil, xerr
	}

	// Calculate summary
	var totalSent, totalReplied int
	for _, day := range dailyStats {
		totalSent += day.EmailsSent
		totalReplied += day.EmailsReplied
	}

	daysActive := len(dailyStats)
	var averageDaily, replyRate float64
	if daysActive > 0 {
		averageDaily = float64(totalSent) / float64(daysActive)
	}
	if totalSent > 0 {
		replyRate = float64(totalReplied) / float64(totalSent) * 100
	}

	analytics := &models.WarmupAnalytics{
		DateRange: models.DateRange{
			From: from,
			To:   to,
		},
		Summary: models.WarmupSummary{
			TotalSent:    totalSent,
			TotalReplied: totalReplied,
			AverageDaily: averageDaily,
			ReplyRate:    replyRate,
			DaysActive:   daysActive,
		},
		DailyStats: dailyStats,
	}

	if emailAccountID != nil {
		analytics.EmailAccountID = *emailAccountID
	}

	return analytics, nil
}

func (s *analyticsService) GetCampaignAnalytics(ctx context.Context, userID, campaignID uuid.UUID) (*models.CampaignAnalytics, *errx.Error) {
	// Get campaign details
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, errx.ErrNotFound
	}

	// Verify ownership
	if campaign.UserID != userID.String() {
		return nil, errx.ErrForbidden
	}

	// Get summary
	summary, xerr := s.analyticsRepo.GetCampaignSummary(ctx, userID, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	// Get sequence stats
	sequences, xerr := s.analyticsRepo.GetSequenceStats(ctx, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	return &models.CampaignAnalytics{
		CampaignID: campaignID,
		Name:       campaign.Name,
		Status:     campaign.Status,
		Summary:    *summary,
		Sequences:  sequences,
	}, nil
}

func (s *analyticsService) GetCampaignDailyStats(ctx context.Context, userID, campaignID uuid.UUID, from, to time.Time) ([]models.CampaignDailyStats, *errx.Error) {
	// Verify campaign ownership
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, errx.ErrNotFound
	}
	if campaign.UserID != userID.String() {
		return nil, errx.ErrForbidden
	}

	return s.analyticsRepo.GetCampaignDailyStats(ctx, campaignID, from, to)
}

func (s *analyticsService) GetAccountStatus(ctx context.Context, userID, accountID uuid.UUID) (*models.EmailAccountStatus, *errx.Error) {
	// Get email account
	email, xerr := s.emailRepo.Get(ctx, userID.String(), accountID.String())
	if xerr != nil {
		return nil, xerr
	}

	// Get daily usage
	usage, xerr := s.analyticsRepo.GetAccountDailyUsage(ctx, accountID, time.Now())
	if xerr != nil {
		// Non-fatal, use empty usage
		usage = &models.AccountDailyUsage{
			Date: time.Now().Format("2006-01-02"),
		}
	}

	// Get errors
	var errors []models.AccountError
	if s.emailAccountErrorsRepo != nil {
		dbErrors, err := s.emailAccountErrorsRepo.GetByAccountID(ctx, accountID, true)
		if err == nil {
			for _, e := range dbErrors {
				errors = append(errors, models.AccountError{
					ID:             e.ID,
					ErrorCode:      e.ErrorCode,
					Severity:       e.Severity,
					Title:          e.Title,
					Message:        e.Message,
					ActionRequired: e.ActionRequired,
					CreatedAt:      e.CreatedAt,
				})
			}
		}
	}
	if errors == nil {
		errors = make([]models.AccountError, 0)
	}

	// Calculate health
	health := calculateAccountHealth(email, errors)

	// Build warmup status if warmup is enabled
	var warmupStatus *models.WarmupStatusInfo
	if email.Warmup != nil {
		warmupStatus = &models.WarmupStatusInfo{
			Enabled:       true,
			StartedAt:     *email.Warmup,
			CurrentVolume: usage.WarmupSent,
			TargetVolume:  calculateTargetVolume(email),
			MaxVolume:     email.WarmupMax,
			ReplyRate:     email.WarmupReplyRate,
			DaysActive:    int(time.Since(*email.Warmup).Hours() / 24),
		}
	}

	return &models.EmailAccountStatus{
		ID:           email.ID,
		Email:        email.Email,
		Provider:     email.Provider,
		Status:       email.Status,
		LastSyncedAt: &email.LastSyncedAt,
		Health:       health,
		Errors:       errors,
		DailyUsage:   *usage,
		WarmupStatus: warmupStatus,
	}, nil
}

func (s *analyticsService) GetAllAccountStatuses(ctx context.Context, userID uuid.UUID) ([]models.EmailAccountStatus, *errx.Error) {
	// Get all email accounts for user
	emailsResult, xerr := s.emailRepo.Search(ctx, userID.String(), "", nil, nil, 1000)
	if xerr != nil {
		return nil, xerr
	}

	statuses := make([]models.EmailAccountStatus, 0, len(emailsResult.Data))
	for _, email := range emailsResult.Data {
		status, xerr := s.GetAccountStatus(ctx, userID, email.ID)
		if xerr != nil {
			continue // Skip failed accounts
		}
		statuses = append(statuses, *status)
	}

	return statuses, nil
}

func (s *analyticsService) GetUsageOverview(ctx context.Context, userID uuid.UUID, period string) (*models.UsageOverview, *errx.Error) {
	// Get email account counts
	accountsUsage, xerr := s.analyticsRepo.GetEmailAccountCounts(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}

	// Get campaign counts
	campaignsUsage, xerr := s.analyticsRepo.GetCampaignCounts(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}

	// Get contact counts
	contactsUsage, xerr := s.analyticsRepo.GetContactCounts(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}

	// API usage would come from rate limit service
	apiUsage := models.APIUsage{
		TotalCalls:   0, // Would be populated from rate limit tracking
		DailyLimit:   50000,
		TopEndpoints: make([]models.EndpointUsage, 0),
	}

	return &models.UsageOverview{
		UserID:        userID,
		Period:        period,
		EmailAccounts: *accountsUsage,
		Campaigns:     *campaignsUsage,
		Contacts:      *contactsUsage,
		API:           apiUsage,
	}, nil
}

// Helper functions

func calculateAccountHealth(email *models.Email, errors []models.AccountError) models.AccountHealth {
	health := models.AccountHealth{
		Status: "healthy",
		Score:  100,
		Issues: make([]string, 0),
	}

	// Check status
	if email.Status != "active" {
		health.Status = "error"
		health.Score -= 50
		health.Issues = append(health.Issues, "Account is not active")
	}

	// Check for critical errors
	for _, e := range errors {
		if e.Severity == "CRITICAL" {
			health.Status = "error"
			health.Score -= 30
			health.Issues = append(health.Issues, e.Title)
		} else if e.Severity == "WARNING" {
			if health.Status == "healthy" {
				health.Status = "warning"
			}
			health.Score -= 10
			health.Issues = append(health.Issues, e.Title)
		}
	}

	// Ensure score doesn't go below 0
	if health.Score < 0 {
		health.Score = 0
	}

	return health
}

func calculateTargetVolume(email *models.Email) int {
	if email.Warmup == nil {
		return 0
	}

	daysActive := int(time.Since(*email.Warmup).Hours() / 24)
	target := email.WarmupBase + (daysActive * email.WarmupIncrease)

	if target > email.WarmupMax {
		return email.WarmupMax
	}

	return target
}

// Dashboard Analytics implementations

func (s *analyticsService) GetDashboardAnalytics(ctx context.Context, userID uuid.UUID, period string) (*models.DashboardAnalytics, *errx.Error) {
	// Calculate date range from period
	var from, to time.Time
	to = time.Now()

	switch period {
	case "7d":
		from = to.AddDate(0, 0, -7)
	case "30d":
		from = to.AddDate(0, 0, -30)
	case "90d":
		from = to.AddDate(0, 0, -90)
	default:
		from = to.AddDate(0, 0, -7) // Default to 7 days
		period = "7d"
	}

	// Get overall stats
	overallStats, xerr := s.analyticsRepo.GetDashboardOverallStats(ctx, userID, from, to)
	if xerr != nil {
		return nil, xerr
	}

	// Get recent activity
	recentActivity, xerr := s.analyticsRepo.GetRecentActivity(ctx, userID, 20)
	if xerr != nil {
		recentActivity = make([]models.RecentActivityItem, 0)
	}

	// Get top campaigns
	topCampaigns, xerr := s.analyticsRepo.GetTopCampaigns(ctx, userID, from, to, 5, "emails_sent")
	if xerr != nil {
		topCampaigns = make([]models.TopCampaignStats, 0)
	}

	// Get account health summary
	accountHealth, xerr := s.analyticsRepo.GetAccountHealthSummary(ctx, userID)
	if xerr != nil {
		accountHealth = &models.AccountHealthSummary{}
	}

	// Get daily trend
	dailyTrend, xerr := s.analyticsRepo.GetDashboardDailyTrend(ctx, userID, from, to)
	if xerr != nil {
		dailyTrend = make([]models.DashboardDailyStats, 0)
	}

	return &models.DashboardAnalytics{
		Period:         period,
		OverallStats:   *overallStats,
		RecentActivity: recentActivity,
		TopCampaigns:   topCampaigns,
		AccountHealth:  *accountHealth,
		DailyTrend:     dailyTrend,
	}, nil
}

func (s *analyticsService) GetCampaignHourlyStats(ctx context.Context, userID, campaignID uuid.UUID, date time.Time) ([]models.CampaignHourlyStats, *errx.Error) {
	// Verify campaign ownership
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, errx.ErrNotFound
	}
	if campaign.UserID != userID.String() {
		return nil, errx.ErrForbidden
	}

	return s.analyticsRepo.GetCampaignHourlyStats(ctx, campaignID, date)
}

func (s *analyticsService) CompareCampaigns(ctx context.Context, userID uuid.UUID, campaignIDs []uuid.UUID, from, to time.Time) (*models.CampaignComparison, *errx.Error) {
	// Validate that all campaigns belong to user
	for _, campaignID := range campaignIDs {
		campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
		if err != nil {
			return nil, errx.ErrNotFound
		}
		if campaign.UserID != userID.String() {
			return nil, errx.ErrForbidden
		}
	}

	return s.analyticsRepo.CompareCampaigns(ctx, userID, campaignIDs, from, to)
}
